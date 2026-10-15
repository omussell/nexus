package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	_ "modernc.org/sqlite"

	"github.com/nexus/vulcanus/client"
)

// NauvisQuery provides access to nauvis records from a vulcanus DuckDB database.
type NauvisQuery struct {
	client *client.Client
}

// NewNauvisQuery opens a vulcanus client for the given DuckDB path.
func NewNauvisQuery(duckdbPath string) (*NauvisQuery, error) {
	c, err := client.Open(duckdbPath)
	if err != nil {
		return nil, fmt.Errorf("open duckdb %s: %w", duckdbPath, err)
	}
	return &NauvisQuery{client: c}, nil
}

// Close releases the DuckDB connection.
func (n *NauvisQuery) Close() error {
	return n.client.Close()
}

// Count returns the nauvis row count.
func (n *NauvisQuery) Count(ctx context.Context) (int, error) {
	return n.client.CountNauvis(ctx)
}

// Record returns the nauvis record for a CROID, or sql.ErrNoRows.
func (n *NauvisQuery) Record(ctx context.Context, croid string) (*client.NauvisRecord, error) {
	rec, err := n.client.NauvisByCroid(ctx, croid)
	return &rec, err
}

// RelationshipQuery provides access to relationship triples from nexus SQLite.
type RelationshipQuery struct {
	conn *sql.DB
}

// NewRelationshipQuery opens the nexus SQLite database.
func NewRelationshipQuery(dbPath string) (*RelationshipQuery, error) {
	dsn := "file:" + dbPath + "?_busy_timeout=5000"
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %s: %w", dbPath, err)
	}
	conn.SetMaxOpenConns(1)
	if err := conn.PingContext(context.Background()); err != nil {
		conn.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	return &RelationshipQuery{conn: conn}, nil
}

// Close releases the SQLite connection.
func (r *RelationshipQuery) Close() error {
	return r.conn.Close()
}

// Relationship mirrors nexus's relEntry: the queried CROID, the relationship
// type, the other CROID involved, and the created_at timestamp.
type Relationship struct {
	Croid        string `json:"croid"`
	Relationship string `json:"relationship"`
	RelatedCroid string `json:"related_croid"`
	CreatedAt    string `json:"created_at"`
}

// ByCroid returns all relationship entries where the given CROID participates.
// Matches the nexus response format: croid is always the queried CROID,
// related_croid is the other side.
func (r *RelationshipQuery) ByCroid(ctx context.Context, croid string) ([]Relationship, error) {
	var entries []Relationship

	// Outgoing: this CROID is the source.
	rows, err := r.conn.QueryContext(ctx,
		`SELECT sc.croid, rt.relationship, tc.croid, re.created_at
		   FROM relationships re
		   JOIN cro_ids sc ON sc.id = re.source_croid_id
		   JOIN cro_ids tc ON tc.id = re.target_croid_id
		   JOIN relationship_types rt ON rt.id = re.relationship_type_id
		  WHERE sc.croid = ?`, croid)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var e Relationship
		if err := rows.Scan(&e.Croid, &e.Relationship, &e.RelatedCroid, &e.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		entries = append(entries, e)
	}
	rows.Close()

	// Incoming: this CROID is the target.
	rows, err = r.conn.QueryContext(ctx,
		`SELECT tc.croid, rt.relationship, sc.croid, re.created_at
		   FROM relationships re
		   JOIN cro_ids sc ON sc.id = re.source_croid_id
		   JOIN cro_ids tc ON tc.id = re.target_croid_id
		   JOIN relationship_types rt ON rt.id = re.relationship_type_id
		  WHERE tc.croid = ?`, croid)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var e Relationship
		if err := rows.Scan(&e.Croid, &e.Relationship, &e.RelatedCroid, &e.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		entries = append(entries, e)
	}
	rows.Close()

	// Deduplicate (handles self-referential edges).
	seen := make(map[string]bool, len(entries))
	out := make([]Relationship, 0, len(entries))
	for _, e := range entries {
		key := e.Croid + "\x00" + e.Relationship + "\x00" + e.RelatedCroid + "\x00" + e.CreatedAt
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, e)
	}
	return out, nil
}

// Count returns the total number of relationship rows.
func (r *RelationshipQuery) Count(ctx context.Context) (int64, error) {
	var n int64
	err := r.conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM relationships").Scan(&n)
	return n, err
}

// Store bundles the data sources that servidor serves to the HTTP layer.
type Store struct {
	nauvis       *NauvisQuery
	relationships *RelationshipQuery
}

// NewStore opens all required data sources.
func NewStore(duckdbPath, nexusDBPath string) (*Store, error) {
	nq, err := NewNauvisQuery(duckdbPath)
	if err != nil {
		return nil, err
	}
	rq, err := NewRelationshipQuery(nexusDBPath)
	if err != nil {
		return nil, err
	}
	return &Store{nauvis: nq, relationships: rq}, nil
}

// Close releases all connections.
func (s *Store) Close() error {
	var errs []error
	if err := s.nauvis.Close(); err != nil {
		errs = append(errs, err)
	}
	if err := s.relationships.Close(); err != nil {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(toStrings(errs...), "; "))
	}
	return nil
}

// NauvisByCroid returns the nauvis record for a CROID.
func (s *Store) NauvisByCroid(ctx context.Context, croid string) (*client.NauvisRecord, error) {
	return s.nauvis.Record(ctx, croid)
}

// NauvisCount returns the nauvis row count.
func (s *Store) NauvisCount(ctx context.Context) (int, error) {
	return s.nauvis.Count(ctx)
}

// RelationshipsByCroid returns all relationship entries for a CROID.
func (s *Store) RelationshipsByCroid(ctx context.Context, croid string) ([]Relationship, error) {
	return s.relationships.ByCroid(ctx, croid)
}

// RelationshipCount returns the total number of relationship rows.
func (s *Store) RelationshipCount(ctx context.Context) (int64, error) {
	return s.relationships.Count(ctx)
}

func toStrings(errs ...error) []string {
	out := make([]string, len(errs))
	for i, e := range errs {
		out[i] = e.Error()
	}
	return out
}
