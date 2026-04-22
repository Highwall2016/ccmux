// Package tmux provides helpers for detecting and interacting with a local
// tmux server.  All functions gracefully return errors when tmux is not
// installed or no server is running — callers must treat tmux as optional.
package tmux

import (
	"crypto/sha256"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

const fieldSep = "\x1f" // field separator for tmux -F format; must not appear in names

// PaneInfo describes a single tmux pane.
type PaneInfo struct {
	TmuxSession string // tmux session name
	WindowIndex int
	WindowName  string
	PaneIndex   int
	PaneID      string // e.g. "%3" — unique within the tmux server
	PID         int    // PID of the pane's foreground process
	Title       string // pane title (usually the running command)
	Target      string // "<session>:<window>.<pane>" canonical target string
	CcmuxID     string // stable ccmux session UUID derived from PaneID
}

// IsRunning returns true when a tmux server is reachable (tmux is installed
// and at least one session exists).
func IsRunning() bool {
	err := exec.Command("tmux", "list-sessions").Run()
	return err == nil
}

// ListPanes returns all panes across all sessions of the running tmux server.
func ListPanes() ([]PaneInfo, error) {
	format := strings.Join([]string{
		"#{session_name}",
		"#{window_index}",
		"#{window_name}",
		"#{pane_index}",
		"#{pane_id}",
		"#{pane_pid}",
		"#{pane_title}",
		"#{pane_active}",
	}, fieldSep)

	out, err := exec.Command("tmux", "list-panes", "-a", "-F", format).Output()
	if err != nil {
		return nil, fmt.Errorf("tmux list-panes: %w", err)
	}

	var panes []PaneInfo
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, fieldSep)
		if len(parts) < 8 {
			continue
		}
		wi, _ := strconv.Atoi(parts[1])
		pi, _ := strconv.Atoi(parts[3])
		pid, _ := strconv.Atoi(parts[5])
		target := fmt.Sprintf("%s:%d.%d", parts[0], wi, pi)
		p := PaneInfo{
			TmuxSession: parts[0],
			WindowIndex: wi,
			WindowName:  parts[2],
			PaneIndex:   pi,
			PaneID:      parts[4],
			PID:         pid,
			Title:       parts[6],
			Target:      target,
			CcmuxID:     deriveCcmuxID(parts[4]),
		}
		panes = append(panes, p)
	}
	return panes, nil
}

// NewWindow creates a new window in the tmux server and returns its target
// string (e.g. "main:2.0").  windowName and command may be empty.
// sessionName selects which tmux session to add the window to; when empty,
// the current/first available session is used.
// The new window is created detached (-d) so the caller's terminal is not
// affected.
func NewWindow(sessionName, windowName, command string) (target string, err error) {
	args := []string{"new-window", "-P", "-d", "-F",
		"#{session_name}:#{window_index}.0"}
	if sessionName != "" {
		args = append(args, "-t", sessionName)
	}
	if windowName != "" {
		args = append(args, "-n", windowName)
	}
	if command != "" {
		args = append(args, command)
	}
	out, execErr := exec.Command("tmux", args...).Output()
	if execErr != nil {
		return "", fmt.Errorf("tmux new-window: %w", execErr)
	}
	return strings.TrimSpace(string(out)), nil
}

// NewSession creates a new detached tmux session and returns the canonical
// target of its initial pane, e.g. "work:0.0".
// sessionName may be empty — tmux will auto-assign a name (e.g. "0").
// command may be empty — tmux will start its configured default-shell.
func NewSession(sessionName, command string) (target string, err error) {
	args := []string{"new-session", "-d",
		"-P", "-F", "#{session_name}:#{window_index}.#{pane_index}"}
	if sessionName != "" {
		args = append(args, "-s", sessionName)
	}
	if command != "" {
		args = append(args, command)
	}
	out, execErr := exec.Command("tmux", args...).Output()
	if execErr != nil {
		return "", fmt.Errorf("tmux new-session: %w", execErr)
	}
	return strings.TrimSpace(string(out)), nil
}

// SplitWindow creates a new pane by splitting an existing one and returns the
// new pane's target string.  target may be empty (splits the active pane).
func SplitWindow(target, command string) (newTarget string, err error) {
	args := []string{"split-window", "-P", "-F",
		"#{session_name}:#{window_index}.#{pane_index}"}
	if target != "" {
		args = append(args, "-t", target)
	}
	if command != "" {
		args = append(args, command)
	}
	out, execErr := exec.Command("tmux", args...).Output()
	if execErr != nil {
		return "", fmt.Errorf("tmux split-window: %w", execErr)
	}
	return strings.TrimSpace(string(out)), nil
}

// KillPane kills a specific tmux pane by target (e.g. "main:0.0").
// If the pane is the last one in its window, the window is also killed.
// If the window is the last one in its session, the session is also killed.
// This is the correct way to terminate a ccmux session that owns a tmux pane.
func KillPane(target string) error {
	if err := exec.Command("tmux", "kill-pane", "-t", target).Run(); err != nil {
		return fmt.Errorf("tmux kill-pane %s: %w", target, err)
	}
	return nil
}

// deriveCcmuxID converts a tmux pane ID (e.g. "%3") into a stable ccmux
// session UUID.  The UUID is deterministic so the same pane always maps to
// the same ccmux session ID across agent restarts.
func deriveCcmuxID(paneID string) string {
	h := sha256.Sum256([]byte("ccmux/tmux/pane/" + paneID))
	b := h[:16]
	b[6] = (b[6] & 0x0f) | 0x40 // UUID version 4
	b[8] = (b[8] & 0x3f) | 0x80 // UUID variant
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
