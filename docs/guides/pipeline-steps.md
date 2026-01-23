# Pipeline Steps

Aether processes data through configurable pipeline steps.

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
- Receives dimped data back
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

### 3. Flattening (FHIR to CSV)

Transforms FHIR NDJSON data into flattened CSV files using SQL-on-FHIR ViewDefinitions.

**What it does:**
- Parses CRTDL file to extract attribute groups
- Builds ViewDefinitions from lookup tables
- Sends data to fhir-flattener service
- Writes CSV files for each attribute group

**Requirements:**
- Pipeline must be started with a CRTDL file (not local dir or HTTP URL)
- fhir-flattener service must be running
- Lookup table (flatten-lookup.json) must be configured

**Configuration:**
```yaml
services:
  flattening:
    service_url: "http://fhir-flattener:8000"
    lookup_path: "/path/to/flatten-lookup.json"
    formats:
      - csv
    timeout: 30m

pipeline:
  enabled_steps:
    - torch
    - dimp
    - flattening
```

**Output:**
CSV files are written to `jobs/<job-id>/csv/` directory, one file per attribute group.

## Typical Pipelines

### Basic Pipeline (TORCH + DIMP)

```yaml
pipeline:
  enabled_steps:
    - torch   # First: get data from TORCH
    - dimp    # Then: pseudonymize it
```

### Full Pipeline (with Flattening)

```yaml
pipeline:
  enabled_steps:
    - torch       # Extract from TORCH
    - dimp        # Pseudonymize
    - flattening  # Transform to CSV
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
