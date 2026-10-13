# How Vulcanus Works

This page explains how Vulcanus operates, from the Nauvis and Fulgora data stores to
the DuckDB database it maintains. It is a companion to the [Overview](index.md);
see there for what Vulcanus is and what it aims to.

## At a glance

Vulcanus is a batch worker. Nauvis and Fulgora each write output files (NDJSON or
Parquet) and maintain a SQLite registry of what they have produced. Vulcanus reads
those registries, locates the output files, and ingests them into DuckDB:

```
Nauvis SQLite ──read──▶  Nauvis output files (.json or .parquet)  ──ingest──▶  DuckDB `nauvis` table
Fulgora SQLite ──read──▶  Fulgora output files (.json or .parquet)  ──ingest──▶  DuckDB `<source>` tables
```

The two providers stay separate: Nauvis lands in a single `nauvis` table, while
each Fulgora source (e.g. `ror`, `retractionwatch`) gets its own table. They can
be ingested independently and re-running replaces the previous snapshot.

After the source tables exist, Vulcanus can build a **derived view** on top of
the Nauvis and retractionwatch tables:

* `retractions` — pairs each Nauvis record with its matching
  `retractionwatch` notice on the paper's DOI (`nauvis.DOI` =
  `retractionwatch.OriginalPaperDOI`), exposing both full records plus the
  matching DOI.

The views are only built when the `-match-retractions` flag is given and are a
projection over the loaded tables — they never copy data.

The ROR matching (`-match-ror`) extracts DOI, funder names, and affiliation names
from the `nauvis` records, matches them against the ROR database in DuckDB using
a parallel worker pool, and writes results to `ror_funder_matches` and
`ror_affiliation_matches` tables.

The OFR matching (`-match-ofr`) extracts funder names from the `nauvis`
records, matches them against the openfunderregistry database in DuckDB using
a parallel worker pool, and writes results to an `ofr_matches` table.
It uses the same LIKE pre-filter followed by fuzzy-score filtering (96 %
threshold) pattern that ROR matching uses, but operates on funder-name →
openfunderregistry-name pairs rather than funder-name → ROR-organization.
Replaced openfunderregistry records (where `replaced` is non-empty) are excluded
from matching.

This is not transformation: Vulcanus never parses or normalises the payload. It
only preserves it, one record at a time, in a single column of JSON text. Every
record a provider saw is ingested; nothing is filtered away.

## The stages

The pipeline is split into three packages and one entry point, each with one job.

### 1. Store readers — `internal/nauvis` and `internal/fulgora`

The store packages read from the SQLite databases that Nauvis and Fulgora maintain.

**Nauvis store** (`internal/nauvis/store.go`):
- `Open(path)` opens the Nauvis SQLite database and returns a `*Store`.
- `Nauvis()` returns every output file recorded with its DOI count.
- `ParquetFiles()` returns the same files but with `.json` replaced by `.parquet`
  in the path, enabling parquet-based ingestion.
- `ByDOI(ctx, doi)` returns the file that recorded a specific DOI.

**Fulgora store** (`internal/fulgora/store.go`):
- `Open(path, root)` opens the Fulgora SQLite database. `root` is the data root
  under which the registry's output paths live.
- `Sources(ctx)` returns, for every source Fulgora has collected, the latest version
  recorded and where its output file lives.
- `Source.OutputPath()` joins root and the stored path to locate the JSON file.
- `Source.ParquetPath()` derives the Parquet path by appending `.parquet` to the
  JSON path (removing `.json` first).

### 2. Ingest — `internal/ingest`

The `ingest` package loads files into DuckDB using the `database/sql` interface
over the duckdb-go driver.

`Run(ctx, src, outDir, outDB, useParquet, lg)` accepts either a `*nauvis.Store`
or a `*fulgora.Store` and routes to the appropriate ingestion function:

```go
func Run(ctx context.Context, src any, outDir, outDB string, useParquet bool, lg *slog.Logger) (int, error) {
    if vis, ok := src.(*nauvis.Store); ok {
        return runNauvis(ctx, vis, outDir, outDB, useParquet, lg)
    }
    if store, ok := src.(*fulgora.Store); ok {
        return runFulgora(ctx, store, outDir, outDB, useParquet, lg)
    }
    return 0, fmt.Errorf("unknown source type %T", src)
}
```

