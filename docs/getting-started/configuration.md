# Configuration

Aether uses a YAML configuration file. Create an `aether.yaml` anywhere on disk and pass its path as the first positional argument on every command. Aether does not auto-discover config files.

## Basic Configuration

```yaml
services:
  torch:
    base_url: "https://your-torch-server.org"
    username: "your-username"
    password: "your-password"

  dimp:
    url: "http://your-dimp-server:32861"

pipeline:
  enabled_steps:
    - torch
    - dimp

jobs_dir: "./jobs"
```

## Service Configuration

### TORCH

```yaml
services:
  torch:
    base_url: "https://your-torch-server.org"
    username: "your-username"
    password: "your-password"
    # OAuth2 client credentials (alternative to username/password):
    # oauth_issuer_uri: "${TORCH_OAUTH_ISSUER_URI}"
    # oauth_client_id: "${TORCH_OAUTH_CLIENT_ID}"
    # oauth_client_secret: "${TORCH_OAUTH_CLIENT_SECRET}"
    extraction_timeout: PT30M         # liveness window (silence before giving up)
    polling_interval: PT5S
    download_stall_timeout: PT1M      # abort a result download after this much inactivity
```

### DIMP

```yaml
services:
  dimp:
    url: "http://your-dimp-server:32861"  # server root; /fhir appended by client
    bundle_split_threshold_mb: 10  # Auto-split large bundles
```

### Flattening

```yaml
services:
  flattening:
    service_url: "http://fhir-flattener:8000"
    lookup_path: "/path/to/flatten-lookup.json"
    formats:
      - csv
    timeout: PT30M
```

### Send

**Direct to FHIR server:**
```yaml
services:
  send:
    send_as: "direct_resource_load"
    url: "https://fhir-server.example.com"  # server root; /fhir appended by client
    batch_size: 100
    auth:
      username: "${FHIR_USER}"
      password: "${FHIR_PASSWORD}"
```

**DSF transfer:**
```yaml
services:
  send:
    send_as: "transfer_load"
    url: "https://transfer-server.example.com"  # server root; /fhir appended by client
    auth:
      oauth_issuer_uri: "${OAUTH_ISSUER}"
      oauth_client_id: "${OAUTH_CLIENT_ID}"
      oauth_client_secret: "${OAUTH_CLIENT_SECRET}"
    transfer:
      project_identifier: "MII-PROJECT"
      organization_identifier: "your-org.example.de"
```

**S3 upload (AWS S3, MinIO, Ceph):**
```yaml
services:
  send:
    send_as: "s3_upload"
    s3:
      bucket: "${S3_BUCKET}"
      region: "eu-central-1"
      access_key_id: "${AWS_ACCESS_KEY_ID}"
      secret_access_key: "${AWS_SECRET_ACCESS_KEY}"
      # endpoint: "http://minio.example.com:9000"   # for non-AWS stores
      # use_path_style: true                         # required for MinIO
      # timeout: PT30M
```

See the [Send step guide](../guides/steps/send.md) for full S3 options and proxy-auth behaviour.

### Local Import

```yaml
services:
  local_import:
    dir: "/path/to/fhir/data"  # Override with --dir flag
```

### Validation

```yaml
services:
  validation:
    url: "http://your-validator:8080/fhir"
    fail_on_error: true  # false to continue pipeline despite validation errors
```

## Pipeline Steps

```yaml
pipeline:
  enabled_steps:
    - torch         # OR local_import OR http_import
    - validation    # Validate FHIR data (optional)
    - dimp          # Pseudonymization
    - wait          # Pause for inspection (optional)
    - flattening    # FHIR to CSV (requires CRTDL)
    - send          # Upload to destination
```

### Step Placement Rules

**Wait steps:**
- Can be placed between any two steps
- Cannot be the first step (needs previous step output)
- Cannot be consecutive (redundant)
- Multiple wait steps are supported at different points in the pipeline

