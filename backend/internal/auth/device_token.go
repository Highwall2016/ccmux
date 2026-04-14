package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// GenerateDeviceToken generates a cryptographically random 32-byte token.
// Returns the raw hex string (given to the caller once) and its HMAC-SHA256 hash.
func GenerateDeviceToken(hmacSecret string) (raw, hashed string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", "", fmt.Errorf("generate device token: %w", err)
	}
	raw = hex.EncodeToString(b)
	hashed = HashDeviceToken(raw, hmacSecret)
	return raw, hashed, nil
}

// HashDeviceToken returns HMAC-SHA256(raw, secret) as a hex string.
func HashDeviceToken(raw, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(raw))
	return hex.EncodeToString(mac.Sum(nil))
}

// HashRefreshToken returns the SHA-256 hash of a raw refresh token as a hex string.
func HashRefreshToken(raw string) string {
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:])
}

// GenerateRefreshToken returns a random 32-byte token and its SHA-256 hash.
func GenerateRefreshToken() (raw, hashed string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", "", fmt.Errorf("generate refresh token: %w", err)
	}
	raw = hex.EncodeToString(b)
	h := sha256.Sum256([]byte(raw))
	hashed = hex.EncodeToString(h[:])
	return raw, hashed, nil
}
