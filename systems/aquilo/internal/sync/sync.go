// Package sync copies item records from a vulcanus DuckDB database into a
// local SQLite database using the store layer.
package sync

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"strings"

	"github.com/nexus/aquilo/internal/store"
	"github.com/nexus/vulcanus/client"
)

// Config holds the parameters for a sync operation.
type Config struct {
	DuckDBPath string // path to vulcanus DuckDB database
	DBPath     string // path to local SQLite DB
	BatchSize  int    // rows per batch insert
}

// Result reports the outcome of a sync operation.
type Result struct {
	Total   int   // total records read from DuckDB
	Synced  int   // records successfully written to SQLite
	Errors  int   // records that failed to write
	LastCroid string // CROID of the last successfully synced record
}

// Run copies all nauvis records from the vulcanus DuckDB database at the
// configured path into the local SQLite database.
func Run(ctx context.Context, cfg Config) (Result, error) {
	if cfg.DuckDBPath == "" {
		return Result{}, fmt.Errorf("sync: duckdb path required")
	}
	if cfg.DBPath == "" {
		return Result{}, fmt.Errorf("sync: db path required")
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 1000
	}

	// Resolve relative paths from CWD so the binary works when run from the
	// aquello system directory (e.g. "./aquilo sync -duckdb ../vulcanus/vulcanus.duckdb").
	duckdbPath, err := filepath.Abs(cfg.DuckDBPath)
	if err != nil {
		return Result{}, fmt.Errorf("sync: resolve duckdb path: %w", err)
	}
	dbPath, err := filepath.Abs(cfg.DBPath)
	if err != nil {
		return Result{}, fmt.Errorf("sync: resolve db path: %w", err)
	}

	vulc, err := client.Open(duckdbPath)
	if err != nil {
		return Result{}, fmt.Errorf("open vulcanus duckdb %s: %w", cfg.DuckDBPath, err)
	}
	defer vulc.Close()

	s, conn, err := store.Open(ctx, dbPath)
	if err != nil {
		return Result{}, fmt.Errorf("open local db %s: %w", cfg.DBPath, err)
	}
	defer conn.Close()

	total, err := vulc.CountNauvis(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("count nauvis: %w", err)
	}

	result := Result{Total: total}

	pageSize := 500
	for offset := 0; offset < total; offset += pageSize {
		if err := ctx.Err(); err != nil {
			return result, fmt.Errorf("sync: context cancelled at offset %d: %w", offset, err)
		}

		recs, err := vulc.QueryNauvis(ctx, offset, pageSize)
		if err != nil {
			result.Errors += total - offset
			return result, fmt.Errorf("query nauvis offset %d: %w", offset, err)
		}
		if len(recs) == 0 {
			break
		}

		croids := make([]string, len(recs))
		records := make([]string, len(recs))
		for i, rec := range recs {
			croids[i] = rec.Croid
			records[i] = strings.TrimSpace(rec.Record)
		}

		if err := s.InsertItemsBulk(ctx, croids, records); err != nil {
			result.Errors += len(records)
			return result, fmt.Errorf("batch insert offset %d: %w", offset, err)
		}

		result.Synced += len(recs)
		result.LastCroid = recs[len(recs)-1].Croid
		log.Printf("aquilo: synced %d/%d records (last: %s)", result.Synced, total, result.LastCroid)
	}

	return result, nil
}
