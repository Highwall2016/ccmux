// Package pty manages local PTY (pseudo-terminal) sessions.
package pty

import (
	"fmt"
	"os"
	"os/exec"
	"sync"

	"github.com/creack/pty"
)

// Session is a single running PTY process.
type Session struct {
	ID      string
	Command string

	ptmx *os.File
	cmd  *exec.Cmd
	mu   sync.Mutex
	done chan struct{}
}

// Start spawns cmd in a new PTY with the given terminal size.
// Returns the Session; call Read to consume output.
func Start(id, shellPath string, args []string, cols, rows uint16) (*Session, error) {
	cmd := exec.Command(shellPath, args...)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	size := &pty.Winsize{Cols: cols, Rows: rows}
	ptmx, err := pty.StartWithSize(cmd, size)
	if err != nil {
		return nil, fmt.Errorf("pty start: %w", err)
	}

	s := &Session{
		ID:      id,
		Command: shellPath,
		ptmx:    ptmx,
		cmd:     cmd,
		done:    make(chan struct{}),
	}
	return s, nil
}

// Write sends input bytes to the PTY master.
func (s *Session) Write(data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.ptmx.Write(data)
	return err
}

// Resize changes the PTY window size.
func (s *Session) Resize(cols, rows uint16) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return pty.Setsize(s.ptmx, &pty.Winsize{Cols: cols, Rows: rows})
}

// ReadOutput reads output from the PTY into buf. Blocks until data is available.
// Returns (0, io.EOF) when the process has exited and the PTY is closed.
func (s *Session) ReadOutput(buf []byte) (int, error) {
	return s.ptmx.Read(buf)
}

// Wait waits for the process to exit and returns its exit code.
func (s *Session) Wait() (int, error) {
	err := s.cmd.Wait()
	close(s.done)
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode(), nil
		}
		return -1, err
	}
	return 0, nil
}

// Kill sends SIGKILL to the session's process so it exits promptly.
// Call Close afterwards to release the PTY master.
func (s *Session) Kill() {
	if s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
}

// Close closes the PTY master file descriptor, signalling EOF to readers.
func (s *Session) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.ptmx.Close()
}

// Done returns a channel that is closed when Wait returns.
func (s *Session) Done() <-chan struct{} {
	return s.done
}
