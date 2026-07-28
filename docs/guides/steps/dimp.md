# DIMP (Pseudonymization)

De-identifies FHIR data using a DIMP service.

## What it does

- Sends FHIR Bundles to DIMP service
- Automatically splits large Bundles (>10 MB by default)
- Saves pseudonymized data

## Configuration

```yaml
services:
  dimp:
    url: "http://your-dimp-server:32861"  # server root; /fhir appended by client
    bundle_split_threshold_mb: 10  # 1-100 MB, default: 10

pipeline:
  enabled_steps:
    - local_import
    - dimp
```

## Configuration Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `url` | string | - | DIMP server root URL (required). Do not include `/fhir` — the client appends it. |
| `bundle_split_threshold_mb` | int | 10 | Split Bundles larger than this (1-100 MB) |

## Bundle Splitting

Large FHIR Bundles are automatically split to prevent HTTP 413 errors:

- Bundles exceeding the threshold are partitioned into smaller chunks
- Each chunk is sent separately to DIMP
- Results are reassembled after processing
- 100% data preservation during split-reassemble

## Output

Pseudonymized files are saved to `jobs/<job-id>/dimp/`
