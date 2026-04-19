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
	"bytes"
	"crypto/rand"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/ccmux/agent/internal/auth"
	"github.com/ccmux/agent/internal/ipc"
	"github.com/ccmux/backend/pkg/protocol"
	"github.com/gorilla/websocket"
	"github.com/vmihailenco/msgpack/v5"
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
		runList(socketPath, os.Args[2:])
	case "attach":
		requireAuth()
		runAttach(socketPath, os.Args[2:])
	case "rename":
		requireAuth()
		runRename(socketPath, os.Args[2:])
	case "device":
		requireAuth()
		runDevice(os.Args[2:])
	case "help", "--help", "-h":
		usage()
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
		fmt.Print("Starting ccmux-agent... ")
		if err := startAgent(); err != nil {
			fmt.Printf("warning: %v\n(start it manually with ccmux-agent)\n", err)
		} else {
			fmt.Println("ready.")
		}
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
	deviceFlag := fs.String("device", "", "remote device name or ID to spawn on")
	_ = fs.Parse(args)

	if *deviceFlag != "" {
		runRemoteSpawn(*deviceFlag, *name, *cols, *rows, *patterns, strings.Join(fs.Args(), " "))
		return
	}

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

func runList(socketPath string, args []string) {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	deviceFlag := fs.String("device", "", "remote device name or ID")
	allFlag := fs.Bool("all", false, "list sessions on all devices")
	_ = fs.Parse(args)

	if *deviceFlag != "" {
		runRemoteList(socketPath, *deviceFlag, false)
		return
	}
	if *allFlag {
		runRemoteList(socketPath, "", true)
		return
	}

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
	fs := flag.NewFlagSet("attach", flag.ExitOnError)
	deviceFlag := fs.String("device", "", "remote device name or ID")
	_ = fs.Parse(args)

	if fs.NArg() == 0 {
		fatalf("usage: ccmux attach [--device NAME|ID] NAME|UUID")
	}
	sessionID := fs.Args()[0]

	if *deviceFlag != "" {
		runRemoteAttach(*deviceFlag, sessionID)
		return
	}

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

func startAgent() error {
	// If the IPC socket already exists, agent is already running.
	socketPath := os.Getenv("CCMUX_IPC_SOCKET")
	if socketPath == "" {
		socketPath = "/tmp/ccmux.sock"
	}
	if _, err := os.Stat(socketPath); err == nil {
		return nil
	}

	// Find ccmux-agent next to the current binary.
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find executable path: %w", err)
	}
	agentBin := filepath.Join(filepath.Dir(exe), "ccmux-agent")
	if _, err := os.Stat(agentBin); err != nil {
		return fmt.Errorf("ccmux-agent not found at %s", agentBin)
	}

	// Open a log file at ~/.ccmux/agent.log.
	home, _ := os.UserHomeDir()
	logPath := filepath.Join(home, ".ccmux", "agent.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		logFile = nil
	}

	cmd := exec.Command(agentBin)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} // detach from terminal
	if logFile != nil {
		cmd.Stdout = logFile
		cmd.Stderr = logFile
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start agent: %w", err)
	}

	// Wait up to 3 seconds for the IPC socket to appear.
	for i := 0; i < 30; i++ {
		time.Sleep(100 * time.Millisecond)
		if _, err := os.Stat(socketPath); err == nil {
			return nil
		}
	}
	return fmt.Errorf("agent started (pid %d) but IPC socket not ready yet", cmd.Process.Pid)
}

func runDevice(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: ccmux device <ls|rm NAME|ID>")
		os.Exit(1)
	}
	switch args[0] {
	case "ls", "list":
		creds, err := auth.LoadCredentials()
		if err != nil {
			fatalf("load credentials: %v", err)
		}
		var devices []remoteDevice
		if err := apiGet(creds, "/api/devices", &devices); err != nil {
			fatalf("%v", err)
		}
		if len(devices) == 0 {
			fmt.Println("(no online devices)")
			return
		}
		const (
			green  = "\033[32m"
			yellow = "\033[33m"
			reset  = "\033[0m"
		)
		for _, d := range devices {
			if d.Online {
				fmt.Printf("%-36s  [%s]  %-8s  %sonline%s\n", d.Name, shortID(d.ID), d.Platform, green, reset)
			} else {
				fmt.Printf("%-36s  [%s]  %-8s  %soffline%s\n", d.Name, shortID(d.ID), d.Platform, yellow, reset)
			}
		}
	case "rm", "remove":
		if len(args) < 2 {
			fatalf("usage: ccmux device rm NAME|ID")
		}
		creds, err := auth.LoadCredentials()
		if err != nil {
			fatalf("load credentials: %v", err)
		}
		dev, err := resolveDevice(creds, args[1])
		if err != nil {
			fatalf("%v", err)
		}
		if err := apiDelete(creds, "/api/devices/"+dev.ID); err != nil {
			fatalf("remove device: %v", err)
		}
		fmt.Printf("removed %s\n", dev.Name)
	default:
		fatalf("unknown device command: %s", args[0])
	}
}

