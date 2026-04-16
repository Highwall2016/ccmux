// ccmux — local control CLI for the ccmux desktop agent.
//
// Usage:
//
//	ccmux auth login
//	ccmux auth status
//	ccmux new [--id UUID] [--cols N] [--rows N] [--patterns P] [COMMAND...]
//	ccmux kill NAME|UUID
//	ccmux list
//	ccmux attach NAME|UUID
//	ccmux rename NAME|UUID NAME
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

	"github.com/ccmux/agent/internal/auth"
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
	case "auth":
		runAuth(os.Args[2:])
	case "new":
		requireAuth()
		runSpawn(socketPath, os.Args[2:])
	case "kill":
		requireAuth()
		runKill(socketPath, os.Args[2:])
	case "list":
		requireAuth()
		runList(socketPath)
	case "attach":
		requireAuth()
		runAttach(socketPath, os.Args[2:])
	case "rename":
		requireAuth()
		runRename(socketPath, os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		usage()
		os.Exit(1)
	}
}

func runAuth(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: ccmux auth <login|status>")
		os.Exit(1)
	}
	switch args[0] {
	case "login":
		creds, err := auth.Login()
		if err != nil {
			fatalf("login failed: %v", err)
		}
		if err := auth.SaveCredentials(creds); err != nil {
			fatalf("save credentials: %v", err)
		}
		fmt.Printf("Logged in as %s\n", creds.Email)
	case "status":
		creds, err := auth.LoadCredentials()
		if err != nil {
			fmt.Println("Not logged in. Run `ccmux auth login` to authenticate.")
			os.Exit(1)
		}
		fmt.Printf("Logged in as %s\n", creds.Email)
		fmt.Printf("Server:    %s\n", creds.ServerURL)
		fmt.Printf("User ID:   %s\n", creds.UserID)
		fmt.Printf("Device ID: %s\n", creds.DeviceID)
	default:
		fatalf("unknown auth command: %s", args[0])
	}
}

func requireAuth() {
	if !auth.IsLoggedIn() {
		fatalf("not logged in — run `ccmux auth login` first")
	}
}

func runSpawn(socketPath string, args []string) {
	fs := flag.NewFlagSet("new", flag.ExitOnError)
	name := fs.String("name", "", "human-readable display name for the session")
	cols := fs.Uint("cols", 0, "terminal width (auto-detected if 0)")
	rows := fs.Uint("rows", 0, "terminal height (auto-detected if 0)")
	patterns := fs.String("patterns", "", "comma-separated extra alert patterns (e.g. \"esc to cancel,do you want\")")
	_ = fs.Parse(args)

	sessionID, err := newUUID()
	if err != nil {
		fatalf("generate UUID: %v", err)
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
	if command == "" {
		command = "bash"
	}

	var alertPatterns []string
	if *patterns != "" {
		for _, p := range strings.Split(*patterns, ",") {
			if p = strings.TrimSpace(p); p != "" {
				alertPatterns = append(alertPatterns, p)
			}
		}
	}

	req := ipc.SpawnRequest{
		Cmd:           "spawn",
		SessionID:     sessionID,
		Name:          *name,
		Command:       command,
		Cols:          uint16(*cols),
		Rows:          uint16(*rows),
		AlertPatterns: alertPatterns,
	}

	var resp ipc.Response
	if err := send(socketPath, req, &resp); err != nil {
		fatalf("ipc: %v", err)
	}
	if !resp.OK {
		fatalf("spawn failed: %s", resp.Error)
	}
	fmt.Println(sessionID)
}

func runKill(socketPath string, args []string) {
	if len(args) == 0 {
		fatalf("usage: ccmux kill NAME|UUID")
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
		fmt.Printf("%s (%s)\n", s.Name, s.ID)
	}
}

func runRename(socketPath string, args []string) {
	if len(args) < 2 {
		fatalf("usage: ccmux rename NAME|UUID NAME")
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
		fatalf("usage: ccmux attach NAME|UUID")
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
	fmt.Fprintf(os.Stderr, "\r\n[ccmux] attached to %s — detach with Ctrl-\\ \r\n", shortID(sessionID))

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

func shortID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(1)
}

func usage() {
	fmt.Fprintln(os.Stderr, `ccmux — control ccmux-agent sessions

Usage:
  ccmux auth login
  ccmux auth status
  ccmux new [--name NAME] [--cols N] [--rows N] [--patterns P1,P2] [COMMAND...]
  ccmux kill NAME|UUID
  ccmux list
  ccmux attach NAME|UUID
  ccmux rename NAME|UUID NAME

Flags (new):
  --name     display name shown in the mobile app and used with kill/attach/rename
             (auto-assigned as 0, 1, 2, … when omitted)
  --cols     terminal width  (auto-detected from current terminal)
  --rows     terminal height (auto-detected from current terminal)
  --patterns comma-separated extra alert patterns; defaults already include
             "error", "failed", "panic", "fatal", "esc to cancel",
             "do you want", "would you like", "are you sure"

  COMMAND defaults to bash when omitted.

Environment:
  CCMUX_IPC_SOCKET  Unix socket path (default: /tmp/ccmux.sock)`)
}
