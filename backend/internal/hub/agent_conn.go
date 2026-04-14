package hub

import (
	"log"
	"time"

	"github.com/gorilla/websocket"
)

// AgentConn represents a desktop agent WebSocket connection.
type AgentConn struct {
	DeviceID string
	conn     *websocket.Conn
	sendCh   chan []byte
	hub      *Hub
}

// NewAgentConn creates an AgentConn and starts its write pump.
func NewAgentConn(deviceID string, conn *websocket.Conn, hub *Hub) *AgentConn {
	a := &AgentConn{
		DeviceID: deviceID,
		conn:     conn,
		sendCh:   make(chan []byte, 256),
		hub:      hub,
	}
	go a.writePump()
	return a
}

// Send enqueues a packet to be written to the agent.
func (a *AgentConn) Send(data []byte) {
	select {
	case a.sendCh <- data:
	default:
		log.Printf("[agent] send buffer full for device=%s, dropping", a.DeviceID)
	}
}

func (a *AgentConn) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		a.conn.Close()
	}()
	for {
		select {
		case msg, ok := <-a.sendCh:
			a.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				a.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := a.conn.WriteMessage(websocket.BinaryMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			a.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := a.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// ReadPump reads incoming packets from the agent.
// handler is called for each binary frame; it should be non-blocking.
func (a *AgentConn) ReadPump(handler func(data []byte)) {
	defer func() {
		close(a.sendCh)
		a.conn.Close()
		a.hub.UnregisterAgent(a.DeviceID)
	}()
	a.conn.SetReadLimit(1 * 1024 * 1024) // 1MB max frame
	a.conn.SetReadDeadline(time.Now().Add(pongWait))
	a.conn.SetPongHandler(func(string) error {
		a.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})
	for {
		_, msg, err := a.conn.ReadMessage()
		if err != nil {
			return
		}
		handler(msg)
	}
}
