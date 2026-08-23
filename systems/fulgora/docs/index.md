# FULGORA

The Fulgora system is responsible for collecting and storing data from various
external sources (such as ROR and Retraction Watch) to enrich and improve the
primary Crossref data.

## Overview

Fulgora focuses on downloading datasets from multiple organizations, breaking
them down into usable formats, and storing them in a standardized way. It
manages data versioning to ensure that downstream systems always have access to
the latest information.

Fulgora is a single Go binary with three subcommands:

* `fetch` — check each source for a new dataset version, download it,
  standardize it, and record it in a local SQLite registry.
* `status` — print the latest collected version per source.
* `serve` — start an HTTP service that other systems query to discover which
  dataset versions are available and where they live on disk.

It is built to be run on a schedule: `fetch` is idempotent, so a version that
is already recorded in the database is skipped rather than downloaded again.

## Key Functionalities

* **Data Collection:** Downloads datasets from external sources like ROR and
  Retraction Watch.
* **Data Standardization:** Breaks down complex datasets (e.g. unpacking an
  archive) into standardized formats under a stable, versioned directory
  layout.
* **Version Management:** Tracks dataset versions in a SQLite registry so
  `fetch` can be re-run idempotently and downstream systems can identify the
  latest available data.
* **Data Enrichment:** Provides additional information used to enhance and
  improve the primary Crossref data collected by other systems.

## How it works

### Registered sources

Two sources are currently registered, each in its own package under
`internal/source/<name>` so a new contributor can add a source without touching
the others:

