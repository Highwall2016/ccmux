// Package hub implements the in-memory WebSocket routing core.
// Hot path: Agent output → Broadcast → clients. Zero external I/O on the critical path.
package hub

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	scrollbackKey    = "session:%s:scrollback"
	scrollbackMaxLen = 50000
	scrollbackTTL    = 24 * time.Hour
)

// Hub routes packets between agent connections and client connections.
type Hub struct {
	agents     map[string]*AgentConn    // deviceID → conn
	subs       map[string][]*ClientConn // sessionID → []conn
	deviceSubs map[string]map[*ClientConn]struct{} // deviceID → set of subscribed clients
	mu         sync.RWMutex

	redis         *redis.Client
	multiInstance bool // enables Redis pub/sub cross-replica routing
}

// New creates a Hub. Pass nil redisClient to disable scrollback (tests only).
func New(redisClient *redis.Client, multiInstance bool) *Hub {
	return &Hub{
		agents:        make(map[string]*AgentConn),
		subs:          make(map[string][]*ClientConn),
		deviceSubs:    make(map[string]map[*ClientConn]struct{}),
		redis:         redisClient,
		multiInstance: multiInstance,
	}
}

// RegisterAgent registers an agent connection for a device.
func (h *Hub) RegisterAgent(deviceID string, conn *AgentConn) {
	h.mu.Lock()
	h.agents[deviceID] = conn
	h.mu.Unlock()
	log.Printf("[hub] agent registered: device=%s", deviceID)
}

// UnregisterAgent removes an agent connection.
func (h *Hub) UnregisterAgent(deviceID string) {
	h.mu.Lock()
	delete(h.agents, deviceID)
	h.mu.Unlock()
	log.Printf("[hub] agent unregistered: device=%s", deviceID)
}

// Subscribe subscribes a client connection to a session's output.
// deviceID is used to track which device this client is watching so that
// BroadcastToDevice can reach it for device-level packets (e.g. TypeTmuxTree).
func (h *Hub) Subscribe(sessionID, deviceID string, conn *ClientConn) {
	h.mu.Lock()
	h.subs[sessionID] = append(h.subs[sessionID], conn)
	if deviceID != "" {
		if h.deviceSubs[deviceID] == nil {
			h.deviceSubs[deviceID] = make(map[*ClientConn]struct{})
		}
		h.deviceSubs[deviceID][conn] = struct{}{}
	}
	h.mu.Unlock()
}

// SubscribeDevice registers a client in the device-level subscription map without
// associating it with any session.  This is called once at connect time so that
// device-scoped broadcasts (TypeDeviceMetrics, TypeTmuxTree) are delivered even
// before the user opens any terminal session.
func (h *Hub) SubscribeDevice(deviceID string, conn *ClientConn) {
	h.mu.Lock()
	if h.deviceSubs[deviceID] == nil {
		h.deviceSubs[deviceID] = make(map[*ClientConn]struct{})
	}
	h.deviceSubs[deviceID][conn] = struct{}{}
	h.mu.Unlock()
}

// CleanupConn removes all device-level subscription records for a connection.
// Call after unsubscribing all sessions when the client connection closes.
func (h *Hub) CleanupConn(conn *ClientConn) {
	h.mu.Lock()
	for deviceID, conns := range h.deviceSubs {
		delete(conns, conn)
		if len(conns) == 0 {
			delete(h.deviceSubs, deviceID)
		}
	}
	h.mu.Unlock()
}

// BroadcastToDevice sends pkt to every client currently subscribed to any
// session belonging to deviceID.  Used for device-scoped packets like TypeTmuxTree.
func (h *Hub) BroadcastToDevice(deviceID string, pkt []byte) {
	h.mu.RLock()
	clients := h.deviceSubs[deviceID]
	h.mu.RUnlock()
	for conn := range clients {
		conn.send(pkt)
	}
}

// Unsubscribe removes a client connection from a session.
func (h *Hub) Unsubscribe(sessionID string, conn *ClientConn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	conns := h.subs[sessionID]
	filtered := conns[:0]
	for _, c := range conns {
		if c != conn {
			filtered = append(filtered, c)
		}
	}
	if len(filtered) == 0 {
		delete(h.subs, sessionID)
	} else {
		h.subs[sessionID] = filtered
	}
}

// Broadcast is the hot path: routes a packet to all subscribed clients.
// Never blocks — drops frames to slow clients.
func (h *Hub) Broadcast(sessionID string, pkt []byte) {
	// 1. In-memory broadcast (always, critical path)
	h.mu.RLock()
	clients := h.subs[sessionID]
	h.mu.RUnlock()
	for _, c := range clients {
		c.send(pkt)
	}

	// 2. Async Redis scrollback persistence (non-blocking)
	if h.redis != nil {
		go h.appendScrollback(sessionID, pkt)
	}

	// 3. Cross-replica pub/sub (only in multi-instance mode)
	if h.multiInstance && h.redis != nil {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			h.redis.Publish(ctx, "session:"+sessionID, pkt)
		}()
	}
}

// GetAgentConn returns the agent connection for a device, if any.
func (h *Hub) GetAgentConn(deviceID string) *AgentConn {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.agents[deviceID]
}

// appendScrollback async-appends output to Redis Streams scrollback buffer.
func (h *Hub) appendScrollback(sessionID string, data []byte) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	key := fmt.Sprintf(scrollbackKey, sessionID)
	h.redis.XAdd(ctx, &redis.XAddArgs{
		Stream: key,
		MaxLen: scrollbackMaxLen,
		Approx: true,
		Values: map[string]any{"d": data},
	})
	// Set TTL on every write; cheap and keeps orphaned streams from accumulating.
	h.redis.Expire(ctx, key, scrollbackTTL)
}

// ReplayScrollback sends historical output to a client, starting from fromOffset.
// After replay it sends TypeScrollbackDone so the client can switch to live stream.
func (h *Hub) ReplayScrollback(ctx context.Context, sessionID, fromOffset string, conn *ClientConn) error {
	if h.redis == nil {
		return nil
	}
	key := fmt.Sprintf(scrollbackKey, sessionID)
	if fromOffset == "" {
		fromOffset = "0"
	}

	for {
		msgs, err := h.redis.XRead(ctx, &redis.XReadArgs{
			Streams: []string{key, fromOffset},
			Count:   200,
			Block:   0,
		}).Result()
		if err != nil || len(msgs) == 0 {
			break
		}
		for _, msg := range msgs[0].Messages {
			if raw, ok := msg.Values["d"]; ok {
				switch v := raw.(type) {
				case string:
					conn.send([]byte(v))
				case []byte:
					conn.send(v)
				}
			}
			fromOffset = msg.ID
		}
		if len(msgs[0].Messages) < 200 {
			break // reached end of stream
		}
	}
	return nil
}