#### JSON ingestion (`ingestFile`)

For JSON files, `ingestFile` reads line by line:

1. Open the file as an NDJSON stream (one JSON object per line).
2. Validate each line with `json.Valid()`.
3. Bind each line as a parameter to `INSERT INTO <table> (record) VALUES (?)`.
4. Count and return the total rows inserted.

This approach processes records one at a time, using a prepared statement for
efficiency.

#### Parquet ingestion (`ingestParquet`)

For Parquet files, `ingestParquet` uses DuckDB's native `read_parquet()` function:

```sql
INSERT INTO nauvis (record)
SELECT to_json(row(*)) FROM read_parquet('path/to/file.parquet')
```

The query:
1. Reads all rows from the Parquet file via `read_parquet()`.
2. Converts each row to a JSON string via `to_json(row(*))`, preserving the
   existing `record JSON` column schema.
3. Inserts all rows in a single bulk operation.

This leverages DuckDB's columnar read capabilities — no Go-side parsing, no
line-by-line iteration. The Parquet files have typed columns (31 columns for
Nauvis: DOI, title, author_name[], funder_name[], etc.) that are efficiently
read and serialized to JSON.

#### Table management

`ensureTable(ctx, tx, name)` creates a table with a single `record` JSON column
if it does not exist. `resetTable(ctx, tx, name)` calls `ensureTable` and then
`DELETE FROM <table>` so re-running a provider replaces its previous snapshot.

Both use `quoted(name)` to safely embed table names in SQL — wrapping in double
quotes and escaping embedded quotes.

### 3. Matching — `internal/ingest/retractions.go`, `internal/ingest/ror_matches.go`, and `internal/ingest/ofr_matches.go`

#### Retraction matching (`RunRetractionMatches`)

When `-match-retractions` is set, Vulcanus rebuilds the `retractions` view:

**`retractions` view** — joins Nauvis against retractionwatch on DOI:
```sql
CREATE OR REPLACE VIEW retractions AS
SELECT
    n.record AS nauvis_record,
    r.record AS retractionwatch_record,
    json_extract_string(n.record, '$.DOI') AS matched_doi
FROM nauvis AS n
JOIN retractionwatch AS r
  ON json_extract_string(n.record, '$.DOI') =
     json_extract_string(r.record, '$.OriginalPaperDOI')
```

The view is a no-op if either `nauvis` or `retractionwatch` table is absent,
so providers can be ingested at different times.

#### ROR matching (`RunRORMatches`)

When `-match-ror` is set, Vulcanus:

1. Queries all nauvis records, extracting DOI, funder names, and affiliation names
   from the `record` JSON column using DuckDB's `json_extract_string()` function.
2. Collects unique funder and affiliation names across all papers.
3. Matches each unique name against the ROR database using a parallel worker pool
   (up to 8 workers). Each worker runs `MatchRORFunder()` or `MatchAffiliation()`
   against the DuckDB ROR table.
4. Writes results to `ror_funder_matches` and `ror_affiliation_matches` tables
   with columns: `paper_doi`, `name`, `ror_id`, `ror_name`, `confidence`.

The matching tables are reset (dropped and recreated) before each run so the
results are idempotent.

#### OFR matching (`RunOFRMatches`)

When `-match-ofr` is set, Vulcanus:

1. Queries all nauvis records, extracting DOI and funder names from the
   `funder_names` JSON column.
2. Collects unique funder names across all papers.
3. Matches each unique name against the openfunderregistry database using a
   parallel worker pool (up to 8 workers). Each worker runs `MatchOFR()`
   against the DuckDB openfunderregistry table.
4. Writes results to an `ofr_matches` table with columns:
   `paper_doi`, `funder_name`, `funder_uri`, `funder_match`, `score`.

The matching table is reset (dropped and recreated) before each run so the
results are idempotent. The step is a no-op if either the `nauvis` or
`openfunderregistry` table is absent.

### 4. Entry point — `main.go`

`main.go` orchestrates the batch workflow:

```
-nauvis-db      Nauvis SQLite database path (default nauvis.db)
-nauvis-dir     Nauvis data root directory (required to ingest nauvis data)
-fulgora-db     Fulgora SQLite database path (default fulgora.db)
-fulgora-dir    Fulgora data root directory (required to ingest fulgora data)
-duckdb         DuckDB file to write (default vulcanus.duckdb)
-only           Which to run: nauvis, fulgora, all, or match (default all)
-parquet        Read Parquet files instead of JSON (default false)
-match-ror      Run ROR matching after ingestion (default false)
-match-retractions Run retraction matching after ingestion (default false)
-match-ofr      Run openfunderregistry matching after ingestion (default false)
```

Flow:
1. Parse flags. If `-only` is "all" and matching flags are set, switch to "match" mode.
2. Validate required flags: `-nauvis-dir` and/or `-fulgora-dir` depending on `-only`.
3. Create an `slog` logger that writes to stderr.
4. If `-only` includes nauvis: open the Nauvis store, ingest files into DuckDB.
5. If `-only` includes fulgora: open the Fulgora store, ingest files into DuckDB.
6. If `-match-retractions`: rebuild the `retractions` view.
7. If `-match-ror`: run ROR matching against nauvis records.
8. If `-match-ofr`: run openfunderregistry matching against nauvis records.
9. Exit.

## The database

Every *source table* has a single column and is loaded identically — one row per
record, the raw JSON document preserved as-is:

| table | column | type | notes |
|-------|--------|------|-------|
| `nauvis` (Nauvis) | `record` | JSON | one row per Crossref work; the raw document as Nauvis wrote it |
| `<source>` (Fulgora, one per source) | `record` | JSON | e.g. `ror`, `retractionwatch`; same shape as `nauvis`, but each source has its own table |

When ingesting via Parquet (`-parquet`), the source files have 31 typed columns
for Nauvis (DOI, title, author_name[], funder_name[], etc.) and source-specific
schemas for Fulgora (e.g. ROR organizations with ror_id, name, type, country, etc.).
DuckDB's `read_parquet()` efficiently reads these columns, and `to_json(row(*))`
converts each row back to a JSON string that matches the existing `record JSON`
column schema.

In addition, retraction matching (`-match-retractions`) derives a **view**
that sits on the same Nauvis↔retractionwatch `JOIN`:

