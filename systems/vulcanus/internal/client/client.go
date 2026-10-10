// Package client provides a typed reader for the DuckDB database produced by
// the vulcanus system.  It is intended to be imported by other Nexus systems
// so they can query ingested nauvis/fulgora records, ROR organizations,
// retraction notices, and ROR match results without touching the raw files.
package client

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/duckdb/duckdb-go/v2"
)

// Client reads data from a vulcanus DuckDB database.  It holds an open
// connection to the underlying database and should be closed when no longer
// needed.
type Client struct {
	db *sql.DB
}

// Open opens the DuckDB database at path for reading and returns a Client.
func Open(path string) (*Client, error) {
	conn, err := duckdb.NewConnector(path, nil)
	if err != nil {
		return nil, fmt.Errorf("open duckdb %s: %w", path, err)
	}
	return &Client{db: sql.OpenDB(conn)}, nil
}

// NewFromDB creates a client from an existing *sql.DB.  The caller retains
// ownership of the database and must close it.
func NewFromDB(db *sql.DB) *Client {
	return &Client{db: db}
}

// Close releases the underlying database connection.
func (c *Client) Close() error {
	return c.db.Close()
}

// --- Nauvis tables ---

// NauvisRecord holds one row from the nauvis table: a Crossref paper record
// plus denormalized fields extracted from the raw JSON.
type NauvisRecord struct {
	Record           string   // full Crossref JSON
	DOI              string
	FunderNames      []string
	AffiliationNames []string
}

// CountNauvis returns the number of rows in the nauvis table.
func (c *Client) CountNauvis(ctx context.Context) (int, error) {
	var n int
	err := c.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM nauvis").Scan(&n)
	return n, err
}

// ListNauvisDOIs returns every DOI stored in the nauvis table, ordered.
func (c *Client) ListNauvisDOIs(ctx context.Context) ([]string, error) {
	rows, err := c.db.QueryContext(ctx, "SELECT doi FROM nauvis ORDER BY doi")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var dois []string
	for rows.Next() {
		var doi string
		if err := rows.Scan(&doi); err != nil {
			return nil, err
		}
		dois = append(dois, doi)
	}
	return dois, rows.Err()
}

// NauvisByDOI returns the nauvis record for a single DOI, or sql.ErrNoRows if
// the DOI is not found.
func (c *Client) NauvisByDOI(ctx context.Context, doi string) (NauvisRecord, error) {
	var rec NauvisRecord
	var funderRaw, affilRaw string
	err := c.db.QueryRowContext(ctx, `
		SELECT CAST(record AS VARCHAR), doi,
		       CASE WHEN funder_names IS NULL THEN '' ELSE CAST(funder_names AS VARCHAR) END,
		       CASE WHEN affiliation_names IS NULL THEN '' ELSE CAST(affiliation_names AS VARCHAR) END
		FROM nauvis WHERE doi = ?
	`, doi).Scan(&rec.Record, &rec.DOI, &funderRaw, &affilRaw)
	if err != nil {
		return rec, err
	}
	rec.FunderNames = parseJSONStrings(funderRaw)
	rec.AffiliationNames = parseJSONStrings(affilRaw)
	return rec, nil
}

// QueryNauvis returns up to limit nauvis records, starting at offset.  If
// limit is zero all records are returned.  Each record includes the raw JSON
// plus denormalized funder and affiliation name arrays.
func (c *Client) QueryNauvis(ctx context.Context, offset, limit int) ([]NauvisRecord, error) {
	q := `
		SELECT CAST(record AS VARCHAR), doi,
		       CASE WHEN funder_names IS NULL THEN '' ELSE CAST(funder_names AS VARCHAR) END,
		       CASE WHEN affiliation_names IS NULL THEN '' ELSE CAST(affiliation_names AS VARCHAR) END
		FROM nauvis
		ORDER BY doi`
	if limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", limit)
		if offset > 0 {
			q += fmt.Sprintf(" OFFSET %d", offset)
		}
	}

	rows, err := c.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []NauvisRecord
	for rows.Next() {
		var rec NauvisRecord
		var funderRaw, affilRaw string
		if err := rows.Scan(&rec.Record, &rec.DOI, &funderRaw, &affilRaw); err != nil {
			return nil, err
		}
		rec.FunderNames = parseJSONStrings(funderRaw)
		rec.AffiliationNames = parseJSONStrings(affilRaw)
		records = append(records, rec)
	}
	return records, rows.Err()
}

