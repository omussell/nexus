// DuckDB-based ROR organization client that queries the `ror` table where
// vulcanus stores full ROR JSON.  All fields are extracted via
// json_extract_string so no schema migration is needed.
package match

import (
	"database/sql"
	"fmt"
	"strings"
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
func Reopen(dbPath string) *Client {
	conn, err := duckdb.NewConnector(dbPath, nil)
	if err != nil {
		panic(fmt.Errorf("open DuckDB at %s: %w", dbPath, err))
	}
	db := sql.OpenDB(conn)
	return &Client{db: db}
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

	// Use DuckDB's JSON functions to extract fields from the `ror` table.
	// json_extract_string(record, '$.names[*].name') collects all name variants.
	q := `
		SELECT
			json_extract_string(record, '$.id') AS id,
			json_extract_string(record, '$.country') AS country,
			json_extract_string(record, '$.status') AS status,
			json_extract_string(record, '$.primary.name') AS primary_name,
			COALESCE(
				json_extract_string(record, '$.primary.name') ||
				COALESCE(',' || json_extract_string(record, '$.names[*].name'), '')
			) AS all_names
		FROM ror
		WHERE LOWER(json_extract_string(record, '$.primary.name')) LIKE LOWER(?)
		   OR LOWER(COALESCE(
				json_extract_string(record, '$.primary.name') ||
				COALESCE(',' || json_extract_string(record, '$.names[*].name'), '')
			   )) LIKE LOWER(?)
		LIMIT ?
	`
	rows, err := c.db.Query(q, keyword+"%", keyword+"%", max)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var orgs []Org
	for rows.Next() {
		var org Org
		if err := rows.Scan(&org.ID, &org.Country, &org.Status, &org.Primary, &org.AllNames); err != nil {
			return orgs // skip bad rows
		}
		// Split AllNames into Names slice.
		if org.AllNames != "" {
			for _, n := range strings.Split(org.AllNames, ",") {
				trimmed := strings.TrimSpace(n)
				if trimmed != "" {
					org.Names = append(org.Names, trimmed)
				}
			}
		}
		orgs = append(orgs, org)
	}
	return orgs
}

// Close releases the underlying database connection.
func (c *Client) Close() error {
	return c.db.Close()
}
