# FULGORA

- Data collection from other sources like ROR and retractionwatch and breaking them down into the usable parts we need
- Improving the data quality, adding information from other sources to the data we collect from Crossref data

The Fulgora system would be responsible for downloading the various datasets from different sources and storing that data in a standard way on storage that other systems can access.

These datasets change over time and are usually tracked by changing their version number. So we would want to regularly check if a new version has been released and download the updated dataset. Other systems would almost always want to know what the latest version available is so that they can get that data for further processing.

For example in a database you could have single table for latest entries like

```
ror | 1.63
orcid | 1.2.3
```

and separate table for all of them like

```
type | version | collected | initial_input | output
ror | 1.63 | collected 2025-01-01 | stored in s3 at s3://fulgora/ror/initial_input/ror-1.63_2025-01... | stored in s3 at s3://fulgora/ror/output/ror-1.63.parquet
ror | 1.64 | collected 2025-01-02 | stored in s3 at s3://fulgora/ror/initial_input/ror-1.64_2025-01... | stored in s3 at s3://fulgora/ror/output/ror-1.64.parquet
ror | 1.65 | collected 2025-01-03 | stored in s3 at s3://fulgora/ror/initial_input/ror-1.65_2025-01... | stored in s3 at s3://fulgora/ror/output/ror-1.65.parquet
orcid | 1.1.1 | collected 2025-01-01 | stored in s3 at s3://fulgora/orcid/initial_input/orcid-1.1.1... | stored in s3 at s3://fulgora/ror/output/orcid-1.1.1.parquet
orcid | 1.2.1 | collected 2025-01-02 | stored in s3 at s3://fulgora/orcid/initial_input/orcid-1.2.1... | stored in s3 at s3://fulgora/ror/output/orcid-1.1.2.parquet
orcid | 1.2.3 | collected 2025-01-03 | stored in s3 at s3://fulgora/orcid/initial_input/orcid-1.2.3... | stored in s3 at s3://fulgora/ror/output/orcid-1.1.3.parquet
```

that latest version table would be foreign key relationship to the main table. 

