# Pipeline Steps

Aether processes data in steps. Currently, two steps are implemented:

## Available Steps

### 1. TORCH (Data Extraction)

Extracts patient data from a TORCH server.

**What it does:**
- Sends your CRTDL query to TORCH
- Waits for extraction to complete
- Downloads the FHIR data

**Configuration:**
```yaml
services:
  torch:
    base_url: "https://your-torch-server.org"
    username: "your-username"
    password: "your-password"

pipeline:
  enabled_steps:
    - torch
```

### 2. DIMP (Pseudonymization)

Removes or masks identifying information to protect patient privacy.

**What it does:**
- Sends FHIR data to the DIMP service
- Receives pseudonymized data back
- Saves the protected data

**Configuration:**
```yaml
services:
  dimp:
    url: "http://your-dimp-server:32861/fhir"

pipeline:
  enabled_steps:
    - torch
    - dimp
```

## Typical Pipeline

Most users will run both steps together:

```yaml
pipeline:
  enabled_steps:
    - torch   # First: get data from TORCH
    - dimp    # Then: pseudonymize it
```

Run with:
```bash
aether pipeline start your-query.crtdl
```

## Monitoring

Check pipeline progress:

```bash
# List all jobs
aether job list

# Check specific job
aether pipeline status <job-id>
```

## Resuming

If a step fails, resume without restarting:

```bash
aether pipeline continue <job-id>
```

Completed steps are not re-run.
