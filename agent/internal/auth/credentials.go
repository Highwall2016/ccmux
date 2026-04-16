// Package auth manages local credential storage for the ccmux CLI.
package auth

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Credentials holds user and device authentication data persisted locally.
type Credentials struct {
	ServerURL    string `json:"server_url"`    // HTTP base URL, e.g. http://host:8080
	UserID       string `json:"user_id"`
	Email        string `json:"email"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	DeviceID     string `json:"device_id"`
	DeviceToken  string `json:"device_token"`
}

// CredentialsPath returns the path to ~/.ccmux/credentials.json.
func CredentialsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".ccmux", "credentials.json"), nil
}

// LoadCredentials reads credentials from ~/.ccmux/credentials.json.
func LoadCredentials() (*Credentials, error) {
	path, err := CredentialsPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Credentials
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

// SaveCredentials writes credentials to ~/.ccmux/credentials.json (mode 0600).
func SaveCredentials(c *Credentials) error {
	path, err := CredentialsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

// IsLoggedIn returns true if valid credentials are stored locally.
func IsLoggedIn() bool {
	c, err := LoadCredentials()
	return err == nil && c.Email != "" && c.DeviceID != ""
}
