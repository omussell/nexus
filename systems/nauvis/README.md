# Nauvis System

Extracts and processes the Crossref snapshot data from JSON-L format into parquet files suitable for other systems.

## Overview

This system downloads (manually provided) datasets containing thousands of JSON-L formatted files which are gzip compressed. Each file is named as a random integer with no discernible structure, containing mixed items within each file that need to be split into separate parquet files for downstream processing.

The goal is to process a 223GB uncompressed dataset spread across ~1TB when extracted, creating individual parquet files for each JSON record for use by other systems in the Nexus pipeline.

## Data Format

Each input file contains JSON-L (JSON Lines) format where multiple JSON objects may be contained within a single file. Each object represents a research publication with fields like:
- `DOI`
- `title`, `author`, `publisher`, and metadata fields
- Example: https://www.crossref.org/purpose/metadata/

## Input Format

The dataset consists of gzip-compressed JSON files named as random integers:
- File names are random integers (no discernible structure)
- No schema is available for the extracted data
- Files contain mixed types within individual JSON-L documents
- Each extracted item should be processed into its own parquet file

Example item from `10.json`:
```json
{
  "DOI": "10.1016/j.ultrasmedbio.2024.01.039",
  "title": ["ULTRASONOGRAPHIC FEATURES OF SUBLINGUAL GLAND STONE"],
  "author": [{"given": "Jeong Kyu", "family": "Kim"}],
  "publisher": "Elsevier BV"
}
```

## Current Implementation - Phase 1

Phase 1 extracts files from the extracted snapshot directory structure:
- Processes JSON-L files with `.jsonl` or `.gz` extensions
- Streams individual objects and validates document structure
- Writes each processed object to parquet format using polars efficiently

## Processing Pipeline

1. **Input Discovery**: Walk through extracted directory, detect gzip files
2. **Stream Processing**: Stream each file line-by-line using polars for efficient JSON parsing
3. **Parallel Extraction**: Use worker pool pattern for performance on large datasets
4. **Output Creation**: Write each record as separate parquet file in organized output directory structure

## Output Structure

The processed data will be stored in an organized directory structure:
```
docs/nauvis/
├── n-item-files/          # Container for all output files
│   ├── 0-999/            # First batch of files
│   │   ├── 1.parquet
│   │   ├── 2.parquet
│   │   └── ... (up to 1000 files)
│   ├── 1000-1999/         # Second batch (files 1000-1999)
│   │   └── ...
│   └── ...
```

Each parquet file contains a single JSON record with all metadata fields.

## Requirements

- Python ≥3.13
- polars >=1.29.0
- Input: Extracted Crossref data directory (containing gzip JSON files)
- Output: Organized parquet files for downstream systems (Aquilo, Vulcanus, etc.)

## Usage

```bash
# Install dependencies
uv sync

# Run the extraction pipeline
python -m nauvis.extract_pipeline --input-dir /path/to/extracted/data --output-dir ./docs/nauvis/n-item-files

# Or use the main CLI entry point
python -m nauvis.main --input /path/to/input --output ./docs/nauvis/n-item-files
```

The pipeline supports progress tracking, parallel processing, and error recovery.
