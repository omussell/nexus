package ingest

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nexus/vulcanus/internal/match"
)

type rormatch struct {
	PaperDOI   string  `json:"paper_doi"`
	Name       string  `json:"name"`
	NameType   string  `json:"name_type"` // "funder" or "affiliation"
	RORID      string  `json:"ror_id"`
	RORName    string  `json:"ror_name"`
	Confidence float64 `json:"confidence"`
}

// RunRORMatches runs ROR matching against the nauvis records stored in the
// DuckDB at dbPath, writing matches to the ror_funder_matches and
// ror_affiliation_matches tables. It is a no-op if either the nauvis or ror
// table does not yet exist.
func RunRORMatches(ctx context.Context, dbPath string) error {
	// Open a read connection for querying nauvis records.
	readDB, err := openDuckDB(dbPath)
	if err != nil {
		return fmt.Errorf("open DuckDB: %w", err)
	}
	defer readDB.Close()

	if !hasTable(ctx, readDB, "nauvis") || !hasTable(ctx, readDB, "ror") {
		return nil
	}

	// Query all nauvis records with DOI and funder/affiliation names.
	records, err := queryNauvisRecords(ctx, readDB)
	if err != nil {
		return fmt.Errorf("query nauvis records: %w", err)
	}

	// Collect all unique funder and affiliation names for deduplicated matching.
	seenFunder := make(map[string]bool)
	seenAffil := make(map[string]bool)
	var funderNames, affilNames []string
	for _, rec := range records {
		for _, n := range rec.FunderNames {
			if !seenFunder[n] {
				seenFunder[n] = true
				funderNames = append(funderNames, n)
			}
		}
		for _, n := range rec.AffiliationNames {
			if !seenAffil[n] {
				seenAffil[n] = true
				affilNames = append(affilNames, n)
			}
		}
	}

	// Match each unique name in parallel using a worker pool.
	numWorkers := runtime.GOMAXPROCS(0)
	if numWorkers > 8 {
		numWorkers = 8
	}
	totalJobs := len(funderNames) + len(affilNames)
	fmt.Printf("vulcanus: ROR matching: %d unique funder names, %d unique affiliation names to match (%d workers)\n", len(funderNames), len(affilNames), numWorkers)

	// Channel of jobs: each job is (type, name).
	type matchJob struct {
		typ  string // "funder" or "affiliation"
		name string
	}
	jobs := make(chan matchJob, totalJobs)
	for _, name := range funderNames {
		jobs <- matchJob{"funder", name}
	}
	for _, name := range affilNames {
		jobs <- matchJob{"affiliation", name}
	}
	close(jobs)

	// Shared result maps protected by a mutex.
	var mu sync.Mutex
	funderMatches := make(map[string][]map[string]interface{})
	affilMatches := make(map[string][]map[string]interface{})

	var processed int64
	start := time.Now()
	reportTicker := time.NewTicker(5 * time.Second)
	defer reportTicker.Stop()

	// Open one DuckDB client per worker up front so an open failure surfaces
	// as an error to RunRORMatches instead of panicking in a worker goroutine.
	clients := make([]*match.DuckDBClient, 0, numWorkers)
	for i := 0; i < numWorkers; i++ {
		c, err := match.NewDuckDBClient(dbPath)
		if err != nil {
			for _, opened := range clients {
				_ = opened.Close()
			}
			return fmt.Errorf("open DuckDB client: %w", err)
		}
		clients = append(clients, c)
	}

	var wg sync.WaitGroup
	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(c *match.DuckDBClient) {
			defer wg.Done()
			defer c.Close()

			for job := range jobs {
				var matches []map[string]interface{}
				if job.typ == "funder" {
					matches = c.MatchFunder(job.name)
				} else {
					matches = c.MatchAffiliation(job.name)
				}
				if len(matches) > 0 {
					mu.Lock()
					if job.typ == "funder" {
						funderMatches[job.name] = matches
					} else {
						affilMatches[job.name] = matches
					}
					mu.Unlock()
				}
				atomic.AddInt64(&processed, 1)
			}
		}(clients[w])
	}

	// Report progress until the workers finish.
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-reportTicker.C:
				p := atomic.LoadInt64(&processed)
				elapsed := time.Since(start)
				rate := 0.0
				if elapsed.Seconds() > 0 {
					rate = float64(p) / elapsed.Seconds()
				}
				remaining := 0.0
				if rate > 0 {
					remaining = float64(totalJobs-int(p)) / rate
				}
				fmt.Printf("vulcanus: ROR matching: %d/%d done (%.0f jobs/sec, ~%.0fm remaining)\n",
					p, totalJobs, rate, remaining/60)
			case <-stop:
				return
			}
		}
	}()

	wg.Wait()
	close(stop)

	fmt.Printf("vulcanus: ROR matching: done, %d funder matches, %d affiliation matches (unique names matched)\n",
		len(funderMatches), len(affilMatches))

	var funderCount, affilCount int

	// Now open a write connection and insert results in a transaction.
	writeDB, err := openDuckDB(dbPath)
	if err != nil {
		return fmt.Errorf("open write DuckDB: %w", err)
	}
	defer writeDB.Close()

	tx, err := writeDB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := resetRorMatchesTable(ctx, tx, "ror_funder_matches"); err != nil {
		return err
	}
	if err := resetRorMatchesTable(ctx, tx, "ror_affiliation_matches"); err != nil {
		return err
	}

	insertFunder := `INSERT INTO ror_funder_matches (paper_doi, name, ror_id, ror_name, confidence) VALUES (?, ?, ?, ?, ?)`
	insertAffil := `INSERT INTO ror_affiliation_matches (paper_doi, name, ror_id, ror_name, confidence) VALUES (?, ?, ?, ?, ?)`
	funderStmt, err := tx.PrepareContext(ctx, insertFunder)
	if err != nil {
		return fmt.Errorf("prepare funder insert: %w", err)
	}
	defer funderStmt.Close()
	affilStmt, err := tx.PrepareContext(ctx, insertAffil)
	if err != nil {
		return fmt.Errorf("prepare affiliation insert: %w", err)
	}
	defer affilStmt.Close()

	// Emit matches for each paper.
	for i, rec := range records {
		if i > 0 && i%5000 == 0 {
			fmt.Printf("vulcanus: ROR matching: emitting matches for %d/%d records\n", i, len(records))
		}
		for _, name := range rec.FunderNames {
			for _, m := range funderMatches[name] {
				if _, err := funderStmt.ExecContext(ctx, rec.DOI, name, m["id"], m["name"], m["confidence"]); err != nil {
					return fmt.Errorf("insert funder match: %w", err)
				}
				funderCount++
			}
		}
		for _, name := range rec.AffiliationNames {
			for _, m := range affilMatches[name] {
				if _, err := affilStmt.ExecContext(ctx, rec.DOI, name, m["id"], m["name"], m["confidence"]); err != nil {
					return fmt.Errorf("insert affiliation match: %w", err)
				}
				affilCount++
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	fmt.Printf("vulcanus: ROR matching: %d funder matches, %d affiliation matches\n", funderCount, affilCount)
	return nil
}

// nauvisRecord holds extracted fields from a nauvis record.
type nauvisRecord struct {
	DOI            string
	FunderNames    []string
	AffiliationNames []string
}

// queryNauvisRecords extracts DOI and funder/affiliation names from all nauvis
// records. Affiliation names come from author entries; the paper-level
// affiliation array is not present in Crossref metadata.
func queryNauvisRecords(ctx context.Context, db *sql.DB) ([]nauvisRecord, error) {
	q := `
		SELECT
			json_extract_string(record, '$.DOI') AS doi,
			CASE
				WHEN json_extract(record, '$.funder[*].name') IS NULL THEN ''
				ELSE CAST(json_extract(record, '$.funder[*].name') AS VARCHAR)
			END AS funder_names,
			CASE
				WHEN json_extract(record, '$.author[*].affiliation[*].name') IS NULL THEN ''
				ELSE CAST(json_extract(record, '$.author[*].affiliation[*].name') AS VARCHAR)
			END AS aff_names
		FROM nauvis`

	rows, err := db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	var records []nauvisRecord
	for rows.Next() {
		var rec nauvisRecord
		var funderRaw, affilRaw string
		if err := rows.Scan(&rec.DOI, &funderRaw, &affilRaw); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}
		rec.FunderNames = splitJSONNames(funderRaw)
		rec.AffiliationNames = splitJSONNames(affilRaw)
		records = append(records, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iterator: %w", err)
	}
	return records, nil
}

// splitJSONNames parses a JSON array of strings (e.g. '["A","B"]') or an empty
// string, returning the individual names. Elements that are not JSON strings
// (null, numbers, objects, ...) are skipped so a single malformed value does
// not discard the rest of the paper's names.
func splitJSONNames(s string) []string {
	if s == "" {
		return nil
	}
	var elements []json.RawMessage
	if err := json.Unmarshal([]byte(s), &elements); err != nil {
		return nil
	}
	// Deduplicate while preserving order.
	seen := make(map[string]bool)
	var result []string
	for _, el := range elements {
		var n string
		if err := json.Unmarshal(el, &n); err != nil {
			continue
		}
		trimmed := strings.TrimSpace(n)
		if trimmed != "" && !seen[trimmed] {
			seen[trimmed] = true
			result = append(result, trimmed)
		}
	}
	return result
}

// resetRorMatchesTable creates (or replaces) the given result table with the
// columns used by RunRORMatches. It drops any prior content so re-running is
// idempotent.
func resetRorMatchesTable(ctx context.Context, tx *sql.Tx, name string) error {
	if _, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS `+quoted(name)); err != nil {
		return fmt.Errorf("drop %s: %w", name, err)
	}
	if _, err := tx.ExecContext(ctx, `CREATE TABLE `+quoted(name)+` (
		paper_doi    TEXT,
		name         TEXT,
		ror_id       TEXT,
		ror_name     TEXT,
		confidence   DOUBLE
	)`); err != nil {
		return fmt.Errorf("create %s: %w", name, err)
	}
	return nil
}

// hasTable reports whether the given table exists in the main schema of the
// main (non-temporary) database. Tables in other schemas and TEMP tables do
// not count. TEMP tables live in a separate "temp" database whose schema is
// also named "main", so filtering on the schema alone is not enough — the
// temporary flag is required. Same check as tablesExist in retractions.go.
func hasTable(ctx context.Context, db *sql.DB, name string) bool {
	var count int
	err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM duckdb_tables()
		 WHERE table_name = ? AND temporary = false AND schema_name = 'main'`, name).Scan(&count)
	return err == nil && count > 0
}
