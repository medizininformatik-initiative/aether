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
    batch_size_mb: 500

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
| `batch_size_mb` | int | 500 | Total memory budget in MB, divided across groups (0 = default) |

## How it Works

1. Parses CRTDL file to extract attribute groups
2. Builds ViewDefinitions from lookup tables
3. Streams FHIR resources from input files in a single pass
4. Routes each resource to its attribute group based on `meta.profile[0]`
5. When a group's batch exceeds `batch_size_mb`, flushes it to fhir-flattener
6. Appends CSV output incrementally (one header, multiple data batches)

Memory usage is bounded by `batch_size_mb` regardless of total dataset size — the budget is divided equally across attribute groups.

## Output

CSV files are written to `jobs/<job-id>/csv/`, one file per attribute group.
