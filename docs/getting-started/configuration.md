# Configuration

Aether uses a YAML configuration file. Create `aether.yaml` in your working directory.

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
    extraction_timeout: PT30M
    polling_interval: PT5S
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

## Environment Variables

Use environment variables for sensitive data:

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

## Next Steps

- [Quick Start](./quick-start.md) - Run your first pipeline
- [Pipeline Steps](../guides/pipeline-steps.md) - Step details
- [Configuration Reference](../api-reference/config-reference.md) - All options
