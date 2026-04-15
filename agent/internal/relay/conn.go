package relay

import (
	"context"
	"fmt"
	"log"
	"math"
	"sync"
	"time"

	"github.com/ccmux/backend/pkg/protocol"
	"github.com/gorilla/websocket"
	"github.com/vmihailenco/msgpack/v5"
)

const (
	writeWait   = 10 * time.Second
	pongWait    = 60 * time.Second
	pingPeriod  = 45 * time.Second
	maxBackoff  = 60 * time.Second
	sendBufSize = 512
)

// InputHandler is called when the backend sends TypeTerminalInput for a session.
type InputHandler func(sessionID string, data []byte)

// ResizeHandler is called when the backend sends TypeResize for a session.
type ResizeHandler func(sessionID string, cols, rows uint16)

// KillHandler is called when the backend sends TypeKillSession for a session.
type KillHandler func(sessionID string)

// Conn manages the persistent WebSocket connection to the ccmux backend.
// It reconnects automatically with exponential backoff.
type Conn struct {
	serverURL   string
	deviceID    string
	deviceToken string

	onInput  InputHandler
	onResize ResizeHandler
	onKill   KillHandler

	sendCh chan []byte

	mu sync.Mutex
	ws *websocket.Conn
}

// NewConn creates a Conn. Call Run to start connecting.
func NewConn(serverURL, deviceID, deviceToken string, onInput InputHandler, onResize ResizeHandler) *Conn {
	return &Conn{
		serverURL:   serverURL,
		deviceID:    deviceID,
		deviceToken: deviceToken,
		onInput:     onInput,
		onResize:    onResize,
		sendCh:      make(chan []byte, sendBufSize),
	}
}

// SetInputHandler replaces the input handler. Must be called before Run.
func (c *Conn) SetInputHandler(h InputHandler) { c.onInput = h }

// SetResizeHandler replaces the resize handler. Must be called before Run.
func (c *Conn) SetResizeHandler(h ResizeHandler) { c.onResize = h }

// SetKillHandler replaces the kill handler. Must be called before Run.
func (c *Conn) SetKillHandler(h KillHandler) { c.onKill = h }

// Send enqueues a packet for delivery to the backend.
// Non-blocking: drops the packet if the buffer is full.
func (c *Conn) Send(data []byte) {
	select {
	case c.sendCh <- data:
	default:
		log.Printf("[relay] send buffer full, dropping %d bytes", len(data))
	}
}

// Run connects to the backend and reconnects on failure until ctx is cancelled.
func (c *Conn) Run(ctx context.Context) {
	attempt := 0
	for {
		if err := c.connect(ctx); err != nil {
			select {
			case <-ctx.Done():
				return
			default:
			}
			backoff := backoffDuration(attempt)
			log.Printf("[relay] disconnected (%v), reconnecting in %s", err, backoff)
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			attempt++
		} else {
			attempt = 0
		}
	}
}

// connect establishes one WebSocket session. Returns when the session ends.
func (c *Conn) connect(ctx context.Context) error {
	dialer := websocket.DefaultDialer
	ws, _, err := dialer.DialContext(ctx, c.serverURL+"/ws/agent", nil)
	if err != nil {
		return err
	}
	defer ws.Close()

	// Step 1: send TypeAuth.
	authPayload, err := msgpack.Marshal(map[string]string{
		"device_id": c.deviceID,
		"token":     c.deviceToken,
	})
	if err != nil {
		return err
	}
	authPkt, _ := (&protocol.Packet{Type: protocol.TypeAuth, Payload: authPayload}).Encode()
	if err := ws.WriteMessage(websocket.BinaryMessage, authPkt); err != nil {
		return err
	}

	// Step 2: read TypeAuthOK.
	ws.SetReadDeadline(time.Now().Add(15 * time.Second))
	_, raw, err := ws.ReadMessage()
	if err != nil {
		return err
	}
	pkt, err := protocol.Decode(raw)
	if err != nil {
		return err
	}
	if pkt.Type != protocol.TypeAuthOK {
		return fmt.Errorf("auth failed: %s", string(pkt.Payload))
	}
	ws.SetReadDeadline(time.Time{})
	log.Printf("[relay] connected to %s as device %s", c.serverURL, c.deviceID)

	c.mu.Lock()
	c.ws = ws
	c.mu.Unlock()

	// Run write pump and read pump concurrently.
	errCh := make(chan error, 2)
	go c.writePump(ctx, ws, errCh)
	go c.readPump(ws, errCh)

	err = <-errCh

	c.mu.Lock()
	c.ws = nil
	c.mu.Unlock()

	ws.Close()
	return err
}

func (c *Conn) writePump(ctx context.Context, ws *websocket.Conn, errCh chan<- error) {
	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			ws.SetWriteDeadline(time.Now().Add(writeWait))
			ws.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			errCh <- ctx.Err()
			return
		case data, ok := <-c.sendCh:
			ws.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				errCh <- nil
				return
			}
			if err := ws.WriteMessage(websocket.BinaryMessage, data); err != nil {
				errCh <- err
				return
			}
		case <-ticker.C:
			ws.SetWriteDeadline(time.Now().Add(writeWait))
			ping, _ := (&protocol.Packet{Type: protocol.TypePing}).Encode()
			if err := ws.WriteMessage(websocket.BinaryMessage, ping); err != nil {
				errCh <- err
				return
			}
		}
	}
}

func (c *Conn) readPump(ws *websocket.Conn, errCh chan<- error) {
	ws.SetReadLimit(1 * 1024 * 1024)
	ws.SetReadDeadline(time.Now().Add(pongWait))
	ws.SetPongHandler(func(string) error {
		ws.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})
	for {
		_, raw, err := ws.ReadMessage()
		if err != nil {
			errCh <- err
			return
		}
		pkt, err := protocol.Decode(raw)
		if err != nil {
			continue
		}
		c.handlePacket(pkt, raw)
	}
}

func (c *Conn) handlePacket(pkt *protocol.Packet, raw []byte) {
	switch pkt.Type {
	case protocol.TypeTerminalInput:
		if c.onInput != nil && pkt.Session != "" && len(pkt.Payload) > 0 {
			c.onInput(pkt.Session, pkt.Payload)
		}

	case protocol.TypeResize:
		if c.onResize != nil && pkt.Session != "" {
			var rp protocol.ResizePayload
			if err := msgpack.Unmarshal(pkt.Payload, &rp); err == nil {
				c.onResize(pkt.Session, rp.Cols, rp.Rows)
			}
		}

	case protocol.TypeKillSession:
		if c.onKill != nil && pkt.Session != "" {
			c.onKill(pkt.Session)
		}

	case protocol.TypePong:
		// latency tracking could go here

	case protocol.TypePing:
		pong, _ := (&protocol.Packet{Type: protocol.TypePong}).Encode()
		c.Send(pong)
	}
}

func backoffDuration(attempt int) time.Duration {
	if attempt == 0 {
		return time.Second
	}
	d := time.Duration(math.Pow(2, float64(attempt))) * time.Second
	if d > maxBackoff {
		return maxBackoff
	}
	return d
}

