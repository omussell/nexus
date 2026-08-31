# VULCANUS
 
The Vulcanus system is responsible for ingesting and indexing the data collected by other systems like Nauvis and Fulgora into a DuckDB database.
 
## Overview
 
Vulcanus takes the "main" data collected by Nauvis and the data from Fulgora, loading it into a central DuckDB database. It handles the mapping of output files from these systems into structured tables, preserving the raw records for efficient downstream querying and analysis.
 
## Key Functionalities
 
* **Data Ingestion:** Consumes NDJSON files from Nauvis and Fulgora systems and loads them into DuckDB.
* **Provider Isolation:** Loads Nauvis data into a single `items` table and each Fulgora source into its own dedicated table.
* **Relational Mapping:** Uses SQLite indices from the source systems to resolve file paths and identify records.
* **Faithful Preservation:** Ingests raw JSON records verbatim into a `JSON` column in DuckDB, preserving the original data structure.
* **DuckDB Integration:** Provides a structured database for downstream analysis using DuckDB's JSON functions.

