# Nauvis Session Summary

This document records the changes made to the Nauvis Go system in one working session.

## Overview of the change

The Nauvis Go program was expanded from a pure "decompress and rewrite" tool into a
pipeline that also records the DOIs of each file's items in a SQLite database, plus a
query mode to look a DOI back up.

## What changed

### `main.go`

- The original decompress-and-rewrite logic was largely removed. The file now only does
  flag parsing and dispatches to the new packages.
- Two modes are supported:
  - `process` (default): `-in/-out/-db/-jobs` — decompress and record items.
  - `-query <DOI>`: look up a recorded DOI and print which file it came from.

### New `internal/` packages (~500 lines across 11 files)

- `extract/` — decompresses `*.json.gz`, validates it is one well-formed JSON value,
  writes the bytes back byte-for-byte unchanged, and extracts the `items[].DOI` list.
  Also lists input files and derives base names.
- `ingest/` — worker-pool orchestrator. Runs `extract` + `store` per file in parallel,
  logs results, and flags duplicate DOIs as errors.
- `store/` — records items (file + DOI) atomically per file in a transaction; detects
  duplicates (DOI already present, or a racing unique-violation) and reports them.
  `GetByDOI` backs the query mode.
- `store/open.go` — opens SQLite via the pure-Go `modernc.org/sqlite` driver, sets WAL
  + foreign-key busy-timeout, caps the connection pool to one writer to serialize
  concurrency safely.
- `migrate/` — embeds and applies `schema.sql` idempotently.
- `db/` — sqlc-generated code from `db/queries/items.sql` (`insert_item`,
  `get_item_by_doi`) plus a `querier.go` interface.

### `go.mod`

- Bumped Go 1.24 → 1.25 and added `modernc.org/sqlite` and related dependencies.
- A `go.sum` was generated alongside.

### Build / config

- `sqlc.yaml` — sqlc configuration (sqlite engine, schema + queries → `internal/db`,
  with interface emission).

## Database schema

`items(id, file, doi UNIQUE)` with an index on `file`, so each DOI maps to the first file
encountered.

## Why

The old tool only re-emitted decompressed files with no index. The new design lets
callers find which input file contains a given DOI, and safely deduplicates DOIs across
the thousands of concurrently processed files.

## Open questions

- sqlc-generated files are committed to the repo, but `go.sum` was initially untracked.
  That should be reconciled before committing.
