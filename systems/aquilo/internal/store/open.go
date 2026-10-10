package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	_ "modernc.org/sqlite" // registers the "sqlite" database/sql driver (pure Go)

	"github.com/nexus/aquilo/internal/db"
	"github.com/nexus/aquilo/internal/migrate"
)

// Open opens the SQLite database at dbPath, applies the schema, returns a ready
// Store and the underlying *sql.DB (keep it open until done).
func Open(ctx context.Context, dbPath string) (*Store, *sql.DB, error) {
	if strings.TrimSpace(dbPath) == "" {
		return nil, nil, errors.New("store: empty database path")
	}

	// _busy_timeout waits up to 5s for the write lock.
	dsn := "file:" + dbPath + "?_busy_timeout=5000"
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, nil, err
	}
	// SQLite is a single-writer store: cap the pool to one writer.
	conn.SetMaxOpenConns(1)

	if err := conn.PingContext(ctx); err != nil {
		conn.Close()
		return nil, nil, err
	}
	if _, err := conn.ExecContext(ctx, "PRAGMA journal_mode=WAL"); err != nil {
		conn.Close()
		return nil, nil, err
	}
	if err := migrate.Apply(ctx, conn); err != nil {
		conn.Close()
		return nil, nil, err
	}

	return New(conn, db.New(conn)), conn, nil
}