// ── Remote (multi-device) helpers ────────────────────────────────────────────

type remoteDevice struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Platform string `json:"platform"`
	Online   bool   `json:"online"`
}

type remoteSession struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Command string `json:"command"`
	Status  string `json:"status"`
}

// tryRefresh attempts to get a new access token using the stored refresh token.
// On success it updates creds in-place and persists to disk.
func tryRefresh(creds *auth.Credentials) error {
	client := &http.Client{Timeout: 10 * time.Second}
	body, _ := json.Marshal(map[string]string{"refresh_token": creds.RefreshToken})
	resp, err := client.Post(creds.ServerURL+"/api/auth/refresh", "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("refresh failed")
	}
	var result struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}
	creds.AccessToken = result.AccessToken
	return auth.SaveCredentials(creds)
}

func apiGet(creds *auth.Credentials, path string, v any) error {
	client := &http.Client{Timeout: 10 * time.Second}
	doReq := func() (*http.Response, error) {
		req, err := http.NewRequest(http.MethodGet, creds.ServerURL+path, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+creds.AccessToken)
		return client.Do(req)
	}
	resp, err := doReq()
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	if resp.StatusCode == http.StatusUnauthorized {
		resp.Body.Close()
		if refreshErr := tryRefresh(creds); refreshErr != nil {
			return fmt.Errorf("session expired — run `ccmux auth login`")
		}
		resp, err = doReq()
		if err != nil {
			return fmt.Errorf("request failed: %w", err)
		}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

func apiDelete(creds *auth.Credentials, path string) error {
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest(http.MethodDelete, creds.ServerURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+creds.AccessToken)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("session expired — run `ccmux auth login`")
	}
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("server returned %d", resp.StatusCode)
	}
	return nil
}

func apiPost(creds *auth.Credentials, path string, body any, v any, expectedStatus int) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest(http.MethodPost, creds.ServerURL+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+creds.AccessToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("session expired — run `ccmux auth login`")
	}
	if resp.StatusCode != expectedStatus {
		return fmt.Errorf("server returned %d", resp.StatusCode)
	}
	if v != nil {
		return json.NewDecoder(resp.Body).Decode(v)
	}
	return nil
}

// resolveDevice finds a device by exact/prefix ID or name match.
func resolveDevice(creds *auth.Credentials, nameOrID string) (*remoteDevice, error) {
	var devices []remoteDevice
	if err := apiGet(creds, "/api/devices", &devices); err != nil {
		return nil, fmt.Errorf("list devices: %w", err)
	}
	lower := strings.ToLower(nameOrID)
	// Exact ID, then ID prefix, then exact name, then name prefix.
	for _, pass := range []func(d remoteDevice) bool{
		func(d remoteDevice) bool { return d.ID == nameOrID },
		func(d remoteDevice) bool { return strings.HasPrefix(d.ID, nameOrID) },
		func(d remoteDevice) bool { return strings.EqualFold(d.Name, nameOrID) },
		func(d remoteDevice) bool { return strings.HasPrefix(strings.ToLower(d.Name), lower) },
	} {
		for _, d := range devices {
			if pass(d) {
				return &d, nil
			}
		}
	}
	return nil, fmt.Errorf("device %q not found", nameOrID)
}

// resolveRemoteSession resolves a session name or ID on a remote device.
func resolveRemoteSession(creds *auth.Credentials, deviceID, nameOrID string) (string, error) {
	var sessions []remoteSession
	if err := apiGet(creds, "/api/devices/"+deviceID+"/sessions", &sessions); err != nil {
		return "", fmt.Errorf("list sessions: %w", err)
	}
	for _, s := range sessions {
		if s.ID == nameOrID || strings.HasPrefix(s.ID, nameOrID) || strings.EqualFold(s.Name, nameOrID) {
			return s.ID, nil
		}
	}
	return "", fmt.Errorf("session %q not found", nameOrID)
}

