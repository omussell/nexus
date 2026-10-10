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
	db, err := openDuckDB(outDB)
	if err != nil {
		return 0, fmt.Errorf("open duckdb: %w", err)
	}
	defer db.Close()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin txn: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Ensure denormalized columns exist (migration).
	if err := migrateSchema(ctx, tx); err != nil {
		return 0, fmt.Errorf("migrate schema: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit migration: %w", err)
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

	// Get column names and types to identify arrays
	rows, err := tx.QueryContext(ctx, `DESCRIBE SELECT * FROM read_parquet(?)`, path)
	if err != nil {
		return 0, fmt.Errorf("describe parquet %s: %w", path, err)
	}
	defer rows.Close()

	var columns []struct {
		columnName string
		columnType string
		isArray    bool
	}
	for rows.Next() {
		var colName, colType string
		var nullable, key, defaultVal, extra sql.NullString
		if err := rows.Scan(&colName, &colType, &nullable, &key, &defaultVal, &extra); err != nil {
			return 0, fmt.Errorf("scan describe: %w", err)
		}
		isArray := strings.Contains(colType, "[]")
		columns = append(columns, struct {
			columnName string
			columnType string
			isArray    bool
		}{colName, colType, isArray})
	}

	// Build struct_pack with array columns converted to JSON strings
	var fields []string
	for _, col := range columns {
		if col.isArray {
			fields = append(fields, fmt.Sprintf("%s := array_to_json(%s)", col.columnName, col.columnName))
		} else {
			fields = append(fields, fmt.Sprintf("%s := %s", col.columnName, col.columnName))
		}
	}

	// Insert all rows, converting columns back to JSON to match the existing table schema.
	query := fmt.Sprintf(`INSERT INTO %s (record) SELECT to_json(STRUCT_PACK(%s)) FROM read_parquet(?)`, quoted(tableName), strings.Join(fields, ", "))
	_, err = tx.ExecContext(ctx, query, path)
	if err != nil {
		return 0, fmt.Errorf("insert parquet %s: %w", path, err)
	}

	lg.Info("ingested parquet", "table", tableName, "file", path, "rows", count)
	return count, nil
}

