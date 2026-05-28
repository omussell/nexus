# VULCANUS


Data pipelines to take the collected data and process it

After Crossref data has been collected by Nauvis and data from other organizations has been collected by Fulgora, we need to process is with Vulcanus. 

The data in Nauvis should be considered the "main" data, with the data from Fulgora being used to enrich and improve the Nauvis data.

The word "matching" is used to describe a method or algorithm of how to map data to metadata from another data source.

For example, citation matching is used to map a string to a DOI: `Boucher RC (2004) New concepts of the pathogenesis... -> 10.1183/090319...`

Other types of matching:

```
Journal article -> preprint DOI
Funder information -> grant DOI
Funder name -> ROR ID / Funder ID
Affiliation -> ROR ID
Person information -> ORCID
```

The output should be a single file per CRO, probably in parquet format, and stored on S3 API compatible storage.
