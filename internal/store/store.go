// Package store provides SQLite-backed persistence for Greenlight.
package store

import (
	"database/sql"
	_ "embed"
	"fmt"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

//go:embed schema.sql
var schemaSQL string

// Store wraps the database connection and query methods.
type Store struct {
	db *sql.DB
}

// Open opens (creating if needed) the SQLite database at path and applies the
// schema. WAL mode and a busy timeout are enabled for concurrency safety
// between the HTTP handlers and the background scheduler.
func Open(path string) (*Store, error) {
	dsn := fmt.Sprintf("file:%s?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=on", path)
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	// SQLite writes are serialized; a single writer connection avoids
	// SQLITE_BUSY under the WAL+busy_timeout config while keeping reads fast.
	db.SetMaxOpenConns(1)
	db.SetConnMaxLifetime(0)

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}
	if _, err := db.Exec(schemaSQL); err != nil {
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	return &Store{db: db}, nil
}

// Close closes the underlying database.
func (s *Store) Close() error { return s.db.Close() }

// DB exposes the raw handle (used sparingly, e.g. health checks).
func (s *Store) DB() *sql.DB { return s.db }

// nullTime converts a *time.Time to a value usable by database/sql.
func nullTime(t *time.Time) interface{} {
	if t == nil {
		return nil
	}
	return *t
}

// scanTime reads a nullable timestamp column.
func scanTime(nt sql.NullTime) *time.Time {
	if nt.Valid {
		t := nt.Time
		return &t
	}
	return nil
}