// ingestParquetNauvis loads a nauvis parquet file into the nauvis table,
// projecting typed columns directly instead of converting everything to JSON.
func ingestParquetNauvis(ctx context.Context, tx *sql.Tx, croid, path string, lg *slog.Logger) (int, error) {
	if _, err := os.Stat(path); err != nil {
		return 0, fmt.Errorf("parquet file %s: %w", path, err)
	}

	var count int
	err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM read_parquet(?)`, path).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count parquet %s: %w", path, err)
	}

	// Project typed columns from the parquet file. The nauvis parquet has columns
	// like doi, title[], author_name[], funder_name[], etc.
	// funder_name[] and author_name[] are arrays that need to be flattened and
	// deduplicated for affiliation_names.
	query := `
		INSERT INTO nauvis (record, croid, doi, funder_names, affiliation_names)
		SELECT
			to_json(T),
			?,
			T.doi,
			CASE WHEN T.funder_name IS NOT NULL
				THEN CAST(array_distinct(array_filter(T.funder_name, x -> x IS NOT NULL AND x != '')) AS JSON)
				ELSE '[]' END,
			CASE WHEN T.author_name IS NOT NULL
				THEN CAST(array_distinct(array_filter(T.author_name, x -> x IS NOT NULL AND x != '')) AS JSON)
				ELSE '[]' END
		FROM read_parquet(?) AS T
	`
	_, err = tx.ExecContext(ctx, query, croid, path, path)
	if err != nil {
		return 0, fmt.Errorf("insert parquet nauvis %s: %w", path, err)
	}

	lg.Info("ingested parquet nauvis", "file", path, "rows", count)
	return count, nil
}

// ingestParquetRor loads a ROR parquet file into the ror table with denormalized columns.
func ingestParquetRor(ctx context.Context, tx *sql.Tx, path string, lg *slog.Logger) (int, error) {
	if _, err := os.Stat(path); err != nil {
		return 0, fmt.Errorf("parquet file %s: %w", path, err)
	}

	var count int
	err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM read_parquet(?)`, path).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count parquet %s: %w", path, err)
	}

	// ROR parquet files have columns like ror_id, name, country, status, etc.
	// We need to extract the names array - ROR typically stores names as a JSON
	// object with type/value fields, but the parquet might have a simpler structure.
	// We extract from the record column and also try direct column access.
	query := `
		INSERT INTO ror (record, ror_id, country, status, names)
		SELECT
			to_json(T),
			COALESCE(T.ror_id, T.id, ''),
			COALESCE(T.country, json_extract_string(T, '$.locations[0].geonames_details.country_code')),
			COALESCE(T.status, json_extract_string(T, '$.status')),
			COALESCE(
				-- Try direct names column (parquet may have it as array or JSON)
				CAST(T.names AS JSON),
				-- Fall back to extracting from JSON if available
				CAST(json_extract(T, '$.names[*].value') AS JSON)
			)
		FROM read_parquet(?) AS T
	`
	_, err = tx.ExecContext(ctx, query, path)
	if err != nil {
		// Fall back to record-based extraction if the direct column access fails
		query = `
			INSERT INTO ror (record, ror_id, country, status, names)
			SELECT
				to_json(T),
				json_extract_string(T, '$.id'),
				COALESCE(json_extract_string(T, '$.locations[0].geonames_details.country_code'), ''),
				COALESCE(json_extract_string(T, '$.status'), ''),
				CAST(json_extract(T, '$.names[*].value') AS JSON)
			FROM read_parquet(?) AS T
		`
		_, err = tx.ExecContext(ctx, query, path)
		if err != nil {
			return 0, fmt.Errorf("insert parquet ror %s: %w", path, err)
		}
	}

	lg.Info("ingested parquet ror", "file", path, "rows", count)
	return count, nil
}

// ingestParquetRetractionWatch loads a retractionwatch parquet file into the
// retractionwatch table with denormalized columns.
func ingestParquetRetractionWatch(ctx context.Context, tx *sql.Tx, path string, lg *slog.Logger) (int, error) {
	if _, err := os.Stat(path); err != nil {
		return 0, fmt.Errorf("parquet file %s: %w", path, err)
	}

	var count int
	err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM read_parquet(?)`, path).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count parquet %s: %w", path, err)
	}

	query := `
		INSERT INTO retractionwatch (record, original_paper_doi, retraction_doi)
		SELECT
			to_json(T),
			COALESCE(T.original_paper_doi, json_extract_string(T, '$.original_paper_doi'), ''),
			COALESCE(T.retraction_doi, json_extract_string(T, '$.retraction_doi'), '')
		FROM read_parquet(?) AS T
	`
	_, err = tx.ExecContext(ctx, query, path)
	if err != nil {
		// Fall back to record-based extraction
		query = `
			INSERT INTO retractionwatch (record, original_paper_doi, retraction_doi)
			SELECT
				to_json(T),
				COALESCE(json_extract_string(T, '$.original_paper_doi'), ''),
				COALESCE(json_extract_string(T, '$.retraction_doi'), '')
			FROM read_parquet(?) AS T
		`
		_, err = tx.ExecContext(ctx, query, path)
		if err != nil {
			return 0, fmt.Errorf("insert parquet retractionwatch %s: %w", path, err)
		}
	}

	lg.Info("ingested parquet retractionwatch", "file", path, "rows", count)
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

// ingestFileDenormalized writes one row per NDJSON line from path, using the
// prepared statement which accepts the record value followed by extraction
// expressions.  The record value is passed to every placeholder so the DB
// engine can evaluate the json_extract calls against it.
func ingestFileDenormalized(ctx context.Context, insert *sql.Stmt, path string, numArgs int) (int, error) {
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
		lineStr := string(line)
		args := make([]any, 0, numArgs)
		for i := 0; i < numArgs; i++ {
			args = append(args, lineStr)
		}
		if _, err := insert.ExecContext(ctx, args...); err != nil {
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

// ensureNauvisTable ensures the nauvis table exists with denormalized columns.
func ensureNauvisTable(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS nauvis (
			record JSON,
			croid TEXT,
			doi TEXT,
			funder_names JSON,
			affiliation_names JSON
		)
	`); err != nil {
		return fmt.Errorf("create nauvis table: %w", err)
	}
	return nil
}

