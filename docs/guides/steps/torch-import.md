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
    extraction_timeout: PT30M  # default
    polling_interval: PT5S     # default
    max_polling_interval: PT30S # default

pipeline:
  enabled_steps:
    - torch
```

## Usage

### With CRTDL file

```bash
aether pipeline start query.crtdl
```

### With TORCH URL

```bash
aether pipeline start "https://torch.example.com/fhir/extraction/result-123"
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
