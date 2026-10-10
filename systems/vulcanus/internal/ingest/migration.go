package ingest

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// migrateSchema adds denormalized columns and indexes to existing source
// tables so matching queries can use typed columns instead of parsing JSON at
// query time.  The function is safe to call on fresh or existing databases.
func migrateSchema(ctx context.Context, tx *sql.Tx) error {
	if err := migrateNauvis(ctx, tx); err != nil {
		return fmt.Errorf("migrate nauvis: %w", err)
	}
	if err := migrateRor(ctx, tx); err != nil {
		return fmt.Errorf("migrate ror: %w", err)
	}
	if err := migrateRetractionWatch(ctx, tx); err != nil {
		return fmt.Errorf("migrate retractionwatch: %w", err)
	}
	return nil
}

func columnExists(ctx context.Context, tx *sql.Tx, table, column string) (bool, error) {
	var count int
	err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM duckdb_columns()
		 WHERE table_name = ? AND column_name = ? AND schema_name = 'main'`,
		table, column).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func addColumn(ctx context.Context, tx *sql.Tx, table, colDef string) error {
	exists, err := columnExists(ctx, tx, table, splitColDef(colDef)[0])
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `ALTER TABLE `+quoted(table)+` ADD COLUMN `+colDef); err != nil {
		return fmt.Errorf("add column %s to %s: %w", colDef, table, err)
	}
	return nil
}

func splitColDef(s string) []string {
	parts := make([]string, 0, 2)
	depth := 0
	inString := false
	current := ""
	for _, r := range s {
		switch {
		case r == '"':
			inString = !inString
			current += string(r)
		case inString:
			current += string(r)
		case r == '(' && !inString:
			depth++
			current += string(r)
		case r == ')' && !inString:
			depth--
			current += string(r)
		case r == ' ' && depth == 0:
			parts = append(parts, current)
			current = ""
		default:
			current += string(r)
		}
	}
	if current != "" {
		parts = append(parts, current)
	}
	return parts
}

func migrateNauvis(ctx context.Context, tx *sql.Tx) error {
	if !tableExists(ctx, tx, "nauvis") {
		return nil
	}

	if err := addColumn(ctx, tx, "nauvis", "doi TEXT"); err != nil {
		return err
	}
	if err := addColumn(ctx, tx, "nauvis", "funder_names JSON"); err != nil {
		return err
	}
	if err := addColumn(ctx, tx, "nauvis", "affiliation_names JSON"); err != nil {
		return err
	}

	// Back-fill from existing record column.
	if _, err := tx.ExecContext(ctx, `
		UPDATE nauvis
		SET doi = json_extract_string(record, '$.DOI'),
			funder_names = json_extract(record, '$.funder[*].name'),
			affiliation_names = json_extract(record, '$.author[*].affiliation[*].name')
		WHERE doi IS NULL OR funder_names IS NULL OR affiliation_names IS NULL
	`); err != nil {
		return fmt.Errorf("backfill nauvis: %w", err)
	}

	// Create indexes (ignore if already exists).
	// Note: DuckDB does not support "USING GIN"; it auto-selects the best index type.
	for _, idx := range []string{
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_nauvis_doi ON nauvis(doi)`,
		`CREATE INDEX IF NOT EXISTS idx_nauvis_funder ON nauvis(funder_names)`,
		`CREATE INDEX IF NOT EXISTS idx_nauvis_affil ON nauvis(affiliation_names)`,
	} {
		if _, err := tx.ExecContext(ctx, idx); err != nil {
			// Ignore "already exists" errors.
			if !isDuplicateIndex(err) {
				return fmt.Errorf("create index %s: %w", idx, err)
			}
		}
	}
	return nil
}

func migrateRor(ctx context.Context, tx *sql.Tx) error {
	if !tableExists(ctx, tx, "ror") {
		return nil
	}

	if err := addColumn(ctx, tx, "ror", "ror_id TEXT"); err != nil {
		return err
	}
	if err := addColumn(ctx, tx, "ror", "country TEXT"); err != nil {
		return err
	}
	if err := addColumn(ctx, tx, "ror", "status TEXT"); err != nil {
		return err
	}
	if err := addColumn(ctx, tx, "ror", "names JSON"); err != nil {
		return err
	}

	// Back-fill from existing record column.
	if _, err := tx.ExecContext(ctx, `
		UPDATE ror
		SET ror_id = json_extract_string(record, '$.id'),
			country = json_extract_string(record, '$.locations[0].geonames_details.country_code'),
			status = json_extract_string(record, '$.status'),
			names = CAST(json_extract(record, '$.names[*].value') AS JSON)
		WHERE ror_id IS NULL OR names IS NULL
	`); err != nil {
		return fmt.Errorf("backfill ror: %w", err)
	}

	// Create indexes (ignore if already exists).
	for _, idx := range []string{
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_ror_id ON ror(ror_id)`,
		`CREATE INDEX IF NOT EXISTS idx_ror_country ON ror(country)`,
		`CREATE INDEX IF NOT EXISTS idx_ror_status ON ror(status)`,
	} {
		if _, err := tx.ExecContext(ctx, idx); err != nil {
			if !isDuplicateIndex(err) {
				return fmt.Errorf("create index %s: %w", idx, err)
			}
		}
	}
	return nil
}

func migrateRetractionWatch(ctx context.Context, tx *sql.Tx) error {
	if !tableExists(ctx, tx, "retractionwatch") {
		return nil
	}

	if err := addColumn(ctx, tx, "retractionwatch", "original_paper_doi TEXT"); err != nil {
		return err
	}
	if err := addColumn(ctx, tx, "retractionwatch", "retraction_doi TEXT"); err != nil {
		return err
	}

	// Back-fill from existing record column.
	if _, err := tx.ExecContext(ctx, `
		UPDATE retractionwatch
		SET original_paper_doi = json_extract_string(record, '$.original_paper_doi'),
			retraction_doi = json_extract_string(record, '$.retraction_doi')
		WHERE original_paper_doi IS NULL
	`); err != nil {
		return fmt.Errorf("backfill retractionwatch: %w", err)
	}

	// Create indexes (ignore if already exists).
	for _, idx := range []string{
		`CREATE INDEX IF NOT EXISTS idx_retraction_doi ON retractionwatch(original_paper_doi)`,
	} {
		if _, err := tx.ExecContext(ctx, idx); err != nil {
			if !isDuplicateIndex(err) {
				return fmt.Errorf("create index %s: %w", idx, err)
			}
		}
	}
	return nil
}

func tableExists(ctx context.Context, tx *sql.Tx, name string) bool {
	var count int
	err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM duckdb_tables()
		 WHERE table_name = ? AND temporary = false AND schema_name = 'main'`,
		name).Scan(&count)
	return err == nil && count > 0
}

// isDuplicateIndex reports whether err indicates the index already exists.
func isDuplicateIndex(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique") ||
		strings.Contains(msg, "duplicate") ||
		strings.Contains(msg, "already exists") ||
		strings.Contains(msg, "already")
}