| view | columns | type | notes |
|------|---------|------|-------|
| `retractions` | `nauvis_record` | JSON | the full Nauvis record (Crossref item) |
| | `retractionwatch_record` | JSON | the full retractionwatch record |
| | `matched_doi` | VARCHAR | the DOI that linked the two (Nauvis's `DOI`) |

Each Fulgora source gets its own table. Denormalized columns are added to the
`ror`, `retractionwatch`, and `openfunderregistry` tables for efficient querying.

The ROR matching tables (`-match-ror`) hold structured match results:

| table | columns | type | notes |
|-------|---------|------|-------|
| `ror_funder_matches` | `paper_doi`, `name`, `ror_id`, `ror_name`, `confidence` | TEXT, TEXT, TEXT, TEXT, DOUBLE | Paper-to-funder ROR matches |
| `ror_affiliation_matches` | `paper_doi`, `name`, `ror_id`, `ror_name`, `confidence` | TEXT, TEXT, TEXT, TEXT, DOUBLE | Paper-to-affiliation ROR matches |

The OFR matching tables (`-match-ofr`) hold structured openfunderregistry-name matches:

| table | columns | type | notes |
|-------|---------|------|-------|
| `ofr_matches` | `paper_doi`, `funder_name`, `funder_uri`, `funder_match`, `score` | TEXT, TEXT, TEXT, TEXT, DOUBLE | Paper-to-funder (openfunderregistry) matches |

Nauvis and Fulgora are kept **separate**: Nauvis always lands in the single
`nauvis` table, while each Fulgora source gets its own table named after the
source. A row in any table is a full JSON object from that provider's output —
one record per input record. Because the column is declared `JSON`,
DuckDB's JSON functions (`JSON_SCHEMA`, `JSON_EXTRACT`, ...) are available
directly against the stored text once the load has happened.

Each run **replaces** the data: `resetTable` empties the target tables before
re-inserting, so re-running a provider produces a clean snapshot without
accumulating duplicates.

## Running Vulcanus

```bash
go run . [-only all|nauvis|fulgora|match] [-nauvis-dir path] [-fulgora-dir path] [-duckdb vulcanus.duckdb] [-parquet] [-match-retractions] [-match-ror] [-match-ofr]
```

| flag | default | meaning |
|------|---------|---------|
| `-only` | `all` | Which provider to ingest: nauvis, fulgora, all, or match |
| `-nauvis-db` | `nauvis.db` | Nauvis SQLite database path |
| `-nauvis-dir` | (required for nauvis) | Nauvis data root (output dir) |
| `-fulgora-db` | `fulgora.db` | Fulgora SQLite database path |
| `-fulgora-dir` | (required for fulgora) | Fulgora data root (output dir) |
| `-duckdb` | `vulcanus.duckdb` | DuckDB database file to write |
| `-parquet` | `false` | Read Parquet files instead of JSON |
| `-match-retractions` | `false` | Run retraction matching after ingestion |
| `-match-ror` | `false` | Run ROR matching after ingestion |
| `-match-ofr` | `false` | Run openfunderregistry matching after ingestion |

### Example

```bash
$ go run . -nauvis-dir /data/nauvis -fulgora-dir /data/fulgora -duckdb vulcanus.duckdb -parquet -match-retractions -match-ror -match-ofr
vulcanus: ingested 1234567 nauvis records into vulcanus.duckdb
vulcanus: ingested 45678 fulgora records into vulcanus.duckdb
vulcanus: retraction matching complete
vulcanus: ROR matching complete
vulcanus: OFR matching: done, 1234 funder names matched
```

The resulting `vulcanus.duckdb` holds source tables, matching views, and match
tables. List them with the DuckDB CLI:

```
$ duckdb vulcanus.duckdb
duckdb> .tables
nauvis            retractionwatch  ror             openfunderregistry
retractions       
ror_funder_matches ror_affiliation_matches ofr_matches
```

The key contract is faithfulness: Vulcanus treats the bytes coming from each
provider's output file as the data, and DuckDB as the place to store them for
later analysis. The `-parquet` flag improves ingestion throughput by leveraging
DuckDB's native Parquet reader instead of line-by-line JSON parsing.

## Parquet ingestion

When the `-parquet` flag is given, Vulcanus reads Parquet files instead of NDJSON.
The workflow:

1. **Nauvis:** `nauvis.ParquetFiles()` queries the SQLite registry and replaces
   `.json` with `.parquet` in each file path. The consolidated Parquet files
   (produced by `nauvis ingest -parquet-dir`) sit alongside the JSON files
   in the output directory (e.g. `out/0.json` and `out/0.json.parquet`).

2. **Fulgora:** `Source.ParquetPath()` derives the Parquet path from the
   JSON output path by replacing `.json` with `.parquet` (e.g.
   `ror/output/ror-1.34.json.parquet`).

3. **Ingestion:** `ingestParquet()` executes:
   ```sql
   INSERT INTO nauvis (record)
   SELECT to_json(row(*)) FROM read_parquet('out/0.json.parquet')
   ```
   DuckDB reads the columnar Parquet file natively, converts all columns (including
   arrays like `author_name`, `funder_name`) to JSON, and inserts into the
   existing `record JSON` column.

This preserves the existing DuckDB schema and matching logic while benefitting
from Parquet's efficient columnar I/O. The Parquet files are produced by Nauvis
and Fulgora when their `-parquet-dir` flag is set during the data collection
phase.

## Design notes

- **Batch ingestion over streaming.** Vulcanus reads all files from the provider's
  SQLite registry in a single run, rather than consuming messages one at a time.
  This is simpler to reason about and avoids the complexity of distributed queues.

- **Round-trip preservation over transformation.** The ingest step preserves
  bytes, not parse results. For JSON, records are bound verbatim into the JSON
  column. For Parquet, columns are converted back to JSON via `to_json(row(*))`.

- **Replace-on-reload.** Tables are emptied before each ingestion, so re-running
  a provider produces a clean snapshot. This avoids duplicate records and
  simplifies matching — views and match tables are always consistent with the
  current data.

- **Separate tables per provider.** Nauvis is the "main" data in `nauvis`;
  Fulgora sources are enrichment data, each in its own table. Keeping them
  separate preserves that distinction and lets either be ingested independently.

- **Matching is optional and additive.** ROR, retraction, and OFR matching are opt-in
  via flags. Without them, Vulcanus only populates the source tables. With them,
  it adds views and match tables on top of the source data.
