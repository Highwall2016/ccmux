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
	"github.com/ccmux/agent/internal/metrics"
	agentpty "github.com/ccmux/agent/internal/pty"
	"github.com/ccmux/agent/internal/relay"
	agenttmux "github.com/ccmux/agent/internal/tmux"
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
		// For tmux-backed sessions, kill the actual tmux pane/session first.
		// Without this, the tmux session keeps running and DiscoverAndRegister
		// will re-announce it as active moments later.
		if target := ptyMgr.TmuxTargetOf(sessionID); target != "" {
			if err := agenttmux.KillPane(target); err != nil {
				log.Printf("[agent] kill tmux pane %s for session %s: %v", target, sessionID, err)
			}
		}
		if err := ptyMgr.Kill(sessionID); err != nil {
			log.Printf("[agent] kill session %s: %v", sessionID, err)
		}
	})
	wsConn.SetSpawnHandler(func(sessionID, name, command string, cols, rows uint16, alertPatterns []string, useTmux, tmuxSplit bool) {
		// Default to a new tmux session when tmux is available.
		// useTmux=false only skips tmux when the caller explicitly requested a
		// bare PTY (future: use a TmuxBare flag); for now any running tmux server
		// is preferred so that every ccmux session is a real tmux session.
		if !useTmux {
			useTmux = agenttmux.IsRunning()
		}
		if useTmux && agenttmux.IsRunning() {
			var target string
			var tmuxErr error
			if tmuxSplit {
				target, tmuxErr = agenttmux.SplitWindow("", command)
			} else {
				target, tmuxErr = agenttmux.NewSession(name, command)
			}
			if tmuxErr != nil {
				log.Printf("[agent] tmux new-session for %s: %v", sessionID, tmuxErr)
				// Fall through to bare PTY.
			} else {
				if err := ptyMgr.SpawnTmux(sessionID, name, target, cols, rows); err != nil {
					log.Printf("[agent] spawn tmux session %s: %v", sessionID, err)
					return
				}
				announceTmuxSession(wsConn, sessionID, name, command, target)
				log.Printf("[agent] spawned tmux session %s (%s) target=%s", sessionID, name, target)
				return
			}
		}
		if err := ptyMgr.Spawn(sessionID, name, command, cols, rows); err != nil {
			log.Printf("[agent] remote spawn session %s: %v", sessionID, err)
			return
		}
		if len(alertPatterns) > 0 {
			ptyMgr.SetExtraAlertPatterns(sessionID, alertPatterns)
		}
		announceSession(wsConn, sessionID, name, command)
		log.Printf("[agent] spawned bare session %s (%s) command=%q", sessionID, name, command)
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
		OnSpawnTmux: func(sessionID, name, tmuxTarget string, cols, rows uint16) error {
			if err := ptyMgr.SpawnTmux(sessionID, name, tmuxTarget, cols, rows); err != nil {
				return err
			}
			announceTmuxSession(wsConn, sessionID, name, "", tmuxTarget)
			return nil
		},
		OnKill: ptyMgr.Kill,
		OnList: func() []ipc.SessionInfo {
			infos := ptyMgr.List()
			out := make([]ipc.SessionInfo, len(infos))
			for i, si := range infos {
				target := ptyMgr.TmuxTargetOf(si.ID)
				out[i] = ipc.SessionInfo{
					ID:         si.ID,
					Name:       si.Name,
					TmuxBacked: target != "",
					TmuxTarget: target,
				}
			}
			return out
		},
		OnInfo: func(sessionID string) (bool, string, error) {
			id, err := ptyMgr.ResolveID(sessionID)
			if err != nil {
				return false, "", err
			}
			target := ptyMgr.TmuxTargetOf(id)
			return target != "", target, nil
		},
		OnResize: ptyMgr.Resize,
		OnRename: func(sessionID, name string) error {
			resolvedID, err := ptyMgr.ResolveID(sessionID)
			if err != nil {
				return err
			}
			// Notify the backend via a TypeRenameSession packet so it updates the
			// DB and broadcasts the new name to connected mobile clients.
			rp := protocol.RenamePayload{SessionID: resolvedID, Name: name}
			payload, err := msgpack.Marshal(&rp)
			if err != nil {
				return err
			}
			pkt, err := (&protocol.Packet{
				Type:    protocol.TypeRenameSession,
				Session: resolvedID,
				Payload: payload,
			}).Encode()
			if err != nil {
				return err
			}
			wsConn.Send(pkt)
			return nil
		},
		OnAttach: func(sessionID string, conn net.Conn, ready chan<- error) {
			cancel, err := ptyMgr.AddOutputConsumer(sessionID, func(data []byte) {
				_, _ = conn.Write(data)
			})
			if err != nil {
				// Signal failure; do NOT close conn — IPC server owns it on error path.
				ready <- err
				return
			}
			defer conn.Close()
			defer cancel()
			// Session found and streaming started; signal OK so IPC server sends
			// the success response to the client.
			ready <- nil
			buf := make([]byte, 4096)
			for {
				n, err := conn.Read(buf)
				if n > 0 {
					_ = ptyMgr.Write(sessionID, buf[:n])
				}
				if err != nil {
					return
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

	// Start tmux auto-discovery if tmux is available.
	// This runs in a background goroutine; the agent works fine without tmux.
	agenttmux.DiscoverAndRegister(
		cfg.DeviceID,
		ptyMgr,
		wsConn,
		cfg.TmuxWatchInterval,
		func(sessionID, name string, tmuxBacked bool, tmuxTarget string) {
			announceTmuxSession(wsConn, sessionID, name, "", tmuxTarget)
		},
	)

	// Metrics collector: samples CPU + memory every 5 s and piggybacks the
	// values onto a TypeTmuxTree packet (with an empty sessions list).
	// TypeTmuxTree is forwarded by ALL backend versions via BroadcastToDevice,
	// so this works even with the deployed backend that predates TypeDeviceMetrics.
	go metrics.Run(ctx, 0, func(s metrics.Snapshot) {
		cpu := s.CPUPercent
		used := s.MemUsedMB
		total := s.MemTotalMB
		tree := protocol.TmuxTreePayload{
			DeviceID:   cfg.DeviceID,
			Sessions:   []protocol.TmuxSessionTree{}, // empty — metrics-only heartbeat
			CPUPercent: &cpu,
			MemUsedMB:  &used,
			MemTotalMB: &total,
		}
		payload, err := msgpack.Marshal(&tree)
		if err != nil {
			return
		}
		pkt, err := (&protocol.Packet{
			Type:    protocol.TypeTmuxTree,
			Payload: payload,
		}).Encode()
		if err != nil {
			return
		}
		log.Printf("[metrics] cpu=%.1f%% mem=%dMB/%dMB → sending via TmuxTree heartbeat", cpu, used, total)
		wsConn.Send(pkt)
	})

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

// announceTmuxSession is like announceSession but includes tmux metadata so
// the backend stores tmux_backed=true and the mobile app shows the correct UI.
func announceTmuxSession(conn *relay.Conn, sessionID, name, command, tmuxTarget string) {
	sp := protocol.SessionStatusPayload{
		SessionID:  sessionID,
		Status:     "active",
		Name:       name,
		Command:    command,
		TmuxBacked: true,
		TmuxTarget: tmuxTarget,
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
