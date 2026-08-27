# AGENTS.md

## What this repo is
A prototype monorepo for the Crossref "Research Nexus" data pipeline. It is **mostly documentation**: most systems under `systems/` contain only a `README.md` a `docs/` folder and a `mkdocs.yml`. Real code is rare — the only Python file right now is the stub `systems/fulgora/retractionwatch/get_retractionwatch.py`. Plan around the state of the repo, not the README aspirations.

## Docs (the main buildable thing)
- Build the whole site from the repo root: `bash scripts/build_docs.sh` (runs 8 `mkdocs build` calls).
- Preview locally after a build: `bash scripts/serve_docs.sh`.
- Source vs output:
  - Root site: source in `documentation/`, output to `docs/`.
  - Each system: source in `systems/<name>/docs/`, output to `docs/systems/<name>/`.
- **`docs/` is generated output (mkdocs `site_dir`). Do not hand-edit files in `docs/`** — edit the `documentation/*.md` or `systems/<name>/docs/*.md` source and rebuild. The generated `docs/` tree is committed for GitHub Pages.
- Each system's `mkdocs.yml` is independent; add new systems by adding their entry there and a `mkdocs build` line to `scripts/build_docs.sh`.

## Python / tooling
- Use `uv` for dependency management (root and per-system).
- Python versions differ by scope: root `pyproject.toml` pins `3.12`; each system pins `3.13` (via `.python-version`). Don't assume one version.

## Gotchas
- Several `README.md` files describe commands for code that does not exist yet. Verify a command's module/package is actually present before running or citing it.
- `.gitignore` excludes data artifacts (`*.zip *.csv *.parquet *.sqlite`) — large datasets are not committed; do not add them.

## Safety

You are not permitted to read or write files outside of the nexus directory. If you need to run tests for a system, run them within that systems directory. Make a temp directory if required.

Do not use `/tmp` or anywhere else for creating temp files. Everything must stay within the nexus directory.

## Git

When commiting changes, at the end of the commit message include your model name in parentheses like `(ornith1.5:35b)`.
