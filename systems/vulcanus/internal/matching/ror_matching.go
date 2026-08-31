// Package ror_matching scans the items table of a DuckDB database, reading each
// record's DOI value out of the stored JSON payload and writing the collected
// DOIs into a dedicated table.
//
// Every row of items holds a single record column with the raw JSON a provider
// emitted. In that JSON the DOI key identifies the resource, with values shaped
// like 10.5555/12345678 (the doi.org prefix, when present, is stripped). This
// package assumes every record has a DOI key; scanning simply gathers those
// values.
package matching

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/duckdb/duckdb-go/v2"
)

// ror_matching is the DuckDB table the DOIs are written to, quoted so it stays
// a valid DuckDB table identifier in the DDL.
const ror_matching = `"ror_matching"`

// errDoiKey is returned when a record has no DOI key to extract.
var errDoiKey = errors.New("ror_matching: record missing DOI key")

// Run scans the items table in outDB, extracting each record's DOI and writing
// them into the ror_matching table. The ror_matching table is dropped and
// recreated on every run, so it holds exactly the DOIs from the latest call.
// Run returns the number of DOI values written.
func Run(ctx context.Context, outDB string) (int, error) {
	conn, err := duckdb.NewConnector(outDB, nil)
	if err != nil {
		return 0, err
	}
	defer conn.Close()
	db := sql.OpenDB(conn)
	defer db.Close()

	rows, err := db.QueryContext(ctx, "SELECT record FROM items")
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	doiValues, err := extractAllDOIs(rows)
	if err != nil {
		return 0, err
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, "DROP TABLE IF EXISTS "+ror_matching); err != nil {
		return 0, err
	}

	if _, err := tx.ExecContext(ctx, `CREATE TABLE `+ror_matching+` (doi VARCHAR)`); err != nil {
		return 0, err
	}

	stmt, err := tx.PrepareContext(ctx, "INSERT INTO "+ror_matching+" (doi) VALUES (?)")
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	for _, v := range doiValues {
		if _, err := stmt.ExecContext(ctx, v); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(doiValues), nil
}

// extractAllDOIs scans every record row and returns its DOI values, skipping
// rows that cannot be parsed.
func extractAllDOIs(rows *sql.Rows) ([]string, error) {
	var out []string
	for rows.Next() {
		var record interface{}
		if err := rows.Scan(&record); err != nil {
			return nil, err
		}
		doi, err := extractDOI(record)
		if err != nil {
			return nil, err
		}
		out = append(out, doi)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// extractDOI returns the DOI held in the decoded record value. Each record has
// a DOI key by assumption.
func extractDOI(record interface{}) (string, error) {
	m, ok := record.(map[string]interface{})
	if !ok {
		return "", errDoiKey
	}
	value, ok := m["DOI"].(string)
	if !ok {
		return "", errDoiKey
	}
	return cleanDOI(value), nil
}

// cleanDOI trims a leading https://doi (or bare doi) prefix and any surrounding
// ASCII whitespace from a DOI value before it is stored.
func cleanDOI(value string) string {
	for _, prefix := range []string{"https://doi.org/", "http://doi.org/", "doi.org/"} {
		if !strings.HasPrefix(value, prefix) {
			continue
		}
		return strings.TrimSpace(value[len(prefix):])
	}
	return strings.TrimSpace(value)
}
