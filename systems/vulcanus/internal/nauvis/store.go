// Package nauvis reads the SQLite database the nauvis system writes and
// exposes the output files it recorded and the DOIs it indexed.
package nauvis

import (
	"context"
	"database/sql"

	_ "modernc.org/sqlite" // registers the "sqlite" driver (pure Go).
)

// Store reads the Nauvis SQLite database produced by nauvis ingest.
type Store struct {
	db *sql.DB
}

// Item is one of nauvis's output files and the DOI count it wrote.
type Item struct {
	Path  string
	Count int
}

// Open opens the Nauvis SQLite database at path for reading and returns a Store.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return &Store{db: db}, nil
}

// Close releases the underlying database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// Items returns every output file nauvis recorded with its DOI count.
func (s *Store) Items() ([]Item, error) {
	rows, err := s.db.Query(
		`SELECT file, COUNT(*)
		 FROM items
		 GROUP BY file
		 ORDER BY file`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []Item
	for rows.Next() {
		var i Item
		if err := rows.Scan(&i.Path, &i.Count); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	return items, rows.Err()
}

// ByDOI returns the file nauvis recorded a DOI in, or sql.ErrNoRows if unknown.
func (s *Store) ByDOI(ctx context.Context, doi string) (string, error) {
	var file string
	err := s.db.QueryRowContext(ctx, `SELECT file FROM items WHERE doi = ?`, doi).Scan(&file)
	return file, err
}
