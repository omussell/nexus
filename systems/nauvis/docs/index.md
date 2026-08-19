# NAUVIS

The Nauvis system extracts and processes Crossref snapshot data from JSON-L format into JSON files suitable for other systems in the Nexus pipeline.

## Overview

This system processes a large dataset where files are individually gzip compressed and named as random integers. The goal is to transform a massive uncompressed dataset (which can reach ~1TB) into properly formatted, individual JSON files for downstream use.

To handle the scale of the data, Nauvis runs multiple workers in parallel to quickly extract and process files.

## Key Functionalities

* **Extraction:** Unpacks gzip-compressed JSON-L files.
* **Processing:** Converts data into standard, properly formatted JSON files.
* **Parallelization:** Utilizes multiple workers to handle thousands of files efficiently.

## Example Workflow

1. Identify a compressed file (e.g., `5.json.gz`).
2. Extract and process it into a properly formatted JSON file (`5.json`).
3. The resulting JSON file is then ready for consumption by other systems in the pipeline.