| source            | upstream                                                        | version label          | standardized output                            |
|-------------------|-----------------------------------------------------------------|------------------------|------------------------------------------------|
| `ror`             | [ROR data dump](https://zenodo.org/communities/ror-data) (Zenodo) | `vX.X-...` -> `X.X`    | zip unpacked into `output/<version>/`          |
| `retractionwatch` | [Retraction Watch CSV](https://gitlab.com/crossref/retraction-watch-data) (GitLab) | commit date of the CSV | `output/retractionwatch-<date>.csv`            |

Each source implements the `Source` interface defined in
`internal/source/source.go`:

* `Name()` — the source identifier (used as the directory name and the database key).
* `CheckLatest(ctx)` — queries the upstream API and reports the newest version
  and download URL, **without downloading the file**.
* `Process(ctx, version, rawPath, outRoot)` — takes the raw downloaded file and
  produces the standardized output, returning the final path to record.

A shared `Download` helper (same package) fetches a URL into a destination path,
writing to a temporary file first and renaming it into place, so an interrupted
download never leaves a partial file behind.

**ROR** queries the Zenodo community API
(`GET /api/communities/ror-data/records?sort=newest`), takes the newest
record's newest file, and derives the version from the filename
(`v1.34-2023-10-12-ror-data.zip` → `1.34`). Processing unpacks the zip into a
versioned directory, refusing any zip entry that would escape the target
directory (zip-slip protection).

**Retraction Watch** has no explicit version number: the CSV is rewritten in
place on the `main` branch of the GitLab project. Checking asks the GitLab API
for the commit that last touched `retraction_watch.csv` and uses its commit
date (`YYYY-MM-DD`) as the version label. Processing copies the CSV into a
versioned output file.

### The fetch workflow

For each requested source, `internal/collect/collect.go` runs:

1. **Check** — `CheckLatest` returns the newest version and download URL.
2. **Skip if known** — if that `(source, version)` pair is already in the
   registry, return the recorded paths immediately (no download). This is what
   makes `fetch` safe to run on a schedule.
3. **Download** — fetch the raw file into
   `<root>/<source>/initial_input/<source>-<version>_<timestamp><ext>`
   (the timestamp stamp is UTC, e.g. `2025-01-01T00-00-05Z`); on failure the
   partial file is removed.
4. **Process** — the source's `Process` produces the standardized output under
   `<root>/<source>/output/`.
5. **Record** — insert a `datasets` row and upsert the `latest` pointer for the
   source (both done by `internal/store`).

### Storage layout

Raw downloads are kept verbatim under `initial_input/`, and the standardized
result under `output/`, per source:

```
<root>/<source>/initial_input/<source>-<version>_<timestamp><ext>   (raw file)
<root>/<source>/output/<...>                                          (standardized)
```

The `--root` flag (or `FULGORA_ROOT`) controls where this tree lives.

### The registry

All records live in a SQLite database (default file `fulgora.sqlite3`, pure Go
driver `modernc.org/sqlite`, WAL mode, single writer connection). Two tables
(defined in `internal/migrate/schema.sql`, applied idempotently on startup):

* `datasets` — one row per collected version of a source: `source`, `version`,
  `collected_at`, `initial_input` path, `output` path. `UNIQUE (source,
  version)` guarantees each version is recorded once.
* `latest` — one row per source, pointing (via `dataset_id`) at the most recent
  `datasets` row.

Persistence queries are written in SQL under `db/queries/` and compiled by
`sqlc` into `internal/db/` (package `db`, generated — do not edit).
The `internal/store` package wraps those queries into the public
`Record` / `Get` / `Latest` / `ListLatest` / `ListBySource` methods.

## Usage

```
fulgora fetch   [source ...]   check for & download new dataset versions
fulgora status  [source ...]   print the latest collected version per source
fulgora serve                 start the HTTP discovery service (default :8090)
```

If no source is named, all registered sources are used.

Flags (each also available as an environment variable):

| flag       | env var        | default        | meaning                          |
|------------|----------------|----------------|----------------------------------|
| `-db`      | `FULGORA_DB`   | `fulgora.sqlite3` | path to the SQLite database    |
| `-root`    | `FULGORA_ROOT` | `.`            | root dir for per-source data     |
| `-addr`    | `FULGORA_ADDR` | `:8090`        | listen address for `serve`       |

### Example session

```console
$ fulgora fetch ror
[ror] collected 1.63 -> fulgora/ror/output/1.63
$ fulgora status
retractionwatch      2026-08-15   (collected 2026-08-15T09:12:31Z)
ror                  1.63   (collected 2026-08-15T09:12:29Z)
```

## HTTP API

Started with `fulgora serve` (default `:8090`), backed by the SQLite registry.
All responses are JSON.

* `GET /` — service banner listing the endpoints.
* `GET /healthz` — liveness probe (pings the database).
* `GET /latest` — every source's latest version.
* `GET /latest/{source}` — one source's latest version (404 if unknown).
* `GET /latest/{source}/history` — every version of a source, newest first.

Each dataset is reported as:

```json
{
  "source": "ror",
  "version": "1.63",
  "collected_at": "2026-08-15T09:12:29Z",
  "initial_input": "fulgora/ror/initial_input/ror-1.63_2026-08-15T09-12-29Z.zip",
  "output": "fulgora/ror/output/1.63"
}
```

## Adding a new source

1. Create `internal/source/<name>/<name>.go` implementing the
   `source.Source` interface (`Name`, `CheckLatest`, `Process`).
2. Register it in the map in `internal/sources/sources.go`.
3. `go build ./... && go test ./...` — that's it. `fetch`, `status`, and
   `serve` pick the source up automatically.

## Development

* Go toolchain (module `github.com/nexus/fulgora`), `modernc.org/sqlite`.
* After changing `db/queries/*.sql`, regenerate the Go code with
  `sqlc generate` (config in `sqlc.yaml`).
* `go vet ./...`, `go build ./...`, `go test ./...`.

## Package layout

```
main.go                        ->  fetch / status / serve subcommands
internal/source/source.go      ->  the Source interface + shared download helper
internal/source/ror            ->  ROR (Zenodo) source
internal/source/retractionwatch->  Retraction Watch (GitLab) source
internal/sources               ->  the registry: map of name -> constructor
internal/collect               ->  the fetch workflow (check, skip, download, process, record)
internal/store                 ->  the query/persistence layer
internal/db                    ->  sqlc-generated code (DO NOT EDIT)
internal/migrate               ->  schema.sql + Apply()
internal/server                ->  HTTP service for discovery
db/queries                     ->  the SQL that sqlc reads
sqlc.yaml                      ->  sqlc config
```