// --- ROR table ---

// ROROrg holds one row from the ror table: a ROR organization record plus
// denormalized fields.
type ROROrg struct {
	Record  string   // full ROR JSON
	RORID   string
	Country string
	Status  string
	Names   []string
}

// CountROR returns the number of rows in the ror table.
func (c *Client) CountROR(ctx context.Context) (int, error) {
	var n int
	err := c.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM ror").Scan(&n)
	return n, err
}

// RORByID returns the ROR organization record for a given ROR ID, or
// sql.ErrNoRows if not found.
func (c *Client) RORByID(ctx context.Context, rorID string) (ROROrg, error) {
	var rec ROROrg
	var namesRaw string
	err := c.db.QueryRowContext(ctx, `
		SELECT CAST(record AS VARCHAR), ror_id, COALESCE(country, ''), COALESCE(status, ''),
		       CASE WHEN names IS NULL THEN '[]' ELSE CAST(names AS VARCHAR) END
		FROM ror WHERE ror_id = ?
	`, rorID).Scan(&rec.Record, &rec.RORID, &rec.Country, &rec.Status, &namesRaw)
	if err != nil {
		return rec, err
	}
	rec.Names = parseJSONStrings(namesRaw)
	return rec, nil
}

// QueryROR searches ROR organizations by keyword across all name fields,
// returning up to max results.  When country is non-empty it adds a country
// filter.  Results exclude withdrawn organizations.
func (c *Client) QueryROR(ctx context.Context, keyword string, country string, max int) ([]ROROrg, error) {
	where := "LOWER(array_to_string(names::VARCHAR, ',')) LIKE LOWER(?) AND status <> 'withdrawn'"
	args := []any{"%" + keyword + "%"}
	if country != "" {
		where += " AND country = ?"
		args = append(args, country)
	}
	args = append(args, max)

	q := `
		SELECT CAST(record AS VARCHAR), ror_id, COALESCE(country, ''), COALESCE(status, ''),
		       CASE WHEN names IS NULL THEN '[]' ELSE CAST(names AS VARCHAR) END
		FROM ror
		WHERE ` + where + `
		LIMIT ?
	`

	rows, err := c.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var orgs []ROROrg
	for rows.Next() {
		var rec ROROrg
		var namesRaw string
		if err := rows.Scan(&rec.Record, &rec.RORID, &rec.Country, &rec.Status, &namesRaw); err != nil {
			continue
		}
		rec.Names = parseJSONStrings(namesRaw)
		if len(rec.Names) == 0 {
			continue
		}
		orgs = append(orgs, rec)
	}
	return orgs, rows.Err()
}

