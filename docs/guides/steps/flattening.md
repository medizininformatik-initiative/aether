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
| `batch_size_mb` | int | 500 | Limit in MB for the JSON bytes held in pending batches, divided across groups (0 = default). See [Memory use](#memory-use) |
| `max_attempts` | int | 0 | Attempts per flattener request, 0-10. `0` uses `retry.max_attempts` |

## Retries and Timeouts

aether sends a flattener request again after a connection error or an HTTP 5xx
response. `max_attempts` sets the number of attempts.

aether does not send a request again when the flattener does not answer within
`timeout`. The flattener can continue to work on the request, and a second
request adds load. If a request times out, decrease `batch_size_mb` or increase
`timeout`.

## Flattener Health Check

The flattener has no health route. aether sends `GET <service_url>/fhir/metadata`
to check that the flattener answers. The check runs at these times:

- `aether pipeline start` checks the flattener before the first step runs, when
  the flattening step is enabled.
- The flattening step checks the flattener again before it sends the first
  request.
- While a request waits for the flattener, aether checks the flattener every
  30 seconds. A check that does not pass within 10 seconds counts as failed.
  After three failed checks in sequence, aether cancels the request and the step
  fails with the error `flattener stopped answering`. A check that passes resets
  the count, so a busy flattener that answers is not cancelled before `timeout`.

The check at start and the check before the first request also require that
the response is a CapabilityStatement that declares the `$run` operation. A
service that answers with another response is not a flattener, and the check
fails without a retry. The checks during a request look only at the HTTP status.

These values are fixed and have no configuration key.

## Lookup File Validation

When the flattening step is enabled, `aether pipeline start` validates the
lookup file before the first step runs. The check validates the JSON structure
against the flatten-lookup schema and the cross-references between elements
(child resolution, parent uniqueness, cycles). Error findings stop the start
with a message that names each defect. Warning findings go to the log, and the
pipeline continues.

## How it Works

Flattening parses the CRTDL to extract attribute groups, builds a ViewDefinition
for each group from the lookup tables, then streams each input file once:

1. **Build the Provenance index of each Bundle:** for each Bundle, the step
   reads its `Provenance` entries. Each Provenance maps its `target[].reference`s
   to an attribute-group ID, taken from the `entity[].what.identifier` entry
   whose system is the attribute-group naming system.
2. **Route and flatten:** the step routes each clinical entry of the Bundle to
   attribute groups by looking up its `ResourceType/id` reference in the index of
   the same Bundle. **A resource with no matching entry in the index of its own
   Bundle is dropped** — it is not written to any group. A resource outside a
   Bundle has no Provenance, so it is also dropped.
3. When a group's batch reaches its share of `batch_size_mb`, the batch is flushed
   to fhir-flattener and its CSV output is appended (one header, multiple data
   batches).

### Memory use

`batch_size_mb` limits the JSON bytes that pending batches hold. The limit is
divided equally across attribute groups. During a flush, the encoding of the
request holds up to two more copies of the batch of that group: one in the
encoder buffer and one in the request body.

`batch_size_mb` does not limit these items:

- The Bundle that the step reads. The step holds the JSON of one Bundle while
  it routes the entries of that Bundle.
- The Provenance index of that Bundle. The step decodes only the Provenance
  entries of the Bundle into maps, and discards the index after the Bundle.

## Output

CSV files are written to `jobs/<job-id>/csv/`, one file per attribute group.
