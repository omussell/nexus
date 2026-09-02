package ingest

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/nexus/vulcanus/internal/nauvis"
)

// runNauvis ingests the Nauvis store into a single `nauvis` table, joining
// each recorded path against outDir.
func runNauvis(ctx context.Context, vis *nauvis.Store, outDir, outDB string) (int, error) {
	files, err := vis.Nauvis()
	if err != nil {
		return 0, fmt.Errorf("list output files: %w", err)
	}

	db, err := openDuckDB(outDB)
	if err != nil {
		return 0, fmt.Errorf("open DuckDB: %w", err)
	}
	defer db.Close()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := resetTable(ctx, tx, "nauvis"); err != nil {
		return 0, err
	}

	insert, err := tx.PrepareContext(ctx, "INSERT INTO nauvis (record) VALUES (?)")
	if err != nil {
		return 0, fmt.Errorf("prepare insert: %w", err)
	}
	defer insert.Close()

	total := 0
	for _, file := range files {
		n, err := ingestFile(ctx, insert, filepath.Join(outDir, file.Path))
		if err != nil {
			return total, fmt.Errorf("ingest %s: %w", file.Path, err)
		}
		total += n
	}

	if err := buildRetractionsView(ctx, tx); err != nil {
		return total, err
	}

	if err := tx.Commit(); err != nil {
		return total, fmt.Errorf("commit transaction: %w", err)
	}
	return total, nil
}
