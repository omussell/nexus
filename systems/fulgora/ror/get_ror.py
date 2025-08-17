"""
https://ror.readme.io/docs/data-dump

Send request to
"https://zenodo.org/api/communities/ror-data/records?q=&sort=newest"

- In the response, the most recent record will be hits.hits[0]
- Find the latest file attached to this record by checking the last item in hits.hits[0].files
- The file download URL is in [files.links.download](files.links.self).
- Download the file: curl -o "v1.34-2023-10-12-ror-data.zip" https://zenodo.org/api/records/8436953/files/v1.34-2023-10-12-ror-data.zip/content

The dump gets updated usually at least once per month
Should query the zenodo api regularly (daily, every few days, weekly) to see if new version is available

The schema is here: https://github.com/ror-community/ror-schema/blob/master/ror_schema_v2_1.json
"""

import requests
import polars as pl
import zipfile
from pathlib import Path

import storage.local_file

extractpath=Path(storage.local_file.initial_input_path / "ror")



# this is the code from Adam Buttrick to match org strings to ROR IDs
# https://github.com/adambuttrick/reconcile_crossref_members/blob/main/ror_search.py
# Could load ROR snapshot data into elasticsearch and run our own ROR API



# this needs splitting up into individual functions
# how to store version info?
# we also need to define how the latest version is updated
# ror:latest and ror:1.63
#
# dont download if already downloaded
def get_ror():
    # Find the latest file URL
    zenodo = requests.get("https://zenodo.org/api/communities/ror-data/records?q=&sort=newest").json()
    rordatafile = zenodo["hits"]["hits"][0]["files"][0]["links"]["self"]

    # Get the zip file
    rorzip = requests.get(rordatafile)
    with open("rordata.zip", mode="wb") as file:
        file.write(rorzip.content)

    # Extract the zip file
    with zipfile.ZipFile("rordata.zip", 'r') as zip_ref:
        zip_ref.extractall(extractpath)

    # we need to find the correct file from the downloaded zip
    p = Path("./data/initial_input/ror/")
    for f in list(p.glob("*")):
        if str(f).endswith("schema_v2.json"):
            print(f)

    rordata = pl.read_json(Path(extractpath / "v1.64-2025-04-28-ror-data_schema_v2.json"), infer_schema_length = None)

    rordata.write_parquet(storage.local_file.output_path / "ror.parquet")

if __name__ == "__main__":
    get_ror()

