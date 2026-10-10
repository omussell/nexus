# Vulcanus Storage Optimization Plan

## Current Problem

All data (nauvis/Crossref works, ROR organizations, Retraction Watch records) is stored as JSON strings in a single column per table. Every query extracts fields via `json_extract_string()`, preventing:

- Column-level compression
- Predicate pushdown / vectorized scans
- Native columnar storage benefits
- Index usage on match-critical fields

Parquet ingestion already reads columnar files but immediately converts them back to JSON (`to_json(STRUCT_PACK(...))`), losing all optimization benefits.

---

## Target Architecture

Keep the existing `record JSON` column for raw data and provenance, but add denormalized columns for fields used in matching/joins:

### nauvis table

| Column | Type | Indexed? | Source |
|--------|------|----------|--------|
| `record` | JSON | no | Full Crossref work JSON (existing) |
| `doi` | TEXT | yes (unique) | `$.DOI` |
| `funder_names` | JSON | no | `$.funder[*].name` as JSON array |
| `affiliation_names` | JSON | no | `$.author[*].affiliation[*].name` as JSON array |

### ror table

| Column | Type | Indexed? | Source |
|--------|------|----------|--------|
| `record` | JSON | no | Full ROR organization JSON (existing) |
| `ror_id` | TEXT | yes (unique) | `$.id` |
| `country` | TEXT | yes | `$.country` |
| `status` | TEXT | yes | `$.status` |
| `names` | JSON | no | `$.names[*].value` as JSON array |

### retractionwatch table (unchanged — already minimal)

| Column | Type | Indexed? | Source |
|--------|------|----------|--------|
| `record` | JSON | no | Full record (existing) |
| `original_paper_doi` | TEXT | yes (indexed) | `$.original_paper_doi` |
| `retraction_doi` | TEXT | no | `$.retraction_doi` |

---

## Migration Strategy

### Phase 1: Schema migration — add columns and populate

A one-time migration that transforms existing tables:

```sql
-- Add columns to existing tables
ALTER TABLE nauvis ADD COLUMN doi TEXT;
ALTER TABLE nauvis ADD COLUMN funder_names JSON;
ALTER TABLE nauvis ADD COLUMN affiliation_names JSON;

ALTER TABLE ror ADD COLUMN ror_id TEXT;
ALTER TABLE ror ADD COLUMN status TEXT;
ALTER TABLE ror ADD COLUMN names JSON;

ALTER TABLE retractionwatch ADD COLUMN original_paper_doi TEXT;
ALTER TABLE retractionwatch ADD COLUMN retraction_doi TEXT;
```

Populate from existing JSON:

```sql
UPDATE nauvis
SET doi = json_extract_string(record, '$.DOI'),
    funder_names = json_extract(record, '$.funder[*].name'),
    affiliation_names = json_extract(record, '$.author[*].affiliation[*].name');

UPDATE ror
SET ror_id = json_extract_string(record, '$.id'),
    status = json_extract_string(record, '$.status'),
    names = CAST(json_extract(record, '$.names[*].value') AS JSON);

UPDATE retractionwatch
SET original_paper_doi = json_extract_string(record, '$.original_paper_doi'),
    retraction_doi = json_extract_string(record, '$.retraction_doi');
```

Create indexes:

```sql
CREATE UNIQUE INDEX idx_nauvis_doi ON nauvis(doi);
CREATE INDEX idx_nauvis_funder ON nauvis(funder_names) USING GIN;
CREATE INDEX idx_nauvis_affil ON nauvis(affiliation_names) USING GIN;

CREATE UNIQUE INDEX idx_ror_id ON ror(ror_id);
CREATE INDEX idx_ror_country ON ror(country);
CREATE INDEX idx_ror_status ON ror(status);

CREATE INDEX idx_retraction_doi ON retractionwatch(original_paper_doi);
```

### Phase 2: Ingest code changes

Modify `ingest.go` `ingestParquet()` and `ingestFile()` to populate denormalized columns:

```go
// ingestParquet with typed columns
INSERT INTO nauvis (record, doi, funder_names, affiliation_names)
SELECT
    to_json(STRUCT_PACK(...)),
    doi,
    funder_name,
    affiliation_name
FROM read_parquet(?)
```

For the JSON path, extract fields during INSERT:

