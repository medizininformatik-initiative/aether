# Validation

Validates FHIR resources against a FHIR validation service before further processing.

## What it does

- Reads FHIR NDJSON files from the previous step's output
- Chunks resources into Bundles and sends them to a validation service concurrently
- Writes per-file OperationOutcome reports (all results) to `validation/` directory
- Stops the pipeline by default when validation errors are found (`fail_on_error`)

## Configuration

```yaml
services:
  validation:
    url: "http://your-validator:8080/fhir"
    max_concurrent_requests: 4
    bundle_chunk_size_mb: 10
    fail_on_error: true

pipeline:
  enabled_steps:
    - local_import
    - validation
    - dimp
```

## Configuration Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `url` | string | - | Validation service URL (required) |
| `max_concurrent_requests` | int | 4 | Concurrent validation requests |
| `bundle_chunk_size_mb` | int | 10 | Bundle chunk size for batching resources (MB) |
| `fail_on_error` | bool | true | Stop pipeline when validation finds data quality errors |

## Error Behavior

Aether distinguishes between two types of errors during validation:

**Infrastructure errors** (connection refused, HTTP 5xx) always fail the step immediately and are retryable.

**Data quality errors** (invalid FHIR resources) are controlled by `fail_on_error`:

| `fail_on_error` | Behavior |
|-----------------|----------|
| `true` (default) | Reports are written, pipeline stops after validation completes |
| `false` | Reports are written, pipeline continues to next step |

When `fail_on_error` stops the pipeline, the validation step itself is still marked as `completed` — it finished its work. The pipeline stops because the data has quality issues, not because the step failed.

## Bundle Chunking

Resources are chunked into FHIR collection Bundles before sending to the validator:

- Resources exceeding the chunk threshold are isolated into their own single-entry chunks
- Chunks are validated concurrently (up to `max_concurrent_requests`)
- Each chunk includes `fullUrl` on all entries as required by FHIR R4

## Output

Validation reports are saved to `jobs/<job-id>/validation/`:

```
jobs/<job-id>/validation/
├── Patient.validation.ndjson        # OperationOutcome for Patient validation
├── Condition.validation.ndjson      # OperationOutcome for Condition validation
└── Observation.validation.ndjson    # OperationOutcome for Observation validation
```

Each report file contains one OperationOutcome per line (NDJSON format) for all validated chunks, including successful validations with informational results.

## Resumption

Validation supports resumption — files with existing reports are skipped on re-run. To re-validate a file, delete its report from the `validation/` directory.
