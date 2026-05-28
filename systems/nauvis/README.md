# NAUVIS

Downloads the Crossref snapshot and stores it in a format readable by other systems

The Crossref snapshot is made publicly available as a torrent. The latest snapshot data is here: https://academictorrents.com/details/b5ee0e102689b3e67023dd024694c0f5f124646f

After downloading the torrent, the snapshot data is a folder with many tens of thousands of JSON-L formatted files which are gzip compressed. Each file is named as a random integer and the contents have no discernible structure. No schema is available.

The total compressed size is 223GB and if you extract all files its over 1TB.

After downloading, we would want to extract each file and convert it into another format like parquet. However, each file contains a mix of different items in the JSON. We dont want all of those to be put into the same parquet file, they should be in separate files.

An example of a single extracted item can be found in the `10.json` file.
