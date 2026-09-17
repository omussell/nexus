# AGENTS.md

## Project Overview
NEXUS is a prototype for a system that links metadata from journals, articles, books, and people. It is organized as a monorepo of independent systems.

## Architecture
The repository is divided into systems in the `systems/` directory:
- `nauvis`: Downloads and indexes Crossref snapshot files (`.json.gz`) into an SQLite database. Preserves byte-for-byte fidelity of decompressed payloads. Uses workers for parallel extraction and handles SQLite concurrency with `MaxOpenConns(1)` and WAL mode.
- `fulgora`: Data collection system that downloads datasets from external sources (ROR, Retraction Watch), standardizes them, and tracks versions in a SQLite database. Provides an HTTP discovery service.
- `croid`: HTTP service for minting and serving CROID (Crossref Research Object ID) strings. Used to track research objects across different systems and provide provenance. Generates URL-safe 32-character strings.
- `vulcanus`: Data pipelines that ingest and index data (from Nauvis and Fulgora) into a DuckDB database. Performs "matching" (e.g., citation matching, funder/affiliation matching) to enrich metadata.
- `gleba`: Tracks statistics associated with data (e.g., citation counts) and provides metrics for patterns and billing.
- `aquilo`: Main API for accessing processed data, combining all collected data into usable JSON output and storing data in ScyllaDB.
- `nexus`: Generates relationships and provenance (e.g., funding links) from the processed data, providing output in RDF format.
- `pomus`: An HTML user interface for accessing processed data. Minimal, server-side rendered, using Pico CSS and HTMX.

## Developer Commands & Workflow
- **Core Tooling**: Uses `go` for backend systems and `uv` / `python` for the web/pipeline components (see `pyproject.toml`).
- **Database Operations**:
  - Most systems use SQLite.
  - Connection pattern: `SetMaxOpenConns(1)`, `WAL` mode, and `_busy_timeout=5000`.
- **SQLC**: Queries in `db/queries/` are compiled via `sqlc`.
  - Run generation from root: `go tool sqlc generate`
- **Documentation**:
  - Generated via `mkdocs`.
  - Commands are usually located in the system-specific `docs/` folders.

## Key Conventions & Gotchas
- **Atomic Writes**: `nauvis` ensures output fidelity by writing to `.tmp` files and renaming them.
- **SQLite Concurrency**: Because SQLite is a single-writer, always ensure `MaxOpenConns(1)` is set when using concurrent workers (e.g., in `nauvis/internal/store` or `croid/internal/server`).
- **Data Fidelity**: `nauvis` preserves the exact byte-for-byte decompressed payload of Crossref snapshots.
- **Monorepo Structure**: Each system in `systems/` is largely self-contained with its own `go.mod`, `docs`, and `internal` logic.

## Verification
- Check `systems/<name>/docs/how-it-works.md` for specific logic flow of a component.
- Verify database migrations in `internal/migrate/schema.sql`.

## Git

When commiting changes, at the end of the commit message include your model name in parentheses like `(ornith1.5:35b)`.
