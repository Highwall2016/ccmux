package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// User represents a registered user.
type User struct {
	ID           string
	Email        string
	PasswordHash string
	CreatedAt    time.Time
}

// CreateUser inserts a new user and returns their ID.
func (db *DB) CreateUser(email, passwordHash string) (string, error) {
	var id string
	err := db.QueryRow(
		`INSERT INTO users (email, password_hash) VALUES ($1, $2) RETURNING id`,
		email, passwordHash,
	).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("create user: %w", err)
	}
	return id, nil
}

// GetUserByEmail looks up a user by email.
func (db *DB) GetUserByEmail(email string) (*User, error) {
	u := &User{}
	err := db.QueryRow(
		`SELECT id, email, password_hash, created_at FROM users WHERE email = $1`,
		email,
	).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get user by email: %w", err)
	}
	return u, nil
}

// GetUserByID looks up a user by ID.
func (db *DB) GetUserByID(id string) (*User, error) {
	u := &User{}
	err := db.QueryRow(
		`SELECT id, email, password_hash, created_at FROM users WHERE id = $1`,
		id,
	).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get user by id: %w", err)
	}
	return u, nil
}
