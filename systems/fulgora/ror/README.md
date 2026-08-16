We want to write a python script to download the data for ROR and convert it to parquet format.


Send request to "https://zenodo.org/api/communities/ror-data/records?q=&sort=newest"

- In the response, the most recent record will be hits.hits[0]
- Find the latest file attached to this record by checking the last item in hits.hits[0].files
- The file download URL is in [files.links.download](files.links.self).
- Download the file: curl -o "v1.34-2023-10-12-ror-data.zip" https://zenodo.org/api/records/8436953/files/v1.34-2023-10-12-ror-data.zip/content

The dump gets updated usually at least once per month
Should query the zenodo api regularly (daily, every few days, weekly) to see if new version is available

The schema is here: https://github.com/ror-community/ror-schema/blob/master/ror_schema_v2_1.json


The documentation is available at https://ror.readme.io/docs/data-dump
