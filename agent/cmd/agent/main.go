// Package main is the entry point for the ccmux desktop agent.
// It manages local PTY sessions and relays their I/O to the ccmux backend
// over a persistent WebSocket connection.
package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/ccmux/agent/internal/config"
	"github.com/ccmux/agent/internal/ipc"
	agentpty "github.com/ccmux/agent/internal/pty"
	"github.com/ccmux/agent/internal/relay"
	"github.com/ccmux/backend/pkg/protocol"
	"github.com/vmihailenco/msgpack/v5"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// wsConn is created first (no goroutines yet) so it can be safely captured
	// by the ptyMgr and batcher closures below without a data race.
	// Goroutines inside Conn only start when Run() is called at the bottom of main.
	wsConn := relay.NewConn(
		cfg.ServerURL,
		cfg.DeviceID,
		cfg.DeviceToken,
		nil, // onInput set after ptyMgr is created
		nil, // onResize set after ptyMgr is created
	)

	// Batcher: coalesces PTY output into 16 ms windows before sending upstream.
	batcher := relay.NewBatcher(func(sessionID string, data []byte) {
		pkt, err := (&protocol.Packet{
			Type:    protocol.TypeTerminalOutput,
			Session: sessionID,
			Payload: data,
		}).Encode()
		if err != nil {
			return
		}
		wsConn.Send(pkt)
	})
	defer batcher.Stop()

	// PTY manager: owns all local terminal sessions.
	ptyMgr := agentpty.NewManager(
		cfg.DefaultShell,
		// onOutput: called by read loop goroutines; may be concurrent.
		func(sessionID string, data []byte) {
			batcher.Add(sessionID, data)
		},
		// onExit: called after Wait() returns.
		func(sessionID string, exitCode int) {
			code := exitCode
			sp := protocol.SessionStatusPayload{
				SessionID: sessionID,
				Status:    "exited",
				ExitCode:  &code,
			}
			payload, err := msgpack.Marshal(&sp)
			if err != nil {
				return
			}
			pkt, err := (&protocol.Packet{
				Type:    protocol.TypeSessionStatus,
				Session: sessionID,
				Payload: payload,
			}).Encode()
			if err != nil {
				return
			}
			wsConn.Send(pkt)
			log.Printf("[agent] session %s exited (code %d)", sessionID, exitCode)
		},
		// onAlert: called when a watch pattern matches PTY output.
		func(sessionID, pattern string, excerpt []byte) {
			log.Printf("[agent] alert session=%s pattern=%q excerpt=%q", sessionID, pattern, excerpt)
			ap := protocol.AlertPayload{Pattern: pattern, Excerpt: excerpt}
			payload, err := msgpack.Marshal(&ap)
			if err != nil {
				return
			}
			pkt, err := (&protocol.Packet{
				Type:    protocol.TypeAlert,
				Session: sessionID,
				Payload: payload,
			}).Encode()
			if err != nil {
				return
			}
			wsConn.Send(pkt)
		},
	)

	// Wire input/resize handlers back into the relay now that ptyMgr exists.
	wsConn.SetInputHandler(func(sessionID string, data []byte) {
		if err := ptyMgr.Write(sessionID, data); err != nil {
			log.Printf("[agent] write to session %s: %v", sessionID, err)
		}
	})
	wsConn.SetResizeHandler(func(sessionID string, cols, rows uint16) {
		if err := ptyMgr.Resize(sessionID, cols, rows); err != nil {
			log.Printf("[agent] resize session %s: %v", sessionID, err)
		}
	})
	wsConn.SetKillHandler(func(sessionID string) {
		if err := ptyMgr.Kill(sessionID); err != nil {
			log.Printf("[agent] kill session %s: %v", sessionID, err)
		}
	})
	// On every (re-)connect the backend marks all "active" sessions for this
	// device as exited.  Re-announce every session the PTY manager is still
	// running so the backend and mobile see the correct live state.
	wsConn.SetConnectHandler(func() {
		for _, si := range ptyMgr.List() {
			announceSession(wsConn, si.ID, si.Name, "")
			log.Printf("[agent] re-announced live session %s (%s)", si.ID, si.Name)
		}
	})

	// IPC server: Unix socket for local session management.
	ipcHandler := ipc.Handler{
		OnSpawn: func(sessionID, name, command string, cols, rows uint16, alertPatterns []string) error {
			if err := ptyMgr.Spawn(sessionID, name, command, cols, rows); err != nil {
				return err
			}
			// Register any extra alert patterns requested by the caller.
			if len(alertPatterns) > 0 {
				ptyMgr.SetExtraAlertPatterns(sessionID, alertPatterns)
			}
			// Notify backend about the new session.
			announceSession(wsConn, sessionID, name, command)
			return nil
		},
		OnKill: ptyMgr.Kill,
		OnList: func() []ipc.SessionInfo {
			infos := ptyMgr.List()
			out := make([]ipc.SessionInfo, len(infos))
			for i, si := range infos {
				out[i] = ipc.SessionInfo{ID: si.ID, Name: si.Name}
			}
			return out
		},
		OnResize: ptyMgr.Resize,
		OnRename: func(sessionID, name string) error {
			// Notify the backend via a TypeRenameSession packet so it updates the
			// DB and broadcasts the new name to connected mobile clients.
			rp := protocol.RenamePayload{SessionID: sessionID, Name: name}
			payload, err := msgpack.Marshal(&rp)
			if err != nil {
				return err
			}
			pkt, err := (&protocol.Packet{
				Type:    protocol.TypeRenameSession,
				Session: sessionID,
				Payload: payload,
			}).Encode()
			if err != nil {
				return err
			}
			wsConn.Send(pkt)
			return nil
		},
		OnAttach: func(sessionID string, conn net.Conn) error {
			defer conn.Close()
			cancel, err := ptyMgr.AddOutputConsumer(sessionID, func(data []byte) {
				_, _ = conn.Write(data)
			})
			if err != nil {
				return err
			}
			defer cancel()
			buf := make([]byte, 4096)
			for {
				n, err := conn.Read(buf)
				if n > 0 {
					_ = ptyMgr.Write(sessionID, buf[:n])
				}
				if err != nil {
					return nil
				}
			}
		},
	}

	// Start IPC server in background.
	go func() {
		if err := ipc.Serve(cfg.IPCSocket, ipcHandler); err != nil {
			select {
			case <-ctx.Done():
			default:
				log.Printf("[ipc] server error: %v", err)
			}
		}
	}()

	log.Printf("[agent] starting — device=%s server=%s ipc=%s", cfg.DeviceID, cfg.ServerURL, cfg.IPCSocket)

	// Run WebSocket relay (blocks, reconnects automatically until ctx is cancelled).
	wsConn.Run(ctx)
	log.Printf("[agent] shutdown complete")
}

// newUUID returns a random UUID v4 string.
func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant bits
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

// announceSession sends TypeSessionStatus "active" to the backend so it can
// create the session record.
func announceSession(conn *relay.Conn, sessionID, name, command string) {
	sp := protocol.SessionStatusPayload{
		SessionID: sessionID,
		Status:    "active",
		Name:      name,
		Command:   command,
	}
	payload, err := msgpack.Marshal(&sp)
	if err != nil {
		return
	}
	pkt, err := (&protocol.Packet{
		Type:    protocol.TypeSessionStatus,
		Session: sessionID,
		Payload: payload,
	}).Encode()
	if err != nil {
		return
	}
	conn.Send(pkt)
}
