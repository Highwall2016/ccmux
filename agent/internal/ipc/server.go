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
	Cmd           string   `json:"cmd"`                      // must be "spawn"
	SessionID     string   `json:"session_id"`               // UUID, always agent-generated
	Name          string   `json:"name,omitempty"`           // optional human-readable display name
	Command       string   `json:"command"`                  // shell command; empty = default shell
	Cols          uint16   `json:"cols"`                     // terminal width; 0 → 80
	Rows          uint16   `json:"rows"`                     // terminal height; 0 → 24
	AlertPatterns []string `json:"alert_patterns,omitempty"` // extra alert patterns beyond defaults
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

// AttachRequest asks the agent to stream a session's I/O over the socket.
// After the server sends {"ok":true}, the connection switches to raw binary:
// bytes written to the socket are forwarded to the PTY as input; bytes read
// from the socket are PTY output. The connection stays open until the client
// disconnects or the session exits.
type AttachRequest struct {
	Cmd       string `json:"cmd"`        // must be "attach"
	SessionID string `json:"session_id"`
}

// RenameRequest asks the agent to rename a session.
type RenameRequest struct {
	Cmd       string `json:"cmd"`        // must be "rename"
	SessionID string `json:"session_id"`
	Name      string `json:"name"`
}

// ResizeRequest asks the agent to resize a session's PTY window.
type ResizeRequest struct {
	Cmd       string `json:"cmd"`        // must be "resize"
	SessionID string `json:"session_id"`
	Cols      uint16 `json:"cols"`
	Rows      uint16 `json:"rows"`
}

// Response is returned for every command.
type Response struct {
	OK       bool     `json:"ok"`
	Error    string   `json:"error,omitempty"`
	Sessions []string `json:"sessions,omitempty"` // set for "list"
}

// Handler dispatches incoming IPC commands.
type Handler struct {
	OnSpawn  func(sessionID, name, command string, cols, rows uint16, alertPatterns []string) error
	OnKill   func(sessionID string) error
	OnList   func() []string
	OnResize func(sessionID string, cols, rows uint16) error
	OnRename func(sessionID, name string) error
	// OnAttach streams raw PTY I/O over conn after sending the OK response.
	// It owns conn for its lifetime and must close it when done.
	OnAttach func(sessionID string, conn net.Conn) error
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
	// attach takes ownership of conn; all other commands close it here.
	ownedByAttach := false
	defer func() {
		if !ownedByAttach {
			conn.Close()
		}
	}()

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
		if err := h.OnSpawn(req.SessionID, req.Name, req.Command, req.Cols, req.Rows, req.AlertPatterns); err != nil {
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

	case "resize":
		var req ResizeRequest
		data, _ := json.Marshal(raw)
		if err := json.Unmarshal(data, &req); err != nil {
			resp.Error = "invalid resize request: " + err.Error()
			break
		}
		if h.OnResize == nil {
			resp.Error = "resize not supported"
			break
		}
		if err := h.OnResize(req.SessionID, req.Cols, req.Rows); err != nil {
			resp.Error = err.Error()
		} else {
			resp.OK = true
		}

	case "rename":
		var req RenameRequest
		data, _ := json.Marshal(raw)
		if err := json.Unmarshal(data, &req); err != nil {
			resp.Error = "invalid rename request: " + err.Error()
			break
		}
		if req.SessionID == "" {
			resp.Error = "session_id is required"
			break
		}
		if req.Name == "" {
			resp.Error = "name is required"
			break
		}
		if h.OnRename == nil {
			resp.Error = "rename not supported"
			break
		}
		if err := h.OnRename(req.SessionID, req.Name); err != nil {
			resp.Error = err.Error()
		} else {
			resp.OK = true
		}

	case "attach":
		var req AttachRequest
		data, _ := json.Marshal(raw)
		if err := json.Unmarshal(data, &req); err != nil {
			_ = enc.Encode(Response{Error: "invalid attach request: " + err.Error()})
			return
		}
		if req.SessionID == "" {
			_ = enc.Encode(Response{Error: "session_id is required"})
			return
		}
		if h.OnAttach == nil {
			_ = enc.Encode(Response{Error: "attach not supported"})
			return
		}
		// Send OK then hand off the raw connection.
		if err := enc.Encode(Response{OK: true}); err != nil {
			return
		}
		ownedByAttach = true
		go func() {
			if err := h.OnAttach(req.SessionID, conn); err != nil {
				log.Printf("[ipc] attach %s: %v", req.SessionID, err)
			}
		}()
		return

	default:
		resp.Error = fmt.Sprintf("unknown command: %q", cmdStr)
	}

	_ = enc.Encode(resp)
}
