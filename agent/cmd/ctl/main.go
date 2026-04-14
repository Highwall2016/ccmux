// ccmux-ctl — local control CLI for the ccmux desktop agent.
//
// Usage:
//
//	ccmux-ctl spawn [--id UUID] [--cols N] [--rows N] [COMMAND...]
//	ccmux-ctl kill SESSION_ID
//	ccmux-ctl list
//
// It communicates with a running ccmux-agent via its Unix domain socket
// (default: /tmp/ccmux.sock, override with CCMUX_IPC_SOCKET).
package main

import (
	"crypto/rand"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"strings"

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

Environment:
  CCMUX_IPC_SOCKET  Unix socket path (default: /tmp/ccmux.sock)`)
}
