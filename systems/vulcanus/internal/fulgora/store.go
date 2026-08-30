// Package fulgora reads the SQLite database the fulgora system writes and
// exposes the latest output file it recorded for each third-party source.
package fulgora

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"

	_ "modernc.org/sqlite" // registers the "sqlite" driver (pure Go).
)

// Store reads the fulgora SQLite database produced by fulgora ingest.
type Store struct {
	db *sql.DB
	// root is the fulgora data root under which the stored output paths live.
	// It is joined with each Source.path to locate a file on disk.
	root string
}

// Source is the latest fulgora output recorded for one third-party source.
type Source struct {
	// name is the fulgora source name (e.g. "ror", "retractionwatch").
	name string
	// path is the output path recorded in the registry, relative to root.
	path string
	// version is the source's own version label recorded by fulgora.
	version string

	// root is the fulgora data root, retained so Output resolves the path. It
	// is only meant to keep Source self-describing; the Store owns the real
	// root.
	root string
}

// Name is the fulgora source name (e.g. "ror", "retractionwatch"), used as the
// DuckDB table identifier.
func (s Source) Name() string { return s.name }

// OutputPath resolves s relative to root, returning the on-disk location of the
// latest output file for this source. The registry stores output paths relative
// to the fulgora data root, so root must match Fulgora's own `-root` value.
func (s Source) OutputPath() string {
	return filepath.Join(s.root, s.path)
}

// Open opens the fulgora SQLite database at path for reading and returns a
// Store. root is the fulgora data root under which the registry's output paths
// live. Each Source.Path is joined against it to find files on disk.
func Open(path, root string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("fulgora: open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	if root == "" {
		return nil, errors.New("fulgora: empty data root")
	}
	return &Store{db: db, root: filepath.Clean(root)}, nil
}

// Close releases the underlying database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// Sources returns, for every source fulgora has collected, the latest version
// recorded and where its output file lives, ordered by source name. Fulgora
// appends new versions to `datasets` on later runs, so we join against `latest`
// to expose only the current version per source.
func (s *Store) Sources(ctx context.Context) ([]Source, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT latest.source, datasets.version, datasets.output
		FROM latest
		JOIN datasets ON datasets.id = latest.dataset_id
		ORDER BY latest.source
	`)
	if err != nil {
		return nil, fmt.Errorf("fulgora: list sources: %w", err)
	}
	defer rows.Close()

	var out []Source
	for rows.Next() {
		var src Source
		if err := rows.Scan(&src.name, &src.version, &src.path); err != nil {
			return nil, fmt.Errorf("fulgora: scan source: %w", err)
		}
		src.root = s.root
		out = append(out, src)
	}
	return out, rows.Err()
}

// Version returns the latest version recorded for source, or sql.ErrNoRows if
// the source has never been collected.
func (s *Store) Version(ctx context.Context, source string) (string, error) {
	var version string
	err := s.db.QueryRowContext(ctx, `SELECT version FROM latest WHERE source = ?`, source).Scan(&version)
	if errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	return version, err
}