```go
insertStmt, err := tx.PrepareContext(ctx, `
    INSERT INTO nauvis (record, doi, funder_names, affiliation_names)
    VALUES (?,
        json_extract_string(?, '$.DOI'),
        json_extract(?, '$.funder[*].name'),
        json_extract(?, '$.author[*].affiliation[*].name')
    )
`)
```

Same pattern for ror and retractionwatch tables.

### Phase 3: Matching code changes

#### ROR matching (`match/duckdb.go`)

Replace JSON extraction query with direct column access:

```go
// Before (json_extract_string on every row)
q := `
    SELECT id, country, status, name_values
    FROM ror
    WHERE LOWER(array_to_string(json_extract_string(record, '$.names[*].value'), ',')) LIKE LOWER(?)
    LIMIT ?
`

// After (direct column scan, country/status used in WHERE clause)
q := `
    SELECT ror_id, country, status, names
    FROM ror
    WHERE LOWER(array_to_string(names::VARCHAR, ',')) LIKE LOWER(?)
      AND status <> 'withdrawn'  -- push down the exclusion
    LIMIT ?
`
```

With the `status` column indexed, DuckDB can skip withdrawn organizations entirely.

#### ROR matching (`match/matching.go`)

The `MatchFunder` function already filters by country region. With the `country` column indexed, the pre-filter query becomes:

```go
// Before: fetch all candidates then filter in Go
// After: let DuckDB pre-filter
query := `SELECT ror_id, country, names FROM ror WHERE status = 'active' AND country = ?`
```

This reduces candidate count dramatically before fuzzy matching.

#### Retraction matching (`ingest/retractions.go`)

Replace JSON-based joins with column-based joins:

```sql
-- Before
FROM nauvis AS n
JOIN retractionwatch AS r
  ON json_extract_string(n.record, '$.doi') = json_extract_string(r.record, '$.original_paper_doi')

-- After (uses indexed TEXT columns, can use hash/nested loop join on DOI)
FROM nauvis AS n
JOIN retractionwatch AS r
  ON n.doi = r.original_paper_doi
```

### Phase 4: Parquet ingestion optimization

Instead of converting parquet columns back to JSON, store them directly into typed columns:

```go
func ingestParquetNauvis(ctx context.Context, tx *sql.Tx, path string, lg *slog.Logger) (int, error) {
    // Direct column projection from parquet into typed table columns
    query := `
        INSERT INTO nauvis (record, doi, funder_names, affiliation_names)
        SELECT
            to_json(T),
            T.doi,
            T.funder_name,
            T.author_name
        FROM read_parquet(?) AS T
    `
    _, err := tx.ExecContext(ctx, query, path)
    return err
}
```

For parquet files that already have the right columns, this is a zero-copy read + select, no `STRUCT_PACK` round-trip.

---

## Performance Expectations

| Operation | Current | After |
|-----------|---------|-------|
| ROR query (LIKE match) | Full table scan, JSON deserialize all rows | Column scan on `names` column, predicate on `status` + `country` |
| ROR matching with country filter | 200 candidates per name (all active), filtered in Go | Candidates pre-filtered by DuckDB |
| Retraction join | Join on extracted JSON strings | Join on indexed TEXT columns |
| Parquet ingest | Read parquet → STRUCT_PACK → to_json → insert JSON string | Read parquet → project columns → insert |
| Storage size | JSON text (duplicated across record column + extracted fields) | Columnar parquet-like storage in DuckDB for typed columns |

DuckDB stores VARCHAR columns using LZ4 compression internally, and the denormalized columns can be scanned using vectorized execution without JSON parsing overhead.

---

## Risk & Rollback

- **Backward compatibility**: The `record JSON` column is preserved byte-for-byte. Downstream consumers that rely on it continue to work.
- **Zero-downtime migration**: The migration is a one-time `ALTER TABLE` + `UPDATE` that can run alongside existing data. The old query path (via JSON) still works for all tables during transition.
- **Rollback**: Drop the new columns and indexes; everything reverts to the existing behavior.

---

## Order of Operations

1. Write migration SQL (Phase 1)
2. Test migration on existing `vulcanus.duckdb`
3. Update `ingest.go` to populate new columns (Phase 2)
4. Update `match/duckdb.go` to use typed columns (Phase 3)
5. Update `match/matching.go` to use country filter (Phase 3)
6. Update `ingest/retractions.go` join (Phase 3)
7. Optimize parquet ingest (Phase 4)
8. Benchmark before/after
