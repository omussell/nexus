// Package store provides the persistence layer for the Fulgora dataset
// registry on top of the sqlc-generated DB queries.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nexus/fulgora/internal/db"
)

// Dataset is a single version of a source's data, with its file locations.
type Dataset struct {
	Source       string
	Version      string
	CollectedAt  time.Time // actual collection time (UTC)
	InitialInput string
	Output       string
}

// LatestInfo mirrors the joined latest+datasets row returned by the sqlc query.
type LatestInfo struct {
	Source       string
	Version      string
	CollectedAt  string
	InitialInput string
	Output       string
}

// Store is the persistence backend for dataset metadata. Safe for concurrent
// use: the underlying *sql.DB is serialized by SQLite (WAL + single-writer).
type Store struct {
	q *db.Queries
}

// New builds a Store over the given sqlc Queries handle.
func New(q *db.Queries) *Store {
	return &Store{q: q}
}

// Record inserts a new dataset row and upserts the latest pointer for its
// source, returning the full record.
//
// If the (source, version) pair already exists in datasets, this returns an
// error so callers can detect a duplicate.
func (s *Store) Record(ctx context.Context, d Dataset) (Dataset, error) {
	if strings.TrimSpace(d.Source) == "" {
		return Dataset{}, errors.New("store: source is required")
	}
	if strings.TrimSpace(d.Version) == "" {
		return Dataset{}, errors.New("store: version is required")
	}

	// Guard against duplicate (source, version).
	_, err := s.q.GetDataset(ctx, db.GetDatasetParams{
		Source:  d.Source,
		Version: d.Version,
	})
	if err == nil {
		return Dataset{}, fmt.Errorf("store: (%s, %s) already recorded", d.Source, d.Version)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Dataset{}, err
	}

	collectedAt := d.CollectedAt.UTC().Format(time.RFC3339)

	res, err := s.q.InsertDataset(ctx, db.InsertDatasetParams{
		Source:       d.Source,
		Version:      d.Version,
		CollectedAt:  collectedAt,
		InitialInput: d.InitialInput,
		Output:       d.Output,
	})
	if err != nil {
		return Dataset{}, err
	}

	datasetID, err := res.LastInsertId()
	if err != nil {
		return Dataset{}, err
	}

	if err := s.q.UpsertLatest(ctx, db.UpsertLatestParams{
		Source:    d.Source,
		Version:   d.Version,
		DatasetID: datasetID,
	}); err != nil {
		return Dataset{}, fmt.Errorf("store: upsert latest: %w", err)
	}

	return Dataset{
		Source:       d.Source,
		Version:      d.Version,
		CollectedAt:  d.CollectedAt,
		InitialInput: d.InitialInput,
		Output:       d.Output,
	}, nil
}

// Get returns the dataset for (source, version), or sql.ErrNoRows if not found.
func (s *Store) Get(ctx context.Context, source, version string) (Dataset, error) {
	row, err := s.q.GetDataset(ctx, db.GetDatasetParams{Source: source, Version: version})
	if err != nil {
		return Dataset{}, err
	}
	return rowToDataset(row), nil
}

// Latest returns the latest-info row for source, or sql.ErrNoRows if the
// source has never been recorded.
func (s *Store) Latest(ctx context.Context, source string) (LatestInfo, error) {
	row, err := s.q.GetLatest(ctx, source)
	if err != nil {
		return LatestInfo{}, err
	}
	return rowToLatestInfo(row), nil
}

// ListLatest returns all current latest pointers, ordered by source name.
func (s *Store) ListLatest(ctx context.Context) ([]LatestInfo, error) {
	rows, err := s.q.ListLatest(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]LatestInfo, len(rows))
	for i, r := range rows {
		out[i] = latestInfoFrom(r.Source, r.Version, r.CollectedAt, r.InitialInput, r.Output)
	}
	return out, nil
}

// ListBySource returns all dataset versions for source, newest first.
func (s *Store) ListBySource(ctx context.Context, source string) ([]Dataset, error) {
	rows, err := s.q.ListDatasetsBySource(ctx, source)
	if err != nil {
		return nil, err
	}
	out := make([]Dataset, len(rows))
	for i, r := range rows {
		out[i] = rowToDataset(r)
	}
	return out, nil
}

func rowToDataset(row db.Dataset) Dataset {
	t, _ := time.Parse(time.RFC3339, row.CollectedAt)
	return Dataset{
		Source:       row.Source,
		Version:      row.Version,
		CollectedAt:  t,
		InitialInput: row.InitialInput,
		Output:       row.Output,
	}
}

func rowToLatestInfo(row db.GetLatestRow) LatestInfo {
	return latestInfoFrom(row.Source, row.Version, row.CollectedAt, row.InitialInput, row.Output)
}

func latestInfoFrom(source, version, collectedAt, initialInput, output string) LatestInfo {
	return LatestInfo{
		Source:       source,
		Version:      version,
		CollectedAt:  collectedAt,
		InitialInput: initialInput,
		Output:       output,
	}
}
