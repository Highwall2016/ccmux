// ccmux-ctl — local control CLI for the ccmux desktop agent.
//
// Usage:
//
//	ccmux-ctl spawn [--id UUID] [--cols N] [--rows N] [COMMAND...]
//	ccmux-ctl kill SESSION_ID
//	ccmux-ctl list
//	ccmux-ctl attach SESSION_ID
//
// It communicates with a running ccmux-agent via its Unix domain socket
// (default: /tmp/ccmux.sock, override with CCMUX_IPC_SOCKET).
package main

import (
	"crypto/rand"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/ccmux/agent/internal/ipc"
	"golang.org/x/term"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	socketPath := os.Getenv("CCMUX_IPC_SOCKET")
	if socketPath == "" {
		socketPath = "/tmp/ccmux.sock"
	}

	switch os.Args[1] {
	case "spawn":
		runSpawn(socketPath, os.Args[2:])
	case "kill":
		runKill(socketPath, os.Args[2:])
	case "list":
		runList(socketPath)
	case "attach":
		runAttach(socketPath, os.Args[2:])
	case "rename":
		runRename(socketPath, os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		usage()
		os.Exit(1)
	}
}

func runSpawn(socketPath string, args []string) {
	fs := flag.NewFlagSet("spawn", flag.ExitOnError)
	id := fs.String("id", "", "session UUID (generated if empty)")
	cols := fs.Uint("cols", 0, "terminal width (auto-detected if 0)")
	rows := fs.Uint("rows", 0, "terminal height (auto-detected if 0)")
	_ = fs.Parse(args)

	if *id == "" {
		var err error
		*id, err = newUUID()
		if err != nil {
			fatalf("generate UUID: %v", err)
		}
	}

	// Auto-detect terminal size.
	if *cols == 0 || *rows == 0 {
		w, h, err := term.GetSize(int(os.Stdout.Fd()))
		if err != nil {
			w, h = 80, 24
		}
		if *cols == 0 {
			*cols = uint(w)
		}
		if *rows == 0 {
			*rows = uint(h)
		}
	}

	command := strings.Join(fs.Args(), " ")

	req := ipc.SpawnRequest{
		Cmd:       "spawn",
		SessionID: *id,
		Command:   command,
		Cols:      uint16(*cols),
		Rows:      uint16(*rows),
	}

	var resp ipc.Response
	if err := send(socketPath, req, &resp); err != nil {
		fatalf("ipc: %v", err)
	}
	if !resp.OK {
		fatalf("spawn failed: %s", resp.Error)
	}
	fmt.Println(*id)
}

func runKill(socketPath string, args []string) {
	if len(args) == 0 {
		fatalf("usage: ccmux-ctl kill SESSION_ID")
	}
	req := ipc.KillRequest{Cmd: "kill", SessionID: args[0]}
	var resp ipc.Response
	if err := send(socketPath, req, &resp); err != nil {
		fatalf("ipc: %v", err)
	}
	if !resp.OK {
		fatalf("kill failed: %s", resp.Error)
	}
	fmt.Println("killed", args[0])
}

func runList(socketPath string) {
	req := ipc.ListRequest{Cmd: "list"}
	var resp ipc.Response
	if err := send(socketPath, req, &resp); err != nil {
		fatalf("ipc: %v", err)
	}
	if !resp.OK {
		fatalf("list failed: %s", resp.Error)
	}
	if len(resp.Sessions) == 0 {
		fmt.Println("(no active sessions)")
		return
	}
	for _, s := range resp.Sessions {
		fmt.Println(s)
	}
}

func runRename(socketPath string, args []string) {
	if len(args) < 2 {
		fatalf("usage: ccmux-ctl rename SESSION_ID NAME")
	}
	req := ipc.RenameRequest{Cmd: "rename", SessionID: args[0], Name: args[1]}
	var resp ipc.Response
	if err := send(socketPath, req, &resp); err != nil {
		fatalf("ipc: %v", err)
	}
	if !resp.OK {
		fatalf("rename failed: %s", resp.Error)
	}
	fmt.Printf("renamed %s → %s\n", args[0], args[1])
}

func runAttach(socketPath string, args []string) {
	if len(args) == 0 {
		fatalf("usage: ccmux-ctl attach SESSION_ID")
	}
	sessionID := args[0]

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		fatalf("connect to %s: %v\n(is ccmux-agent running?)", socketPath, err)
	}
	defer conn.Close()

	// Send attach request and read the initial JSON response.
	req := ipc.AttachRequest{Cmd: "attach", SessionID: sessionID}
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		fatalf("send: %v", err)
	}
	var resp ipc.Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		fatalf("read response: %v", err)
	}
	if !resp.OK {
		fatalf("attach failed: %s", resp.Error)
	}

	// Put local terminal in raw mode so keystrokes go straight to the PTY.
	fd := int(os.Stdin.Fd())
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		fatalf("raw mode: %v", err)
	}
	defer term.Restore(fd, oldState)

	// Print detach hint on a new line (raw mode means \n doesn't scroll).
	fmt.Fprintf(os.Stderr, "\r\n[ccmux] attached to %s — detach with Ctrl-\\ \r\n", sessionID[:8])

	// Handle SIGWINCH: resize the remote PTY when the local terminal resizes.
	winchCh := make(chan os.Signal, 1)
	signal.Notify(winchCh, syscall.SIGWINCH)
	go func() {
		for range winchCh {
			w, h, err := term.GetSize(fd)
			if err != nil {
				continue
			}
			sendResizeIPC(socketPath, sessionID, uint16(w), uint16(h))
		}
	}()
	defer signal.Stop(winchCh)

	// Send initial size immediately.
	if w, h, err := term.GetSize(fd); err == nil {
		sendResizeIPC(socketPath, sessionID, uint16(w), uint16(h))
	}

	// Handle Ctrl-\ (FS, 0x1c) as detach key.
	stdinProxy := &detachReader{r: os.Stdin}

	done := make(chan struct{}, 2)
	// PTY output → local stdout.
	go func() {
		_, _ = io.Copy(os.Stdout, conn)
		done <- struct{}{}
	}()
	// Send a carriage return so the shell reprints its prompt immediately on attach.
	_, _ = conn.Write([]byte("\r"))
	// Local stdin → PTY.
	go func() {
		_, _ = io.Copy(conn, stdinProxy)
		done <- struct{}{}
	}()

	<-done
	signal.Stop(winchCh)
	fmt.Fprintf(os.Stderr, "\r\n[ccmux] detached\r\n")
}

