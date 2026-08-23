# FULGORA

Fulgora is the **data collection** system of the Research Nexus pipeline. It
downloads datasets from external sources, standardizes them, stores them in a
reproducible layout, and tracks their versions so other systems always know
what the newest available version is.

Currently two sources are implemented; each lives in its own package so new
contributors can add a source without touching the others:

| source          | upstream                                                        | version label        |
|-----------------|-----------------------------------------------------------------|----------------------|
| `ror`           | [ROR data dump](https://zenodo.org/communities/ror-data) (Zenodo) | `vX.X-...` -> `X.X` |
| `retractionwatch` | [Retraction Watch CSV](https://gitlab.com/crossref/retraction-watch-data) | commit date of the CSV |

## Architecture

```
cmd: main.go            ->  fetch / status / serve
internal/source         ->  the Source interface + shared download helper
internal/source/ror     ->  ROR (Zenodo) source
internal/source/retractionwatch -> Retraction Watch (GitLab) source
internal/sources        ->  the registry: add a source here
internal/collect        ->  the fetch workflow (check, download, process, record)
internal/store          ->  the query/persistence layer
internal/db             ->  sqlc-generated code (DO NOT EDIT)
internal/migrate        ->  schema.sql + Apply()
internal/server         ->  HTTP service for discovery
db/queries              ->  the SQL that sqlc reads
sqlc.yaml               ->  sqlc config
```

## Storage layout

Raw downloads are kept verbatim under `initial_input/`, and the standardized
result under `output/`, per source:

```
<root>/<source>/initial_input/<source>-<version>_<timestamp><ext>   (raw file)
<root>/<source>/output/<...>                                          (standardized)
```

The `--root` flag (or `FULGORA_ROOT`) controls where this tree lives. Metadata
about every collected version — and which one is the latest — is recorded in a
SQLite database.

## Schema

Two tables: `datasets` (one row per collected version of a source) and `latest`
(one row per source pointing at the newest `datasets` row). See
`internal/migrate/schema.sql`.

## CLI

```
fulgora fetch   [source ...]   check for & download new dataset versions
fulgora status  [source ...]   print the latest collected version per source
fulgora serve                 start the HTTP discovery service (default :8090)
```

`fetch` is idempotent: a version already recorded in the database is skipped,
so it is safe to run on a schedule (daily/weekly). If no source is named, all
registered sources are processed.

Flags (or `FULGORA_DB`, `FULGORA_ROOT`, `FULGORA_ADDR` env vars): `-db`,
`-root`, `-addr`.

## HTTP API

Started with `fulgora serve`:

- `GET /`                         — service banner
- `GET /healthz`                  — liveness probe
- `GET /latest`                   — every source's latest version
- `GET /latest/{source}`          — one source's latest version
- `GET /latest/{source}/history`  — every version of a source (newest first)

## Adding a new source

1. Create `internal/source/<name>/<name>.go` implementing the
   `source.Source` interface (`Name`, `CheckLatest`, `Process`).
2. Add a factory function to `internal/sources/sources.go`.
3. `go build ./... && go test ./...` — that's it. `fetch`, `status`, and `serve`
   pick the source up automatically.

## Development

- Go toolchain (module `github.com/nexus/fulgora`), `modernc.org/sqlite`.
- After changing `db/queries/*.sql`, regenerate with `sqlc generate`.
- `go vet ./...`, `go build ./...`, `go test ./...`.
