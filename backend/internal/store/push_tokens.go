package store

import "fmt"

// PushToken represents a mobile push notification token.
type PushToken struct {
	ID         string
	UserID     string
	Platform   string // ios | android
	Token      string
	DeviceName string
}

// UpsertPushToken inserts or ignores a duplicate push token.
func (db *DB) UpsertPushToken(userID, platform, token, deviceName string) error {
	_, err := db.Exec(
		`INSERT INTO push_tokens (user_id, platform, token, device_name)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (user_id, token) DO UPDATE SET device_name = EXCLUDED.device_name`,
		userID, platform, token, deviceName,
	)
	if err != nil {
		return fmt.Errorf("upsert push token: %w", err)
	}
	return nil
}

// DeletePushToken removes a specific push token.
func (db *DB) DeletePushToken(userID, token string) error {
	_, err := db.Exec(
		`DELETE FROM push_tokens WHERE user_id=$1 AND token=$2`,
		userID, token,
	)
	return err
}

// GetPushTokensForUser returns all push tokens for a user.
func (db *DB) GetPushTokensForUser(userID string) ([]*PushToken, error) {
	rows, err := db.Query(
		`SELECT id, user_id, platform, token, COALESCE(device_name, '')
		 FROM push_tokens WHERE user_id=$1`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("get push tokens: %w", err)
	}
	defer rows.Close()

	var tokens []*PushToken
	for rows.Next() {
		t := &PushToken{}
		if err := rows.Scan(&t.ID, &t.UserID, &t.Platform, &t.Token, &t.DeviceName); err != nil {
			return nil, err
		}
		tokens = append(tokens, t)
	}
	return tokens, rows.Err()
}

// DeleteExpiredPushToken removes a token that returned 410 Gone from APNs/FCM.
func (db *DB) DeleteExpiredPushToken(token string) error {
	_, err := db.Exec(`DELETE FROM push_tokens WHERE token=$1`, token)
	return err
}
