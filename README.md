# NEXUS

## Research Nexus

As part of my work at Crossref as [SRE](https://www.crossref.org/people/oliver-mussell/), we've been working towards building the "[Research Nexus](https://www.crossref.org/documentation/research-nexus/)" which basically means taking all of the metadata about journals, articles, books, people and forming relationships/links between them.

This repo is meant to act as a prototype for a system design to achieve this goal.

Crossref creates a snapshot of all their data (~250GB compressed) which is published once per year, after which you can use their API to get more recent updates. Other companies like ROR and Orcid also publish their data in a similar way. So we need to get all of that data, link it together and publish the updated data.

## Prototype

The weird system names come from [factorio: space age](https://www.youtube.com/watch?v=OiczN-8QKDA)

```
Nauvis    Fulgora
  |          |
  >   CROID  <
        |
        ^
Vulcanus Gleba
        v
        |
      Aquilo
        |
      Nexus
```


Get project init done for python prototype for all 5 systems so can run CI/CD etc.
Get public data file to use as input data
ID service? The snowflakeID used by twitter is a separate service which uses scala+thrift
  - Maybe that should be part of the requesting a DOI process? Not all stuff gets a DOI. Maybe service should be give DOI if needed, but always give our service ID too.

Should be python prototype, but if you get all this working, make a more proper version in go+pgx+sqlc
Write to filesystem maybe as shim instead of using S3 for prototype
Stick to HTTP and JSON, ignore XML and other formats for now.
Ignore auth for now

Real prod would be Go+postgres+sqlc
MQ would be postgres channel/notify
Filesystem would be S3
https://brandur.org/sqlc

Prototype: 

- Litestar
- attrs/cattrs
- filesystem+sql+mq
- Celery
- htmx+pico css
- process and store as files in parquet format
- json-ld, for plain storage and also for rdf compatibility when storing relationships
- buildbot for CI
- playwright for frontend testing
- pytest for unit+integration tests
- mypy for type checking, eventually move to ty
- ruff
- uv for managing the project

could this be one or more postgres databases instead of redis+sqlite?
Can do k/v, can store json, dont worry about scale

Data seeding with fixtures: https://docs.advanced-alchemy.litestar.dev/latest/usage/database_seeding.html


```
frontend uses htmx and static html/css
|
aquilo api - reads from fileystem or sql for different purposes. These storages should be the interface between generating data and making it accessible to users
nexus pulls from aquilo to store relationships etc. in different RDF format, probably filesystem
|
vulcanus runs data pipelines, polars and duckdb, to extract the data and merge it together. Matching etc.
gleba is event driven, gets notified of changes and updates reports and billing info
|
croid creates new IDs as new data gets created
|
nauvis builds the initial dataset from the snapshot and keeps it up to date
fulgora checks for updates to datasets and gets them
```

Frontend:

- Create main website, documentation site, blog.
- Frontend should be main deposit page / webdeposit, search, chooser etc.
- Microfrontends style, multiple independently deployed similar frontends


We need 5 systems. 
1. Get data from customers
1. Get data from other companies
1. Process the data
1. Generate reports/stats
1. Produce snapshots/API output

Stop the cascade
Self organising - We shouldnt be telling users how to send us relationships between 2 metadata. Make it so that records can be updated by deposit and they'll update the metadata themselves.

Hourglass shape because our systems take data in from many places and generate a single file with all enriched metadata. Then we do interplanetary logistics by taking that, putting into other systems which do wider analysis, querying across datasets etc.

How do you handle giving the data to others? Snapshot and API are great apart from everyone does it differently and it requires O^n requests where n is number of customers and O is number of records to sync.



vulcanus should know the DOIs, ask fulgora, do you have any retractionwatch data about this DOI? Yes, here it is or no go away. Vulcanus pulls what it wants from the API.
Each MQ message is a list of DOIs, then processing of each message has narrower scope for processing and failures


MQ message to notify other systems about updates to specific CROs
They send a HTTP request to the OpenAPI specced REST API to get data. Send subsequent POST requests to update data.



Single quickwit database (maybe part of aquilo?) for search type queries which is kept up to date with everything
Would be backend for CRMDS for example
Keep it central so that more than one service can query it like how cayenne is used by our services now

Provenance - In aquilo?
If we have a standard ID for a research object across all systems, then we could do MQ message to say "system X did action Y on ID Z". That would track the changes done to an object over time.
Would then be relatively straightforward to webapp with db to present it. Would postgres survive that many events/changes?
Maybe if postgres is on aquilo with merged dataset, having the provenance info in separate table or whatever would make it easier to link between them.

Frontend
https://csszengarden.com/221/
HTML and CSS only
Use mox as template
Can use https://developer.mozilla.org/en-US/docs/Web/CSS/columns instead of table pagination

This image is great:
https://www.researchobject.org/images/research-objects-illustration.png

CRO = crossref research object
https://en.wikipedia.org/wiki/Research_Object
CROID = crossref research object identifier 


For each CRO:
Digital identity - Use unique identifiers as names for things, such as DOIs for publications or data, and ORCID ids for researchers.
Data aggregation - Use some form of aggregation to associated related things together that are part of the broader study, investigation etc. so that others may more readily discover those related resources.
Annotation - Provide additional metadata about those things, how they relate to each other, their provenance, how they were produced etc.

could you have central db like citus, dragonfly or s3 and have everything interact with that? Rather than trying to sync data between systems.
CROID could be the key of dragonflydb, and value is json
The CROID should be something indexable by a database, file name on filesystem compatible and URL encodable so could have id.crossref.org/CROID
Modified snowflakeid: 
https://en.wikipedia.org/wiki/Snowflake_ID
Mastodon's modified format has 48 bits for a millisecond-level timestamp, as it uses the UNIX epoch. The remaining 16 bits are for sequence data.
Could use epoch of crossrefs start date at zeroth second
How to handle stuff created in the past? Convert created date to new date? Do we have data with a created date prior to crossref start?
We have 1000 million documents in ES. How many deposits per year do we get? How much data is in each type of dataset we'll sync? Is 5.3 x 10^36 enough, which is randomness of uuidv4? 
You also need to keep the ID short because if its included in every CRO then it will add up to a lot of data.
Use NanoID, with 23 `[0-9][A-Z][a-z][-_]` characters

Would use sql to map CROID to DOI, ROR ID, ORCID, Funder ID etc.

Do you need a service which keeps sql, search, and filesystem etc. in sync whenever changes to CROID?
That would be aquilo, and makes it so that every service can see the most up to date data at all times.
Cloudfront/caching with S3 backend is for members getting individual CROIDs
That also means we can expose the data we used to generate the data, which is needed for provenance. Like we are going to have a CROID annotated with "fulgora got the retractionwatch data on this date at this version, which was used to amend this DOI". You would want that retractionwatch data and the deposit at that time.

Use rabbitmq as the MQ (erlang)
The streams feature looks interesting

Can you get away with doing as much as possible in postgres?
For example, linking a CROID to another ID could be simple relation link via foreign key

Get initial flow working:

- Nauvis getting deposits and storing it
- Vulcanus picking up the new deposits and processing it
- Aquilo presenting it
- Gleba basic stats
- Fulgora get and store ROR

Can store as parquet files
If stored as parquet files, then it has s3 api, can use polars and duckdb to interact with it.

Might need locking service like that google lock service (chubby), which means you are locking access to a specific CROID while you change it. Maybe in redis?

It should be CROID creating an MQ message when it creates a new ID which is then subscribed to by vulcanus and other downstreams

Each services rest api gets its own client library for interacting with it, which is stored in that apps codebase


## nauvis
receive deposit, store on S3
That s3 is final resting place for the objects
Shouldnt be direct deposit to final s3 because there is some processing required
Can read that object from s3 via rest api immediately
Enrichment services are simultaneously notified of new deposit and work to do their parts as data pipelines. Eventually consistent / eventually better quality
Should flow towards the apex, never go backwards. Like shouldnt be pulling deposit data more than once in same pipeline.
Store diff from deposits so we can track change of doi over time based on deposit data.

would this be the only place that needs authentication?

## vulcanus

data pipelines and everything
taking all the raw data and spitting out coherent data structures
Output should be parquet or other standard format
1 file per CRO
Maybe straight into S3 bucket which aquilo then uses to convert into different file types or content types


## fulgora

Data collection from other sources like ROR and retractionwatch and breaking them down into the usable parts we need
Quality, adding information from other sources to the deposits



## gleba 

Stuff that "spoils" or data that becomes stale if not updated, counters which change often and need to stay updated constantly
Reports, billing
resolution reports, doi count reports, citation count, reference count



## aquilo

Combine all the data we've collected into the usable output
Convert the data into the various output types, json, xml, parquet, CSL whatever bibliographic stuff
the final rest api, frontend, snapshots etc.
Could snapshot be incremental? Why is it full dataset every month? Every customer downloading a 200GB file is expensive. Why not make it full monthly and incremental weekly/daily?

## NEXUS

Beyond aquilo

Create relationships between all the items
Is the provenance stuff similar or the same as relationships? No, provenance is showing flow of research object whereas relationships are links between objects
