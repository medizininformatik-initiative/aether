# Configuration Reference

Complete reference for all Aether configuration options.

## Configuration Schema

```yaml
services:
  torch:
    base_url: string
    username: string
    password: string
    extraction_timeout_minutes: integer  # default: 30
    polling_interval_seconds: integer    # default: 5
    max_polling_interval_seconds: integer # default: 30

  dimp:
    url: string
    bundle_split_threshold_mb: integer   # 1-100, default: 10

  flattening:
    service_url: string
    lookup_path: string
    formats: [string]                    # ["csv"]
    timeout: duration                    # default: 30m

  send:
    send_as: string                      # "direct_resource_load" or "transfer_load"
    url: string
    batch_size: integer                  # 1-1000, default: 100
    auth:
      username: string
      password: string
      oauth_issuer_uri: string
      oauth_client_id: string
      oauth_client_secret: string
    transfer:
      project_identifier: string
      organization_identifier: string

  local_import:
    dir: string

pipeline:
  enabled_steps: [string]

retry:
  max_attempts: integer                  # 1-10, default: 5
  initial_backoff_ms: integer            # default: 1000
  max_backoff_ms: integer                # default: 30000

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
    username: "${TORCH_USER}"
    password: "${TORCH_PASSWORD}"
    extraction_timeout_minutes: 30
    polling_interval_seconds: 5
    max_polling_interval_seconds: 30
```

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `base_url` | string | - | TORCH server URL (required if torch step enabled) |
| `username` | string | - | Authentication username |
| `password` | string | - | Authentication password |
| `extraction_timeout_minutes` | int | 30 | Max wait time for extraction |
| `polling_interval_seconds` | int | 5 | Initial status check interval |
| `max_polling_interval_seconds` | int | 30 | Max interval (exponential backoff cap) |

### DIMP

DIMP pseudonymization service.

```yaml
services:
  dimp:
    url: "http://dimp:32861/fhir"
    bundle_split_threshold_mb: 10
```

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `url` | string | - | DIMP service URL (required if dimp step enabled) |
| `bundle_split_threshold_mb` | int | 10 | Split Bundles larger than this (1-100 MB) |

### Flattening

fhir-flattener service for FHIR to CSV transformation.

```yaml
services:
  flattening:
    service_url: "http://fhir-flattener:8000"
    lookup_path: "/config/flatten-lookup.json"
    formats:
      - csv
    timeout: 30m
```

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `service_url` | string | - | fhir-flattener service URL |
| `lookup_path` | string | - | Path to lookup table file |
| `formats` | []string | ["csv"] | Output formats |
| `timeout` | duration | 30m | Request timeout |

### Send

Destination server for uploading processed data.

#### Direct Resource Load

Upload FHIR resources directly to a FHIR server.

```yaml
services:
  send:
    send_as: "direct_resource_load"
    url: "https://fhir-server.example.com/fhir"
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
    url: "https://transfer.example.com/fhir"
    auth:
      oauth_issuer_uri: "${OAUTH_ISSUER}"
      oauth_client_id: "${OAUTH_CLIENT}"
      oauth_client_secret: "${OAUTH_SECRET}"
    transfer:
      project_identifier: "MII-PROJECT"
      organization_identifier: "your-org.example.de"
```

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `send_as` | string | - | `direct_resource_load` or `transfer_load` |
| `url` | string | - | FHIR server base URL |
| `batch_size` | int | 100 | Resources per transaction (direct mode, 1-1000) |

**Authentication (choose one):**

| Option | Description |
|--------|-------------|
| `auth.username` + `auth.password` | Basic authentication |
| `auth.oauth_issuer_uri` + `oauth_client_id` + `oauth_client_secret` | OAuth2 client credentials |

**Transfer settings (transfer_load mode only):**

| Option | Description |
|--------|-------------|
| `transfer.project_identifier` | MII project identifier |
| `transfer.organization_identifier` | Organization identifier |

### Local Import

Default directory for local FHIR imports.

```yaml
services:
  local_import:
    dir: "/data/fhir"
```

| Option | Type | Description |
|--------|------|-------------|
| `dir` | string | Default import directory (overridable with `--dir` flag) |

## Pipeline

```yaml
pipeline:
  enabled_steps:
    - local_import
    - dimp
    - flattening
```

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
| `validation` | Validate FHIR data (placeholder) |
| `csv_conversion` | Convert to CSV (placeholder) |
| `parquet_conversion` | Convert to Parquet (placeholder) |

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

All string values support environment variable substitution:

```yaml
services:
  torch:
    username: "${TORCH_USERNAME}"
    password: "${TORCH_PASSWORD}"
  send:
    url: "${FHIR_SERVER_URL}"
```

## Example Configurations

### TORCH + DIMP

```yaml
services:
  torch:
    base_url: "https://torch.hospital.org"
    username: "${TORCH_USER}"
    password: "${TORCH_PASS}"
  dimp:
    url: "http://dimp:32861/fhir"

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
    url: "http://dimp:32861/fhir"
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
    username: "${TORCH_USER}"
    password: "${TORCH_PASS}"
  dimp:
    url: "http://dimp:32861/fhir"
  send:
    send_as: "transfer_load"
    url: "https://transfer.mii.de/fhir"
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
