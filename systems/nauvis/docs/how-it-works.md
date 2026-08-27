# How Nauvis Works

This page explains how the Nauvis program operates, from input on disk to the
SQLite database it maintains. It assumes you are reading it as companion to the
[Overview](index.md); for "what Nauvis is see there.

## At a glance

Nauvis takes a directory of individually gzip-compressed Crossref snapshot files
(`*.json.gz`, named as bare integers such as `5.json.gz`), and for each file:

1. decompresses it,
2. writes the decompressed bytes back unchanged as a plain `.json` file, and
3. records the DOIs of its items in a SQLite database.

It then offers the inverse lookup of going the other way: given a DOI, which
input file first contained it.

Two things distinguish Nauvis from a plain `gunzip` loop:

- **byte-for-byte fidelity** – it re-emits the decompressed payload exactly as
  produced, without re-formatting, re-ordering, or dropping fields; every input
  file has exactly one matching output file; and
- **an index** – it tracks which DOI appears in which file, deduplicating DOIs
  that occur in more than one file.

## Input and output layout

| Path        | Kind   | Meaning                                                            |
|-------------|--------|--------------------------------------------------------------------|
| `./data`    | input  | Directory holding the `*.json.gz` source files (default `-in`).      |
| `./out`     | output | Directory holding the decompressed `*.json` files (default `-out`).  |
| `./nauvis.sqlite3` | db | SQLite database of `items` (default `-db`). |

Each `data/<n>.json.gz` produces one `out/<n>.json`. The output filename is the
input's base name with the trailing `.gz` stripped, so `data/5.json.gz` →
`out/5.json`. One file is the uncompressed example (`10.json`), kept as a manual
demo of the expected structure.

## The three stages

The pipeline is split into three packages, each with one job.

### 1. Extract — `internal/extract`

`extract.Process(inPath, outPath)` handles a single file:

1. **Decompress** — open the file and stream it through a gzip reader into memory.
2. **Validate** — confirm the bytes are a single, syntactically valid JSON value
   (`json.Valid`). This catches truncated or corrupt payloads before anything is
   written.
3. **Write** — emit the bytes to the output path using an *atomic write* (write
   to `out/<n>.json.tmp`, then rename into place), so a process that is killed
   mid-file never leaves a half-written `.json`.
4. **Extract** — unmarshal just the `items[].DOI` fields, in input order.

`extract.Files(dir)` lists the directory and returns the files it knows how to
process: anything ending in `.json.gz` or (uncompressed) `.json`. This is what
lets the same folder hold both the compressed inputs and the example file.

The key contract is that the written bytes equal the decompressed bytes — Nauvis
does not interpret or normalise the data, it only preserves it.

### 2. Store — `internal/store`

`store.Store` records `(file, doi)` pairs in SQLite. Its main method is
`RecordMany`:

```
for each DOI in the file:
    if the DOI is empty, skip it
    if the DOI already exists in items:
        record it as a duplicate (another file already claimed it)
        continue
    try to insert:
        on success: count it as new
        on a UNIQUE-constraint failure:
            it raced with a concurrent file → treat as a duplicate, continue
    if anything else fails: return the error
commit the whole thing as one transaction
```

Two independent guards catch the same duplicate:

- **Fast path** — `GetItemByDOI` before inserting. Because `doi` is `UNIQUE`, a
  hit means another file already recorded it.
- **Race path** — SQLite lets a `BEGIN IMMEDIATE`-style insert win the race when
  two workers try at exactly the same time, aborting the loser with an error.
  That UNIQUE violation is normalised into the *same* "duplicate" outcome rather
  than being treated as a failure.

Both paths report the duplicate (which file, which DOI) and let the winning
file keep the DOI — so a DOI always maps to the first file `ingest` processed.

`store.GetByDOI` is the read side: it queries `items` by DOI, backing the
`-query` command. This is what is not visible at the shell by default (`file` and
`doi` are the only columns returned to callers).

### 3. Ingest — `internal/ingest`

`ingest.Run` is the one responsible for everything running *in parallel*. For
each file it calls `extract.Process` then `store.RecordMany`, and reports the
outcome:

- **workers** — a fixed pool of `jobs` goroutines (default `GOMAXPROCS`) reading
  from a shared work channel. Each worker does `extract` + `store` for the next
  file handed to it.
- **deduplication** — duplicates detected by `store` are logged, and a file that
  fails to process increments the failure count.
- **result** — on success it logs the output path and the number of items
  actually inserted.

## Concurrency and the SQLite catch

The workers run concurrently, but SQLite is a *single-writer* database. To keep
that safe, `store.Open` does three things:

- **One writer** — `SetMaxOpenConns(1)` caps the connection pool to a single
  connection, so all writers are inherently serialised rather than racing on
  the pool.
- **WAL mode** — writes happen in write-ahead-log mode, letting readers proceed
  while a write is in flight.
- **Foreign keys + busy timeout** — the DSN sets `_fk=1&_busy_timeout=5000`, so
  the driver waits up to 5 seconds for the write lock before erroring.

`migrate.Schema()/Apply` embeds `internal/migrate/schema.sql` and runs it on every
start; the statement lists are idempotent (`CREATE TABLE IF NOT EXISTS`), so the
schema is safe to apply repeatedly without migrating.

## The database

Single table, single unique key:

| column | type    | constraint | notes |
|--------|---------|------------|-------|
| `id`   | TEXT NOT NULL |      | auto-increment primary key |
| `file` | TEXT    | NOT NULL   | output file the item came from |
| `doi`  | TEXT    | NOT NULL UNIQUE | one per file; the first inserted stays; indexed on `idx_items_file` |

Because `doi` is unique, cross-file is resolved to the first file `ingest`
happened to process; concurrent inserters never lose a row.

The SQL queries live in `db/queries/items.sql` (`InsertItem`, `GetItemByDOI`) and
are generated into `internal/db` by [sqlc](https://sqlc.dev) (see
`sqlc.yaml`). The generated code is checked in so the repo is buildable without a
sqlc install, and the `Querier` interface lets tests swap in a fake.

The full schema is at `internal/migrate/schema.sql`; see `sqlc.yaml` for how the queries are generated.

## Running Nauvis

```
go run . [-query "DOI"] [-in ./data] [-out ./out] [-db ./nauvis.sqlite3] [-jobs N]
```

- **process** (default): `-in` directory of `*.json.gz`, `-out` for the `.json`
  files, `-db` for the SQLite file, `-jobs` for the worker count (defaults to
  `GOMAXPROCS`). Logs `file + output-path` per file; fails non-zero if any file
  fails. `go build` produces the binary that is checked in for a fast warm start.
- **query** (`-query "DOI"`): looks up a DOI and prints which file holds it, then
  exits `1` if the DOI was not recorded. The built binary is present in the repo
  as a convenience for demos. Run it directly once the DB is populated
  (`./nauvis -query "10.1038/sci.repo01"`) or `go run . -query "<DOI>"`.

To rebuild the query after schema changes:

```
go tool sqlc generate          # from the repo root
```

## Design notes

- **Round-trip safety over speed.** The whole extract step is built to preserve
  bytes, not to speed up parsing. There's no value in re-serializing JSON that
  may already be compact.
- **`doi` uniqueness is the central invariant.** Everything — dedup, query, the
  "first file wins" promise — hangs off the `UNIQUE` constraint on `doi`.
- **`main.go` is dispatcher only.** All logic lives in `extract`, `store`, and
  `ingest`; `main.go` exists to parse flags and pick a mode.
- **Build writes go via atomic rename** (`out/<n>.json.tmp` → `out/<n>.json`) so
  a partial frame is never observable.
