package store

import (
	"database/sql"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	_ "github.com/lib/pq"
)

// DB wraps a *sql.DB and provides all store implementations.
type DB struct {
	*sql.DB
}

// Open opens a PostgreSQL connection and returns a DB.
func Open(dsn string) (*DB, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	return &DB{db}, nil
}

// Migrate runs all SQL migration files from the provided FS in filename order.
func (db *DB) Migrate(migrationsFS fs.FS) error {
	entries, err := fs.ReadDir(migrationsFS, ".")
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}

	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)

	for _, name := range files {
		data, err := fs.ReadFile(migrationsFS, name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}
		if _, err := db.Exec(string(data)); err != nil {
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
	}
	return nil
}
