# NEXUS

The Nexus system manages the provenance and relationship metadata generated during the data processing lifecycle.

## Overview

As data is processed and enriched, transformations and matching operations generate important metadata. Nexus captures these transformations as "provenance" and defines "relationships" between different entities.

## Key Functionalities

* **Provenance Tracking:** Maintains a record of how data has been modified or updated (e.g., which specific snapshot was used to update metadata via Vulcanus).
* **Relationship Management:** Defines and stores connections between entities, such as:
    * Identifying a funder for a journal article.
    * Linking authors to their respective organizations.
* **RDF Metadata:** Stores and provides this metadata in RDF (Resource Description Framework) format.