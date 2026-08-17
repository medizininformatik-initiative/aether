# Configuration Reference

Complete reference for all Aether configuration options.

## Configuration Schema

```yaml
services:
  torch:
    base_url: string
    auth:
      username: string
      password: string
      oauth_issuer_uri: string      # OAuth2 client-credentials (alt to username/password)
      oauth_client_id: string
      oauth_client_secret: string
      api_key: string
      api_key_header: string        # default: x-api-key
    extraction_timeout: duration    # default: PT30M
    polling_interval: duration      # default: PT5S
    max_polling_interval: duration  # default: PT30S
    download_stall_timeout: duration # default: PT1M

  dimp:
    url: string
    bundle_split_threshold_mb: integer   # 1-100, default: 10
    auth:
      username: string
      password: string
      oauth_issuer_uri: string
      oauth_client_id: string
      oauth_client_secret: string
      api_key: string
      api_key_header: string             # default: x-api-key

  flattening:
    service_url: string
    lookup_path: string
    formats: [string]                    # ["csv"]
    timeout: duration                    # default: PT30M
    batch_size_mb: integer               # default: 500

  send:
    send_as: string                      # "direct_resource_load", "transfer_load", or "s3_upload"
    url: string                          # required for FHIR modes; ignored for s3_upload
    batch_size: integer                  # 0-1000, default: 100 (direct_resource_load only)
    auth:                                # FHIR auth, or proxy auth in s3_upload mode
      username: string
      password: string
      oauth_issuer_uri: string
      oauth_client_id: string
      oauth_client_secret: string
      api_key: string                    # FHIR modes only; ignored for s3_upload
      api_key_header: string             # default: x-api-key
    transfer:                            # transfer_load only
      project_identifier: string
      organization_identifier: string
    s3:                                  # s3_upload only
      bucket: string                     # required
      region: string                     # required
      access_key_id: string              # required
      secret_access_key: string          # required
      endpoint: string                   # custom S3-compatible endpoint
      use_path_style: boolean            # default: false
      timeout: duration                  # default: PT30M

  validation:
    url: string
    max_concurrent_requests: integer   # default: 4
    bundle_chunk_size_mb: integer      # default: 10
    fail_on_error: boolean             # default: true

  local_import:
    dir: string
    recursive: boolean                 # default: false

  crtdl_preprocessing:
    enabled: boolean                       # default: false
    enrichments_path: string               # Path to external JSON file
    enrichments:                           # Inline enrichment rules
      - group_reference: string
        create_if_not_exists:              # Optional: create group if not in CRTDL
          group_name: string
        attributes_to_add:
          - attribute_ref: string
            must_have: boolean
            linked_groups: [string]        # Profile URLs, resolved to group IDs

pipeline:
  enabled_steps: [string]

retry:
  max_attempts: integer                  # 1-10, default: 5
  initial_backoff_ms: integer            # default: 1000
  max_backoff_ms: integer                # default: 30000

tls:
  ca_cert_path: string                   # PEM bundle of additional trusted certs
  insecure_skip_verify: boolean          # default: false

compression:
  enabled: boolean                       # default: true
  level: string                          # fastest, default, better, best

jobs_dir: string                         # default: ./jobs
```

## Services

### TORCH

TORCH server for FHIR data extraction.

```yaml
services:
  torch:
    base_url: "https://torch.example.org"
    auth:
      username: "${TORCH_USER}"
      password: "${TORCH_PASSWORD}"
    extraction_timeout: PT30M
    polling_interval: PT5S
    max_polling_interval: PT30S
```

