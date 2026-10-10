// Package nauvis reads the SQLite database the nauvis system writes and
// exposes the output files it recorded and the DOIs it indexed.
package nauvis

import (
	"context"
	"database/sql"
	"path/filepath"

	_ "modernc.org/sqlite" // registers the "sqlite" driver (pure Go).
)

// Store reads the Nauvis SQLite database produced by nauvis ingest.
type Store struct {
	db *sql.DB
}

// Nauvis is one of nauvis's output files and the DOI count it wrote.
type Nauvis struct {
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

// Nauvis returns every output file nauvis recorded with its DOI count.
func (s *Store) Nauvis() ([]Nauvis, error) {
	rows, err := s.db.Query(
		`SELECT file, COUNT(*)
		 FROM nauvis
		 GROUP BY file
		 ORDER BY file`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var navis []Nauvis
	for rows.Next() {
		var i Nauvis
		if err := rows.Scan(&i.Path, &i.Count); err != nil {
			return nil, err
		}
		navis = append(navis, i)
	}
	return navis, rows.Err()
}

// ByDOI returns the file nauvis recorded a DOI in, or sql.ErrNoRows if unknown.
func (s *Store) ByDOI(ctx context.Context, doi string) (string, error) {
	var file string
	err := s.db.QueryRowContext(ctx, `SELECT file FROM nauvis WHERE doi = ?`, doi).Scan(&file)
	return file, err
}

// CroidRecord holds a DOI, CROID, and the path to its CROID-based parquet file.
type CroidRecord struct {
	Doi   string
	Croid string
	Path  string
}

// Croids returns a map from DOI to CROID for all rows that have a non-empty
// CROID value.  This is used by downstream systems that need the CROID as the
// primary identifier.
func (s *Store) Croids() (map[string]string, error) {
	rows, err := s.db.Query(
		`SELECT doi, croid FROM nauvis WHERE croid IS NOT NULL AND croid != ''`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	m := make(map[string]string)
	for rows.Next() {
		var doi, croid string
		if err := rows.Scan(&doi, &croid); err != nil {
			return nil, err
		}
		m[doi] = croid
	}
	return m, rows.Err()
}

// ParquetFiles returns the CROID-based parquet file paths from the nauvis-parquet directory,
// keyed by DOI. The parquetDir parameter is the root directory containing the CROID-named parquet files.
func (s *Store) ParquetFiles(parquetDir string) ([]CroidRecord, error) {
	rows, err := s.db.Query(
		`SELECT doi, croid FROM nauvis WHERE croid IS NOT NULL AND croid != '' ORDER BY doi`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []CroidRecord
	for rows.Next() {
		var doi, croid string
		if err := rows.Scan(&doi, &croid); err != nil {
			return nil, err
		}
		records = append(records, CroidRecord{
			Doi:   doi,
			Croid: croid,
			Path:  filepath.Join(parquetDir, croid+".parquet"),
		})
	}
	return records, rows.Err()
}

// ParquetFiles returns the output file paths (parquet format) recorded in the store.
// Files that don't end in ".json" are returned as-is.
func (s *Store) ParquetFilesLegacy() ([]Nauvis, error) {
	rows, err := s.db.Query(
		`SELECT file || '.parquet', COUNT(*)
		 FROM nauvis
		 GROUP BY file
		 ORDER BY file`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var navis []Nauvis
	for rows.Next() {
		var i Nauvis
		if err := rows.Scan(&i.Path, &i.Count); err != nil {
			return nil, err
		}
		navis = append(navis, i)
	}
	return navis, rows.Err()
}
