package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// RefreshToken represents a stored refresh token record.
type RefreshToken struct {
	ID        string
	UserID    string
	TokenHash string
	ExpiresAt time.Time
	Revoked   bool
	CreatedAt time.Time
}

// CreateRefreshToken stores a hashed refresh token and returns its ID.
func (db *DB) CreateRefreshToken(userID, tokenHash string, expiresAt time.Time) (string, error) {
	var id string
	err := db.QueryRow(
		`INSERT INTO refresh_tokens (user_id, token_hash, expires_at) VALUES ($1, $2, $3) RETURNING id`,
		userID, tokenHash, expiresAt,
	).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("create refresh token: %w", err)
	}
	return id, nil
}

// GetRefreshToken looks up a token by its hash.
func (db *DB) GetRefreshToken(tokenHash string) (*RefreshToken, error) {
	t := &RefreshToken{}
	err := db.QueryRow(
		`SELECT id, user_id, token_hash, expires_at, revoked, created_at
		 FROM refresh_tokens WHERE token_hash = $1`,
		tokenHash,
	).Scan(&t.ID, &t.UserID, &t.TokenHash, &t.ExpiresAt, &t.Revoked, &t.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get refresh token: %w", err)
	}
	return t, nil
}

// RevokeRefreshToken marks a token as revoked.
func (db *DB) RevokeRefreshToken(tokenHash string) error {
	_, err := db.Exec(`UPDATE refresh_tokens SET revoked = TRUE WHERE token_hash = $1`, tokenHash)
	return err
}

// DeleteExpiredRefreshTokens removes all expired and revoked tokens.
func (db *DB) DeleteExpiredRefreshTokens() error {
	_, err := db.Exec(`DELETE FROM refresh_tokens WHERE expires_at < NOW() OR revoked = TRUE`)
	return err
}
