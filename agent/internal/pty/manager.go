package pty

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// SessionInfo pairs a session ID with its display name.
type SessionInfo struct {
	ID   string
	Name string
}

const readBufSize = 32 * 1024 // 32 KB read buffer per session

// alertCooldown is the minimum time between alert notifications per session.
const alertCooldown = 30 * time.Second

// ansiEscape matches ANSI/VT100 escape sequences so they can be stripped
// before pattern matching, avoiding false negatives on coloured output.
var ansiEscape = regexp.MustCompile(`\x1b(?:\[[0-9;]*[A-Za-z]|[@-_][0-9;]*[@-~]?|[^\[])`)

// defaultAlertPatterns are matched (case-insensitive) against PTY output after
// ANSI escape codes are stripped.  The patterns cover both generic error words
// and Claude Code interactive prompts so that mobile users are notified when
// Claude Code asks a question or requires confirmation.
var defaultAlertPatterns = [][]byte{
	// Generic errors
	[]byte("error"),
	[]byte("failed"),
	[]byte("panic"),
	[]byte("fatal"),
	// Claude Code interactive prompts
	[]byte("esc to cancel"),  // shown on every Claude Code tool-use approval
	[]byte("do you want"),    // permission / proceed questions
	[]byte("would you like"), // permission / proceed questions
	[]byte("are you sure"),   // confirmation prompts
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

	// names maps session ID → display name.
	names   map[string]string
	namesMu sync.RWMutex

	// alertAt tracks the last alert time per session to enforce cooldown.
	alertAt   map[string]time.Time
	alertAtMu sync.Mutex

	// consumers holds local attach subscribers per session.
	consumers   map[string][]*localConsumer
	consumersMu sync.RWMutex

	// extraPatterns holds additional alert patterns per session (in addition to
	// defaultAlertPatterns).  Keys are session IDs.
	extraPatterns   map[string][][]byte
	extraPatternsMu sync.RWMutex
}

// NewManager creates a Manager.
func NewManager(shell string, onOutput OutputFunc, onExit StatusFunc, onAlert AlertFunc) *Manager {
	return &Manager{
		sessions:      make(map[string]*Session),
		names:         make(map[string]string),
		onOutput:      onOutput,
		onExit:        onExit,
		onAlert:       onAlert,
		defaultShell:  shell,
		alertAt:       make(map[string]time.Time),
		consumers:     make(map[string][]*localConsumer),
		extraPatterns: make(map[string][][]byte),
	}
}

// nextName returns the lowest non-negative integer (as a string) that is not
// already used as a session name.
func (m *Manager) nextName() string {
	m.namesMu.RLock()
	inUse := make(map[string]bool, len(m.names))
	for _, n := range m.names {
		inUse[n] = true
	}
	m.namesMu.RUnlock()
	for i := 0; ; i++ {
		if !inUse[strconv.Itoa(i)] {
			return strconv.Itoa(i)
		}
	}
}

// resolveID returns the session ID for nameOrID.
// It first checks if nameOrID is a live session ID, then searches by name.
// Returns ("", false) when not found.
func (m *Manager) resolveID(nameOrID string) (string, bool) {
	m.mu.RLock()
	_, byID := m.sessions[nameOrID]
	m.mu.RUnlock()
	if byID {
		return nameOrID, true
	}
	m.namesMu.RLock()
	defer m.namesMu.RUnlock()
	for id, name := range m.names {
		if name == nameOrID {
			return id, true
		}
	}
	return "", false
}

// ResolveID resolves a live session name or UUID to its canonical UUID.
func (m *Manager) ResolveID(nameOrID string) (string, error) {
	id, ok := m.resolveID(nameOrID)
	if !ok {
		return "", fmt.Errorf("session %q not found", nameOrID)
	}
	return id, nil
}

// SetExtraAlertPatterns registers additional (case-insensitive) alert patterns
// for a specific session.  Call before or after Spawn; patterns are checked on
// every output chunk for the lifetime of the session.
func (m *Manager) SetExtraAlertPatterns(sessionID string, patterns []string) {
	compiled := make([][]byte, 0, len(patterns))
	for _, p := range patterns {
		if p != "" {
			compiled = append(compiled, []byte(strings.ToLower(p)))
		}
	}
	m.extraPatternsMu.Lock()
	m.extraPatterns[sessionID] = compiled
	m.extraPatternsMu.Unlock()
}

// Spawn starts a new PTY session. command may be empty (uses default shell).
// If name is empty a numeric name is auto-assigned (0, 1, 2, …).
// If command contains shell metacharacters or multiple words with arguments,
// it is executed via the default shell so that quoting works correctly.
func (m *Manager) Spawn(id, name, command string, cols, rows uint16) error {
	if name == "" {
		name = m.nextName()
	}
	if command == "" {
		command = m.defaultShell
	}

	var shell string
	var args []string

	parts := strings.Fields(command)
	// Use "sh -c <command>" when the command string contains spaces or
	// shell metacharacters so arguments with embedded spaces are preserved.
	if len(parts) > 1 {
		shell = m.defaultShell
		args = []string{"-c", command}
	} else {
		shell = parts[0]
	}

	sess, err := Start(id, shell, args, cols, rows)
	if err != nil {
		return fmt.Errorf("spawn session %s: %w", id, err)
	}

	m.mu.Lock()
	m.sessions[id] = sess
	m.mu.Unlock()
	m.namesMu.Lock()
	m.names[id] = name
	m.namesMu.Unlock()

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
		// Remove from map; this is a no-op when Kill() already removed it.
		m.mu.Lock()
		_, stillInMap := m.sessions[id]
		delete(m.sessions, id)
		m.mu.Unlock()
		m.extraPatternsMu.Lock()
		delete(m.extraPatterns, id)
		m.extraPatternsMu.Unlock()
		m.namesMu.Lock()
		delete(m.names, id)
		m.namesMu.Unlock()
		sess.Close()
		// Only fire onExit for natural exits.  When Kill() removed the session
		// first, the backend was already notified "killed" by the REST handler.
		if stillInMap {
			m.onExit(id, exitCode)
		}
	}()

	return nil
}