// ensureRorTable ensures the ror table exists with denormalized columns.
func ensureRorTable(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS ror (
			record JSON,
			ror_id TEXT,
			country TEXT,
			status TEXT,
			names JSON
		)
	`); err != nil {
		return fmt.Errorf("create ror table: %w", err)
	}
	return nil
}

// ensureRetractionWatchTable ensures the retractionwatch table exists with denormalized columns.
func ensureRetractionWatchTable(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS retractionwatch (
			record JSON,
			original_paper_doi TEXT,
			retraction_doi TEXT
		)
	`); err != nil {
		return fmt.Errorf("create retractionwatch table: %w", err)
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

// resetNauvisTable ensures the nauvis table exists with denormalized columns
// and empties it.
func resetNauvisTable(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS nauvis`); err != nil {
		return fmt.Errorf("drop nauvis: %w", err)
	}
	return ensureNauvisTable(ctx, tx)
}

// resetRorTable ensures the ror table exists with denormalized columns
// and empties it.
func resetRorTable(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS ror`); err != nil {
		return fmt.Errorf("drop ror: %w", err)
	}
	return ensureRorTable(ctx, tx)
}

// resetRetractionWatchTable ensures the retractionwatch table exists with
// denormalized columns and empties it.
func resetRetractionWatchTable(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS retractionwatch`); err != nil {
		return fmt.Errorf("drop retractionwatch: %w", err)
	}
	return ensureRetractionWatchTable(ctx, tx)
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

	var total int
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin txn: %w", err)
	}
	defer tx.Rollback()

	if err := resetNauvisTable(ctx, tx); err != nil {
		return 0, fmt.Errorf("reset nauvis table: %w", err)
	}

	if useParquet {
		records, err := vis.ParquetFiles(outDir)
		if err != nil {
			return 0, fmt.Errorf("list parquet files: %w", err)
		}
		for _, rec := range records {
			n, err := ingestParquetNauvis(ctx, tx, rec.Croid, rec.Path, lg)
			if err != nil {
				return 0, fmt.Errorf("ingest %s: %w", rec.Path, err)
			}
			total += n
		}
	} else {
		// Build DOI->CROID lookup for non-parquet ingest.
		croids, err := vis.Croids()
		if err != nil {
			return 0, fmt.Errorf("lookup croids: %w", err)
		}

		n, err := ingestNDJSONWithCroids(ctx, tx, outDir, croids, lg)
		if err != nil {
			return 0, err
		}
		total += n
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}
	return total, nil
}

// ingestNDJSONWithCroids reads all nauvis NDJSON files from outDir, extracts
// each record's DOI, looks up its CROID from the croids map, and inserts it
// into the nauvis DuckDB table with denormalized columns.
func ingestNDJSONWithCroids(ctx context.Context, tx *sql.Tx, outDir string, croids map[string]string, lg *slog.Logger) (int, error) {
	files, err := os.ReadDir(outDir)
	if err != nil {
		return 0, fmt.Errorf("read dir %s: %w", outDir, err)
	}

	insertStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO nauvis (record, croid, doi, funder_names, affiliation_names)
		VALUES (?, ?, ?, json_extract_string(?, '$.funder[*].name'), json_extract_string(?, '$.author[*].affiliation[*].name'))
	`)
	if err != nil {
		return 0, fmt.Errorf("prepare insert: %w", err)
	}
	defer insertStmt.Close()

	var total int
	for _, fi := range files {
		if fi.Type()&os.ModeType != 0 {
			continue
		}
		if !strings.HasSuffix(fi.Name(), ".json") {
			continue
		}

		path := filepath.Join(outDir, fi.Name())
		n, err := ingestFileWithCroids(ctx, insertStmt, path, croids)
		if err != nil {
			return 0, fmt.Errorf("ingest %s: %w", fi.Name(), err)
		}
		total += n
		lg.Info("ingested NDJSON", "file", fi.Name(), "rows", n)
	}
	return total, nil
}

