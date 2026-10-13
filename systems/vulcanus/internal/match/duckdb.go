// DuckDB-based ROR organization client that queries the `ror` table where
// vulcanus stores full ROR JSON plus denormalized columns (ror_id, country,
// status, names) for efficient matching.
package match

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/duckdb/duckdb-go/v2"
)

// Org represents a ROR organization record extracted from the `ror` table.
type Org struct {
	ID          string
	Country     string
	Status      string
	Primary     string
	AllNames    string
	Names       []string
	Acronyms    []string
	Descendents []string
}

// Client wraps DuckDB-based ROR organization access, reading from the `ror`
// table that vulcanus populates via the Fulgora ROR source.
type Client struct {
	db            *sql.DB
	countryFilter string
	mu            sync.RWMutex
}

// Reopen opens an existing DuckDB database file.
func Reopen(dbPath string) (*Client, error) {
	conn, err := duckdb.NewConnector(dbPath, nil)
	if err != nil {
		return nil, fmt.Errorf("open DuckDB at %s: %w", dbPath, err)
	}
	db := sql.OpenDB(conn)
	return &Client{db: db}, nil
}

// NewClientFromDB creates a client from an existing *sql.DB.
func NewClientFromDB(db *sql.DB) *Client {
	return &Client{db: db}
}

// SetCountryFilter restricts queries to organizations in the given country code.
func (c *Client) SetCountryFilter(country string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.countryFilter = country
}

// Count returns the number of rows in the `ror` table.
func (c *Client) Count() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var n int
	if err := c.db.QueryRow("SELECT COUNT(*) FROM ror").Scan(&n); err != nil {
		return 0
	}
	return n
}

// Query searches the `ror` table for organizations whose name fields contain
// the given keyword (case-insensitive).  When a country filter is set, it is
// applied as an additional WHERE clause to reduce the candidate set before
// fuzzy matching.
func (c *Client) Query(keyword string, max int) []Org {
	c.mu.RLock()
	countryFilter := c.countryFilter
	c.mu.RUnlock()

	if keyword == "" {
		return nil
	}

	// Build WHERE clause using denormalized columns.
	where := "LOWER(array_to_string(names::VARCHAR, ',')) LIKE LOWER(?)"
	if countryFilter != "" {
		where += " AND country = ?"
	}

	q := `
		SELECT
			ror_id AS id,
			COALESCE(country, '') AS country,
			COALESCE(status, '') AS status,
			names::VARCHAR AS names
		FROM ror
		WHERE ` + where + `
		  AND status <> 'withdrawn'
		LIMIT ?
	`

	var args []any
	args = append(args, "%"+keyword+"%")
	if countryFilter != "" {
		args = append(args, countryFilter)
	}
	args = append(args, max)

	rows, err := c.db.Query(q, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var orgs []Org
	for rows.Next() {
		var org Org
		var namesJSON string
		if err := rows.Scan(&org.ID, &org.Country, &org.Status, &namesJSON); err != nil {
			continue // skip bad rows
		}
		// Parse names (JSON array string like '["A","B"]') into Names slice.
		if namesJSON != "" && namesJSON != "[]" {
			if err := json.Unmarshal([]byte(namesJSON), &org.Names); err != nil {
				continue
			}
		}
		if len(org.Names) > 0 {
			orgs = append(orgs, org)
		}
	}
	return orgs
}

// Close releases the underlying database connection.
func (c *Client) Close() error {
	return c.db.Close()
}

// OFRClient wraps DuckDB-based access to the openfunderregistry table,
// used to resolve nauvis funder names to their canonical openfunderregistry
// entries.
type OFRClient struct {
	db *sql.DB
}

// NewOFRClient opens an existing DuckDB database file for querying the
// openfunderregistry table.
func NewOFRClient(dbPath string) (*OFRClient, error) {
	conn, err := duckdb.NewConnector(dbPath, nil)
	if err != nil {
		return nil, fmt.Errorf("open DuckDB at %s: %w", dbPath, err)
	}
	db := sql.OpenDB(conn)
	return &OFRClient{db: db}, nil
}

// NewOFRClientFromDB creates a client from an existing *sql.DB.
func NewOFRClientFromDB(db *sql.DB) *OFRClient {
	return &OFRClient{db: db}
}

// OFRCandidate holds one openfunderregistry entry found for a given search.
type OFRCandidate struct {
	URI  string
	Name string
}

// QueryOFR searches the openfunderregistry table for funder names that
// contain the given keyword (case-insensitive). Returns the best matches up
// to max. The replaced column is excluded — only active openfunderregistry
// records are returned.
func (c *OFRClient) QueryOFR(keyword string, max int) []OFRCandidate {
	if keyword == "" || c.db == nil {
		return nil
	}

	q := `
		SELECT funder_uri, funder_name
		FROM openfunderregistry
		WHERE LOWER(funder_name) LIKE LOWER(?)
		  AND replaced = ''
		LIMIT ?
	`

	rows, err := c.db.Query(q, "%"+keyword+"%", max)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var candidates []OFRCandidate
	for rows.Next() {
		var uri, name string
		if err := rows.Scan(&uri, &name); err != nil {
			continue
		}
		if name == "" {
			continue
		}
		candidates = append(candidates, OFRCandidate{URI: uri, Name: name})
	}
	return candidates
}

// Count returns the number of rows in the openfunderregistry table.
func (c *OFRClient) Count() int {
	if c.db == nil {
		return 0
	}
	var n int
	if err := c.db.QueryRow("SELECT COUNT(*) FROM openfunderregistry WHERE replaced = ''").Scan(&n); err != nil {
		return 0
	}
	return n
}

// MatchOFR resolves a funder name against the openfunderregistry table.
// It performs a LIKE pre-filter, computes fuzzy similarity scores, filters
// by threshold, and returns the best match (or nil if nothing exceeds the
// threshold). The result map contains keys "uri", "name", and "score".
func (c *OFRClient) MatchOFR(input string) []map[string]interface{} {
	if input == "" || c.db == nil {
		return nil
	}

	inputNorm := New(input).Normalized()
	candidates := c.QueryOFR(input, 200)
	if len(candidates) == 0 {
		return nil
	}

	bestIdx := -1
	bestScore := 0.0
	for i, cand := range candidates {
		candNorm := New(cand.Name).Normalized()
		score := PartialRatio(inputNorm, candNorm)
		if score > bestScore {
			bestScore = score
			bestIdx = i
		}
	}
	if bestIdx < 0 || bestScore < 96.0 {
		return nil
	}

	return []map[string]interface{}{
		{
			"uri":    candidates[bestIdx].URI,
			"name":   candidates[bestIdx].Name,
			"score":  bestScore,
			"input":  input,
		},
	}
}

// Close releases the underlying database connection.
func (c *OFRClient) Close() error {
	if c.db == nil {
		return nil
	}
	return c.db.Close()
}