// Write sends input to a session's PTY. nameOrID may be the session's display
// name or its UUID.
func (m *Manager) Write(nameOrID string, data []byte) error {
	id, ok := m.resolveID(nameOrID)
	if !ok {
		return fmt.Errorf("session %q not found", nameOrID)
	}
	m.mu.RLock()
	sess, ok := m.sessions[id]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("session %q not found", nameOrID)
	}
	return sess.Write(data)
}

// Resize changes the terminal window size of a session. nameOrID may be the
// session's display name or its UUID.
func (m *Manager) Resize(nameOrID string, cols, rows uint16) error {
	id, ok := m.resolveID(nameOrID)
	if !ok {
		return fmt.Errorf("session %q not found", nameOrID)
	}
	m.mu.RLock()
	sess, ok := m.sessions[id]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("session %q not found", nameOrID)
	}
	return sess.Resize(cols, rows)
}

// Kill terminates a session. nameOrID may be the session's display name or its
// UUID. The session is removed from the map immediately under the write lock so
// that `list` and `attach` stop seeing it right away.
func (m *Manager) Kill(nameOrID string) error {
	id, ok := m.resolveID(nameOrID)
	if !ok {
		return fmt.Errorf("session %q not found", nameOrID)
	}
	m.mu.Lock()
	sess, ok := m.sessions[id]
	if ok {
		delete(m.sessions, id)
	}
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("session %q not found", nameOrID)
	}
	m.extraPatternsMu.Lock()
	delete(m.extraPatterns, id)
	m.extraPatternsMu.Unlock()
	m.namesMu.Lock()
	delete(m.names, id)
	m.namesMu.Unlock()
	sess.Kill()
	sess.Close()
	return nil
}

// List returns info for all active sessions.
func (m *Manager) List() []SessionInfo {
	m.mu.RLock()
	ids := make([]string, 0, len(m.sessions))
	for id := range m.sessions {
		ids = append(ids, id)
	}
	m.mu.RUnlock()

	m.namesMu.RLock()
	infos := make([]SessionInfo, 0, len(ids))
	for _, id := range ids {
		infos = append(infos, SessionInfo{ID: id, Name: m.names[id]})
	}
	m.namesMu.RUnlock()
	return infos
}

// AddOutputConsumer registers fn to receive PTY output for the selected
// session. nameOrID may be the session's display name or its UUID.
// Returns a cancel func that deregisters the consumer when called.
func (m *Manager) AddOutputConsumer(nameOrID string, fn func([]byte)) (cancel func(), err error) {
	sessionID, ok := m.resolveID(nameOrID)
	if !ok {
		return nil, fmt.Errorf("session %q not found", nameOrID)
	}
	m.mu.RLock()
	_, ok = m.sessions[sessionID]
	m.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("session %q not found", nameOrID)
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

// stripANSI removes ANSI/VT100 escape sequences from b so that pattern
// matching works on plain text regardless of terminal colouring.
func stripANSI(b []byte) []byte {
	return ansiEscape.ReplaceAll(b, nil)
}

// checkAlerts scans a PTY output chunk for alert patterns and fires onAlert
// at most once per session per alertCooldown window.
// ANSI escape codes are stripped before matching so coloured output does not
// mask patterns (e.g. Claude Code's coloured prompts).
func (m *Manager) checkAlerts(sessionID string, chunk []byte) {
	plain := bytes.ToLower(stripANSI(chunk))

	// Build the combined pattern list: defaults + session-specific extras.
	m.extraPatternsMu.RLock()
	extra := m.extraPatterns[sessionID]
	m.extraPatternsMu.RUnlock()

	patterns := defaultAlertPatterns
	if len(extra) > 0 {
		combined := make([][]byte, len(defaultAlertPatterns)+len(extra))
		copy(combined, defaultAlertPatterns)
		copy(combined[len(defaultAlertPatterns):], extra)
		patterns = combined
	}

	var matchedPat []byte
	for _, pat := range patterns {
		if bytes.Contains(plain, pat) {
			matchedPat = pat
			break
		}
	}
	if matchedPat == nil {
		return
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

	// Use the ANSI-stripped text as the excerpt so the notification body is
	// readable on mobile without raw escape sequences.
	plainExcerpt := stripANSI(chunk)
	if len(plainExcerpt) > 120 {
		plainExcerpt = plainExcerpt[:120]
	}
	m.onAlert(sessionID, string(matchedPat), plainExcerpt)
}