// ingestFileWithCroids reads an NDJSON file, extracts each record's DOI,
// looks up its CROID, and inserts into the prepared statement.
func ingestFileWithCroids(ctx context.Context, insert *sql.Stmt, path string, croids map[string]string) (int, error) {
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
		lineStr := string(line)

		// Extract DOI to look up CROID.
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(line, &raw); err != nil {
			continue
		}
		var croid string
		if doiRaw, ok := raw["DOI"]; ok {
			var doi string
			if err := json.Unmarshal(doiRaw, &doi); err == nil && doi != "" {
				if c, ok := croids[doi]; ok {
					croid = c
				}
			}
		}

		if _, err := insert.ExecContext(ctx, lineStr, croid, lineStr, lineStr, lineStr); err != nil {
			return total, fmt.Errorf("bind line %d of %s: %w", total+1, filepath.Base(path), err)
		}
		total++
	}
	if err := scanner.Err(); err != nil {
		return total, fmt.Errorf("read %s: %w", filepath.Base(path), err)
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
		switch src.Name() {
		case "ror":
			if err := resetRorTable(ctx, tx); err != nil {
				return 0, fmt.Errorf("reset ror table: %w", err)
			}

			if useParquet {
				n, err := ingestParquetRor(ctx, tx, src.ParquetPath(), lg)
				if err != nil {
					return 0, fmt.Errorf("ingest ror parquet: %w", err)
				}
				total += n
			} else {
				insertStmt, err := tx.PrepareContext(ctx, `
					INSERT INTO ror (record, ror_id, country, status, names)
					VALUES (?,
						json_extract_string(?, '$.id'),
						json_extract_string(?, '$.locations[0].geonames_details.country_code'),
						json_extract_string(?, '$.status'),
						CAST(json_extract(?, '$.names[*].value') AS JSON)
					)
				`)
				if err != nil {
					return 0, fmt.Errorf("prepare ror insert: %w", err)
				}
				n, err := ingestFileDenormalized(ctx, insertStmt, src.OutputPath(), 5)
				insertStmt.Close()
				if err != nil {
					return 0, fmt.Errorf("ingest ror: %w", err)
				}
				total += n
			}

		case "retractionwatch":
			if err := resetRetractionWatchTable(ctx, tx); err != nil {
				return 0, fmt.Errorf("reset retractionwatch table: %w", err)
			}

			if useParquet {
				n, err := ingestParquetRetractionWatch(ctx, tx, src.ParquetPath(), lg)
				if err != nil {
					return 0, fmt.Errorf("ingest retractionwatch parquet: %w", err)
				}
				total += n
			} else {
				insertStmt, err := tx.PrepareContext(ctx, `
					INSERT INTO retractionwatch (record, original_paper_doi, retraction_doi)
					VALUES (?,
						json_extract_string(?, '$.original_paper_doi'),
						json_extract_string(?, '$.retraction_doi')
					)
				`)
				if err != nil {
					return 0, fmt.Errorf("prepare retractionwatch insert: %w", err)
				}
				n, err := ingestFileDenormalized(ctx, insertStmt, src.OutputPath(), 3)
				insertStmt.Close()
				if err != nil {
					return 0, fmt.Errorf("ingest retractionwatch: %w", err)
				}
				total += n
			}

		default:
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
			insertStmt.Close()
			if err != nil {
				return 0, fmt.Errorf("ingest %s: %w", src.Name(), err)
			}
			total += n
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}
	return total, nil
}
