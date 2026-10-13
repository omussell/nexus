package ingest

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// retractionsViewName is the DuckDB view that joins the `nauvis` table's
// DOI against the `retractionwatch` table's OriginalPaperDOI, exposing both
// full records side by side.
const retractionsViewName = "retractions"

// RunRetractionMatches rebuilds the `retractions` view over the DuckDB at
// dbPath. It is a no-op if either the `nauvis` or `retractionwatch` table does
// not yet exist — providers can be ingested at different times, and the view
// simply becomes available the moment both tables are present.
func RunRetractionMatches(ctx context.Context, dbPath string) error {
	db, err := openDuckDB(dbPath)
	if err != nil {
		return fmt.Errorf("open DuckDB: %w", err)
	}
	defer db.Close()

	if !hasTable(ctx, db, "nauvis") || !hasTable(ctx, db, "retractionwatch") {
		return nil
	}

	// Ensure the index on original_paper_doi exists for the join query.
	if _, err := db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_retraction_doi ON retractionwatch(original_paper_doi)`); err != nil {
		return fmt.Errorf("create retraction index: %w", err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := buildRetractionsView(ctx, tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

// buildRetractionsView atomically creates (or replaces) the `retractions` view
// inside the caller's transaction, so the view lands or rolls back exactly with
// the rest of the ingest. It is a no-op if the `nauvis` or `retractionwatch`
// table has not yet been ingested — providers can be imported at different
// times, and the view simply becomes available the moment both tables are
// present.
func buildRetractionsView(ctx context.Context, tx *sql.Tx) error {
	if exist, err := tablesExist(ctx, tx, "nauvis", "retractionwatch"); err != nil {
		return fmt.Errorf("check for source tables: %w", err)
	} else if !exist {
		return nil
	}
	stmt := `
		CREATE OR REPLACE VIEW ` + retractionsViewName + ` AS
		SELECT
			n.record AS nauvis_record,
			r.record AS retractionwatch_record,
			r.retraction_doi AS retraction_notice_doi
		FROM nauvis AS n
		JOIN retractionwatch AS r
   		  ON n.doi = r.original_paper_doi`
	if _, err := tx.ExecContext(ctx, stmt); err != nil {
		return fmt.Errorf("create view %s: %w", retractionsViewName, err)
	}
	return nil
}

// tablesExist reports whether every one of names exists in the main schema of
// the transaction's DuckDB database. A missing table is not an error — it just
// yields false; anything else is propagated. TEMP tables do not count: they
// live in a separate "temp" database whose schema is also named "main", so
// filtering on the schema alone is not enough — the temporary flag is
// required.
func tablesExist(ctx context.Context, tx *sql.Tx, names ...string) (bool, error) {
	for _, n := range names {
		var got string
		err := tx.QueryRowContext(ctx,
			`SELECT table_name FROM duckdb_tables()
			 WHERE table_name = ? AND temporary = false AND schema_name = 'main'`,
			n).Scan(&got)
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("lookup table %q: %w", n, err)
		}
	}
	return true, nil
}
