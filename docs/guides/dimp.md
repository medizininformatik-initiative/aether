# DIMP

DIMP (**D**e-identify, **M**inimize, **P**seudonymize) provides de-identification, minimization, and pseudonymization for FHIR data, protecting patient privacy while keeping the data useful for research.

## What DIMP Does

- Removes or masks identifying information (names, addresses, etc.)
- Generates consistent pseudonyms for patient identifiers
- Preserves clinical data (diagnoses, procedures, lab values)

## Configuration

Add DIMP to your `aether.yaml`:

```yaml
services:
  dimp:
    url: "http://your-dimp-server:32861/fhir"

pipeline:
  enabled_steps:
    - torch   # or local_import
    - dimp    # Pseudonymize after import

jobs_dir: "./jobs"
```

## Running Pseudonymization

```bash
aether pipeline start your-query.crtdl
```

Aether will:
1. Extract data from TORCH (or import from files)
2. Send it to DIMP for dimping
3. Save the protected data in the jobs folder

## Output

Results are saved in:

```
jobs/<job-id>/
├── status.json          # Job status
└── dimp_results.ndjson  # Pseudonymized data
```

## Large Bundles

For large datasets, Aether automatically splits bundles before sending to DIMP:

```yaml
services:
  dimp:
    url: "http://your-dimp-server:32861/fhir"
    bundle_split_threshold_mb: 10   # Split bundles larger than 10MB
```

