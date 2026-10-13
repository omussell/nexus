# Open Funder Registry

The Crossref Open Funder Registry provides a comprehensive list of research
funders along with their DOI identifiers.

## Source

| Property | Value |
|----------|-------|
| Upstream | [Crossref funderNames](https://doi.crossref.org/funderNames?mode=list) |
| Format | CSV (`uri`, `primary_name_display`, `replaced`) |
| Update frequency | Irregular (maintained by Crossref) |

## Fields

| Column | Description |
|--------|-------------|
| `uri` | DOI-resolvable URL for the funder, e.g. `http://dx.doi.org/10.13039/100004343` |
| `primary_name_display` | Human-readable name of the funder |
| `replaced` | `"R"` if this funder has been replaced by another funder entry |

## Processing

The raw CSV is downloaded and converted to NDJSON (one JSON object per line)
under `output/openfunderregistry-<date>.json`.

## Versioning

The CSV has no explicit version number. The version label is the current date
(`YYYY-MM-DD`), so the dataset is re-downloaded on each new day.
