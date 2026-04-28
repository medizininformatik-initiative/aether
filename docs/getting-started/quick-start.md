# Quick Start

Get Aether running in 5 minutes.

## 1. Create Configuration

Create `aether.yaml` in your working directory:

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

Replace URLs and credentials with your actual server details.

## 2. Run a Pipeline

```bash
aether pipeline start your-crtdl.json
```

Aether will:
1. Send your CRTDL query to TORCH
2. Wait for extraction to complete
3. Download the FHIR data
4. Pseudonymize it via DIMP
5. Save results in `jobs/<job-id>/pseudonymized/`

Output files use `.ndjson.zst` extension (zstd compressed).

## 3. Check Progress

```bash
# List all jobs
aether job list

# Check a specific job
aether pipeline status <job-id>
```

## 4. Resume if Needed

If a pipeline fails or pauses, resume it:

```bash
aether pipeline continue <job-id>
```

## Alternative: Direct TORCH URL

If you already have a TORCH extraction URL, skip the CRTDL submission:

```bash
aether pipeline start "https://torch.example.com/fhir/extraction/result-123"
```

Aether auto-detects URLs containing `/fhir/extraction/` or `/fhir/result/` and polls them directly. See [TORCH Integration](../guides/torch-integration.md#direct-torch-url-import) for details.

## Alternative: Local Import

Process local FHIR files instead of TORCH extraction:

```yaml
services:
  local_import:
    dir: "/path/to/fhir/data"
  dimp:
    url: "http://your-dimp-server:32861"

pipeline:
  enabled_steps:
    - local_import
    - dimp
```

```bash
# Use config directory
aether pipeline start

# Or override with --dir flag
aether pipeline start --dir /other/path
```

## Next Steps

- [Configuration](./configuration.md) - All configuration options
- [Pipeline Steps](../guides/pipeline-steps.md) - Available steps
- [CLI Commands](../api-reference/cli-commands.md) - Command reference
