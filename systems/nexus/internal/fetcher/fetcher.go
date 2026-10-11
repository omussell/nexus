// Package fetcher reads matched data from Vulcanus's DuckDB and Fulgora's
// SQLite registry, then generates is-related-to relationship triplets for
// Nexus: "croid of nauvis data is-related-to croid of fulgora dataset".
package fetcher

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"

	"github.com/duckdb/duckdb-go/v2"
	"github.com/nexus/nexus/internal/store"
	_ "modernc.org/sqlite"
)

// paperMatches tracks which relationship kinds apply to a single nauvis paper.
type paperMatches struct {
	rorFunder  bool
	rorAffil   bool
	retraction bool
}

// Run reads matching data from the DuckDB at vulcanusDBPath and the Fulgora
// SQLite database at fulgoraDBPath, then stores is-related-to relationship
// records in nexusStore. It writes one relationship per unique (nauvis croid,
// fulgora dataset croid, relationship kind) triplet.
//
// The three relationship kinds are:
//   - "ror_funder"     — nauvis paper linked to the ROR dataset via funder matching
//   - "ror_affiliation" — nauvis paper linked to the ROR dataset via affiliation matching
//   - "retraction"     — nauvis paper linked to the Retraction Watch dataset via DOI match
//
// lg may be nil to use the default logger.
func Run(ctx context.Context, vulcanusDBPath, fulgoraDBPath string, nexusStore *store.Store, lg *slog.Logger) error {
	if lg == nil {
		lg = slog.Default()
	}

	// Open DuckDB for Vulcanus data.
	vulcanusDB, err := openDuckDB(vulcanusDBPath)
	if err != nil {
		return fmt.Errorf("open vulcanus duckdb: %w", err)
	}
	defer vulcanusDB.Close()

	// Open Fulgora SQLite for dataset croids.
	fulgoraDB, err := openSQLite(fulgoraDBPath)
	if err != nil {
		return fmt.Errorf("open fulgora sqlite: %w", err)
	}
	defer fulgoraDB.Close()

	// Collect nauvis croids keyed by DOI.
	nauvisCroids, err := queryNauvisCroids(ctx, vulcanusDB)
	if err != nil {
		return fmt.Errorf("query nauvis croids: %w", err)
	}

	// Collect fulgora dataset croids keyed by source.
	fulgoraCroids, err := queryFulgoraDatasetCroids(ctx, fulgoraDB)
	if err != nil {
		return fmt.Errorf("query fulgora dataset croids: %w", err)
	}

	rorCroid := fulgoraCroids["ror"]
	retrCroid := fulgoraCroids["retraction_watch"]

	// Collect matching triplets: nauvis DOI → relationship kinds.
	paperMatchesMap := make(map[string]*paperMatches)

	// ROR funder matches.
	if rorCroid != "" {
		if err := collectRorFunderMatches(ctx, vulcanusDB, nauvisCroids, paperMatchesMap); err != nil {
			return fmt.Errorf("collect ror funder matches: %w", err)
		}
	}

	// ROR affiliation matches.
	if rorCroid != "" {
		if err := collectRorAffilMatches(ctx, vulcanusDB, nauvisCroids, paperMatchesMap); err != nil {
			return fmt.Errorf("collect ror affiliation matches: %w", err)
		}
	}

	// Retraction matches.
	if retrCroid != "" {
		if err := collectRetractionMatches(ctx, vulcanusDB, nauvisCroids, paperMatchesMap); err != nil {
			return fmt.Errorf("collect retraction matches: %w", err)
		}
	}

	// Insert relationships.
	totalInserted := 0
	totalSkipped := 0

	for doi, m := range paperMatchesMap {
		croid, ok := nauvisCroids[doi]
		if !ok {
			continue
		}

		if m.rorFunder {
			ins, skip, err := insertIfNew(ctx, nexusStore, croid, rorCroid, "ror_funder")
			if err != nil {
				return fmt.Errorf("insert ror_funder %s: %w", croid, err)
			}
			totalInserted += ins
			totalSkipped += skip
		}

		if m.rorAffil {
			ins, skip, err := insertIfNew(ctx, nexusStore, croid, rorCroid, "ror_affiliation")
			if err != nil {
				return fmt.Errorf("insert ror_affiliation %s: %w", croid, err)
			}
			totalInserted += ins
			totalSkipped += skip
		}

		if m.retraction {
			ins, skip, err := insertIfNew(ctx, nexusStore, croid, retrCroid, "retraction")
			if err != nil {
				return fmt.Errorf("insert retraction %s: %w", croid, err)
			}
			totalInserted += ins
			totalSkipped += skip
		}
	}

	lg.Info("producer: done",
		"inserted", totalInserted,
		"skipped", totalSkipped,
		"papers", len(paperMatchesMap))

	return nil
}

