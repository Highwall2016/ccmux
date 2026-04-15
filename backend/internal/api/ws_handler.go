package api

import (
	"net/http"
	"time"

	"github.com/ccmux/backend/internal/auth"
	"github.com/ccmux/backend/internal/hub"
	"github.com/ccmux/backend/internal/store"
	"github.com/ccmux/backend/pkg/protocol"
	"github.com/gorilla/websocket"
	"github.com/vmihailenco/msgpack/v5"
)

var wsUpgrader = websocket.Upgrader{
	HandshakeTimeout: 10 * time.Second,
	ReadBufferSize:   4096,
	WriteBufferSize:  4096,
	CheckOrigin:      func(r *http.Request) bool { return true },
}

// agentAuthPayload is the first message the agent sends after WebSocket upgrade.
type agentAuthPayload struct {
	DeviceID string `msgpack:"device_id"`
	Token    string `msgpack:"token"` // raw device token (HMAC-verified server-side)
}

// handleAgentWS handles a desktop daemon WebSocket connection.
func (a *App) handleAgentWS(w http.ResponseWriter, r *http.Request) {
	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	// Step 1: read TypeAuth within timeout.
	conn.SetReadDeadline(time.Now().Add(15 * time.Second))
	_, raw, err := conn.ReadMessage()
	if err != nil {
		conn.Close()
		return
	}
	pkt, err := protocol.Decode(raw)
	if err != nil || pkt.Type != protocol.TypeAuth {
		wsSendAuthFail(conn, "invalid auth packet")
		conn.Close()
		return
	}
	var ap agentAuthPayload
	if err := msgpack.Unmarshal(pkt.Payload, &ap); err != nil || ap.DeviceID == "" {
		wsSendAuthFail(conn, "invalid auth payload")
		conn.Close()
		return
	}

	// Step 2: validate device + token.
	device, err := a.DB.GetDeviceByID(ap.DeviceID)
	if err != nil || device == nil {
		wsSendAuthFail(conn, "device not found")
		conn.Close()
		return
	}
	if !store.ValidateDeviceToken(ap.Token, device.DeviceToken, a.HMACSecret) {
		wsSendAuthFail(conn, "invalid token")
		conn.Close()
		return
	}
	conn.SetReadDeadline(time.Time{})

	// Step 3: confirm auth and register.
	if err := wsSendPacket(conn, &protocol.Packet{Type: protocol.TypeAuthOK}); err != nil {
		conn.Close()
		return
	}
	agentConn := hub.NewAgentConn(ap.DeviceID, conn, a.Hub)
	a.Hub.RegisterAgent(ap.DeviceID, agentConn)
	_ = a.DB.TouchDevice(ap.DeviceID)
	// Mark any sessions left "active" from a prior agent run as exited.
	// The agent will immediately re-announce its currently live sessions via
	// TypeSessionStatus "active", so the DB converges to the true state.
	_ = a.DB.MarkDeviceSessionsExited(ap.DeviceID)

	// Step 4: pump messages from agent.
	agentConn.ReadPump(func(data []byte) {
		pkt, err := protocol.Decode(data)
		if err != nil {
			return
		}
		switch pkt.Type {
		case protocol.TypeTerminalOutput:
			// Hot path: broadcast raw packet bytes to subscribed clients.
			a.Hub.Broadcast(pkt.Session, data)
			_ = a.DB.TouchSession(pkt.Session)

		case protocol.TypeSessionStatus:
			var sp protocol.SessionStatusPayload
			if err := msgpack.Unmarshal(pkt.Payload, &sp); err != nil {
				return
			}
			if sp.Status == "active" {
				// Agent is announcing a newly spawned (or re-announced on reconnect)
				// session.  UpsertSession creates the record if new; ReactivateSession
				// flips it back to active if MarkDeviceSessionsExited cleared it on
				// the previous disconnect.
				_ = a.DB.UpsertSession(sp.SessionID, device.ID, sp.Command, sp.Name, 0, 0)
				_ = a.DB.ReactivateSession(sp.SessionID)
			} else {
				_ = a.DB.UpdateStatus(sp.SessionID, sp.Status, sp.ExitCode)
				// Push notification on session exit.
				if sp.Status == "exited" && sp.ExitCode != nil && a.Notify != nil {
					name := sp.Name
					if name == "" {
						name = sp.SessionID[:8]
					}
					exitCode := *sp.ExitCode
					go a.Notify.NotifySessionExit(device.UserID, name, sp.Command, exitCode)
				}
			}
			// Notify subscribed clients of status change.
			a.Hub.Broadcast(sp.SessionID, data)

		case protocol.TypeAlert:
			var ap protocol.AlertPayload
			if err := msgpack.Unmarshal(pkt.Payload, &ap); err != nil {
				return
			}
			a.Hub.Broadcast(pkt.Session, data)
			if a.Notify != nil {
				// Resolve user for push notification.
				if sess, err := a.DB.GetSessionByID(pkt.Session); err == nil && sess != nil {
					name := pkt.Session[:8]
					go a.Notify.NotifyAlert(device.UserID, pkt.Session, name, ap.Excerpt)
				}
			}

		case protocol.TypeRenameSession:
			var rp protocol.RenamePayload
			if err := msgpack.Unmarshal(pkt.Payload, &rp); err != nil || rp.SessionID == "" {
				return
			}
			_ = a.DB.RenameSession(rp.SessionID, rp.Name)
			// Broadcast updated name as a TypeSessionStatus so clients refresh.
			sp := protocol.SessionStatusPayload{
				SessionID: rp.SessionID,
				Status:    "active",
				Name:      rp.Name,
			}
			if payload, err := msgpack.Marshal(&sp); err == nil {
				if bcast, err := (&protocol.Packet{
					Type:    protocol.TypeSessionStatus,
					Session: rp.SessionID,
					Payload: payload,
				}).Encode(); err == nil {
					a.Hub.Broadcast(rp.SessionID, bcast)
				}
			}

		case protocol.TypePing:
			reply, _ := (&protocol.Packet{Type: protocol.TypePong}).Encode()
			agentConn.Send(reply)
			// Refresh last_seen so the mobile app keeps showing the device as
			// online.  The agent pings every 45s; mobile's isOnline window is 90s.
			_ = a.DB.TouchDevice(device.ID)
		}
	})
}

