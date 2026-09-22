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

// retractionProvenanceViewName is the DuckDB view that reduces each
// Nauvis↔retractionwatch match to a single provenance string asserting which
// retraction notice (by its own RetractionDOI) retracts which Nauvis record.
const retractionProvenanceViewName = "retraction_provenance"

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
	return buildRetractionProvenanceView(ctx, tx)
}

// buildRetractionProvenanceView atomically creates (or replaces) the
// `retraction_provenance` view inside the caller's transaction, deriving a
// single consumer-ready provenance string from each Nauvis↔retractionwatch
// match. Like `buildRetractionsView`, it is a no-op if either source table is
// absent, so both provider tables must be present before the view becomes
// queryable.
func buildRetractionProvenanceView(ctx context.Context, tx *sql.Tx) error {
	if exist, err := tablesExist(ctx, tx, "nauvis", "retractionwatch"); err != nil {
		return fmt.Errorf("check for source tables: %w", err)
	} else if !exist {
		return nil
	}
	stmt := `
		CREATE OR REPLACE VIEW ` + retractionProvenanceViewName + ` AS
		SELECT
			n.record AS nauvis_record,
			r.record AS retractionwatch_record,
			json_extract_string(n.record, '$.DOI') AS paper_doi,
			json_extract_string(r.record, '$.RetractionDOI') AS retraction_doi,
			json_extract_string(n.record, '$.DOI') || ' is-retracted-by ' || json_extract_string(r.record, '$.RetractionDOI') AS provenance
		FROM nauvis AS n
		JOIN retractionwatch AS r
		  ON json_extract_string(n.record, '$.DOI') =
		  	json_extract_string(r.record, '$.OriginalPaperDOI')

		UNION ALL

		SELECT
			r.record AS nauvis_record,
			n.record AS retractionwatch_record,
			json_extract_string(r.record, '$.RetractionDOI') AS paper_doi,
			json_extract_string(n.record, '$.DOI') AS retraction_doi,
			json_extract_string(r.record, '$.RetractionDOI') || ' retracts ' || json_extract_string(n.record, '$.DOI') AS provenance
		FROM retractionwatch AS r
		JOIN nauvis AS n
		  ON json_extract_string(n.record, '$.DOI') =
		  	json_extract_string(r.record, '$.OriginalPaperDOI')`
	if _, err := tx.ExecContext(ctx, stmt); err != nil {
		return fmt.Errorf("create view %s: %w", retractionProvenanceViewName, err)
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
