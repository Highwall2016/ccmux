// Package ipc provides a Unix domain socket server for local session management.
// The ccmux-agent listens on /tmp/ccmux.sock (configurable). A companion CLI
// tool can send JSON commands to spawn or kill sessions without network access.
package ipc

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
)

// SpawnRequest asks the agent to start a new PTY session.
type SpawnRequest struct {
	Cmd       string `json:"cmd"`        // must be "spawn"
	SessionID string `json:"session_id"` // must be a UUID (caller-generated)
	Command   string `json:"command"`    // shell command; empty = default shell
	Cols      uint16 `json:"cols"`       // terminal width; 0 → 80
	Rows      uint16 `json:"rows"`       // terminal height; 0 → 24
}

// KillRequest asks the agent to terminate a PTY session.
type KillRequest struct {
	Cmd       string `json:"cmd"`        // must be "kill"
	SessionID string `json:"session_id"`
}

// ListRequest asks for all active session IDs.
type ListRequest struct {
	Cmd string `json:"cmd"` // must be "list"
}

// Response is returned for every command.
type Response struct {
	OK       bool     `json:"ok"`
	Error    string   `json:"error,omitempty"`
	Sessions []string `json:"sessions,omitempty"` // set for "list"
}

// Handler dispatches incoming IPC commands.
type Handler struct {
	OnSpawn func(sessionID, command string, cols, rows uint16) error
	OnKill  func(sessionID string) error
	OnList  func() []string
}

// Serve starts the Unix socket server and blocks until ctx is cancelled.
// It removes any stale socket file before binding.
func Serve(socketPath string, h Handler) error {
	// Clean up leftover socket from a previous run.
	_ = os.Remove(socketPath)

	l, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("ipc listen: %w", err)
	}
	defer l.Close()
	log.Printf("[ipc] listening on %s", socketPath)

	for {
		conn, err := l.Accept()
		if err != nil {
			return err
		}
		go h.handle(conn)
	}
}

func (h *Handler) handle(conn net.Conn) {
	defer conn.Close()

	dec := json.NewDecoder(conn)
	enc := json.NewEncoder(conn)

	var raw map[string]json.RawMessage
	if err := dec.Decode(&raw); err != nil {
		_ = enc.Encode(Response{Error: "invalid JSON: " + err.Error()})
		return
	}

	var cmdStr string
	if v, ok := raw["cmd"]; ok {
		_ = json.Unmarshal(v, &cmdStr)
	}

	var resp Response
	switch cmdStr {
	case "spawn":
		var req SpawnRequest
		data, _ := json.Marshal(raw)
		if err := json.Unmarshal(data, &req); err != nil {
			resp.Error = "invalid spawn request: " + err.Error()
			break
		}
		if req.SessionID == "" {
			resp.Error = "session_id is required"
			break
		}
		if req.Cols == 0 {
			req.Cols = 80
		}
		if req.Rows == 0 {
			req.Rows = 24
		}
		if err := h.OnSpawn(req.SessionID, req.Command, req.Cols, req.Rows); err != nil {
			resp.Error = err.Error()
		} else {
			resp.OK = true
		}

	case "kill":
		var req KillRequest
		data, _ := json.Marshal(raw)
		if err := json.Unmarshal(data, &req); err != nil {
			resp.Error = "invalid kill request: " + err.Error()
			break
		}
		if err := h.OnKill(req.SessionID); err != nil {
			resp.Error = err.Error()
		} else {
			resp.OK = true
		}

	case "list":
		resp.OK = true
		resp.Sessions = h.OnList()

	default:
		resp.Error = fmt.Sprintf("unknown command: %q", cmdStr)
	}

	_ = enc.Encode(resp)
}