// handleClientWS handles a mobile client WebSocket connection.
func (a *App) handleClientWS(w http.ResponseWriter, r *http.Request) {
	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	// Step 1: read TypeAuth (JWT in Payload) within timeout.
	conn.SetReadDeadline(time.Now().Add(15 * time.Second))
	_, raw, err := conn.ReadMessage()
	if err != nil {
		conn.Close()
		return
	}
	pkt, err := protocol.Decode(raw)
	if err != nil || pkt.Type != protocol.TypeAuth {
		wsSendAuthFail(conn, "invalid auth packet")
		conn.Close()
		return
	}
	claims, err := auth.ParseToken(string(pkt.Payload), a.JWTSecret)
	if err != nil {
		wsSendAuthFail(conn, "invalid token")
		conn.Close()
		return
	}
	conn.SetReadDeadline(time.Time{})

	// Step 2: confirm auth.
	if err := wsSendPacket(conn, &protocol.Packet{Type: protocol.TypeAuthOK}); err != nil {
		conn.Close()
		return
	}

	clientConn := hub.NewClientConn(claims.UserID, conn, a.Hub)

	// Track subscriptions for cleanup when the connection closes.
	var subscribed []string

	clientConn.ReadPump(func(data []byte) {
		pkt, err := protocol.Decode(data)
		if err != nil {
			return
		}
		switch pkt.Type {
		case protocol.TypeSubscribe:
			var sp protocol.SubscribePayload
			if err := msgpack.Unmarshal(pkt.Payload, &sp); err != nil || sp.SessionID == "" {
				return
			}
			// Verify ownership: session → device → user.
			session, err := a.DB.GetSessionByID(sp.SessionID)
			if err != nil || session == nil {
				return
			}
			dev, err := a.DB.GetDeviceByID(session.DeviceID)
			if err != nil || dev == nil || dev.UserID != claims.UserID {
				return
			}
			// Subscribe to live stream first (to not miss output during replay).
			a.Hub.Subscribe(sp.SessionID, clientConn)
			subscribed = append(subscribed, sp.SessionID)
			go func(sid, offset string) {
				_ = a.Hub.ReplayScrollback(r.Context(), sid, offset, clientConn)
				done, _ := (&protocol.Packet{
					Type:    protocol.TypeScrollbackDone,
					Session: sid,
				}).Encode()
				clientConn.Send(done)
			}(sp.SessionID, sp.FromOffset)

		case protocol.TypeUnsubscribe:
			a.Hub.Unsubscribe(pkt.Session, clientConn)

		case protocol.TypeTerminalInput, protocol.TypeResize:
			// Verify ownership before forwarding to agent.
			session, err := a.DB.GetSessionByID(pkt.Session)
			if err != nil || session == nil {
				return
			}
			dev, err := a.DB.GetDeviceByID(session.DeviceID)
			if err != nil || dev == nil || dev.UserID != claims.UserID {
				return
			}
			if pkt.Type == protocol.TypeResize {
				var rp protocol.ResizePayload
				if err := msgpack.Unmarshal(pkt.Payload, &rp); err == nil {
					_ = a.DB.ResizeSession(pkt.Session, int(rp.Cols), int(rp.Rows))
				}
			}
			if agentConn := a.Hub.GetAgentConn(dev.ID); agentConn != nil {
				agentConn.Send(data)
			}

		case protocol.TypePing:
			reply, _ := (&protocol.Packet{Type: protocol.TypePong}).Encode()
			clientConn.Send(reply)
		}
	})

	// ReadPump has returned (connection closed): unsubscribe from all sessions.
	for _, sid := range subscribed {
		a.Hub.Unsubscribe(sid, clientConn)
	}
}

func wsSendAuthFail(conn *websocket.Conn, msg string) {
	_ = wsSendPacket(conn, &protocol.Packet{Type: protocol.TypeAuthFail, Payload: []byte(msg)})
}

func wsSendPacket(conn *websocket.Conn, pkt *protocol.Packet) error {
	data, err := pkt.Encode()
	if err != nil {
		return err
	}
	return conn.WriteMessage(websocket.BinaryMessage, data)
}
