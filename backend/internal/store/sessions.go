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

// CreateSession inserts a new terminal session and returns its generated UUID.
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

// UpsertSession inserts a session with a caller-supplied UUID (used by the desktop
// agent, which generates the ID before the session is known to the backend).
// If the session already exists, the name is updated when the agent supplies a
// non-empty one (e.g. on reconnect re-announce after the agent assigned a name
// that hadn't been persisted yet).
func (db *DB) UpsertSession(id, deviceID, command, name string, cols, rows int) error {
	if cols == 0 {
		cols = 80
	}
	if rows == 0 {
		rows = 24
	}
	_, err := db.Exec(
		`INSERT INTO terminal_sessions (id, device_id, command, name, cols, rows)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (id) DO UPDATE
		   SET name = CASE WHEN EXCLUDED.name != '' THEN EXCLUDED.name ELSE terminal_sessions.name END`,
		id, deviceID, command, name, cols, rows,
	)
	return err
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

// ListSessionsByDevice returns only active sessions for a device.
func (db *DB) ListSessionsByDevice(deviceID string) ([]*TerminalSession, error) {
	rows, err := db.Query(
		`SELECT id, device_id, name, command, status, exit_code, cols, rows,
		        started_at, ended_at, last_activity
		 FROM terminal_sessions WHERE device_id = $1 AND status = 'active'
		 ORDER BY started_at DESC`,
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

// RenameSession updates the display name of a session.
func (db *DB) RenameSession(id, name string) error {
	_, err := db.Exec(`UPDATE terminal_sessions SET name=$1 WHERE id=$2`, name, id)
	return err
}

// TouchSession updates last_activity.
func (db *DB) TouchSession(id string) error {
	_, err := db.Exec(`UPDATE terminal_sessions SET last_activity=NOW() WHERE id=$1`, id)
	return err
}

// ResizeSession updates the terminal dimensions for a session.
func (db *DB) ResizeSession(id string, cols, rows int) error {
	_, err := db.Exec(`UPDATE terminal_sessions SET cols=$1, rows=$2 WHERE id=$3`, cols, rows, id)
	return err
}

// ReactivateSession flips a session from "exited" back to "active" and clears
// ended_at / exit_code.  Only takes effect when status is currently "exited"
// (i.e. sessions marked exited by MarkDeviceSessionsExited on disconnect).
// Sessions with status "killed" are intentionally left untouched.
func (db *DB) ReactivateSession(id string) error {
	_, err := db.Exec(
		`UPDATE terminal_sessions
		 SET status='active', ended_at=NULL, exit_code=NULL
		 WHERE id=$1 AND status='exited'`,
		id,
	)
	return err
}

// MarkDeviceSessionsExited marks all "active" sessions for a device as exited
// (exit_code = -1).  Called when the device agent (re-)connects so that stale
// sessions from a previous agent run are cleaned up before the agent
// re-announces its currently live sessions.
func (db *DB) MarkDeviceSessionsExited(deviceID string) error {
	_, err := db.Exec(
		`UPDATE terminal_sessions SET status='exited', exit_code=-1, ended_at=NOW()
		 WHERE device_id=$1 AND status='active'`,
		deviceID,
	)
	return err
}
