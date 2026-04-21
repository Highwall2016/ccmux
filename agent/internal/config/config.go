// Package config loads agent configuration from environment variables,
// falling back to ~/.ccmux/credentials.json when env vars are absent.
package config

import (
	"fmt"
	"os"
	"os/user"
	"time"

	"github.com/ccmux/agent/internal/auth"
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
	// TmuxWatchInterval is how often the agent polls for new/gone tmux panes.
	// Set to 0 to disable tmux auto-discovery.
	TmuxWatchInterval time.Duration
}

// Load reads configuration from environment variables first, then falls back
// to ~/.ccmux/credentials.json for any values that are still missing.
func Load() (*Config, error) {
	var watchInterval time.Duration
	if s := os.Getenv("CCMUX_TMUX_WATCH"); s != "" {
		watchInterval, _ = time.ParseDuration(s)
	}
	cfg := &Config{
		ServerURL:         os.Getenv("CCMUX_SERVER_URL"),
		DeviceID:          os.Getenv("CCMUX_DEVICE_ID"),
		DeviceToken:       os.Getenv("CCMUX_DEVICE_TOKEN"),
		IPCSocket:         os.Getenv("CCMUX_IPC_SOCKET"),
		DefaultShell:      os.Getenv("CCMUX_SHELL"),
		TmuxWatchInterval: watchInterval,
	}

	// Fill missing values from the credentials file.
	if cfg.ServerURL == "" || cfg.DeviceID == "" || cfg.DeviceToken == "" {
		creds, err := auth.LoadCredentials()
		if err == nil {
			if cfg.ServerURL == "" && creds.ServerURL != "" {
				cfg.ServerURL = auth.HTTPToWS(creds.ServerURL)
			}
			if cfg.DeviceID == "" {
				cfg.DeviceID = creds.DeviceID
			}
			if cfg.DeviceToken == "" {
				cfg.DeviceToken = creds.DeviceToken
			}
		}
	}

	if cfg.ServerURL == "" {
		return nil, fmt.Errorf("server URL not set — run `ccmux auth login` or set CCMUX_SERVER_URL")
	}
	if cfg.DeviceID == "" {
		return nil, fmt.Errorf("device ID not set — run `ccmux auth login` or set CCMUX_DEVICE_ID")
	}
	if cfg.DeviceToken == "" {
		return nil, fmt.Errorf("device token not set — run `ccmux auth login` or set CCMUX_DEVICE_TOKEN")
	}
	if cfg.IPCSocket == "" {
		cfg.IPCSocket = "/tmp/ccmux.sock"
	}
	if cfg.DefaultShell == "" {
		cfg.DefaultShell = loginShell()
	}
	if cfg.TmuxWatchInterval == 0 {
		cfg.TmuxWatchInterval = 5 * time.Second
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
