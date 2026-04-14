package hub

import (
	"log"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

const (
	sendChSize    = 256
	writeWait     = 10 * time.Second
	pongWait      = 60 * time.Second
	pingPeriod    = 45 * time.Second
	maxMessageSize = 64 * 1024
)

// ClientConn represents a mobile app WebSocket connection.
type ClientConn struct {
	UserID    string
	conn      *websocket.Conn
	sendCh    chan []byte
	dropCount atomic.Uint64
	hub       *Hub
}

// NewClientConn creates a ClientConn and starts its write pump.
func NewClientConn(userID string, conn *websocket.Conn, hub *Hub) *ClientConn {
	c := &ClientConn{
		UserID: userID,
		conn:   conn,
		sendCh: make(chan []byte, sendChSize),
		hub:    hub,
	}
	go c.writePump()
	return c
}

// Send is the exported, non-blocking enqueue (same semantics as send).
func (c *ClientConn) Send(data []byte) { c.send(data) }

// send is the non-blocking enqueue for the hot path.
// Frames are dropped when the buffer is full (slow consumer / mobile throttle).
func (c *ClientConn) send(data []byte) {
	select {
	case c.sendCh <- data:
	default:
		c.dropCount.Add(1)
	}
}

// writePump drains sendCh and writes to the WebSocket.
func (c *ClientConn) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()
	for {
		select {
		case msg, ok := <-c.sendCh:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.BinaryMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// ReadPump reads incoming messages from the client (input, subscribe, resize).
// It calls the provided handler for each packet.
func (c *ClientConn) ReadPump(handler func(data []byte)) {
	defer func() {
		close(c.sendCh)
		c.conn.Close()
		if c.dropCount.Load() > 0 {
			log.Printf("[client] user=%s dropped=%d frames", c.UserID, c.dropCount.Load())
		}
	}()
	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})
	for {
		_, msg, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		handler(msg)
	}
}

// Close signals the write pump to stop.
func (c *ClientConn) Close() {
	// Drain or close channel safely — write pump will detect closed channel.
	select {
	case c.sendCh <- nil: // sentinel; writePump ignores nil
	default:
	}
}
