# Configuration

Aether uses a YAML file for configuration. Create `aether.yaml` in your working directory.

## Basic Configuration

```yaml
services:
  torch:
    base_url: "https://your-torch-server.org"
    username: "your-username"
    password: "your-password"

  dimp:
    url: "http://your-dimp-server:32861/fhir"

pipeline:
  enabled_steps:
    - torch
    - dimp

jobs_dir: "./jobs"
```

## Configuration Options

### TORCH Settings

```yaml
services:
  torch:
    base_url: "https://your-torch-server.org"  # TORCH server URL
    username: "your-username"                   # Your username
    password: "your-password"                   # Your password
    extraction_timeout_minutes: 30              # How long to wait (default: 30)
    polling_interval_seconds: 5                 # Check interval (default: 5)
```

### DIMP Settings

```yaml
services:
  dimp:
    url: "http://your-dimp-server:32861/fhir"  # DIMP service URL
    bundle_split_threshold_mb: 10               # Split large files (default: 10)
```

### Pipeline Settings

```yaml
pipeline:
  enabled_steps:
    - torch   # Extract from TORCH
    - dimp    # Pseudonymize the data
```

### Jobs Directory

```yaml
jobs_dir: "./jobs"  # Where to store job data and results
```

## Troubleshooting

**"TORCH server unreachable"**
- Check `base_url` is correct
- Verify network connection to TORCH server

**"Authentication failed"**
- Check username and password
- Ensure your account has access

**"DIMP service unavailable"**
- Check `dimp.url` is correct
- Verify the DIMP service is running

**"Jobs directory does not exist"**
- The directory is created automatically
- Ensure you have write permissions

## Next Steps

- [Quick Start](./quick-start.md) - Run your first pipeline
- [TORCH Integration](../guides/torch-integration.md) - Learn more about TORCH
