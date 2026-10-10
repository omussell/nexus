// Package store provides the persistence layer for the Aquilo item store
// on top of the sqlc-generated DB queries.
package store

import (
	"context"
	"database/sql"

	"github.com/nexus/aquilo/internal/db"
)

// Store is the persistence backend for item records. Safe for concurrent
// use: the underlying *sql.DB is serialized by SQLite (WAL + single-writer).
type Store struct {
	db  *sql.DB
	q   *db.Queries
}

// New builds a Store on top of the shared *sql.DB and its sqlc Queries handle.
func New(d *sql.DB, q *db.Queries) *Store {
	return &Store{db: d, q: q}
}

// CountItems returns the number of items in the store.
func (s *Store) CountItems(ctx context.Context) (int, error) {
	n, err := s.q.CountItems(ctx)
	return int(n), err
}

// InsertItem inserts an item with the given croid and record JSON.
func (s *Store) InsertItem(ctx context.Context, croid string, recordJSON string) error {
	return s.q.InsertItem(ctx, db.InsertItemParams{
		Croid:      croid,
		RecordJson: db.JSONB(recordJSON),
	})
}

// InsertItemsBulk inserts a batch of item records.
func (s *Store) InsertItemsBulk(ctx context.Context, croids []string, records []string) error {
	if len(croids) != len(records) {
		return sql.ErrConnDone
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	q := s.q.WithTx(tx)
	for i, croid := range croids {
		if err := q.InsertItem(ctx, db.InsertItemParams{
			Croid:      croid,
			RecordJson: db.JSONB(records[i]),
		}); err != nil {
			if rollErr := tx.Rollback(); rollErr != nil {
				return rollErr
			}
			return err
		}
	}
	return tx.Commit()
}

// GetItemByCroid returns the item record by its CROID,
// or sql.ErrNoRows if not found.
func (s *Store) GetItemByCroid(ctx context.Context, croid string) (db.Item, error) {
	return s.q.GetItemByCroid(ctx, croid)
}

// ListItemCroids returns up to limit item CROIDs starting at offset, ordered by CROID.
func (s *Store) ListItemCroids(ctx context.Context, offset, limit int) ([]string, error) {
	return s.q.ListItemsCroids(ctx, db.ListItemsCroidsParams{
		Limit:  int64(limit),
		Offset: int64(offset),
	})
}

// QueryItems returns item records with their CROID and raw JSON, up to limit
// starting at offset.
func (s *Store) QueryItems(ctx context.Context, offset, limit int) ([]db.Item, error) {
	return s.q.QueryItems(ctx, db.QueryItemsParams{
		Limit:  int64(limit),
		Offset: int64(offset),
	})
}
