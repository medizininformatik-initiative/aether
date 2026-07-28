# TORCH Import

Extracts patient data from a TORCH server. Supports two modes: CRTDL-based extraction and direct TORCH URL import.

## What it does

**CRTDL mode** (submit + poll + download):
- Sends CRTDL query to TORCH
- Polls for extraction completion
- Downloads FHIR NDJSON data

**Direct URL mode** (poll + download):
- Polls an existing TORCH extraction/result URL
- Downloads FHIR NDJSON data when ready
- Skips the extraction submission step

## Configuration

```yaml
services:
  torch:
    base_url: "https://your-torch-server.org"
    username: "your-username"
    password: "your-password"
    extraction_timeout: PT30M    # default
    polling_interval: PT5S       # default
    max_polling_interval: PT30S  # default
    file_ready_retries: 10       # default
    file_ready_interval: PT10S   # default
    download_stall_timeout: PT1M # default

pipeline:
  enabled_steps:
    - torch
```

## Usage

### With CRTDL file

```bash
aether pipeline start aether.yaml crtdl.json
```

### With TORCH URL

```bash
aether pipeline start aether.yaml crtdl.json "https://torch.example.com/fhir/extraction/result-123"
```

URLs containing `/fhir/extraction/` or `/fhir/result/` are automatically recognized as TORCH URLs. See the [TORCH Integration guide](../torch-integration.md#direct-torch-url-import) for details.

## Configuration Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `base_url` | string | - | TORCH server URL (required) |
| `username` | string | - | Authentication username |
| `password` | string | - | Authentication password |
| `extraction_timeout` | duration | PT30M | Max wait time for extraction |
| `polling_interval` | duration | PT5S | Initial status check interval |
| `max_polling_interval` | duration | PT30S | Max interval (exponential backoff) |
| `file_ready_retries` | int | 10 | Retries while waiting for files to appear after extraction completes |
| `file_ready_interval` | duration | PT10S | Interval between file-availability checks |
| `download_stall_timeout` | duration | PT1M | Inactivity window while streaming a result file; the download is canceled only if no bytes arrive for this long. `0` uses the built-in default. |
