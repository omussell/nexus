# VULCANUS

The Vulcanus system is responsible for processing and enriching the data collected by other systems like Nauvis and Fulgora.

## Overview

Vulcanus takes the "main" data collected by Nauvis and enriches it with data from Fulgora. A key part of this process is "matching"—using algorithms to map data from one source to metadata from another.

## Key Functionalities

* **Data Enrichment:** Enhances Nauvis data with additional information from Fulgora.
* **Data Matching:**
    * Maps citation strings to DOIs.
    * Maps journal articles to preprint DOIs.
    * Maps funder info to grant DOIs.
    * Maps funder names to ROR/Funder IDs.
    * Maps affiliations to ROR IDs.
    * Maps person information to ORCID.
* **Data Output:** Produces single files per CRO, ideally in Parquet format.
* **Processing Engine:** Utilizes DuckDB to assist with processing Parquet files.