// AllROROrgs returns every non-withdrawn ROR organization.  Use with caution
// on large datasets.
func (c *Client) AllROROrgs(ctx context.Context) ([]ROROrg, error) {
	rows, err := c.db.QueryContext(ctx, `
		SELECT CAST(record AS VARCHAR), ror_id, COALESCE(country, ''), COALESCE(status, ''),
		       CASE WHEN names IS NULL THEN '[]' ELSE CAST(names AS VARCHAR) END
		FROM ror WHERE status <> 'withdrawn'
		ORDER BY ror_id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var orgs []ROROrg
	for rows.Next() {
		var rec ROROrg
		var namesRaw string
		if err := rows.Scan(&rec.Record, &rec.RORID, &rec.Country, &rec.Status, &namesRaw); err != nil {
			continue
		}
		rec.Names = parseJSONStrings(namesRaw)
		if len(rec.Names) == 0 {
			continue
		}
		orgs = append(orgs, rec)
	}
	return orgs, rows.Err()
}

// --- RetractionWatch table ---

// RetractionRecord holds one row from the retractionwatch table.
type RetractionRecord struct {
	Record        string
	OriginalPaperDOI string
	RetractionDOI   string
}

// CountRetractions returns the number of rows in the retractionwatch table.
func (c *Client) CountRetractions(ctx context.Context) (int, error) {
	var n int
	err := c.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM retractionwatch").Scan(&n)
	return n, err
}

// RetractionsByDOIs returns all retraction records matching any of the given
// DOIs, and sql.ErrNoRows when none match.
func (c *Client) RetractionsByDOI(ctx context.Context, doi string) ([]RetractionRecord, error) {
	rows, err := c.db.QueryContext(ctx, `
		SELECT CAST(record AS VARCHAR), original_paper_doi, retraction_doi
		FROM retractionwatch WHERE original_paper_doi = ?
	`, doi)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []RetractionRecord
	for rows.Next() {
		var rec RetractionRecord
		if err := rows.Scan(&rec.Record, &rec.OriginalPaperDOI, &rec.RetractionDOI); err != nil {
			continue
		}
		records = append(records, rec)
	}
	return records, rows.Err()
}

// QueryRetractions returns up to limit retraction records starting at offset.
func (c *Client) QueryRetractions(ctx context.Context, offset, limit int) ([]RetractionRecord, error) {
	q := `
		SELECT CAST(record AS VARCHAR), original_paper_doi, retraction_doi
		FROM retractionwatch
		ORDER BY original_paper_doi`
	if limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", limit)
		if offset > 0 {
			q += fmt.Sprintf(" OFFSET %d", offset)
		}
	}

	rows, err := c.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []RetractionRecord
	for rows.Next() {
		var rec RetractionRecord
		if err := rows.Scan(&rec.Record, &rec.OriginalPaperDOI, &rec.RetractionDOI); err != nil {
			continue
		}
		records = append(records, rec)
	}
	return records, rows.Err()
}

// --- ROR matches tables ---

// RORMatch holds one row from the ror_funder_matches or ror_affiliation_matches table.
type RORMatch struct {
	PaperDOI   string
	Name       string   // original funder/affiliation name string
	RORID      string
	RORName    string
	Confidence float64
}

// CountFunderMatches returns the number of rows in ror_funder_matches.
func (c *Client) CountFunderMatches(ctx context.Context) (int, error) {
	var n int
	err := c.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM ror_funder_matches").Scan(&n)
	return n, err
}

// FunderMatchesByDOI returns all funder matches for a given paper DOI.
func (c *Client) FunderMatchesByDOI(ctx context.Context, doi string) ([]RORMatch, error) {
	return c.queryMatches(ctx, "ror_funder_matches", doi)
}

// CountAffiliationMatches returns the number of rows in ror_affiliation_matches.
func (c *Client) CountAffiliationMatches(ctx context.Context) (int, error) {
	var n int
	err := c.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM ror_affiliation_matches").Scan(&n)
	return n, err
}

// AffiliationMatchesByDOI returns all affiliation matches for a given paper DOI.
func (c *Client) AffiliationMatchesByDOI(ctx context.Context, doi string) ([]RORMatch, error) {
	return c.queryMatches(ctx, "ror_affiliation_matches", doi)
}

// QueryFunderMatches returns up to limit funder match rows starting at offset.
func (c *Client) QueryFunderMatches(ctx context.Context, offset, limit int) ([]RORMatch, error) {
	return c.queryAllMatches(ctx, "ror_funder_matches", offset, limit)
}

// QueryAffiliationMatches returns up to limit affiliation match rows starting at offset.
func (c *Client) QueryAffiliationMatches(ctx context.Context, offset, limit int) ([]RORMatch, error) {
	return c.queryAllMatches(ctx, "ror_affiliation_matches", offset, limit)
}

// --- Retractions view ---

// RetractionView holds one row from the retractions view, which joins nauvis
// and retractionwatch tables.
type RetractionView struct {
	NauvisRecord       string // full nauvis JSON for the paper
	RetractionWatchRecord string // full retractionwatch JSON
	RetractionNoticeDOI string
}

// QueryRetractionsView returns retractions join records (nauvis + retractionwatch).
func (c *Client) QueryRetractionsView(ctx context.Context, offset, limit int) ([]RetractionView, error) {
	q := `
		SELECT CAST(nauvis_record AS VARCHAR), CAST(retractionwatch_record AS VARCHAR), retraction_notice_doi
		FROM retractions
		ORDER BY nauvis_record`
	if limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", limit)
		if offset > 0 {
			q += fmt.Sprintf(" OFFSET %d", offset)
		}
	}

	rows, err := c.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []RetractionView
	for rows.Next() {
		var rec RetractionView
		if err := rows.Scan(&rec.NauvisRecord, &rec.RetractionWatchRecord, &rec.RetractionNoticeDOI); err != nil {
			continue
		}
		records = append(records, rec)
	}
	return records, rows.Err()
}

// --- Provenance view ---

// ProvenanceRow holds one row from the retraction_provenance view.
type ProvenanceRow struct {
	NauvisRecord        string
	RetractionWatchRecord string
	PaperDOI            string
	RetractionNoticeDOI string
	Provenance          string
}

// QueryProvenance returns all rows from the retraction_provenance view.
func (c *Client) QueryProvenance(ctx context.Context, offset, limit int) ([]ProvenanceRow, error) {
	q := `
		SELECT CAST(nauvis_record AS VARCHAR), CAST(retractionwatch_record AS VARCHAR), paper_doi, retraction_notice_doi, provenance
		FROM retraction_provenance
		ORDER BY paper_doi`
	if limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", limit)
		if offset > 0 {
			q += fmt.Sprintf(" OFFSET %d", offset)
		}
	}

	rows, err := c.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rows2 []ProvenanceRow
	for rows.Next() {
		var rec ProvenanceRow
		if err := rows.Scan(&rec.NauvisRecord, &rec.RetractionWatchRecord, &rec.PaperDOI, &rec.RetractionNoticeDOI, &rec.Provenance); err != nil {
			continue
		}
		rows2 = append(rows2, rec)
	}
	return rows2, rows.Err()
}

// --- Metadata helpers ---

// Tables lists all user tables and views in the DuckDB database.
func (c *Client) Tables(ctx context.Context) ([]string, error) {
	rows, err := c.db.QueryContext(ctx, `
		SELECT table_name FROM duckdb_tables()
		WHERE schema_name = 'main' AND temporary = false
		ORDER BY table_name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		tables = append(tables, name)
	}
	return tables, rows.Err()
}

