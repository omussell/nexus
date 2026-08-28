# How Vulcanus Works

This page explains how the Vulcanus program operates, from the NDJSON files
Nauvis wrote out on disk to the DuckDB it maintains. It is a companion to the
[Overview](index.md); see there for what Vulcanus is and what it aims to.

## At a glance

Vulcanus reverses Nauvis's index. Nauvis records which `(file, doi)` pairs it
saw; Vulcanus reads that SQLite index back, follows each recorded output file,
and loads the **complete contents** of every JSON line into a DuckDB. Each line
of each NDJSON file becomes one row of a single `items` table, storing the raw
JSON document as-is.

```
Nauvis SQLite   ──list──▶   NDJSON files on disk   ──read──▶   DuckDB items table
 (file, doi)                        *.ndjson                         record
```

This is not transformation: Vulcanus never parses or normalises the payload. It
only preserves it, one line at a time, in a single column of JSON text. Every
record that Nauvis saw is ingested; nothing is filtered by DOI.

## The three stages

The pipeline is split into three packages, each with one job.

### 1. Nauvis store — `internal/nauvis`

`nauvis.Open(path)` reads the SQLite database Nauvis wrote and returns a `Store`.
Because Nauvis is the source of truth for which files exist, Vulcanus does not
scan the filesystem itself — it asks the store.

`store.Items()` returns every output file Nauvis recorded, with its DOI count:

```sql
SELECT file, COUNT(*)
FROM items
GROUP BY file
ORDER BY file
```

Each `Item` carries the `Path` (relative to the Nauvis output directory) and the
`Count` of DOIs in that file. The query is grouping over the per-item rows
Nauvis produced, so the count is derived, not stored.

`store.ByDOI` is the inverse lookup: given a DOI, which file first recorded it,
or `sql.ErrNoRows` if unknown. This matches the read side Nauvis exposes and can
underpin lookups when Vulcanus needs to decide where an item came from.

`Open` caps the pool to a single connection (`SetMaxOpenConns(1)`) so reads never
races; this matches Nauvis's own single-writer discipline the way DuckDB's
serialised access serialises every write on Vulcanus's side.

### 2. Ingest — `internal/ingest`

`ingest.Run` ties the two halves together:

```
for each file Nauvis listed:
    open the DuckDB (once, in a single transaction)
    ensure items(record JSON) exists
    for each file:
        read it as NDJSON, one line at a time
        skip blank lines
        reject any line that is not valid JSON
        bind each raw line as one INSERT into items
    commit everything
```

The method takes a `*nauvis.Store`, the base `outDir` every Nauvis path is
joined against, and the `outDB` DuckDB file to write. It returns the total number
of rows ingested across every file.

The implementation:
- **One transaction** — all rows are written in a single `BEGIN … COMMIT`. This
  keeps the DuckDB load atomic: either the whole run lands, or nothing does.
- **A prepared statement** — `INSERT INTO items (record) VALUES (?)` is prepared
  once and reused per line, so the bind cost is minimal.
- **Line-by-line scanning** — each file is read with a `bufio.Scanner` that grows
  up to an `8 MiB` line, so large JSON documents can still be inserted.
- **Faithful preservation** — the bytes of each line are bound verbatim into the
  `JSON` column. No round-tripping through a struct, no re-serialising, no field
  selection: whatever Nauvis emitted is exactly what lands in DuckDB.
- **`CREATE TABLE IF NOT EXISTS`** — the schema uses the same idempotent guard
  Nauvis uses, so reloading never errors and the table is safe to re-run.

`ingestFile` is the per-file worker. Invalid JSON aborts the run with an error
(never silently skipped), because a corrupt record almost certainly means a
corrupt upstream write that should be traced rather than hidden.

### 3. Entry point — `main.go`

`main.go` is dispatcher only. It parses three flags, opens the Nauvis store,
defers its close, and calls `ingest.Run`:

```
-db       path to the Nauvis SQLite database (default nauvis.sqlite3)
-visdir   path to the Nauvis output directory (no default)
-duckdb   the DuckDB file to write (default vulcanus.duckdb)
```

On success it prints a one-line summary — the row count and the cleaned output
path — and exits. Any earlier failure logs and exits non-zero; `Run`'s returned
error is never swallowed.

## The database

A single table, a single column:

| column   | type    | notes |
|----------|---------|-------|
| `record` | JSON    | one row per NDJSON line; the raw document as Nauvis wrote it |

Each row is a full JSON object from a Nauvis output file. Vulcanus stores every
line it is handed — one record per input record — which is what makes the DB
useful for downstream work: reading a `record` gives back the entire item, not a
projection. Because the column is declared `JSON`, DuckDB's JSON functions
(`JSON_SCHEMA`, `JSON_EXTRACT`, `...`) are available directly against the stored
text once the load has happened.

## Running Vulcanus

```
go run . [-db nauvis.sqlite3] [-visdir <nauvis-out>] [-duckdb vulcanus.duckdb]
```

- **process** (default): `-db` the Nauvis SQLite file, `-visdir` the Nauvis
  output directory it wrote (this is where the NDJSON files live, joined against
  the paths returned by the store), and `-duckdb` the DuckDB file to write.

The key contract is faithfulness: Vulcanus treats the bytes coming out of Nauvis
as the data, and DuckDB as the place to store them for later analysis.

## Design notes

- **DuckDB for the work, SQLite for the map.** Nauvis's SQLite `items` is an
  index of file-to-DOI relationships; DuckDB is the wide column store that makes
  it cheap to run queries over the full records once they are on disk.
- **Round-trip preservation over transformation.** The ingest step preserves
  bytes, not parse results. There is no value in re-serialising JSON that is
  already compact.
- **Faithful line counts.** Blank lines are skipped but every non-blank line is
  counted and bound, so the returned total equals the number of actual records
  loaded — a useful cross-check against Nauvis's own DOIs.
- **`main.go` is dispatcher only.** All logic lives in `nauvis` and `ingest`;
  `main.go` only parses flags and picks a mode.
