# Storage

When running the systems for tests, we might want to store data as files on the local file system, whereas when deployed in a production environment they would be stored as files on an S3 API compatible object storage.

Similarly, when storing data in a database, for tests we might want to use SQLite but when deployed in production we would use PostgreSQL.

For databases, we would probably have all systems connect to the same PostgreSQL server and have a separate database per system.
