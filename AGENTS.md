# AGENTS.md

## Project Overview

NEXUS is a prototype monorepo linking metadata from journals, articles, books, and people. Organized as independent systems in `systems/`.

## Systems

| System | Purpose |
|---|---|
| `nauvis` | Downloads/indexes Crossref `.json.gz` into SQLite; preserves byte-for-byte fidelity; uses workers with `MaxOpenConns(1)` + WAL mode |
| `fulgora` | Downloads/standardizes datasets (ROR, Retraction Watch); tracks versions in SQLite; provides HTTP discovery |
| `croid` | HTTP service minting/serving CROID strings (URL-safe 32-char) |
| `vulcanus` | Ingests/Nauvis+Fulgora data into DuckDB; performs citation/funder matching to enrich metadata |
| `gleba` | Tracks data statistics (citation counts) and provides metrics/patterns |
| `aquilo` | API for processed data; stores in ScyllaDB; serves JSON output |
| `nexus` | Generates relationships/provenance (funding links) from processed data; RDF output |
| `pomus` | HTML UI for accessing processed data; server-rendered with Pico CSS + HTMX |

## Developer Commands

- `go test ./...` — run all tests in a system directory
- `go build ./...` — build a system's binary
- `sqlc generate` — regenerate SQL client code from sqlc queries (aquilo, croid, fulgora, nauvis)
- `uv run mkdocs build -f ./mkdocs.yml -d ./docs` — build docs (root)
- `uv run mkdocs build -f systems/<sys>/mkdocs.yml -d ../../docs/systems/<sys>` — build per-system docs
- `python3 -m http.server -d docs` — serve docs locally

## Database Operations

- Each system stores data in an sqlite database in systems/<sys>/<sys>.db
- `aquilo`, `croid`, `fulgora`, and `nauvis` use sqlc for interacting with sqlite
- `vulcanus` uses DuckDB as column store for ingested/nauvis/fulgora data

## Testing

- Build the binary inside the system directory like systems/<sys>/<sys>
- If any other files are required to be created for testing, create them in the system directory.

## Key Conventions & Gotchas

- **SQLite concurrency**: Always `MaxOpenConns(1)` with concurrent workers (e.g., `nauvis/internal/store`, `croid/internal/server`)
- **Module boundary**: Each system in `systems/` is self-contained with its own `go.mod`, `docs/`, `internal/`
- **Go version**: Systems use `go 1.25.0`

## Verification

- Check `systems/<name>/docs/how-it-works.md` for logic flow
- Run `go test ./...` in any system directory to verify changes
- For vulcanus: verify DuckDB tables via `duckdb vulcanus.duckdb` + `.tables`

## Git

Commit messages must end with your model name in parentheses, e.g. `(ornith1.5:35b)` or `(qwen3.6:35b-a3b)`.
