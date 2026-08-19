// Package store implements the identity and persistence logic for CROID on
// top of the sqlc-generated DB layer.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nexus/croid/internal/croid"
	"github.com/nexus/croid/internal/db"
)

// Record is a CROID and the research-object identity it was issued for.
type Record struct {
	Croid     string    `json:"croid"`
	CroType   string    `json:"cro_type"`
	CroValue  string    `json:"cro_value"`
	System    string    `json:"system"`
	CreatedAt time.Time `json:"created_at"`
	// Created reports whether this call minted a new CROID (true) versus
	// returned a pre-existing one (false). It drives the HTTP status code
	// (201 vs 200) and is excluded from the JSON body.
	Created bool `json:"-"`
}

// Identity is the (cro_type, cro_value, system) triple a CROID is minted for.
type Identity struct {
	CroType  string
	CroValue string
	System   string
}

// Store issues and looks up CROIDs against a SQLite database. It is safe for
// concurrent use: the underlying *sql.DB and the SQL it issues are serialized
// by SQLite (WAL + single-writer), and Create de-duplicates on the unique
// (cro_type, cro_value, system) constraint.
type Store struct {
	q *db.Queries
}

// New builds a Store over the given sqlc Queries handle.
// New builds a Store over the given sqlc Queries handle.
func New(q *db.Queries) *Store {
	return &Store{q: q}
}

// GetByCroid returns the record identified by croid, or sql.ErrNoRows if it
// does not exist.
func (s *Store) GetByCroid(ctx context.Context, c string) (Record, error) {
	row, err := s.q.GetCroidByCroid(ctx, c)
	if err != nil {
		return Record{}, err
	}
	return toRecord(row), nil
}

// Create returns the CROID for the given identity. If a CROID already exists
// for that identity it is returned with Record.Created false; otherwise a new
// CROID is minted and inserted with Record.Created true.
//
// Create is idempotent across concurrent callers racing on the same identity:
// the loser of the race hits the unique constraint, re-reads the winner's row,
// and returns it, so every caller observes the same CROID.
func (s *Store) Create(ctx context.Context, id Identity) (Record, error) {
	if strings.TrimSpace(id.CroType) == "" {
		return Record{}, errors.New("store: cro_type is required")
	}
	if strings.TrimSpace(id.System) == "" {
		return Record{}, errors.New("store: system is required")
	}
	if strings.TrimSpace(id.CroValue) == "" {
		return Record{}, errors.New("store: cro_value is required")
	}

	// Fast path: an identity already has a CROID.
	if rec, err := s.getByIdentity(ctx, id); err == nil {
		return rec, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Record{}, err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	c := newCROID()

	err := s.q.InsertCroid(ctx, db.InsertCroidParams{
		Croid:     c,
		CroType:   id.CroType,
		CroValue:  id.CroValue,
		System:    id.System,
		CreatedAt: now,
	})
	if err == nil {
		t, _ := time.Parse(time.RFC3339, now)
		return Record{
			Croid:     c,
			CroType:   id.CroType,
			CroValue:  id.CroValue,
			System:    id.System,
			CreatedAt: t,
			Created:   true,
		}, nil
	}

	// A unique-constraint violation means a concurrent Create won the race for
	// this identity. Read the winner's row and return it as existing.
	if isUniqueViolation(err) {
		existing, gerr := s.getByIdentity(ctx, id)
		if gerr != nil {
			return Record{}, fmt.Errorf("store: concurrent create for (%q, %q, %q): %w", id.CroType, id.CroValue, id.System, gerr)
		}
		return existing, nil
	}

	return Record{}, err
}

func (s *Store) getByIdentity(ctx context.Context, id Identity) (Record, error) {
	row, err := s.q.GetCroidByIdentity(ctx, db.GetCroidByIdentityParams{
		CroType:  id.CroType,
		CroValue: id.CroValue,
		System:   id.System,
	})
	if err != nil {
		return Record{}, err
	}
	return toRecord(row), nil
}

// toRecord adapts a db row into a Record.
func toRecord(row db.CroID) Record {
	t, err := time.Parse(time.RFC3339, row.CreatedAt)
	if err != nil {
		t = time.Time{}
	}
	return Record{
		Croid:     row.Croid,
		CroType:   row.CroType,
		CroValue:  row.CroValue,
		System:    row.System,
		CreatedAt: t,
	}
}

// isUniqueViolation reports whether err is a SQLite unique-constraint
// failure (extended error message SQLITE_CONSTRAINT_UNIQUE) or any of the
// generic UNIQUE-family driver errors.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique") || strings.Contains(msg, "sqlite_constraint") || strings.Contains(msg, "constraint failed")
}

// newCROID wraps croid.Generate for testability.
var newCROID = func() string {
	s, err := croid.Generate()
	if err != nil {
		panic(err)
	}
	return s
}
