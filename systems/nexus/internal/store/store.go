package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/nexus/nexus/internal/db"
)

// Relationship represents a single relationship record.
type Relationship struct {
	ID           int64
	Source       string
	Target       string
	Relationship string
	Evidence     string
	CreatedAt    time.Time
}

// Store is the persistence backend for relationships. Safe for concurrent use:
// the underlying *sql.DB is serialized by SQLite (WAL + single-writer).
type Store struct {
	q *db.Queries
}

// New builds a Store over the given sqlc Queries handle.
func New(d *sql.DB, q *db.Queries) *Store {
	return &Store{q: q}
}

// Insert adds a new relationship and returns its ID.
func (s *Store) Insert(ctx context.Context, r Relationship) (int64, error) {
	if r.Source == "" || r.Target == "" || r.Relationship == "" {
		return 0, sql.ErrConnDone
	}
	now := time.Now().UTC().Format(time.RFC3339)
	evidence := sql.NullString{String: r.Evidence, Valid: r.Evidence != ""}
	res, err := s.q.InsertRelationship(ctx, db.InsertRelationshipParams{
		Source:       r.Source,
		Target:       r.Target,
		Relationship: r.Relationship,
		Evidence:     evidence,
		CreatedAt:    now,
	})
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// GetByTarget looks up a single relationship by target and returns it.
func (s *Store) GetByTarget(ctx context.Context, target string) (Relationship, error) {
	row, err := s.q.GetRelationshipByTarget(ctx, target)
	if err != nil {
		return Relationship{}, err
	}
	return rowToRel(row), nil
}

// ListBySource returns all relationships for a given source, ordered by ID.
func (s *Store) ListBySource(ctx context.Context, source string) ([]Relationship, error) {
	rows, err := s.q.ListBySource(ctx, source)
	if err != nil {
		return nil, err
	}
	out := make([]Relationship, len(rows))
	for i, r := range rows {
		out[i] = rowToRel(r)
	}
	return out, nil
}

// ListByRelationship returns all relationships matching the given type.
func (s *Store) ListByRelationship(ctx context.Context, rel string) ([]Relationship, error) {
	rows, err := s.q.ListByRelationship(ctx, rel)
	if err != nil {
		return nil, err
	}
	out := make([]Relationship, len(rows))
	for i, r := range rows {
		out[i] = rowToRel(r)
	}
	return out, nil
}

// ListAll returns every relationship, ordered by ID.
func (s *Store) ListAll(ctx context.Context) ([]Relationship, error) {
	rows, err := s.q.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Relationship, len(rows))
	for i, r := range rows {
		out[i] = rowToRel(r)
	}
	return out, nil
}

func rowToRel(r db.Relationship) Relationship {
	t, _ := time.Parse(time.RFC3339, r.CreatedAt)
	rel := Relationship{
		ID:         r.ID,
		Source:     r.Source,
		Target:     r.Target,
		Relationship: r.Relationship,
		CreatedAt:  t,
	}
	if r.Evidence.Valid {
		rel.Evidence = r.Evidence.String
	}
	return rel
}
