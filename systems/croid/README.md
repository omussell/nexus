# CROID
CROID is an acronym of "Crossref Research Object ID". CRO is an acronym of "Crossref Research Object". That doesnt mean that Crossref "owns" a research object, its just the name of the identifier we can use to track its metadata across different systems.

We need to use an identifier attached to a research object that is separate from other identifiers like DOI and ORCID. We will use a CROID to identify objects that may already have an identifier like a DOI or ORCID but others do not for example our copy of a specific snapshot of the ROR data.

The purpose of using another identifier is so that we can track research objects across systems using relationship metadata and so that we can provide provenance data on how a research object may have had its metadata quality changed by other systems.

A CROID is a URL safe string of 32 characters with the alphabet `[0-9][A-Z][a-z][-_]`.

In addition to creating and managing CROIDs, it also provides a web service available at https://id.crossref.org which allows us to serve the metadata directly for that CROID in a similar way to DOIs and ROR IDs, e.g. https://id.crossref.org/croid/aaaaa12345aaaaa12345aaaaa12345aa.

The CROID service stores data in a database. It may make sense to have the key be the CROID and the value be JSON so that you can have stuff like which system requested it and its date created
