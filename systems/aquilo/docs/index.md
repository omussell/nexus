# AQUILO

The Aquilo system provides the final API and data delivery layer for the Nexus pipeline, serving the processed and enriched data in a usable format.

## Overview

Once all data processing and enrichment steps are complete, Aquilo exposes the final dataset through an API. This allows external organizations to access the high-quality JSON data produced by the pipeline.

## Key Functionalities

* **Data API:** Provides an interface for users and external systems to query processed data in JSON format.
* **Data Storage:** Efficiently stores the final, high-quality dataset using ScyllaDB.
* **Service Delivery:** Acts as the primary entry point for consumers of the Nexus data ecosystem.
