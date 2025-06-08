# NEXUS

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
Fake handle service to create fake DOIs?
ID service? The snowflakeID used by twitter is a separate service which uses scala+thrift
  - Maybe that should be part of the requesting a DOI process? Not all stuff gets a DOI. Maybe service should be give DOI if needed, but always give our service ID too.

Should be python prototype, but if you get all this working, make a more proper version in go+pgx+sqlc
Write to filesystem maybe as shim instead of using S3 for prototype
Stick to HTTP and JSON, ignore XML and other formats for now.
Ignore auth for now

This isnt even that complicated, crossref has made it complicated.
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

Emphasis on quality processing. I am ror service, grab DOIs files which dont have ror info. Then grab files which do and need updating.
That should be relatively straightforward to find objects which dont have each specific enrichment like ROR info by using polars+duckdb


postgres
maybe citus?


Doctests
mkdocs
Cell based architecture
uv for deps
ruff for formatting
ty for type checking
pydantic

arq+redis/dragonfly for async jobs

Data pipeline with polars and/or duckdb

Stick to HTTP+JSON. This is a prototype, doesnt need to be in go or use msgpack/RPC


vulcanus should know the DOIs, ask fulgora, do you have any retractionwatch data about this DOI? Yes, here it is, no go away. Vulcanus pulls what it wants
Could vulcanus populate SQS with what it wants to ask, fulgora compiles it, then when ready vulcanus pulls it? Like how space platform uses logistics requests to pull from planet.
Each SQS message is a list of DOIs, then processing of each message has narrower scope for processing and failures

Could you implement as something like:
- Vulcanus sends HTTP request to fulgora to request a task ID
- Vulcanus adds DOIs to SQS message(s) with the given task ID
- Fulgora picks up the messages, does the processing to get the data for them and stores one bigger maybe parquet file
- Vulcanus waits 10 minutes, or check task ID status to see if done
- When done, vulcanus downloads the compiled file and processes it
Theres probably a better way of doing that. Im trying to scale it for many research objects and quality reprocessing


MQ message to notify other systems about updates to specific CROs
They send a HTTP request to the OpenAPI specced REST API to get data. Send subsequent POST requests to update data.


You could replace ORM with REDIS-OM: https://redis.io/blog/introducing-redis-om-for-python/
Store data as JSON in dragonfly


From london meeting:

Need IAM to let members self service their own organisation. How do you do that with authelia?
How do you set up API keys with authelia?

Single quickwit database (maybe part of aquilo?) for search type queries which is kept up to date with everything
Would be backend for CRMDS for example
Keep it central so that more than one service can query it like how cayenne is used by our services now

Do we need auth on every system or only deposits?

How to implement multiple resolution / chooser?

Provenance - In aquilo?
If we have a standard ID for a research object across all systems, then we could do pub/sub or RPC style message to say "system X did action Y on ID Z". That would track the changes done to an object over time.
Would then be relatively straightforward to webapp with db to present it. Would postgres survive that many events/changes?
Maybe if citus is on aquilo with merged dataset, having the provenance info in separate table or whatever would make it easier to link between them.

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

Would use redis to map CROID to DOI, ROR ID, ORCID, Funder ID etc.

Maybe you need id.crossref.org to be part of aquilo.
Do you need a service which keeps citus, quickwit, dragonfly and S3 etc. in sync whenever changes to CROID?
That would be aquilo, and makes it so that every service can see the most up to date data at all times.
Cloudfront/caching with S3 backend is for members getting individual CROIDs
That also means we can expose the data we used to generate the data, which is needed for provenance. Like we are going to have a CROID annotated with "fulgora got the retractionwatch data on this date at this version, which was used to amend this DOI". You would want that retractionwatch data and the deposit at that time.

Use rabbitmq as the MQ (erlang)
The streams feature looks interesting

Can you get away with doing as much as possible in postgres?
Or is that going to hit manifold problems?
You can have multiple DBs obviously
For example, linking a CROID to another ID could be simple relation link via foreign key

Use ARQ with redis for job scheduling

Get initial flow working:

- Nauvis getting deposits and storing it
- Vulcanus picking up the new deposits and processing it
- Aquilo presenting it
- Gleba basic stats
- Fulgora get and store ROR

Use seaweedfs for single storage
Can store as parquet files
Use redis as filer for lots of files: https://github.com/seaweedfs/seaweedfs/wiki/Filer-Redis-Setup
If stored as parquet files, then it has s3 api, can use polars and duckdb to interact with it.

Create locking service like that google lock service, which means you are locking access to a specific CROID while you change it. Maybe in redis?

It should be CROID creating an MQ message when it creates a new ID which is then subscribed to by vulcanus and other downstreams

Each services rest api gets its own client library for interacting with it, which is stored in that apps codebase

It probably makes sense to stick with Redis as database at least for prototype
In reality everything you're doing with redis would be in postgres instead
Go + pgx + postgres would be far more performant

According to doi.org there are 450,000,000 registered DOIs. 
Redis has 2 billion keys limit

Could try scylladb for aquilo. Cassandra, nosql document store
Stick to redis at first

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
