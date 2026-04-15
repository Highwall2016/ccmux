package pty

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"strings"
	"sync"
	"time"
)

const readBufSize = 32 * 1024 // 32 KB read buffer per session

// alertCooldown is the minimum time between alert notifications per session.
const alertCooldown = 30 * time.Second

// defaultAlertPatterns are matched (case-insensitive) against PTY output chunks.
var defaultAlertPatterns = [][]byte{
	[]byte("error"),
	[]byte("failed"),
	[]byte("panic"),
	[]byte("fatal"),
}

// OutputFunc is called with each chunk of PTY output.
type OutputFunc func(sessionID string, data []byte)

// StatusFunc is called when a session's process exits.
type StatusFunc func(sessionID string, exitCode int)

// AlertFunc is called when a watch pattern matches PTY output.
type AlertFunc func(sessionID, pattern string, excerpt []byte)

type localConsumer struct {
	fn func([]byte)
}

// Manager owns all live PTY sessions and drives their I/O loops.
type Manager struct {
	sessions     map[string]*Session
	mu           sync.RWMutex
	onOutput     OutputFunc
	onExit       StatusFunc
	onAlert      AlertFunc // may be nil
	defaultShell string

	// alertAt tracks the last alert time per session to enforce cooldown.
	alertAt   map[string]time.Time
	alertAtMu sync.Mutex

	// consumers holds local attach subscribers per session.
	consumers   map[string][]*localConsumer
	consumersMu sync.RWMutex
}

// NewManager creates a Manager.
func NewManager(shell string, onOutput OutputFunc, onExit StatusFunc, onAlert AlertFunc) *Manager {
	return &Manager{
		sessions:     make(map[string]*Session),
		onOutput:     onOutput,
		onExit:       onExit,
		onAlert:      onAlert,
		defaultShell: shell,
		alertAt:      make(map[string]time.Time),
		consumers:    make(map[string][]*localConsumer),
	}
}

// Spawn starts a new PTY session. command may be empty (uses default shell).
func (m *Manager) Spawn(id, command string, cols, rows uint16) error {
	if command == "" {
		command = m.defaultShell
	}
	parts := strings.Fields(command)
	shell := parts[0]
	args := parts[1:]

	sess, err := Start(id, shell, args, cols, rows)
	if err != nil {
		return fmt.Errorf("spawn session %s: %w", id, err)
	}

	m.mu.Lock()
	m.sessions[id] = sess
	m.mu.Unlock()

	// Read loop: runs until the PTY is closed.
	go func() {
		buf := make([]byte, readBufSize)
		for {
			n, err := sess.ReadOutput(buf)
			if n > 0 {
				chunk := make([]byte, n)
				copy(chunk, buf[:n])
				m.onOutput(id, chunk)
				m.consumersMu.RLock()
				for _, c := range m.consumers[id] {
					c.fn(chunk)
				}
				m.consumersMu.RUnlock()
				if m.onAlert != nil {
					m.checkAlerts(id, chunk)
				}
			}
			if err != nil {
				if err != io.EOF {
					log.Printf("[pty] session %s read error: %v", id, err)
				}
				break
			}
		}
	}()

	// Wait loop: waits for the process to exit.
	go func() {
		exitCode, err := sess.Wait()
		if err != nil {
			log.Printf("[pty] session %s wait error: %v", id, err)
		}
		m.mu.Lock()
		delete(m.sessions, id)
		m.mu.Unlock()
		sess.Close()
		m.onExit(id, exitCode)
	}()

	return nil
}

// Write sends input to a session's PTY.
func (m *Manager) Write(id string, data []byte) error {
	m.mu.RLock()
	sess, ok := m.sessions[id]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("session %s not found", id)
	}
	return sess.Write(data)
}

// Resize changes the terminal window size of a session.
func (m *Manager) Resize(id string, cols, rows uint16) error {
	m.mu.RLock()
	sess, ok := m.sessions[id]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("session %s not found", id)
	}
	return sess.Resize(cols, rows)
}

// Kill terminates a session by closing its PTY.
func (m *Manager) Kill(id string) error {
	m.mu.RLock()
	sess, ok := m.sessions[id]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("session %s not found", id)
	}
	sess.Close()
	return nil
}

// List returns the IDs of all active sessions.
func (m *Manager) List() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ids := make([]string, 0, len(m.sessions))
	for id := range m.sessions {
		ids = append(ids, id)
	}
	return ids
}

// AddOutputConsumer registers fn to receive PTY output for sessionID.
// Returns a cancel func that deregisters the consumer when called.
func (m *Manager) AddOutputConsumer(sessionID string, fn func([]byte)) (cancel func(), err error) {
	m.mu.RLock()
	_, ok := m.sessions[sessionID]
	m.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}
	c := &localConsumer{fn: fn}
	m.consumersMu.Lock()
	m.consumers[sessionID] = append(m.consumers[sessionID], c)
	m.consumersMu.Unlock()

	return func() {
		m.consumersMu.Lock()
		defer m.consumersMu.Unlock()
		cs := m.consumers[sessionID]
		out := cs[:0]
		for _, existing := range cs {
			if existing != c {
				out = append(out, existing)
			}
		}
		if len(out) == 0 {
			delete(m.consumers, sessionID)
		} else {
			m.consumers[sessionID] = out
		}
	}, nil
}

// checkAlerts scans a PTY output chunk for alert patterns and fires onAlert
// at most once per session per alertCooldown window.
func (m *Manager) checkAlerts(sessionID string, chunk []byte) {
	lower := bytes.ToLower(chunk)
	for _, pat := range defaultAlertPatterns {
		if !bytes.Contains(lower, pat) {
			continue
		}
		m.alertAtMu.Lock()
		last := m.alertAt[sessionID]
		now := time.Now()
		if now.Sub(last) < alertCooldown {
			m.alertAtMu.Unlock()
			return
		}
		m.alertAt[sessionID] = now
		m.alertAtMu.Unlock()

		// Extract a short excerpt (first 120 bytes of the chunk).
		excerpt := chunk
		if len(excerpt) > 120 {
			excerpt = excerpt[:120]
		}
		m.onAlert(sessionID, string(pat), excerpt)
		return // one alert per chunk max
	}
}
