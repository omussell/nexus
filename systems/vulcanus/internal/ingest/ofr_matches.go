package ingest

import (
	"context"
	"database/sql"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nexus/vulcanus/internal/match"
)

// RunOFRMatches runs openfunderregistry matching against the nauvis records
// stored in the DuckDB at dbPath, writing matches to the `ofr_matches` table.
// It is a no-op if either the nauvis or openfunderregistry table does not
// yet exist.
func RunOFRMatches(ctx context.Context, dbPath string) error {
	// Open a read connection for querying nauvis records.
	readDB, err := openDuckDB(dbPath)
	if err != nil {
		return fmt.Errorf("open DuckDB: %w", err)
	}
	defer readDB.Close()

	if !hasTable(ctx, readDB, "nauvis") || !hasTable(ctx, readDB, "openfunderregistry") {
		return nil
	}

	// Query all nauvis records with DOI and funder names.
	records, err := queryOFRRecords(ctx, readDB)
	if err != nil {
		return fmt.Errorf("query nauvis OFR records: %w", err)
	}

	// Collect all unique funder names for deduplicated matching.
	seen := make(map[string]bool)
	var ofrNames []string
	for _, rec := range records {
		for _, name := range rec.OFRName {
			if !seen[name] {
				seen[name] = true
				ofrNames = append(ofrNames, name)
			}
		}
	}

	numWorkers := runtime.GOMAXPROCS(0)
	if numWorkers > 8 {
		numWorkers = 8
	}

	fmt.Printf("vulcanus: OFR matching: %d unique funder names to match (%d workers)\n", len(ofrNames), numWorkers)

	// Channel of jobs.
	type matchJob struct {
		name string
	}
	jobs := make(chan matchJob, len(ofrNames))
	for _, name := range ofrNames {
		jobs <- matchJob{name}
	}
	close(jobs)

	// Shared result map protected by a mutex.
	var mu sync.Mutex
	ofrMatches := make(map[string][]map[string]interface{})

	var processed int64
	start := time.Now()
	reportTicker := time.NewTicker(5 * time.Second)
	defer reportTicker.Stop()

	// Open one DuckDB client per worker up front.
	clients := make([]*match.OFRClient, 0, numWorkers)
	for i := 0; i < numWorkers; i++ {
		c, err := match.NewOFRClient(dbPath)
		if err != nil {
			for _, opened := range clients {
				_ = opened.Close()
			}
			return fmt.Errorf("open OFRClient: %w", err)
		}
		clients = append(clients, c)
	}

	var wg sync.WaitGroup
	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(c *match.OFRClient) {
			defer wg.Done()
			defer c.Close()

			for job := range jobs {
				matches := c.MatchOFR(job.name)
				if len(matches) > 0 {
					mu.Lock()
					ofrMatches[job.name] = matches
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
					remaining = float64(len(ofrNames)-int(p)) / rate
				}
				fmt.Printf("vulcanus: OFR matching: %d/%d done (%.0f jobs/sec, ~%.0fm remaining)\n",
					p, len(ofrNames), rate, remaining/60)
			case <-stop:
				return
			}
		}
	}()

	wg.Wait()
	close(stop)

	fmt.Printf("vulcanus: OFR matching: done, %d funder names matched\n", len(ofrMatches))

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

	if err := resetOFRMatchesTable(ctx, tx); err != nil {
		return err
	}

	insertStmt, err := tx.PrepareContext(ctx, `INSERT INTO ofr_matches (nauvis_item_doi, funder_name, funder_uri, funder_match, score) VALUES (?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare insert: %w", err)
	}
	defer insertStmt.Close()

	// Emit matches for each nauvis item.
	var matchCount int
	for i, rec := range records {
		if i > 0 && i%5000 == 0 {
			fmt.Printf("vulcanus: OFR matching: emitting matches for %d/%d records\n", i, len(records))
		}
		for _, name := range rec.OFRName {
			for _, m := range ofrMatches[name] {
				if _, err := insertStmt.ExecContext(ctx, rec.DOI, name, m["uri"], m["name"], m["score"]); err != nil {
					return fmt.Errorf("insert OFR match: %w", err)
				}
				matchCount++
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	fmt.Printf("vulcanus: OFR matching: %d nauvis item-funder matches written\n", matchCount)
	return nil
}

// ofrRecord holds extracted fields from a nauvis record for OFR matching.
type ofrRecord struct {
	DOI       string
	OFRName   []string
}

// queryOFRRecords extracts DOI and funder names from all nauvis records.
func queryOFRRecords(ctx context.Context, db *sql.DB) ([]ofrRecord, error) {
	q := `
		SELECT
			doi,
			CASE WHEN funder_names IS NULL THEN ''
				ELSE CAST(funder_names AS VARCHAR)
			END
		FROM nauvis
		WHERE json_extract_string(record, '$.type') = 'journal-article'`

	rows, err := db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	var records []ofrRecord
	for rows.Next() {
		var rec ofrRecord
		var ofrRaw string
		if err := rows.Scan(&rec.DOI, &ofrRaw); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}
		rec.OFRName = splitJSONNames(ofrRaw)
		records = append(records, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iterator: %w", err)
	}
	return records, nil
}

// resetOFRMatchesTable creates (or replaces) the ofr_matches result table,
// dropping any prior content so re-running is idempotent.
func resetOFRMatchesTable(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS ofr_matches`); err != nil {
		return fmt.Errorf("drop ofr_matches: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `CREATE TABLE ofr_matches (
		nauvis_item_doi  TEXT,
		funder_name   TEXT,
		funder_uri    TEXT,
		funder_match  TEXT,
		score         DOUBLE
	)`); err != nil {
		return fmt.Errorf("create ofr_matches: %w", err)
	}
	return nil
}
