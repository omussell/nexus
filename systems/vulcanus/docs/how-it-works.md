# How Vulcanus Works

This page explains how the Vulcanus program operates, from the NDJSON files
Nauvis and Fulgora wrote out on disk to the DuckDB it maintains. It is a
companion to the [Overview](index.md); see there for what Vulcanus is and what
it aims to.

## At a glance

Vulcanus reverses each provider's index. Each provider (Nauvis, Fulgora)
records which output files it saw in its own SQLite database; Vulcanus reads
that index back, follows each recorded output file, and loads the **complete
contents** of every JSON line into DuckDB. Each line of each NDJSON file becomes
one row, storing the raw JSON document as-is.

```
Nauvis SQLite   ──list──▶   NDJSON files on disk   ──read──▶   DuckDB `nauvis` table
 (file, doi)                        *.ndjson                         record

Fulgora SQLite  ──list──▶   NDJSON files on disk   ──read──▶   DuckDB `<source>` tables
 (source, output)                 <source>/output/*.json          record
```

The two providers stay separate: Nauvis lands in a single `nauvis` table, while
each Fulgora source (e.g. `ror`, `retractionwatch`) gets its own table. They can
be imported at different times without clobbering each other.

This is not transformation: Vulcanus never parses or normalises the payload. It
only preserves it, one line at a time, in a single column of JSON text. Every
record a provider saw is ingested; nothing is filtered away.

## The stages

The pipeline is split into three packages and one entry point, each with one job.

### 1. Nauvis store — `internal/nauvis`

`nauvis.Open(path)` reads the SQLite database Nauvis wrote and returns a `Store`.
Because Nauvis is the source of truth for which files exist, Vulcanus does not
scan the filesystem itself — it asks the store.

`store.Nauvis()` returns every output file Nauvis recorded, with its DOI count:

```sql
SELECT file, COUNT(*)
FROM nauvis
GROUP BY file
ORDER BY file
```

Each `Nauvis` record carries the `Path` (relative to the Nauvis output directory)
and the `Count` of DOIs in that file. The query is grouping over the per-item rows
Nauvis produced, so the count is derived, not stored.

`store.ByDOI` is the inverse lookup: given a DOI, which file first recorded it,
or `sql.ErrNoRows` if unknown. This matches the read side Nauvis exposes and can
underpin lookups when Vulcanus needs to decide where an item came from.

`Open` caps the pool to a single connection (`SetMaxOpenConns(1)`) so reads never
races; this matches Nauvis's own single-writer discipline the way DuckDB's
serialised access serialises every write on Vulcanus's side.

### 2. Fulgora store — `internal/fulgora`

`fulgora.Open(path, root)` reads the SQLite registry Fulgora wrote and returns
a `Store`. `root` is the Fulgora data root, which the registry's recorded output
paths are relative to; `Source.Output()` joins each against it to find the file
on disk.

`store.Sources(ctx)` returns the latest output for every source Fulgora has
collected, by joining `latest` against `datasets`:

```sql
SELECT latest.source, datasets.version, datasets.output
FROM latest
JOIN datasets ON datasets.id = latest.dataset_id
ORDER BY latest.source
```

Each `Source` carries its `Name()` (used as the DuckDB table identifier) and
`OutputPath()` (the resolved path of its latest NDJSON file, joined against
`root`). Fulgora appends new versions to `datasets` on later runs, so the join
against `latest` exposes only the current version per source. Like the Nauvis
store, `Open` caps the pool to a single connection so reads never race.

### 3. Ingest — `internal/ingest`

`ingest.Run` is provider-aware and ties the halves of either provider together.
Given a `*nauvis.Store` it loads into the single `nauvis` table; given a
`*fulgora.Store` it loads into one table per source:

```
open the DuckDB (once, in a single transaction)
for each file the provider listed:
    ensure the target table (a single `record JSON` column) exists, then clear it
    read the file as NDJSON, one line at a time
        skip blank lines
        reject any line that is not valid JSON
        bind each raw line as one INSERT
commit everything
```

The method takes the provider store, the base `outDir` every path is resolved
against, and the `outDB` DuckDB file to write. It returns the total number of
rows ingested across every file. Clearing the target table first (rather than
appending) is what makes re-running a provider a replacement of its previous
snapshot — and, because each provider only clears its own tables, the two can be
imported at different times without clobbering each other.

The implementation:
- **One transaction** — all rows are written in a single `BEGIN … COMMIT`. This
  keeps the DuckDB load atomic: either the whole run lands, or nothing does.
- **A prepared statement** — `INSERT INTO "records" (record) VALUES (?)` is
  prepared once per table and reused per line, so the bind cost is minimal.
- **Line-by-line scanning** — each file is read with a `bufio.Scanner` that grows
  up to an `8 MiB` line, so large JSON documents can still be inserted.
- **Faithful preservation** — the bytes of each line are bound verbatim into the
  `JSON` column. No round-tripping through a struct, no re-serialising, no field
  selection: whatever a provider emitted is exactly what lands in DuckDB.
