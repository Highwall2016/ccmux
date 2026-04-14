package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// TerminalSession represents a PTY session on a device.
type TerminalSession struct {
	ID           string
	DeviceID     string
	Name         string
	Command      string
	Status       string // active | exited | killed
	ExitCode     *int
	Cols         int
	Rows         int
	StartedAt    time.Time
	EndedAt      *time.Time
	LastActivity time.Time
}

// CreateSession inserts a new terminal session.
func (db *DB) CreateSession(deviceID, command, name string, cols, rows int) (string, error) {
	var id string
	err := db.QueryRow(
		`INSERT INTO terminal_sessions (device_id, command, name, cols, rows)
		 VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		deviceID, command, name, cols, rows,
	).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}
	return id, nil
}

// GetSessionByID retrieves a session.
func (db *DB) GetSessionByID(id string) (*TerminalSession, error) {
	s := &TerminalSession{}
	err := db.QueryRow(
		`SELECT id, device_id, name, command, status, exit_code, cols, rows,
		        started_at, ended_at, last_activity
		 FROM terminal_sessions WHERE id = $1`,
		id,
	).Scan(&s.ID, &s.DeviceID, &s.Name, &s.Command, &s.Status, &s.ExitCode,
		&s.Cols, &s.Rows, &s.StartedAt, &s.EndedAt, &s.LastActivity)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}
	return s, nil
}

// ListSessionsByDevice returns all sessions for a device.
func (db *DB) ListSessionsByDevice(deviceID string) ([]*TerminalSession, error) {
	rows, err := db.Query(
		`SELECT id, device_id, name, command, status, exit_code, cols, rows,
		        started_at, ended_at, last_activity
		 FROM terminal_sessions WHERE device_id = $1 ORDER BY started_at DESC`,
		deviceID,
	)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()

	var sessions []*TerminalSession
	for rows.Next() {
		s := &TerminalSession{}
		if err := rows.Scan(&s.ID, &s.DeviceID, &s.Name, &s.Command, &s.Status, &s.ExitCode,
			&s.Cols, &s.Rows, &s.StartedAt, &s.EndedAt, &s.LastActivity); err != nil {
			return nil, err
		}
		sessions = append(sessions, s)
	}
	return sessions, rows.Err()
}

// UpdateStatus updates a session's status and optionally its exit code.
func (db *DB) UpdateStatus(id, status string, exitCode *int) error {
	if exitCode != nil {
		_, err := db.Exec(
			`UPDATE terminal_sessions SET status=$1, exit_code=$2, ended_at=NOW()
			 WHERE id=$3`,
			status, *exitCode, id,
		)
		return err
	}
	_, err := db.Exec(
		`UPDATE terminal_sessions SET status=$1 WHERE id=$2`,
		status, id,
	)
	return err
}

// TouchSession updates last_activity.
func (db *DB) TouchSession(id string) error {
	_, err := db.Exec(`UPDATE terminal_sessions SET last_activity=NOW() WHERE id=$1`, id)
	return err
}
