# VULCANUS
 
Data pipelines to ingest and index collected data.
 
After Crossref data has been collected by Nauvis and data from other organizations has been collected by Fulgora, Vulcanus ingests this data into a DuckDB database.
 
The data in Nauvis is considered the "main" data, while data from Fulgora is used to enrich and improve the Nauvis data in downstream processing.
 
The word "matching" is used to describe methods or algorithms for mapping data to metadata from another data source.
 
For example, citation matching is used to map a string to a DOI: `Boucher RC (2004) New concepts of the pathogenesis... -> 10.1183/090319...`
 
Other types of matching include:
 
```
Journal article -> preprint DOI
Funder information -> grant DOI
Funder name -> ROR ID / Funder ID
Affiliation -> ROR ID
Person information -> ORCID
```
 
The output is a DuckDB database containing the raw records, which can be queried and exported to formats like Parquet.
 
Vulcanus utilizes DuckDB to provide an efficient storage and query engine for these records.

