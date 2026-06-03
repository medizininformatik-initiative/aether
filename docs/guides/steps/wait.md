# Wait

Pauses the pipeline for manual data inspection or modification.

## What it does

- Creates an empty `{previous_step}_wait/` directory
- Pauses pipeline execution
- Resumes when `pipeline continue` is run and files are present

## Constraints

- Cannot be the first step
- No consecutive wait steps allowed

## Configuration

```yaml
pipeline:
  enabled_steps:
    - local_import
    - wait          # Pause after import
    - dimp
    - wait          # Pause after pseudonymization
    - flattening
```

## Usage

```bash
# Pipeline pauses automatically at wait step
aether pipeline start aether.yaml crtdl.json

# Check status - shows "waiting"
aether pipeline status aether.yaml <job-id>

# Place modified data in the wait directory
# e.g., jobs/<job-id>/import_wait/

# Resume pipeline
aether pipeline continue aether.yaml <job-id>
```

## Wait Directory

The wait directory is created empty. You must place your data files there before continuing:

- After import step: `jobs/<job-id>/import_wait/`
- After DIMP step: `jobs/<job-id>/dimp_wait/`

The pipeline will only continue when files are present in the wait directory.