// queryNauvisCroids returns a map from DOI to croid for all nauvis records
// that have a non-empty croid.
func queryNauvisCroids(ctx context.Context, db *sql.DB) (map[string]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT doi, croid FROM nauvis WHERE croid IS NOT NULL AND croid != ''`)
	if err != nil {
		return nil, fmt.Errorf("query nauvis: %w", err)
	}
	defer rows.Close()

	m := make(map[string]string)
	for rows.Next() {
		var doi, croid string
		if err := rows.Scan(&doi, &croid); err != nil {
			return nil, fmt.Errorf("scan nauvis row: %w", err)
		}
		m[doi] = croid
	}
	return m, rows.Err()
}

// queryFulgoraDatasetCroids returns a map from source name to croid for the
// latest version of each dataset.
func queryFulgoraDatasetCroids(ctx context.Context, db *sql.DB) (map[string]string, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT d.source, d.croid
		FROM datasets d
		JOIN latest l ON l.dataset_id = d.id
		WHERE d.croid IS NOT NULL AND d.croid != ''
	`)
	if err != nil {
		return nil, fmt.Errorf("query fulgora datasets: %w", err)
	}
	defer rows.Close()

	m := make(map[string]string)
	for rows.Next() {
		var source, croid string
		if err := rows.Scan(&source, &croid); err != nil {
			return nil, fmt.Errorf("scan fulgora row: %w", err)
		}
		m[source] = croid
	}
	return m, rows.Err()
}

// collectRorFunderMatches finds all paper DOIs that have ror_funder matches and
// records them in paperMatchesMap.
func collectRorFunderMatches(ctx context.Context, db *sql.DB, nauvisCroids map[string]string, out map[string]*paperMatches) error {
	rows, err := db.QueryContext(ctx, `SELECT DISTINCT paper_doi FROM ror_funder_matches`)
	if err != nil {
		return fmt.Errorf("query ror_funder_matches: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var doi string
		if err := rows.Scan(&doi); err != nil {
			return fmt.Errorf("scan ror_funder row: %w", err)
		}
		if _, ok := nauvisCroids[doi]; ok {
			if out[doi] == nil {
				out[doi] = &paperMatches{}
			}
			out[doi].rorFunder = true
		}
	}
	return rows.Err()
}

// collectRorAffilMatches finds all paper DOIs that have ror_affiliation matches
// and records them in paperMatchesMap.
func collectRorAffilMatches(ctx context.Context, db *sql.DB, nauvisCroids map[string]string, out map[string]*paperMatches) error {
	rows, err := db.QueryContext(ctx, `SELECT DISTINCT paper_doi FROM ror_affiliation_matches`)
	if err != nil {
		return fmt.Errorf("query ror_affiliation_matches: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var doi string
		if err := rows.Scan(&doi); err != nil {
			return fmt.Errorf("scan ror_affil row: %w", err)
		}
		if _, ok := nauvisCroids[doi]; ok {
			if out[doi] == nil {
				out[doi] = &paperMatches{}
			}
			out[doi].rorAffil = true
		}
	}
	return rows.Err()
}

// collectRetractionMatches finds all paper DOIs that have retraction matches
// and records them in paperMatchesMap.
func collectRetractionMatches(ctx context.Context, db *sql.DB, nauvisCroids map[string]string, out map[string]*paperMatches) error {
	rows, err := db.QueryContext(ctx, `SELECT DISTINCT original_paper_doi FROM retractionwatch`)
	if err != nil {
		return fmt.Errorf("query retractionwatch: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var doi string
		if err := rows.Scan(&doi); err != nil {
			return fmt.Errorf("scan retraction row: %w", err)
		}
		if _, ok := nauvisCroids[doi]; ok {
			if out[doi] == nil {
				out[doi] = &paperMatches{}
			}
			out[doi].retraction = true
		}
	}
	return rows.Err()
}

// insertIfNew inserts a relationship if (source, target, relationship) does not
// already exist. Returns the number inserted (0 or 1) and the number skipped.
func insertIfNew(ctx context.Context, s *store.Store, source, target, rel string) (int, int, error) {
	existing, err := s.ListBySource(ctx, source)
	if err != nil {
		return 0, 0, err
	}
	for _, r := range existing {
		if r.Target == target && r.Relationship == rel {
			return 0, 1, nil
		}
	}
	if _, err := s.Insert(ctx, store.Relationship{
		Source:       source,
		Target:       target,
		Relationship: rel,
	}); err != nil {
		return 0, 0, err
	}
	return 1, 0, nil
}

func openDuckDB(path string) (*sql.DB, error) {
	conn, err := duckdb.NewConnector(path, nil)
	if err != nil {
		return nil, fmt.Errorf("duckdb connect: %w", err)
	}
	return sql.OpenDB(conn), nil
}

func openSQLite(path string) (*sql.DB, error) {
	return sql.Open("sqlite", path)
}
