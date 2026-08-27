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

See [How Nauvis Works](docs/how-it-works.md) for the code-level walkthrough: the three stages (extract, store, ingest), how duplicates and SQLite single-writer concurrency are handled, and the database schema.
