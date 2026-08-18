# Nauvis System

Extracts and processes the Crossref snapshot data from JSON-L format into parquet files suitable for other systems.

## Overview

This system downloads a dataset containing thousands of JSON-L formatted files which are individually gzip compressed. Each file is named as a random integer with no discernible structure, containing mixed items within each file that need to be split into separate parquet files for downstream processing.

The goal is to process a 223GB uncompressed dataset spread across ~1TB when extracted, creating individual parquet files for each JSON record for use by other systems in the Nexus pipeline.

The file `10.json` is a file in this dataset that can be used as an example.

Example workflow:

- Extract 10.json.gz into 10.json
- Convert 10.json into parquet format as 10.parquet
- Remove 10.json. Keep 10.json.gz.

Since there are many thousands of files to process, this system runs multiple workers in parallel to quickly extract and convert the total dataset.

## Data Format

Each input file contains JSON-L (JSON Lines) format where multiple JSON objects may be contained within a single file. Each object represents a research publication with fields like:
- `DOI`
- `title`, `author`, `publisher`, and metadata fields

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
