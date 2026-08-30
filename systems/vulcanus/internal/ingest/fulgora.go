package ingest

import (
	"context"
	"fmt"

	"github.com/nexus/vulcanus/internal/fulgora"
)

// runFulgora ingests the Fulgora store into one DuckDB table per source, each
// holding the complete NDJSON contents of that source's latest output file.
// Nauvis and Fulgora therefore stay in separate tables within the shared
// outDB.
func runFulgora(ctx context.Context, store *fulgora.Store, outDir, outDB string) (int, error) {
	sources, err := store.Sources(ctx)
	if err != nil {
		return 0, fmt.Errorf("list fulgora sources: %w", err)
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

	var total int
	for _, s := range sources {
		name := s.Name()
		if name == "" {
			return total, fmt.Errorf("fulgora source with empty name")
		}
		if err := resetTable(ctx, tx, name); err != nil {
			return total, err
		}
		insert, err := tx.PrepareContext(ctx, `INSERT INTO `+quoted(name)+` (record) VALUES (?)`)
		if err != nil {
			return total, fmt.Errorf("prepare insert for %s: %w", name, err)
		}
		n, err := ingestFile(ctx, insert, s.OutputPath())
		insert.Close()
		if err != nil {
			return total, fmt.Errorf("ingest %s: %w", name, err)
		}
		total += n
	}

	if err := tx.Commit(); err != nil {
		return total, fmt.Errorf("commit transaction: %w", err)
	}
	return total, nil
}
