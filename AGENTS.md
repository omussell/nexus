# Research Nexus Core Principles

The codebase represents a multi-systems data integration pipeline for academic metadata. Agents must be aware of the overall workflow dependencies and data semantics.

## Architecture & Boundaries

*   **Systems Components**: The platform comprises interconnected services in the `systems/` directory:
    *   `Nauvis`: Ingests Crossref snapshots. Implemented in **Go**. Stores data as files and maintains associated metadata in a **PostgreSQL** database.
    *   `Fulgora`: Ingests external organization data (e.g., ROR). Implemented in **Python**. Stores data as files on **S3**.
    *   `CROID`: Essential for generating and standardizing unique identifier strings used across all data types. Implemented in **Go**. Stores data as files and maintains associated metadata in a **PostgreSQL** database.
    *   `Storage`: Defines the mechanism and type of data persistence used by all systems.
    *   `Vulcanus`: Runs the primary data pipelines, consuming snapshots from Nauvis/Fulgora and structuring the data. Implemented in **Python**. Consumes data stored on S3 and uses **DuckDB** internally for data enrichment pipelines.
    *   `Gleba`: Dedicated to collecting and maintaining statistical metadata (e.g., citation counts). Implemented in **Go**. Stores data as files and maintains associated metadata in a **PostgreSQL** database.
    *   `Nexus`: **Crucial step.** Generates two types of metadata:
        1.  **Provenance**: Records which source (snapshot, system run) created or modified a piece of data.
        2.  **Relationships**: Determines and links abstract relationships (e.g., "Organization X funded Article Y"). Implemented in **Go**. Stores final structured data as files on **S3** in **RDF** format.
    *   `Aquilo`: Main API for processed data access. Implemented in **Go**. Stores and presents processed data using **ScyllaDB** for JSON format access.
    *   `Pomus`: Frontend (read-only UI using HTMX/Pico CSS) for viewing data from `Aquilo`/`Nexus`.

## Workflow & Conventions

*   **Data Flow**: Processing is not linear. Data streams often flow `Source -> (Nauvis/Fulgora) -> CROID -> Vulcanus -> (Gleba/Nexus) -> Storage -> Aquilo`.
*   **Schema/Linking**: Always assume that any new data must be localized and traceable using `CROID` logic before being processed by `Vulcanus`.
*   **Execution Order**: When verifying changes or running a full development cycle, the standard sequence is:
    1.  Code Changes ->
    2.  `lint` ->
    3.  `typecheck` ->
    4.  `test`
*   **Source of Truth**: Trust the READMEs and configuration files found in the `systems/*/` directories over general assumptions. The system uses many cross-cutting concerns (ID assignment, relationship mapping, provenance tracking).
