// Package config loads agent configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"os/user"
)

// Config holds all agent runtime configuration.
type Config struct {
	// ServerURL is the WebSocket URL of the ccmux backend (ws:// or wss://).
	ServerURL string
	// DeviceID is the UUID of this device as registered in the backend.
	DeviceID string
	// DeviceToken is the raw HMAC token for this device (sent once at registration).
	DeviceToken string
	// IPCSocket is the path to the Unix domain socket for local IPC.
	IPCSocket string
	// DefaultShell is the shell to use when spawning sessions.
	DefaultShell string
}

// Load reads configuration from environment variables.
func Load() (*Config, error) {
	cfg := &Config{
		ServerURL:   os.Getenv("CCMUX_SERVER_URL"),
		DeviceID:    os.Getenv("CCMUX_DEVICE_ID"),
		DeviceToken: os.Getenv("CCMUX_DEVICE_TOKEN"),
		IPCSocket:   os.Getenv("CCMUX_IPC_SOCKET"),
		DefaultShell: os.Getenv("CCMUX_SHELL"),
	}

	if cfg.ServerURL == "" {
		return nil, fmt.Errorf("CCMUX_SERVER_URL is required")
	}
	if cfg.DeviceID == "" {
		return nil, fmt.Errorf("CCMUX_DEVICE_ID is required")
	}
	if cfg.DeviceToken == "" {
		return nil, fmt.Errorf("CCMUX_DEVICE_TOKEN is required")
	}
	if cfg.IPCSocket == "" {
		cfg.IPCSocket = "/tmp/ccmux.sock"
	}
	if cfg.DefaultShell == "" {
		cfg.DefaultShell = loginShell()
	}
	return cfg, nil
}

// loginShell returns the current user's login shell, falling back to /bin/sh.
func loginShell() string {
	if shell := os.Getenv("SHELL"); shell != "" {
		return shell
	}
	u, err := user.Current()
	if err != nil {
		return "/bin/sh"
	}
	_ = u
	return "/bin/sh"
}
