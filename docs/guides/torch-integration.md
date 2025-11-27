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

### Reusing Previous Extractions

If you already have a TORCH result URL:

```bash
aether pipeline start http://torch-server/fhir/result/abc123
```

This skips the extraction and downloads directly.

## Troubleshooting

**"TORCH server unreachable"**
- Check your `base_url`
- Verify network connectivity

**"Authentication failed"**
- Verify username and password
- Check your account has access

**"Extraction timeout"**
- Increase `extraction_timeout_minutes`
- Large cohorts may take longer

**"No patients matched"**
- Review your CRTDL query criteria
- Test the query on TORCH directly
