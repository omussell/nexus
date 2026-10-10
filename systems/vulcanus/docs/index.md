# VULCANUS

Vulcanus ingests data from Nauvis and Fulgora into a DuckDB database, enabling citation and retraction analysis.

## Overview

Vulcanus is a batch worker that reads output files from Nauvis and Fulgora (stored as JSON or Parquet) and ingests them into a DuckDB database. After ingestion it can optionally run ROR matching against nauvis records and retraction matching between nauvis and retractionwatch records.

## Key Functionalities

* **Batch Ingestion:** Reads Nauvis output files from a JSON or Parquet store and ingests them into a single `nauvis` table in DuckDB.
* **Fulgora Ingestion:** Reads output files from each Fulgora source (e.g. `ror`, `retractionwatch`) and ingests them into separate tables per source.
* **Provider Isolation:** Nauvis DOI records land in a single `nauvis` table; each Fulgora source gets its own table.
* **JSON Parquet Support:** When the `-parquet` flag is given, Vulcanus reads Parquet files via DuckDB's `read_parquet()` function, converting columns back to JSON via `to_json(row(*))` to preserve the existing table schema.
* **Faithful Preservation:** Ingests raw JSON records verbatim into a `JSON` column in DuckDB — one row per record.
* **Retraction Matching:** Optional `-match-retractions` flag rebuilds the `retractions` view, which pairs each Nauvis record with its `retractionwatch` notice on the paper's DOI.
* **ROR Matching:** Optional `-match-ror` flag runs ROR entity matching against nauvis records, finding matching funder and affiliation organizations.
* **Table Reset:** Each run empties the target tables before re-inserting, so re-running replaces the previous snapshot rather than accumulating duplicates.

## Running Vulcanus

```bash
go run . \
  [-only all|nauvis|fulgora|match] \
  [-nauvis-db nauvis.sqlite3] \
  [-nauvis-dir /path/to/nauvis] \
  [-fulgora-db fulgora.sqlite3] \
  [-fulgora-dir /path/to/fulgora] \
  [-duckdb vulcanus.duckdb] \
  [-parquet] \
  [-match-retractions] \
  [-match-ror]
```

| flag | default | meaning |
|------|---------|---------|
| `-only` | `all` | Which provider to ingest: `nauvis`, `fulgora`, `all`, or `match` (to only run matching without ingestion) |
| `-nauvis-db` | `nauvis.sqlite3` | Path to the nauvis SQLite database |
| `-nauvis-dir` | (required when -only includes nauvis) | Path to the nauvis system directory (data root) |
| `-fulgora-db` | `fulgora.sqlite3` | Path to the fulgora SQLite database |
| `-fulgora-dir` | (required when -only includes fulgora) | Path to the fulgora data root (output dir) |
| `-duckdb` | `vulcanus.duckdb` | DuckDB database file to write |
| `-parquet` | `false` | Read Parquet files instead of JSON |
| `-match-retractions` | `false` | Run retractionwatch matching (rebuild the retraction views) after ingestion |
| `-match-ror` | `false` | Run ROR matching after ingestion |

### Examples

Ingest all sources with JSON files:

```bash
go run . -nauvis-dir /data/nauvis -fulgora-dir /data/fulgora -duckdb vulcanus.duckdb
```

Ingest with Parquet files and run all matching:

```bash
go run . -nauvis-dir /data/nauvis -fulgora-dir /data/fulgora -duckdb vulcanus.duckdb -parquet -match-retractions -match-ror
vulcanus: ingested 1234567 nauvis records into vulcanus.duckdb
vulcanus: ingested 45678 fulgora records into vulcanus.duckdb
vulcanus: retraction matching complete
vulcanus: ROR matching complete
```

Run matching only (no ingestion):

```bash
go run . -only match -duckdb vulcanus.duckdb -match-retractions -match-ror
vulcanus: retraction matching complete
vulcanus: ROR matching complete
```

## How it works

See [How Vulcanus Works](how-it-works.md) for a detailed explanation of the ingestion flow, DuckDB tables, and views.

## Deployment

Vulcanus is designed for batch execution. It runs to completion and exits:

1. Parse flags and open stores
2. Ingest Nauvis files (JSON or Parquet) into DuckDB `nauvis` table
3. Ingest Fulgora files (JSON or Parquet) into DuckDB `<source>` tables
4. If `-match-retractions`: rebuild retraction views
5. If `-match-ror`: run ROR matching and write results
6. Exit

## Dependencies

Nauvis and Fulgora must be run first to produce output files and update their SQLite registries:

```bash
# Nauvis: ingest and produce JSON/Parquet output
go run github.com/nexus/nauvis ingest -in /data/crossref -out /data/nauvis/out -parquet-dir /data/nauvis/parquet

# Fulgora: collect and produce JSON/Parquet output
go run github.com/nexus/fulgora collect -root /data/fulgora -parquet-dir /data/fulgora/parquet
```

Then Vulcanus can read the output:

```bash
go run . -nauvis-dir /data/nauvis -fulgora-dir /data/fulgora -duckdb vulcanus.duckdb -parquet
```
