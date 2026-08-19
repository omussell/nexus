# CROID

The CROID (Crossref Research Object ID) system provides unique, URL-safe identifiers for research objects to track metadata across different systems.

## Overview

A CROID acts as an identifier that is separate from other existing identifiers like DOIs or ORCIDs. This allows the Nexus pipeline to track research objects even when they are undergoing metadata quality changes or when they lack a standardized global identifier.

## Key Functionalities

* **Unique Identification:** Generates 32-character, URL-safe, randomly generated strings to identify research objects.
* **Metadata Association:** Connects original identifiers (e.g., DOI, ORCID) to a CROID for cross-system tracking.
* **Web Service:** Provides a web service at `https://id.crossref.org` to retrieve metadata via API.
* **Provenance & Relationship Support:** Enables tracking of how a research object's metadata has changed across different systems (e.g., Nauvis, Vulcanus).
