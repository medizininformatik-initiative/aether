# TORCH Integration

TORCH is a service for extracting patient data from clinical systems. Aether connects to TORCH to download data based on your query.

## How It Works

1. You provide a CRTDL query file (defines which patients/data you want)
2. Aether sends it to TORCH
3. TORCH extracts the matching data
4. Aether downloads the results

## Configuration

Add TORCH credentials to your `aether.yaml`:

```yaml
services:
  torch:
    base_url: "https://your-torch-server.org"
    username: "your-username"
    password: "your-password"

pipeline:
  enabled_steps:
    - torch
    - dimp
```

## Running a TORCH Query

```bash
aether pipeline start your-query.crtdl
```

Aether will show progress as it:
1. Submits your query
2. Waits for extraction
3. Downloads the data
4. Continues to DIMP (if enabled)

## Advanced Options

### Timeout Settings

For large queries that take longer:

```yaml
services:
  torch:
    base_url: "https://your-torch-server.org"
    username: "your-username"
    password: "your-password"
    extraction_timeout_minutes: 60   # Default is 30
    polling_interval_seconds: 10     # Default is 5
```

### Direct TORCH URL Import

If you already have a TORCH extraction or result URL, you can pass it directly to skip the CRTDL submission step:

```bash
aether pipeline start "https://torch.example.com/fhir/extraction/result-123"
```

Aether auto-detects TORCH URLs by looking for `/fhir/extraction/` or `/fhir/result/` in the URL (case-sensitive). When a TORCH URL is provided, Aether:

1. **Skips extraction submission** — does not send a CRTDL query
2. **Polls the URL** — sends GET requests with exponential backoff until the extraction is complete (HTTP 200) or times out
3. **Downloads all result files** — fetches multiple NDJSON files from the extraction result

This is useful when:
- Reusing results from a previous extraction
- Sharing extraction URLs between team members
- Resuming a download from a known TORCH endpoint

#### URL patterns

URLs must contain one of these path segments to be recognized as TORCH URLs:

- `/fhir/extraction/` — e.g., `https://torch.example.com/fhir/extraction/result-123`
- `/fhir/result/` — e.g., `https://torch.example.com/fhir/result/abc-xyz`

All other HTTP(S) URLs are treated as plain [HTTP imports](./steps/http-import.md) (single-file download, no polling).

#### Configuration

TORCH URL imports still require TORCH configuration for authentication:

```yaml
services:
  torch:
    base_url: "https://your-torch-server.org"
    username: "your-username"
    password: "your-password"

pipeline:
  enabled_steps:
    - torch
    - dimp
```

The `extraction_timeout_minutes` and polling interval settings also apply.

#### Comparison: CRTDL vs TORCH URL vs HTTP

| | CRTDL | TORCH URL | HTTP URL |
|---|---|---|---|
| Input example | `query.crtdl` | `https://torch/fhir/result/123` | `https://example.com/data.ndjson` |
| Submits extraction | Yes | No | No |
| Polls for completion | Yes | Yes | No |
| Downloads multiple files | Yes | Yes | No (single file) |
| Requires TORCH auth | Yes | Yes | No |
| First pipeline step | `torch` | `torch` | `http_import` |

