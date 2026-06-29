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
aether pipeline start aether.yaml your-crtdl.json
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
    extraction_timeout: PT1H     # Default is PT30M
    polling_interval: PT10S      # Default is PT5S
```

`extraction_timeout` is a **liveness window**, not a total cap: it bounds how long aether waits *without a response from TORCH*, and it resets on every status response (`200`/`202`). A multi-hour extraction that keeps responding never trips it, so you no longer need to size the timeout to the whole extraction — the default `PT30M` means "give up after 30 minutes of TORCH silence". See [ADR 0001](../adr/0001-extraction-timeout-liveness.md).

### Polling Resilience

During status polling, transient HTTP errors (timeouts, connection resets) and transient TORCH responses (`503`/`429`/`408`) are treated as recoverable: aether logs a warning and keeps polling with exponential backoff rather than failing the extraction. A `500` is a terminal job failure and fails fast. Because the timeout is a liveness window (reset on every `200`/`202`), polling only stops when TORCH stays silent for longer than `extraction_timeout`.

### Resuming an Interrupted Extraction

When aether submits an extraction it persists the TORCH **job handle** (job ID + status URL) to disk *before* the long poll. If aether is interrupted (process killed, connection lost, timeout), `aether pipeline continue <job-id>` **re-attaches** to the same in-flight TORCH job and resumes polling — it does not re-submit the CRTDL, so the source FHIR server is not extracted twice. If the handle is no longer valid on TORCH (`404`/`410`, i.e. the job was cancelled or deleted), aether logs a warning, clears the stale handle, and submits a fresh extraction.

### Direct TORCH URL Import

If you already have a TORCH extraction or result URL, you can pass it directly to skip the CRTDL submission step:

```bash
aether pipeline start aether.yaml crtdl.json "https://torch.example.com/fhir/extraction/result-123"
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

The `extraction_timeout` and polling interval settings also apply.

#### Comparison: CRTDL vs TORCH URL vs HTTP

| | CRTDL | TORCH URL | HTTP URL |
|---|---|---|---|
| Input example | `crtdl.json` | `https://torch/fhir/result/123` | `https://example.com/data.ndjson` |
| Submits extraction | Yes | No | No |
| Polls for completion | Yes | Yes | No |
| Downloads multiple files | Yes | Yes | No (single file) |
| Requires TORCH auth | Yes | Yes | No |
| First pipeline step | `torch` | `torch` | `http_import` |

