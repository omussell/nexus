// DuckDB-based ROR organization client that queries the `ror` table where
// vulcanus stores full ROR JSON.  All fields are extracted via
// json_extract_string so no schema migration is needed.
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
	ID        string
	Country   string
	Status    string
	Primary   string
	AllNames  string
	Names     []string
	Acronyms  []string
	Descendents []string
}

// Client wraps DuckDB-based ROR organization access, reading from the `ror`
// table that vulcanus populates via the Fulgora ROR source.
type Client struct {
	db *sql.DB
	mu sync.RWMutex
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
// the given keyword (case-insensitive).
func (c *Client) Query(keyword string, max int) []Org {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if keyword == "" {
		return nil
	}

	q := `
		SELECT
			json_extract_string(record, '$.id') AS id,
			COALESCE(json_extract_string(record, '$.country'), '') AS country,
			COALESCE(json_extract_string(record, '$.status'), '') AS status,
			CAST(json_extract(record, '$.names[*].value') AS VARCHAR) AS name_values
		FROM ror
		WHERE LOWER(array_to_string(json_extract_string(record, '$.names[*].value'), ',')) LIKE LOWER(?)
		LIMIT ?
	`
	rows, err := c.db.Query(q, keyword+"%", max)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var orgs []Org
	for rows.Next() {
		var org Org
		if err := rows.Scan(&org.ID, &org.Country, &org.Status, &org.AllNames); err != nil {
			continue // skip bad rows
		}
		// Parse AllNames (JSON array string like '["A","B"]') into Names slice.
		if org.AllNames != "" && org.AllNames != "[]" {
			if err := json.Unmarshal([]byte(org.AllNames), &org.Names); err != nil {
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
