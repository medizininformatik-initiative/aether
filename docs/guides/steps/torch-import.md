# TORCH Import

Extracts patient data from a TORCH server using CRTDL queries.

## What it does

- Sends CRTDL query to TORCH
- Polls for extraction completion
- Downloads FHIR NDJSON data

## Configuration

```yaml
services:
  torch:
    base_url: "https://your-torch-server.org"
    username: "your-username"
    password: "your-password"
    extraction_timeout_minutes: 30  # default
    polling_interval_seconds: 5     # default
    max_polling_interval_seconds: 30 # default

pipeline:
  enabled_steps:
    - torch
```

## Usage

```bash
aether pipeline start query.crtdl
```

## Configuration Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `base_url` | string | - | TORCH server URL (required) |
| `username` | string | - | Authentication username |
| `password` | string | - | Authentication password |
| `extraction_timeout_minutes` | int | 30 | Max wait time for extraction |
| `polling_interval_seconds` | int | 5 | Initial status check interval |
| `max_polling_interval_seconds` | int | 30 | Max interval (exponential backoff) |
