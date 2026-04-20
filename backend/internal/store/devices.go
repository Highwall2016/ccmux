package store

import (
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// Device represents a registered desktop machine.
type Device struct {
	ID          string
	UserID      string
	Name        string
	DeviceToken string // stored as HMAC-SHA256 hash
	Platform    string
	LastSeen    *time.Time
	CreatedAt   time.Time
}

// CreateDevice inserts a new device. tokenHash is the HMAC-SHA256 of the raw token.
// If a device with the same name and platform already exists for the user, it is
// replaced (old record deleted, new one inserted) so stale registrations don't
// accumulate. The new device ID is returned along with the replaced device's ID
// (empty if none existed).
func (db *DB) CreateDevice(userID, name, platform, tokenHash string) (deviceID string, replacedID string, err error) {
	tx, err := db.Begin()
	if err != nil {
		return "", "", fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// Find and delete an existing same-name+platform device for this user.
	_ = tx.QueryRow(
		`DELETE FROM devices WHERE user_id = $1 AND name = $2 AND platform = $3 RETURNING id`,
		userID, name, platform,
	).Scan(&replacedID)

	if err := tx.QueryRow(
		`INSERT INTO devices (user_id, name, platform, device_token)
		 VALUES ($1, $2, $3, $4) RETURNING id`,
		userID, name, platform, tokenHash,
	).Scan(&deviceID); err != nil {
		return "", "", fmt.Errorf("create device: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return "", "", fmt.Errorf("commit: %w", err)
	}
	return deviceID, replacedID, nil
}

// GetDeviceByID retrieves a device by ID.
func (db *DB) GetDeviceByID(id string) (*Device, error) {
	d := &Device{}
	err := db.QueryRow(
		`SELECT id, user_id, name, device_token, platform, last_seen, created_at
		 FROM devices WHERE id = $1`,
		id,
	).Scan(&d.ID, &d.UserID, &d.Name, &d.DeviceToken, &d.Platform, &d.LastSeen, &d.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get device: %w", err)
	}
	return d, nil
}

// ListDevicesByUser returns all devices for a user.
func (db *DB) ListDevicesByUser(userID string) ([]*Device, error) {
	rows, err := db.Query(
		`SELECT id, user_id, name, device_token, platform, last_seen, created_at
		 FROM devices WHERE user_id = $1 ORDER BY created_at`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list devices: %w", err)
	}
	defer rows.Close()

	var devices []*Device
	for rows.Next() {
		d := &Device{}
		if err := rows.Scan(&d.ID, &d.UserID, &d.Name, &d.DeviceToken, &d.Platform, &d.LastSeen, &d.CreatedAt); err != nil {
			return nil, err
		}
		devices = append(devices, d)
	}
	return devices, rows.Err()
}

// DeleteDevice removes a device by ID.
func (db *DB) DeleteDevice(id, userID string) error {
	_, err := db.Exec(`DELETE FROM devices WHERE id = $1 AND user_id = $2`, id, userID)
	return err
}

// TouchDevice updates last_seen for a device.
func (db *DB) TouchDevice(id string) error {
	_, err := db.Exec(`UPDATE devices SET last_seen = NOW() WHERE id = $1`, id)
	return err
}

// ValidateDeviceToken checks a raw token against the stored HMAC hash.
func ValidateDeviceToken(rawToken, storedHash, hmacSecret string) bool {
	expected := hmacDeviceToken(rawToken, hmacSecret)
	return hmac.Equal([]byte(expected), []byte(storedHash))
}

func hmacDeviceToken(raw, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(raw))
	return hex.EncodeToString(mac.Sum(nil))
}
