// Package ingest loads the NDJSON files the nauvis and fulgora systems wrote
// out into a DuckDB database, one row per item, storing the complete contents
// of each item.
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
	_ "modernc.org/sqlite" // registers the "sqlite" driver (pure Go).

	"github.com/nexus/vulcanus/internal/fulgora"
	"github.com/nexus/vulcanus/internal/nauvis"
)

// Run ingests the data held by vis (a *nauvis.Store, landing in a single
// `items` table) or store (a *fulgora.Store, landing in one table per
// source) into outDB. outDir is the base directory each of the provider's
// recorded paths is joined against.
//
// All rows are written in a single transaction, so the load is atomic: either
// the whole run lands or nothing does. Runs against the same outDB therefore
// never interfere with each other — ingesting Nauvis now and Fulgora later is
// safe. Returns the number of rows ingested.
func Run(ctx context.Context, src any, outDir, outDB string) (int, error) {
	if vis, ok := src.(*nauvis.Store); ok {
		return runNauvis(ctx, vis, outDir, outDB)
	}
	if store, ok := src.(*fulgora.Store); ok {
		return runFulgora(ctx, store, outDir, outDB)
	}
	return 0, fmt.Errorf("unknown source type %T", src)
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

// openDuckDB opens the DuckDB database at path, returning a handle usable
// through the database/sql interface.
func openDuckDB(path string) (*sql.DB, error) {
	conn, err := duckdb.NewConnector(path, nil)
	if err != nil {
		return nil, fmt.Errorf("connect to DB %s: %w", path, err)
	}
	return sql.OpenDB(conn), nil
}