- **Safe table names** — provider and source names that become table identifiers
  are wrapped in double quotes with any embedded quotes doubled, so registry
  values can't break (or inject into) the DDL.

`ingestFile` is the per-file worker. Invalid JSON aborts the run with an error
(never silently skipped), because a corrupt record almost certainly means a
corrupt upstream write that should be traced rather than hidden.

### 4. Entry point — `main.go`

`main.go` is dispatcher only. It parses the flags, opens whichever store or
stores are selected, defers their close, and calls `ingest.Run` for each:

```
-only        which provider to ingest: nauvis, fulgora, or all (default all)
-nauvis-db   path to the Nauvis SQLite database (default nauvis.sqlite3)
-nauvis-dir  path to the Nauvis data root (needs `-only nauvis`)
-fulgora-db  path to the Fulgora SQLite database (default fulgora.sqlite3)
-fulgora-dir path to the Fulgora data root (only used with `-only fulgora`)
-duckdb      the DuckDB file to write (all modes write here)
```

The two providers are independent: `-only nauvis` needs only `-nauvis-dir` and
`-nauvis-db`, `-only fulgora` needs only `-fulgora-db` and `-fulgora-dir`. This is what lets a run
import Nauvis now and Fulgora later (or any subset) into the same DuckDB
database.

On success it prints a one-line summary per provider — the row count and the
cleaned output path — and exits. Any earlier failure logs and exits non-zero;
`Run`'s returned error is never swallowed.

## The database

Every table has a single column and is loaded identically — one row per
NDJSON line, the raw JSON document preserved as-is:

| table | column   | type    | notes |
|-------|----------|---------|-------|
| `nauvis` (Nauvis) | `record` | JSON    | one row per NDJSON line; the raw document as Nauvis wrote it |
| `<source>` (Fulgora, one per source) | `record` | JSON    | e.g. `ror`, `retractionwatch`; same shape as `nauvis`, but each source has its own table |

Nauvis and Fulgora are kept **separate**: Nauvis always lands in the single
`nauvis` table, while each Fulgora source gets its own table named after the
source. A row in any table is a full JSON object from that provider's output —
one record per input record. Because the column is declared `JSON`,
DuckDB's JSON functions (`JSON_SCHEMA`, `JSON_EXTRACT`, ...) are available
directly against the stored text once the load has happened.

Re-running a provider replaces its own tables (the single `nauvis` table for
Nauvis, one table per source for Fulgora) and leaves the other provider's tables
untouched, so imports of the two can happen at different times without
clobbering each other.

## Running Vulcanus

```
go run . -only {nauvis|fulgora|all} \
     [-nauvis-db nauvis.sqlite3] [-nauvis-dir <nauvis-root>] \
     [-fulgora-db fulgora.sqlite3] [-fulgora-dir <fulgora-root>] \
    [-duckdb vulcanus.duckdb]
```

- **`-only nauvis`**: `-nauvis-db` the Nauvis SQLite file, `-nauvis-dir` the Nauvis data
  root (where the NDJSON files live, joined against the paths returned by the
  store), and `-duckdb` the DuckDB file to write.
- **`-only fulgora`**: `-fulgora-db` the Fulgora SQLite registry, `-fulgora-dir` the Fulgora
  data root (under which each source's `output/...` file lives), and `-duckdb`.
- **`-only all`** (default): both, using all six flags.

`-only all` ran against the Nauvis and Fulgora test data produced:

```
$ go run . -only all \
    -nauvis-db ../nauvis/nauvis.sqlite3 -nauvis-dir ../nauvis \
    -fulgora-db ../fulgora/fulgora.sqlite3 -fulgora-dir ../fulgora
vulcanus: ingested 55000 nauvis records into vulcanus.duckdb
vulcanus: ingested 209438 fulgora records into vulcanus.duckdb
```

The resulting `vulcanus.duckdb` holds one table per source; list them with the
DuckDB CLI:

```
$ duckdb vulcanus.duckdb
duckdb> .tables
nauvis            retractionwatch  ror
```

The key contract is faithfulness: Vulcanus treats the bytes coming out of each
provider as the data, and DuckDB as the place to store them for later analysis.

## Design notes

- **DuckDB for the work, SQLite for the map.** Each provider's SQLite database
  is an index of the files it wrote (Nauvis: file-to-DOI relationships; Fulgora:
  source-to-latest-output); DuckDB is the column store that makes it cheap to run
  queries over the full records once they are on disk.
- **Round-trip preservation over transformation.** The ingest step preserves
  bytes, not parse results. There is no value in re-serialising JSON that is
  already compact.
- **Faithful line counts.** Blank lines are skipped but every non-blank line is
  counted and bound, so the returned total equals the number of actual records
  loaded for that provider — a useful cross-check against the provider's own
  records.
- **Separate tables per provider.** Nauvis is the "main" data in `nauvis`; Fulgora
  sources are enrichment data, each in its own table. Keeping them separate
  preserves that distinction and lets either be imported independently.
- **`main.go` is dispatcher only.** All logic lives in `nauvis`, `fulgora`, and
  `ingest`; `main.go` only parses flags and picks a mode.
