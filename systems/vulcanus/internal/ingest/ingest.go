// Package ingest loads the NDJSON files nauvis wrote out into a DuckDB
// database, one row per item, storing the complete contents of each item.
package ingest

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/duckdb/duckdb-go/v2"
	_ "modernc.org/sqlite" // registers the "sqlite" driver (pure Go).

	"github.com/nexus/vulcanus/internal/nauvis"
)

// Run ingests every file nauvis recorded for its output directory into a single
// DuckDB database by writing one row per item. outDir is the base every Nauvis
// output path is resolved against and outDB is the target DuckDB file. Returns
// the total number of rows ingested across every file.
func Run(ctx context.Context, vis *nauvis.Store, outDir, outDB string) (int, error) {
	files, err := vis.Items()
	if err != nil {
		return 0, fmt.Errorf("list output files: %w", err)
	}

	db, err := openDuckDB(outDB)
	if err != nil {
		return 0, fmt.Errorf("open DuckDB: %w", err)
	}
	defer db.Close()

	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS items (record JSON)`); err != nil {
		return 0, fmt.Errorf("create items table: %w", err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	insert, err := tx.PrepareContext(ctx, `INSERT INTO items (record) VALUES (?)`)
	if err != nil {
		return 0, fmt.Errorf("prepare insert: %w", err)
	}
	defer insert.Close()

	total, err := ingestAll(ctx, outDir, files, insert)
	if err != nil {
		return total, err
	}
	if err := tx.Commit(); err != nil {
		return total, fmt.Errorf("commit transaction: %w", err)
	}
	return total, nil
}

// ingestAll ingests every listed file into items.
func ingestAll(ctx context.Context, outDir string, files []nauvis.Item, insert *sql.Stmt) (int, error) {
	total := 0
	for _, f := range files {
		n, err := ingestFile(ctx, insert, filepath.Join(outDir, f.Path))
		if err != nil {
			return total, fmt.Errorf("ingest %s: %w", f.Path, err)
		}
		total += n
	}
	return total, nil
}

// ingestFile writes one row per NDJSON line from path into items, storing the
// complete contents of every item.
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

// openDuckDB opens the DuckDB database at path, returning a handle usable
// through the database/sql interface.
func openDuckDB(path string) (*sql.DB, error) {
	conn, err := duckdb.NewConnector(path, nil)
	if err != nil {
		return nil, fmt.Errorf("connect to DB %s: %w", path, err)
	}
	return sql.OpenDB(conn), nil
}
