// Package store implements the item-recording logic for Nauvis on top of the
// sqlc-generated DB layer. It records which items (by DOI) exist in the
// extracted files and lets callers look a DOI back up to its file.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/nexus/nauvis/internal/db"
)

// Item is a DOI and the file it was found in.
type Item struct {
	File string `json:"file"`
	Doi  string `json:"doi"`
}

// Duplicate reports a DOI that was found in more than one file; the database
// keeps the first file recorded for that DOI.
type Duplicate struct {
	Doi  string
	File string
}

// Store records items against a SQLite database via the sqlc Generated code.
// It is safe for concurrent use: every query runs on the shared *sql.DB, which
// SQLite serializes (WAL + single writer).
type Store struct {
	db  *sql.DB
	querier db.Querier
}

// New builds a Store on top of the shared *sql.DB and its sqlc Queries handle.
func New(d *sql.DB, q *db.Queries) *Store {
	return &Store{db: d, querier: q}
}

// RecordMany records every (file, doi) pair inside a single transaction so a
// file's items are stored atomically. A DOI already present in the database is
// skipped and reported in the returned slice (a DOI appearing in more than one
// file is the caller's error); insertion of a brand-new DOI that races with a
// concurrently recorded one is also reported as a duplicate. It returns the
// duplicates and the number of rows inserted.
func (s *Store) RecordMany(ctx context.Context, file string, dois []string) (dupes []Duplicate, inserted int, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("begin: %w", err)
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	q := s.querier.WithTx(tx)
	for _, doi := range dois {
		if doi == "" {
			continue
		}
		if _, gerr := q.GetItemByDOI(ctx, doi); gerr == nil {
			dupes = append(dupes, Duplicate{Doi: doi, File: file})
			continue
		} else if !errors.Is(gerr, sql.ErrNoRows) {
			return nil, 0, fmt.Errorf("record %s: %w", doi, gerr)
		}
		if ierr := q.InsertItem(ctx, db.InsertItemParams{File: file, Doi: doi}); ierr != nil {
			if isUniqueViolation(ierr) {
				dupes = append(dupes, Duplicate{Doi: doi, File: file})
				continue
			}
			return nil, 0, fmt.Errorf("insert %s: %w", doi, ierr)
		}
		inserted++
	}
	if err = tx.Commit(); err != nil {
		return nil, 0, fmt.Errorf("commit %s: %w", file, err)
	}
	return dupes, inserted, nil
}

// GetByDOI returns the file a DOI was found in, or sql.ErrNoRows if it has not
// been recorded.
func (s *Store) GetByDOI(ctx context.Context, doi string) (Item, error) {
	it, err := s.querier.GetItemByDOI(ctx, doi)
	if err != nil {
		return Item{}, err
	}
	return Item{File: it.File, Doi: it.Doi}, nil
}

// isUniqueViolation reports whether err is a SQLite unique-constraint failure
// (extended error message SQLITE_CONSTRAINT_UNIQUE) or any of the generic
// UNIQUE-family driver errors.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique") || strings.Contains(msg, "sqlite_constraint") || strings.Contains(msg, "constraint failed")
}
