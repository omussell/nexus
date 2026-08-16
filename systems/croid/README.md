# CROID

CROID is an acronym of "Crossref Research Object ID". CRO is an acronym of "Crossref Research Object". That doesnt mean that Crossref "owns" a research object, its just the name of the identifier we can use to track its metadata across different systems.

We need to use an identifier attached to a research object that is separate from other identifiers like DOI and ORCID. We will use a CROID to identify objects that may already have an identifier like a DOI or ORCID but others do not for example our copy of a specific snapshot of the ROR data.

The purpose of using another identifier is so that we can track research objects across systems using relationship metadata and so that we can provide provenance data on how a research object may have had its metadata quality changed by other systems.

A CROID is a URL safe string of 32 characters with the alphabet `[0-9][A-Z][a-z][-_]`. The characters are randomly generated.

In addition to creating and managing CROIDs, it also provides a web service available at https://id.crossref.org which allows us to serve the metadata directly for that CROID in a similar way to DOIs and ROR IDs, e.g. https://id.crossref.org/croid/aaaaa12345_aaaaa12345-aaaaa12345.

The CROID service will create new CROID's when it receives a POST request to its API. It stores that data in a database. It serves the data from the database to the API so that other services can then send get requests to retrieve information.

Another reason is because we want to have an ID that is URL safe to make getting information easier. A DOI as an identifier can contain many characters that are not URL safe. 

## How it works

You send a POST request to https://id.crossref.org/croid with a body like:

```
{
  "cro_type": "DOI",
  "cro_value": "10.5555/12345678",
  "system": "nauvis"
}
```

then in the database it is stored as:

```
id: primary key
cro_type: "DOI",
cro_value: "10.5555/12345678",
system: "nauvis"
croid: The generated CROID like "aaaaa12345_aaaaa12345-aaaaa12345"
created_at: A timestamp of the time this record was created
```

You might want to find the DOI for a given CROID, so you send a HTTP GET request to `https://id.crossref.org/croid/aaaaa12345_aaaaa12345-aaaaa12345` and its responds with:

```
{
  "cro_type": "DOI",
  "cro_value": "10.5555/12345678",
  "system": "nauvis",
  "croid": "aaaaa12345_aaaaa12345-aaaaa12345",
  "created_at": "2026-05-28T19:04:17+01:00"
}
```
