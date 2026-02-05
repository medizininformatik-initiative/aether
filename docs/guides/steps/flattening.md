# Flattening (FHIR to CSV)

Transforms FHIR NDJSON into CSV files using SQL-on-FHIR ViewDefinitions.

## Requirements

- Pipeline must start with a CRTDL file
- fhir-flattener service running
- Lookup table configured

## Configuration

```yaml
services:
  flattening:
    service_url: "http://fhir-flattener:8000"
    lookup_path: "/path/to/flatten-lookup.json"
    formats:
      - csv
    timeout: 30m

pipeline:
  enabled_steps:
    - local_import
    - dimp
    - flattening
```

## Configuration Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `service_url` | string | - | fhir-flattener service URL |
| `lookup_path` | string | - | Path to lookup table file |
| `formats` | []string | ["csv"] | Output formats |
| `timeout` | duration | 30m | Request timeout |

## How it Works

1. Parses CRTDL file to extract attribute groups
2. Builds ViewDefinitions from lookup tables
3. Filters resources by profile
4. Sends data to fhir-flattener service
5. Writes CSV files for each attribute group

## Output

CSV files are written to `jobs/<job-id>/csv/`, one file per attribute group.