The `auth` block has the same fields and the same rules as the `auth` block of
[DIMP](#dimp) and [Send](#send).

::: warning Deprecated
Earlier versions put `username`, `password`, `oauth_issuer_uri`,
`oauth_client_id`, and `oauth_client_secret` directly in the `torch` block.
Aether still accepts these keys, and the matching `AETHER_SERVICES_TORCH_USERNAME`
style variables, but writes a warning. A later major version removes them.
A configuration file that uses both shapes together is an error.
:::

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `base_url` | string | - | TORCH server URL (required if torch step enabled) |
| `auth.username` | string | - | Basic Auth username |
| `auth.password` | string | - | Basic Auth password |
| `auth.oauth_issuer_uri` | string | - | OAuth 2.0 issuer URI. When set, Aether fetches client-credentials bearer tokens instead of using Basic Auth. |
| `auth.oauth_client_id` | string | - | OAuth 2.0 client ID |
| `auth.oauth_client_secret` | string | - | OAuth 2.0 client secret |
| `auth.api_key` | string | - | API key. Sent in its own header, not in `Authorization`. |
| `auth.api_key_header` | string | `x-api-key` | Header that carries `api_key` |
| `extraction_timeout` | duration | PT30M | Liveness window, not a total cap: max time to wait without a response from TORCH. Reset on every status response (200/202), so a long but responsive extraction never trips it. See [ADR 0001](../adr/0001-extraction-timeout-liveness.md). |
| `polling_interval` | duration | PT5S | Initial status check interval |
| `max_polling_interval` | duration | PT30S | Max interval (exponential backoff cap) |
| `file_ready_retries` | int | 10 | Number of retries for file availability check |
| `file_ready_interval` | duration | PT10S | Interval between file availability checks |
| `download_stall_timeout` | duration | PT1M | Inactivity window while streaming a result file to disk: the download is canceled only if no bytes arrive for this long. `0` uses the built-in default. |

### DIMP

DIMP pseudonymization service.

```yaml
services:
  dimp:
    url: "http://dimp:32861"
    bundle_split_threshold_mb: 10
    auth:
      api_key: "${DIMP_API_KEY}"
```

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `url` | string | - | DIMP server root URL (required if dimp step enabled). Do not include `/fhir` — the client appends it. |
| `bundle_split_threshold_mb` | int | 10 | Split Bundles larger than this (1-100 MB) |
| `auth.username` | string | - | Basic Auth username |
| `auth.password` | string | - | Basic Auth password |
| `auth.oauth_issuer_uri` | string | - | OAuth 2.0 issuer URI. When set, Aether fetches client-credentials bearer tokens. |
| `auth.oauth_client_id` | string | - | OAuth 2.0 client ID |
| `auth.oauth_client_secret` | string | - | OAuth 2.0 client secret |
| `auth.api_key` | string | - | API key. Sent in its own header, not in `Authorization`. |
| `auth.api_key_header` | string | `x-api-key` | Header that carries `api_key`. The FHIR Pseudonymizer reads `x-api-key`; change it only for a proxy that expects a different header. |

Basic Auth and OAuth 2.0 both set the `Authorization` header. Aether rejects a
config that sets both. The API key uses its own header, thus you can set it
together with Basic Auth or OAuth 2.0. Aether also rejects an incomplete set of
fields for one scheme.

`api_key_header` must be a valid HTTP header name. You can set it to
`Authorization` to send a static token, for example `api_key: "Bearer abc"`. But
Aether rejects `Authorization` together with Basic Auth or OAuth 2.0, because
the API key would replace their credentials.

### Flattening

fhir-flattener service for FHIR to CSV transformation.

```yaml
services:
  flattening:
    service_url: "http://fhir-flattener:8000"
    lookup_path: "/config/flatten-lookup.json"
    formats:
      - csv
    timeout: PT30M
```

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `service_url` | string | - | fhir-flattener service URL |
| `lookup_path` | string | - | Path to lookup table file |
| `formats` | []string | ["csv"] | Output formats |
| `timeout` | duration | 30m | Request timeout |
| `batch_size_mb` | int | 500 | Total memory budget in MB, divided across attribute groups (0 = use default) |

### Send

Destination server or object store for uploading processed data. Mode is selected via `send_as`.

#### Direct Resource Load

Upload FHIR resources directly to a FHIR server.

```yaml
services:
  send:
    send_as: "direct_resource_load"
    url: "https://fhir-server.example.com"
    batch_size: 100
    auth:
      username: "${FHIR_USER}"
      password: "${FHIR_PASSWORD}"
```

#### Transfer Load

Package files for DSF-based transfer.

```yaml
services:
  send:
    send_as: "transfer_load"
    url: "https://transfer.example.com"
    auth:
      oauth_issuer_uri: "${OAUTH_ISSUER}"
      oauth_client_id: "${OAUTH_CLIENT}"
      oauth_client_secret: "${OAUTH_SECRET}"
    transfer:
      project_identifier: "MII-PROJECT"
      organization_identifier: "your-org.example.de"
```

#### S3 Upload

Upload files to an S3-compatible bucket (AWS S3, MinIO, Ceph).

```yaml
services:
  send:
    send_as: "s3_upload"
    s3:
      bucket: "${S3_BUCKET}"
      region: "eu-central-1"
      access_key_id: "${AWS_ACCESS_KEY_ID}"
      secret_access_key: "${AWS_SECRET_ACCESS_KEY}"
      # endpoint: "http://minio.example.com:9000"
      # use_path_style: true
      # timeout: PT30M
```

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `send_as` | string | - | `direct_resource_load`, `transfer_load`, or `s3_upload` |
| `url` | string | - | FHIR server root URL — required for FHIR modes, ignored for `s3_upload`. Do not include `/fhir`; the client appends it. |
| `batch_size` | int | 100 | Resources per transaction (`direct_resource_load` only, 0-1000) |

**Authentication (choose one for FHIR modes):**

| Option | Description |
|--------|-------------|
| `auth.username` + `auth.password` | Basic authentication |
| `auth.oauth_issuer_uri` + `oauth_client_id` + `oauth_client_secret` | OAuth 2.0 client credentials |

In `s3_upload` mode the `auth` block is optional and used only as upstream proxy authentication (basic auth via `Proxy-Authorization`); the S3 API itself is authenticated via `s3.access_key_id` / `s3.secret_access_key`.

**Transfer settings (transfer_load mode only):**

| Option | Description |
|--------|-------------|
| `transfer.project_identifier` | MII project identifier |
| `transfer.organization_identifier` | Organization identifier |

**S3 settings (s3_upload mode only):**

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `s3.bucket` | string | - | Target bucket name (required) |
| `s3.region` | string | - | AWS region, for example `eu-central-1` (required) |
| `s3.access_key_id` | string | - | S3 access key (required) |
| `s3.secret_access_key` | string | - | S3 secret key (required) |
| `s3.endpoint` | string | - | Custom endpoint URL (MinIO, Ceph, etc.). Leave empty for AWS S3. |
| `s3.use_path_style` | bool | false | Use path-style addressing (required for MinIO and many S3-compatible stores) |
| `s3.timeout` | duration | PT30M | Per-request timeout |

### Validation

FHIR validation service for data quality checks.

```yaml
services:
  validation:
    url: "http://validator:8080/fhir"
    max_concurrent_requests: 4
    bundle_chunk_size_mb: 10
    fail_on_error: true
```

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `url` | string | - | Validation service URL (required if validation step enabled) |
| `max_concurrent_requests` | int | 4 | Concurrent validation requests |
| `bundle_chunk_size_mb` | int | 10 | Bundle chunk size for batching resources (MB) |
| `fail_on_error` | bool | true | Stop pipeline when validation finds data quality errors |

When `fail_on_error` is `true` (default), the pipeline stops after the validation step completes with errors. When `false`, validation reports are written but the pipeline continues.

### Local Import

Default directory for local FHIR imports.

```yaml
services:
  local_import:
    dir: "/data/fhir"
    recursive: false
```

| Option | Type | Description |
|--------|------|-------------|
| `dir` | string | Default import directory (overridable with `--dir` flag) |
| `recursive` | bool | Scan subdirectories of `dir` for NDJSON files. Default `false` (top level only). |

### CRTDL Preprocessing

Enriches CRTDL documents with additional attributes before sending to TORCH. This is required when using DIMP pseudonymization, which needs certain identifier attributes (for example, `Patient.identifier`) to be present in the CRTDL extraction query.

```yaml
services:
  crtdl_preprocessing:
    enabled: true
    enrichments:
      - group_reference: "https://www.medizininformatik-initiative.de/fhir/core/modul-person/StructureDefinition/PatientPseudonymisiert"
        create_if_not_exists:
          group_name: "PatientPseudonymisiert"
        attributes_to_add:
          - attribute_ref: "Patient.identifier"
            must_have: false
```

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `enabled` | bool | false | Enable CRTDL preprocessing |
| `enrichments_path` | string | - | Path to external JSON enrichment file |
| `enrichments` | list | - | Inline enrichment rules (mutually exclusive with `enrichments_path`) |

**Enrichment rule options:**

| Option | Type | Description |
|--------|------|-------------|
| `group_reference` | string | Profile URL of the CRTDL attribute group to enrich (required) |
| `create_if_not_exists.group_name` | string | If group is missing from CRTDL, create it with this name |
| `attributes_to_add[].attribute_ref` | string | FHIR attribute reference to add (required) |
| `attributes_to_add[].must_have` | bool | Whether the attribute is required for extraction |
| `attributes_to_add[].linked_groups` | []string | Profile URLs to resolve to group IDs for cross-references |

**External JSON file format:**

When using `enrichments_path`, the file uses camelCase field names:

```json
[
  {
    "groupReference": "https://www.medizininformatik-initiative.de/fhir/core/modul-person/StructureDefinition/PatientPseudonymisiert",
    "createIfNotExists": {
      "groupName": "PatientPseudonymisiert"
    },
    "attributesToAdd": [
      {
        "attributeRef": "Patient.identifier",
        "mustHave": false
      }
    ]
  }
]
```

A shorter syntax is also supported for group creation:

```json
{
  "groupReference": "https://example.org/fhir/StructureDefinition/Patient",
  "addGroupIfNotExists": true,
  "attributesToAdd": [
    {"attributeRef": "Patient.identifier", "mustHave": false}
  ]
}
```

When `addGroupIfNotExists` is `true`, the group name is automatically derived from the last segment of the profile URL (for example, `"Patient"` from the preceding URL). Use `createIfNotExists` with an explicit `groupName` if you need a custom name.

> **Note:** `addGroupIfNotExists` and `createIfNotExists` are mutually exclusive. Unknown fields in the JSON file will produce an error.

## Pipeline

```yaml
pipeline:
  enabled_steps:
    - local_import
    - dimp
    - flattening
```

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `enabled_steps` | []string | - | Pipeline steps to execute in order |

**Available steps:**

| Step | Description |
|------|-------------|
| `torch` | Import via TORCH (requires CRTDL) |
| `local_import` | Import from local directory |
| `http_import` | Import from HTTP URL |
| `dimp` | Pseudonymize via DIMP |
| `wait` | Pause for manual inspection |
| `flattening` | Transform to CSV (requires CRTDL) |
| `send` | Upload to destination server |
| `validation` | Validate FHIR data against profiles |

**Rules:**
- One import step must be first (torch, local_import, or http_import)
- Wait step cannot be first or consecutive
- Flattening requires CRTDL input

## Retry

```yaml
retry:
  max_attempts: 5
  initial_backoff_ms: 1000
  max_backoff_ms: 30000
```

| Option | Type | Default | Range | Description |
|--------|------|---------|-------|-------------|
| `max_attempts` | int | 5 | 1-10 | Max retry attempts for transient errors |
| `initial_backoff_ms` | int | 1000 | - | Initial backoff delay |
| `max_backoff_ms` | int | 30000 | - | Max backoff delay |

Exponential backoff: `wait = min(initial * 2^attempt, max)`

## TLS

Trust custom or internal certificates and, when needed, disable verification entirely. Applied to every outgoing HTTP client (TORCH, DIMP, validation, flattening, send, HTTP import).

```yaml
tls:
  ca_cert_path: "/path/to/certs.pem"
  insecure_skip_verify: false
```

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `ca_cert_path` | string | - | PEM bundle of additional CA or server certificates to trust. System CAs remain trusted alongside these. Supports `${ENV}` substitution. |
| `insecure_skip_verify` | bool | false | Skip TLS verification entirely. Development/testing only. |

## Compression

```yaml
compression:
  enabled: true
  level: "default"
```

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `enabled` | bool | true | Enable zstd compression |
| `level` | string | "default" | Compression level |

**Compression levels:**

| Level | Speed | Ratio | Use Case |
|-------|-------|-------|----------|
| `fastest` | ~500 MB/s | ~3-4x | Large datasets, CPU-constrained |
| `default` | ~200 MB/s | ~4-5x | Balanced (recommended) |
| `better` | ~100 MB/s | ~5-6x | Storage-constrained |
| `best` | ~50 MB/s | ~6-7x | Archival |

Output files use `.ndjson.zst` extension when enabled. Aether auto-detects and reads both compressed and uncompressed files.

## Jobs Directory

```yaml
jobs_dir: "./jobs"
```

Directory for job state and data files.

## Environment Variables

Aether exposes two independent environment-variable mechanisms.

### In-file substitution (`${VAR}`)

All string values support `${VAR}` substitution, expanded when the file is read:

```yaml
services:
  torch:
    auth:
      username: "${TORCH_USERNAME}"
      password: "${TORCH_PASSWORD}"
  send:
    url: "${FHIR_SERVER_URL}"
```

### Overrides (`AETHER_*`)

Every configuration key can be overridden with an `AETHER_`-prefixed variable,
including keys omitted from the YAML file. The name is the full config path,
uppercased, with `.` replaced by `_`:

```bash
export AETHER_SERVICES_TORCH_BASE_URL="http://torch.internal:8080"
export AETHER_SERVICES_DIMP_URL="http://dimp.internal:8080"
export AETHER_RETRY_MAX_ATTEMPTS=8
```

Scope is **all** keys (nested service blocks, durations, integers, booleans).
Precedence: CLI flags → `AETHER_*` env → config file → built-in defaults. An unset
variable leaves the key at its file value or default.

## Example Configurations

### TORCH + DIMP

```yaml
services:
  torch:
    base_url: "https://torch.hospital.org"
    auth:
      username: "${TORCH_USER}"
      password: "${TORCH_PASS}"
  dimp:
    url: "http://dimp:32861"

pipeline:
  enabled_steps:
    - torch
    - dimp

jobs_dir: "./jobs"
```

### Local Import with Flattening

```yaml
services:
  local_import:
    dir: "/data/fhir"
  dimp:
    url: "http://dimp:32861"
  flattening:
    service_url: "http://fhir-flattener:8000"
    lookup_path: "/config/lookup.json"

pipeline:
  enabled_steps:
    - local_import
    - dimp
    - flattening

compression:
  enabled: true
  level: "default"

jobs_dir: "./jobs"
```

### Full Pipeline with Send

```yaml
services:
  torch:
    base_url: "https://torch.hospital.org"
    auth:
      username: "${TORCH_USER}"
      password: "${TORCH_PASS}"
  dimp:
    url: "http://dimp:32861"
  send:
    send_as: "transfer_load"
    url: "https://transfer.mii.de"
    auth:
      oauth_issuer_uri: "${OAUTH_ISSUER}"
      oauth_client_id: "${OAUTH_CLIENT}"
      oauth_client_secret: "${OAUTH_SECRET}"
    transfer:
      project_identifier: "MII-PROJECT"
      organization_identifier: "hospital.example.de"

pipeline:
  enabled_steps:
    - torch
    - dimp
    - send

retry:
  max_attempts: 5

compression:
  enabled: true

jobs_dir: "/data/aether/jobs"
```

## Next Steps

- [CLI Commands](./cli-commands.md) - Command reference
- [Pipeline Steps](../guides/pipeline-steps.md) - Step details