func runRemoteList(socketPath, deviceFlag string, all bool) {
	const (
		green  = "\033[32m"
		yellow = "\033[33m"
		reset  = "\033[0m"
	)

	creds, err := auth.LoadCredentials()
	if err != nil {
		fatalf("load credentials: %v", err)
	}

	found := false

	// When --all, always show local device first via IPC (reliable, no timing issues).
	if all {
		req := ipc.ListRequest{Cmd: "list"}
		var resp ipc.Response
		if ipcErr := send(socketPath, req, &resp); ipcErr == nil && resp.OK {
			hostname, _ := os.Hostname()
			fmt.Printf("device  %-36s  [local]  %sonline%s\n", hostname, green, reset)
			if len(resp.Sessions) == 0 {
				fmt.Printf("  %s(no active sessions)%s\n", yellow, reset)
			} else {
				for _, s := range resp.Sessions {
					fmt.Printf("  %-12s  %s  %sactive%s\n", s.Name, shortID(s.ID), green, reset)
				}
			}
			found = true
		}
	}

	// Fetch remote devices from backend, skipping the local device.
	var devices []remoteDevice
	if apiErr := apiGet(creds, "/api/devices", &devices); apiErr != nil {
		fmt.Fprintf(os.Stderr, "warning: could not fetch devices from server: %v\n", apiErr)
	} else {
		if !all && deviceFlag != "" {
			dev, resolveErr := resolveDevice(creds, deviceFlag)
			if resolveErr != nil {
				fatalf("%v", resolveErr)
			}
			devices = []remoteDevice{*dev}
		}
	}
	for _, d := range devices {
		if d.ID == creds.DeviceID {
			continue // already shown via IPC above
		}
		found = true
		if !d.Online {
			fmt.Printf("device  %-36s  [%s]  %soffline%s\n", d.Name, shortID(d.ID), yellow, reset)
			continue
		}
		var sessions []remoteSession
		if err := apiGet(creds, "/api/devices/"+d.ID+"/sessions", &sessions); err != nil {
			fmt.Printf("device  %-36s  [%s]  %sonline%s\n", d.Name, shortID(d.ID), green, reset)
			fmt.Printf("  %s(could not fetch sessions: %v)%s\n", yellow, err, reset)
			continue
		}
		fmt.Printf("device  %-36s  [%s]  %sonline%s\n", d.Name, shortID(d.ID), green, reset)
		if len(sessions) == 0 {
			fmt.Printf("  %s(no active sessions)%s\n", yellow, reset)
			continue
		}
		for _, s := range sessions {
			name := s.Name
			if name == "" {
				name = "-"
			}
			fmt.Printf("  %-12s  %s  %-16s  %sactive%s\n", name, shortID(s.ID), s.Command, green, reset)
		}
	}

	if !found {
		fmt.Println("(no active sessions on any online device)")
	}
}

func runRemoteSpawn(deviceFlag, name string, cols, rows uint, patterns, command string) {
	creds, err := auth.LoadCredentials()
	if err != nil {
		fatalf("load credentials: %v", err)
	}
	dev, err := resolveDevice(creds, deviceFlag)
	if err != nil {
		fatalf("%v", err)
	}

	if cols == 0 || rows == 0 {
		w, h, err := term.GetSize(int(os.Stdout.Fd()))
		if err != nil {
			w, h = 80, 24
		}
		if cols == 0 {
			cols = uint(w)
		}
		if rows == 0 {
			rows = uint(h)
		}
	}
	if command == "" {
		command = "bash"
	}

	var alertPatterns []string
	if patterns != "" {
		for _, p := range strings.Split(patterns, ",") {
			if p = strings.TrimSpace(p); p != "" {
				alertPatterns = append(alertPatterns, p)
			}
		}
	}

	body := map[string]any{
		"name":          name,
		"command":       command,
		"cols":          cols,
		"rows":          rows,
		"alert_patterns": alertPatterns,
	}
	var result struct {
		SessionID string `json:"session_id"`
	}
	if err := apiPost(creds, "/api/devices/"+dev.ID+"/sessions", body, &result, http.StatusAccepted); err != nil {
		fatalf("spawn on %s: %v", dev.Name, err)
	}
	fmt.Println(result.SessionID)
}

