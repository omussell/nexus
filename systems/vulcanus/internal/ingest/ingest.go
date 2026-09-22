// Package ingest provides utilities for working with the DuckDB database
// that Vulcanus writes to.
package ingest

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/duckdb/duckdb-go/v2"
)

// OpenDB opens the DuckDB database at path, returning a handle usable through
// the database/sql interface.
func OpenDB(path string) (*sql.DB, error) {
	return openDuckDB(path)
}

func openDuckDB(path string) (*sql.DB, error) {
	conn, err := duckdb.NewConnector(path, nil)
	if err != nil {
		return nil, fmt.Errorf("connect to DB %s: %w", path, err)
	}
	return sql.OpenDB(conn), nil
}

// ingestFile writes one row per NDJSON line from path into the given insert,
// storing the complete contents of every element.
func ingestFile(ctx context.Context, insert *sql.Stmt, path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)

	total := 0
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		if !json.Valid(line) {
			return total, fmt.Errorf("invalid JSON at line %d of %s", total+1, filepath.Base(path))
		}
		if _, err := insert.ExecContext(ctx, string(line)); err != nil {
			return total, fmt.Errorf("bind line %d of %s: %w", total+1, filepath.Base(path), err)
		}
		total++
	}
	if err := scanner.Err(); err != nil {
		return total, fmt.Errorf("read %s: %w", filepath.Base(path), err)
	}
	return total, nil
}

// ensureTable ensures a table named name with a single `record` JSON column
// exists inside the caller's transaction.
func ensureTable(ctx context.Context, tx *sql.Tx, name string) error {
	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS `+quoted(name)+` (record JSON)`); err != nil {
		return fmt.Errorf("create table %s: %w", name, err)
	}
	return nil
}

// resetTable ensures the named table exists and empties it, so re-running a
// provider replaces its previous snapshot instead of accumulating duplicates.
func resetTable(ctx context.Context, tx *sql.Tx, name string) error {
	if err := ensureTable(ctx, tx, name); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM `+quoted(name)); err != nil {
		return fmt.Errorf("clear table %s: %w", name, err)
	}
	return nil
}

// quoted wraps name in double quotes, escaping any embedded double quotes, so
// the result is safe to use as a DuckDB table identifier.
func quoted(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
