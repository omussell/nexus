// Package ingest loads the NDJSON files the nauvis and fulgora systems wrote
// out into a DuckDB database, one row per record, storing the complete contents
// of each record.
package ingest

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/duckdb/duckdb-go/v2"
	_ "modernc.org/sqlite" // registers the "sqlite" driver (pure Go).

	"github.com/nexus/vulcanus/internal/fulgora"
	"github.com/nexus/vulcanus/internal/nauvis"
)

// Run ingests the data held by vis (a *nauvis.Store, landing in a single
// `nauvis` table) or store (a *fulgora.Store, landing in one table per
// source) into outDB. outDir is the base directory each of the provider's
// recorded paths is joined against.
//
// If useParquet is true, Parquet files are read via DuckDB's read_parquet()
// instead of parsing NDJSON line-by-line.
//
// All rows are written in a single transaction, so the load is atomic: either
// the whole run lands or nothing does. Runs against the same outDB therefore
// never interfere with each other — ingesting Nauvis now and Fulgora later is
// safe. Returns the number of rows ingested.
func Run(ctx context.Context, src any, outDir, outDB string, useParquet bool, lg *slog.Logger) (int, error) {
	if lg == nil {
		lg = slog.Default()
	}
	if vis, ok := src.(*nauvis.Store); ok {
		return runNauvis(ctx, vis, outDir, outDB, useParquet, lg)
	}
	if store, ok := src.(*fulgora.Store); ok {
		return runFulgora(ctx, store, outDir, outDB, useParquet, lg)
	}
	return 0, fmt.Errorf("unknown source type %T", src)
}

// ingestParquet loads a parquet file into the named DuckDB table using read_parquet(),
// converting all columns back to a single JSON string per row to match the existing
// table schema.
func ingestParquet(ctx context.Context, tx *sql.Tx, tableName, path string, lg *slog.Logger) (int, error) {
	// Verify the parquet file exists before attempting to read it.
	if _, err := os.Stat(path); err != nil {
		return 0, fmt.Errorf("parquet file %s: %w", path, err)
	}

	// Count rows in the parquet file.
	var count int
	err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM read_parquet(?)`, path).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count parquet %s: %w", path, err)
	}

	// Insert all rows, converting columns back to JSON to match the existing table schema.
	_, err = tx.ExecContext(ctx, `INSERT INTO `+quoted(tableName)+` (record) SELECT to_json(row(*)) FROM read_parquet(?)`, path)
	if err != nil {
		return 0, fmt.Errorf("insert parquet %s: %w", path, err)
	}

	lg.Info("ingested parquet", "table", tableName, "file", path, "rows", count)
	return count, nil
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

// runNauvis ingests all nauvis output files into outDB, returning the number
// of records loaded. Each distinct file is stored in the `nauvis` table.
func runNauvis(ctx context.Context, vis *nauvis.Store, outDir, outDB string, useParquet bool, lg *slog.Logger) (int, error) {
	db, err := openDuckDB(outDB)
	if err != nil {
		return 0, fmt.Errorf("open duckdb: %w", err)
	}
	defer db.Close()

	var items []nauvis.Nauvis
	if useParquet {
		items, err = vis.ParquetFiles()
	} else {
		items, err = vis.Nauvis()
	}
	if err != nil {
		return 0, fmt.Errorf("list nauvis: %w", err)
	}

	var total int
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin txn: %w", err)
	}
	defer tx.Rollback()

	if err := resetTable(ctx, tx, "nauvis"); err != nil {
		return 0, fmt.Errorf("reset nauvis table: %w", err)
	}

	insertStmt, err := tx.PrepareContext(ctx, `INSERT INTO nauvis (record) VALUES (?)`)
	if err != nil {
		return 0, fmt.Errorf("prepare insert: %w", err)
	}
	defer insertStmt.Close()

	for _, item := range items {
		path := filepath.Join(filepath.Clean(outDir), item.Path)
		var n int
		if useParquet {
			n, err = ingestParquet(ctx, tx, "nauvis", path, lg)
		} else {
			n, err = ingestFile(ctx, insertStmt, path)
		}
		if err != nil {
			return 0, fmt.Errorf("ingest %s: %w", item.Path, err)
		}
		total += n
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}
	return total, nil
}

// runFulgora ingests all fulgora output files into outDB, creating one table
// per source. Returns the total number of records loaded.
func runFulgora(ctx context.Context, store *fulgora.Store, outDir, outDB string, useParquet bool, lg *slog.Logger) (int, error) {
	db, err := openDuckDB(outDB)
	if err != nil {
		return 0, fmt.Errorf("open duckdb: %w", err)
	}
	defer db.Close()

	sources, err := store.Sources(ctx)
	if err != nil {
		return 0, fmt.Errorf("list fulgora sources: %w", err)
	}

	var total int
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin txn: %w", err)
	}
	defer tx.Rollback()

	for _, src := range sources {
		if err := resetTable(ctx, tx, src.Name()); err != nil {
			return 0, fmt.Errorf("reset table %s: %w", src.Name(), err)
		}

		insertStmt, err := tx.PrepareContext(ctx, `INSERT INTO `+quoted(src.Name())+` (record) VALUES (?)`)
		if err != nil {
			return 0, fmt.Errorf("prepare insert %s: %w", src.Name(), err)
		}

		var path string
		if useParquet {
			path = src.ParquetPath()
		} else {
			path = src.OutputPath()
		}

		var n int
		if useParquet {
			n, err = ingestParquet(ctx, tx, src.Name(), path, lg)
		} else {
			n, err = ingestFile(ctx, insertStmt, path)
		}
		if err != nil {
			insertStmt.Close()
			return 0, fmt.Errorf("ingest %s: %w", src.Name(), err)
		}
		total += n
		insertStmt.Close()
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}
	return total, nil
}
