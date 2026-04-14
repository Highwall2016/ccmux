// Package main is the entry point for the ccmux desktop agent.
// It manages local PTY sessions and relays their I/O to the ccmux backend
// over a persistent WebSocket connection.
package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"log"
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

	// IPC server: Unix socket for local session management.
	ipcHandler := ipc.Handler{
		OnSpawn: func(callerID, command string, cols, rows uint16) error {
			// If the caller didn't supply an ID, generate a UUID v4.
			sessionID := callerID
			if sessionID == "" {
				var err error
				sessionID, err = newUUID()
				if err != nil {
					return fmt.Errorf("generate session ID: %w", err)
				}
			}
			if err := ptyMgr.Spawn(sessionID, command, cols, rows); err != nil {
				return err
			}
			// Notify backend about the new session.
			announceSession(wsConn, sessionID, command)
			return nil
		},
		OnKill: ptyMgr.Kill,
		OnList: ptyMgr.List,
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
func announceSession(conn *relay.Conn, sessionID, command string) {
	sp := protocol.SessionStatusPayload{
		SessionID: sessionID,
		Status:    "active",
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