**Processing steps (dimp, flattening):**
- Should only appear once in the pipeline
- Multiple instances are not supported (output directories would be overwritten)

**Import steps (torch, local_import, http_import):**
- Must be first
- Only one import step allowed

## Compression

```yaml
compression:
  enabled: true        # default: true
  level: "default"     # fastest, default, better, best
```

Output files use `.ndjson.zst` extension when enabled.

## TLS

Trust custom or internal certificates and, when needed, disable verification entirely:

```yaml
tls:
  # PEM bundle of additional CA or server certificates to trust
  # (system CAs are still trusted alongside these)
  ca_cert_path: "${CA_CERT_PATH}"

  # Skip certificate verification — development/testing only
  insecure_skip_verify: false
```

`tls` applies to every outgoing HTTP client, including TORCH, DIMP, validation, flattening, send (FHIR + S3), and HTTP import.

## Retry

Transient failures (network errors, 5xx responses, S3 `SlowDown` / `ServiceUnavailable` / timeouts) are retried with exponential backoff:

```yaml
retry:
  max_attempts: 5            # 1-10
  initial_backoff_ms: 1000
  max_backoff_ms: 30000
```

## CRTDL Preprocessing

Enriches CRTDL files with extra attributes (e.g. pseudonymisation identifiers) before sending them to TORCH. Disabled by default.

```yaml
services:
  crtdl_preprocessing:
    enabled: true

    # Option A: external rules file
    enrichments_path: "/path/to/dimp-enrichments.json"

    # Option B: inline rules (mutually exclusive with enrichments_path)
    # enrichments:
    #   - group_reference: "https://www.medizininformatik-initiative.de/fhir/core/modul-person/StructureDefinition/Patient"
    #     create_if_not_exists:
    #       group_name: "Patient"
    #     attributes_to_add:
    #       - attribute_ref: "Patient.identifier:PseudonymisierterIdentifier"
    #         must_have: true
```

## Environment Variables

aether supports two independent environment-variable mechanisms.

### In-file substitution: `${VAR}`

Any string value in the YAML file may reference an environment variable with
`${VAR}`. The reference is expanded when the file is read — useful for keeping
secrets out of the file:

```yaml
services:
  torch:
    username: "${TORCH_USERNAME}"
    password: "${TORCH_PASSWORD}"
```

```bash
export TORCH_USERNAME="researcher"
export TORCH_PASSWORD="secret"
```

### Overrides: `AETHER_*`

Any configuration key can be overridden with an `AETHER_`-prefixed environment
variable, **even when the key is absent from the YAML file**. The variable name
is the full config path, uppercased, with dots replaced by underscores:

| Config key                          | Environment variable                       |
| ----------------------------------- | ------------------------------------------ |
| `jobs_dir`                          | `AETHER_JOBS_DIR`                          |
| `services.torch.base_url`           | `AETHER_SERVICES_TORCH_BASE_URL`          |
| `services.dimp.url`                 | `AETHER_SERVICES_DIMP_URL`                |
| `retry.max_attempts`                | `AETHER_RETRY_MAX_ATTEMPTS`               |
| `services.send.s3.bucket`           | `AETHER_SERVICES_SEND_S3_BUCKET`          |

```bash
export AETHER_SERVICES_TORCH_BASE_URL="http://torch.internal:8080"
```

Scope: **all** keys are overridable, including nested service blocks, durations
(`AETHER_SERVICES_TORCH_EXTRACTION_TIMEOUT=PT45M`), integers, and booleans.
Precedence is CLI flags → `AETHER_*` env → config file → built-in defaults, so an
override wins over a value set in the file. Keys whose variable is unset keep
their default.

## Next Steps

- [Quick Start](./quick-start.md) - Run your first pipeline
- [Pipeline Steps](../guides/pipeline-steps.md) - Step details
- [Configuration Reference](../api-reference/config-reference.md) - All options