func runRemoteAttach(deviceFlag, sessionNameOrID string) {
	creds, err := auth.LoadCredentials()
	if err != nil {
		fatalf("load credentials: %v", err)
	}
	dev, err := resolveDevice(creds, deviceFlag)
	if err != nil {
		fatalf("%v", err)
	}
	sessionID, err := resolveRemoteSession(creds, dev.ID, sessionNameOrID)
	if err != nil {
		fatalf("%v on device %s", err, dev.Name)
	}

	wsURL := auth.HTTPToWS(creds.ServerURL) + "/ws/client"
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		fatalf("connect to server: %v", err)
	}
	defer ws.Close()

	// Authenticate with JWT.
	authPkt, _ := (&protocol.Packet{
		Type:    protocol.TypeAuth,
		Payload: []byte(creds.AccessToken),
	}).Encode()
	if err := ws.WriteMessage(websocket.BinaryMessage, authPkt); err != nil {
		fatalf("send auth: %v", err)
	}
	ws.SetReadDeadline(time.Now().Add(15 * time.Second))
	_, raw, err := ws.ReadMessage()
	if err != nil {
		fatalf("read auth response: %v", err)
	}
	ws.SetReadDeadline(time.Time{})
	pkt, err := protocol.Decode(raw)
	if err != nil || pkt.Type != protocol.TypeAuthOK {
		fatalf("authentication failed")
	}

	// Subscribe to the session.
	subPayload, _ := msgpack.Marshal(&protocol.SubscribePayload{SessionID: sessionID})
	subPkt, _ := (&protocol.Packet{
		Type:    protocol.TypeSubscribe,
		Session: sessionID,
		Payload: subPayload,
	}).Encode()
	if err := ws.WriteMessage(websocket.BinaryMessage, subPkt); err != nil {
		fatalf("subscribe: %v", err)
	}

	// Put terminal in raw mode.
	fd := int(os.Stdin.Fd())
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		fatalf("raw mode: %v", err)
	}
	defer term.Restore(fd, oldState)

	fmt.Fprintf(os.Stderr, "\r\n[ccmux] attached to %s/%s — detach with Ctrl-\\\r\n", dev.Name, shortID(sessionID))

	// Handle SIGWINCH: send TypeResize to backend.
	winchCh := make(chan os.Signal, 1)
	signal.Notify(winchCh, syscall.SIGWINCH)
	go func() {
		for range winchCh {
			w, h, err := term.GetSize(fd)
			if err != nil {
				continue
			}
			rp, _ := msgpack.Marshal(&protocol.ResizePayload{Cols: uint16(w), Rows: uint16(h)})
			resizePkt, _ := (&protocol.Packet{
				Type:    protocol.TypeResize,
				Session: sessionID,
				Payload: rp,
			}).Encode()
			_ = ws.WriteMessage(websocket.BinaryMessage, resizePkt)
		}
	}()
	defer signal.Stop(winchCh)

	// Send initial size.
	if w, h, err := term.GetSize(fd); err == nil {
		rp, _ := msgpack.Marshal(&protocol.ResizePayload{Cols: uint16(w), Rows: uint16(h)})
		resizePkt, _ := (&protocol.Packet{
			Type:    protocol.TypeResize,
			Session: sessionID,
			Payload: rp,
		}).Encode()
		_ = ws.WriteMessage(websocket.BinaryMessage, resizePkt)
	}

	done := make(chan struct{}, 2)

	// WS → stdout.
	go func() {
		defer func() { done <- struct{}{} }()
		for {
			_, raw, err := ws.ReadMessage()
			if err != nil {
				return
			}
			p, err := protocol.Decode(raw)
			if err != nil {
				continue
			}
			switch p.Type {
			case protocol.TypeTerminalOutput, protocol.TypeScrollback:
				_, _ = os.Stdout.Write(p.Payload)
			case protocol.TypeScrollbackDone:
				// live stream starts now
			case protocol.TypeSessionStatus:
				var sp protocol.SessionStatusPayload
				if err := msgpack.Unmarshal(p.Payload, &sp); err == nil {
					if sp.Status == "exited" || sp.Status == "killed" {
						return
					}
				}
			}
		}
	}()

	// stdin → WS.
	stdinProxy := &detachReader{r: os.Stdin}
	go func() {
		defer func() { done <- struct{}{} }()
		buf := make([]byte, 4096)
		for {
			n, err := stdinProxy.Read(buf)
			if n > 0 {
				inputPkt, _ := (&protocol.Packet{
					Type:    protocol.TypeTerminalInput,
					Session: sessionID,
					Payload: append([]byte(nil), buf[:n]...),
				}).Encode()
				if werr := ws.WriteMessage(websocket.BinaryMessage, inputPkt); werr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	<-done
	signal.Stop(winchCh)
	fmt.Fprintf(os.Stderr, "\r\n[ccmux] detached\r\n")
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
  ccmux new [--name NAME] [--cols N] [--rows N] [--patterns P1,P2] [--device D] [COMMAND...]
  ccmux kill NAME|UUID
  ccmux list [--device D | --all]
  ccmux attach [--device D] NAME|UUID
  ccmux rename NAME|UUID NAME
  ccmux device ls
  ccmux device rm NAME|ID

Flags (new):
  --name     display name shown in the mobile app and used with kill/attach/rename
             (auto-assigned as 0, 1, 2, … when omitted)
  --cols     terminal width  (auto-detected from current terminal)
  --rows     terminal height (auto-detected from current terminal)
  --patterns comma-separated extra alert patterns; defaults already include
             "error", "failed", "panic", "fatal", "esc to cancel",
             "do you want", "would you like", "are you sure"
  --device   remote device name or ID prefix; routes the command to another
             computer on the same account instead of the local agent

Flags (list):
  --device   show sessions only on the named device
  --all      show sessions on all devices in your account

  COMMAND defaults to bash when omitted.

Environment:
  CCMUX_IPC_SOCKET  Unix socket path (default: /tmp/ccmux.sock)`)
}
