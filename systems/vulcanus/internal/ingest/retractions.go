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
			json_extract_string(n.record, '$.DOI') AS matched_doi
		FROM nauvis AS n
		JOIN retractionwatch AS r
		  ON json_extract_string(n.record, '$.DOI') =
		  	json_extract_string(r.record, '$.OriginalPaperDOI')`
	if _, err := tx.ExecContext(ctx, stmt); err != nil {
		return fmt.Errorf("create view %s: %w", retractionsViewName, err)
	}
	return nil
}

// tablesExist reports whether every one of names exists in the main schema of
// the transaction's DuckDB database. A missing table is not an error — it just
// yields false; anything else is propagated.
func tablesExist(ctx context.Context, tx *sql.Tx, names ...string) (bool, error) {
	for _, n := range names {
		var got string
		err := tx.QueryRowContext(ctx,
			`SELECT table_name FROM information_schema.tables
			 WHERE table_schema = 'main' AND table_name = ?`,
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
