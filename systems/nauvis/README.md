# Nauvis System

Extracts and processes the Crossref snapshot data from JSON-L format into JSON files suitable for other systems.

## Overview

This system downloads a dataset containing thousands of JSON-L formatted files which are individually gzip compressed. Each file is named as a random integer with no discernible structure.

The goal is to process a 223GB uncompressed dataset spread across ~1TB when extracted, creating a JSON file for each input gz file for use by other systems in the Nexus pipeline.

The file `10.json` is a dummy file for this dataset that can be used to demonstrate how the data may be structured.

Example workflow:

- Extract 5.json.gz into 5.json
- The dataset isn't guaranteed to have created the JSON file in JSON, it may have added unnecessary whitespace, so ensure 5.json is properly formatted as JSON.

Since there are many thousands of files to process, this system runs multiple workers in parallel to quickly extract and process the total dataset.

## Data Flow

```
data/<N>.json.gz
       │
       ▼
  extract.Process()
  ┌──> out/<N>.json (NDJSON, unchanged)
  │
  └──> []json.RawMessage (full JSON per item)
         │
         ▼
  CROID Client — POST /croid ───► CROID service
         │                              │
         ▼                              ▼
    []json.RawMessage              CROID string
         │                              │
         ▼                              ▼
   parquet.Convert()          RecordMany(ctx, file, dois)
   ┌──> Parquet bytes                  ▼
   │                              SQLite (unchanged)
   ▼
   parquet/<CROID>.parquet
```

## Usage

```bash
# Basic processing (default, unchanged behavior):
nauvis -in data -out out -db nauvis.db

# With CROID minting and Parquet export:
nauvis -in data -out out -db nauvis.db \
  -croid-url http://localhost:9090 \
  -parquet-dir parquet/

# Suppress duplicate DOI logging:
nauvis -in data -out out -db nauvis.db -ignore-dupes
```

## CLI Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-in` | `data` | Directory containing `*.json.gz` input files |
| `-out` | `out` | Directory to write decompressed `*.json` files |
| `-db` | `nauvis.db` | Path to the SQLite database file |
| `-jobs` | `GOMAXPROCS` | Number of parallel workers |
| `-query` | (empty) | Look up a recorded DOI and print its file |
| `-serve` | `false` | Start the HTTP server on `POST /query` |
| `-host` | `localhost:8080` | Address the HTTP server listens on |
| `-croid-url` | (empty) | CROID service URL (e.g. `http://localhost:9090`); empty disables CROID minting |
| `-parquet-dir` | (empty) | Directory to write Parquet files named by CROID; empty disables Parquet export |
| `-ignore-dupes` | `false` | Do not log duplicate DOIs across files |

## Modes

- **process** (default): extracts and records all files from `-in` to `-out` and `-db`
- **query** (`-query "DOI"`): looks up a DOI and prints its source file
- **serve** (`-serve`): starts an HTTP server answering `POST /query`

## See Also

- [How Nauvis Works](docs/how-it-works.md) — code-level walkthrough: extract, store, ingest stages, concurrency model, and database schema