// detachReader wraps stdin and returns io.EOF on Ctrl-\ (0x1c).
type detachReader struct {
	r io.Reader
}

func (d *detachReader) Read(p []byte) (int, error) {
	n, err := d.r.Read(p)
	for i := 0; i < n; i++ {
		if p[i] == 0x1c { // Ctrl-\
			return i, io.EOF
		}
	}
	return n, err
}

// sendResizeIPC sends a resize command over a fresh IPC connection.
func sendResizeIPC(socketPath, sessionID string, cols, rows uint16) {
	req := ipc.ResizeRequest{
		Cmd:       "resize",
		SessionID: sessionID,
		Cols:      cols,
		Rows:      rows,
	}
	var resp ipc.Response
	_ = send(socketPath, req, &resp) // best-effort; ignore errors
}

// send connects to the Unix socket, sends req as JSON, and decodes the response.
func send(socketPath string, req any, resp *ipc.Response) error {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return fmt.Errorf("connect to %s: %w\n(is ccmux-agent running?)", socketPath, err)
	}
	defer conn.Close()

	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return fmt.Errorf("encode request: %w", err)
	}
	return json.NewDecoder(conn).Decode(resp)
}

func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(1)
}

func usage() {
	fmt.Fprintln(os.Stderr, `ccmux-ctl — control ccmux-agent sessions

Usage:
  ccmux-ctl spawn [--id UUID] [--cols N] [--rows N] [COMMAND...]
  ccmux-ctl kill SESSION_ID
  ccmux-ctl list
  ccmux-ctl attach SESSION_ID
  ccmux-ctl rename SESSION_ID NAME

Environment:
  CCMUX_IPC_SOCKET  Unix socket path (default: /tmp/ccmux.sock)`)
}
