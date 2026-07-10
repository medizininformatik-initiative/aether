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

Flattening parses the CRTDL to extract attribute groups, builds a ViewDefinition
for each group from the lookup tables, then runs **two passes** over the input
files:

1. **Pass 1 — build the Provenance index:** scans every input file for
   `Provenance` resources only (clinical resources are discarded to keep memory
   low). Each Provenance maps its `target[].reference`s to an attribute-group ID,
   taken from the `entity[].what.identifier` entry whose system is the
   attribute-group naming system.
2. **Pass 2 — stream, route, and flatten:** streams the clinical resources and
   routes each one to attribute groups by looking up its `ResourceType/id`
   reference in the Provenance index. **A resource with no matching entry in the
   index is dropped** — it is not written to any group.
3. When a group's batch exceeds its share of `batch_size_mb`, the batch is flushed
   to fhir-flattener and its CSV output is appended (one header, multiple data
   batches).

Memory usage is bounded by `batch_size_mb` regardless of total dataset size — the budget is divided equally across attribute groups.

## Output

CSV files are written to `jobs/<job-id>/csv/`, one file per attribute group.