// Count returns the row count for the named table or view.
func (c *Client) Count(ctx context.Context, table string) (int, error) {
	var n int
	err := c.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+quoteIdent(table)).Scan(&n)
	return n, err
}

// --- Internal helpers ---

func (c *Client) queryMatches(ctx context.Context, tableName, doi string) ([]RORMatch, error) {
	rows, err := c.db.QueryContext(ctx, `
		SELECT paper_doi, name, ror_id, ror_name, confidence
		FROM `+quoteIdent(tableName)+` WHERE paper_doi = ?
	`, doi)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var matches []RORMatch
	for rows.Next() {
		var m RORMatch
		if err := rows.Scan(&m.PaperDOI, &m.Name, &m.RORID, &m.RORName, &m.Confidence); err != nil {
			continue
		}
		matches = append(matches, m)
	}
	return matches, rows.Err()
}

func (c *Client) queryAllMatches(ctx context.Context, tableName string, offset, limit int) ([]RORMatch, error) {
	q := `
		SELECT paper_doi, name, ror_id, ror_name, confidence
		FROM ` + quoteIdent(tableName) + `
		ORDER BY paper_doi`
	if limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", limit)
		if offset > 0 {
			q += fmt.Sprintf(" OFFSET %d", offset)
		}
	}

	rows, err := c.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var matches []RORMatch
	for rows.Next() {
		var m RORMatch
		if err := rows.Scan(&m.PaperDOI, &m.Name, &m.RORID, &m.RORName, &m.Confidence); err != nil {
			continue
		}
		matches = append(matches, m)
	}
	return matches, rows.Err()
}

func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func parseJSONStrings(s string) []string {
	if s == "" || s == "[]" {
		return nil
	}
	var elements []string
	if err := json.Unmarshal([]byte(s), &elements); err != nil {
		return nil
	}
	// Deduplicate while preserving order.
	seen := make(map[string]bool)
	var result []string
	for _, el := range elements {
		trimmed := strings.TrimSpace(el)
		if trimmed != "" && !seen[trimmed] {
			seen[trimmed] = true
			result = append(result, trimmed)
		}
	}
	return result
}
