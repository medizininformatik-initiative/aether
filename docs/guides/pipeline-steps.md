# Pipeline Steps

Aether processes data through configurable pipeline steps.

## Import Steps

One import step must be first in the pipeline. Only enable one at a time.

| Step | Description |
|------|-------------|
| [TORCH Import](./steps/torch-import.md) | Extract data from TORCH server using CRTDL queries or a direct TORCH URL |
| [Local Import](./steps/local-import.md) | Import FHIR NDJSON files from local directory |
| [HTTP Import](./steps/http-import.md) | Download FHIR NDJSON files from HTTP URL |

## Processing Steps

| Step | Description |
|------|-------------|
| [Validation](./steps/validation.md) | Validate FHIR data against profiles |
| [DIMP](./steps/dimp.md) | Pseudonymize FHIR data using DIMP service |
| [Flattening](./steps/flattening.md) | Transform FHIR NDJSON to CSV |
| [Wait](./steps/wait.md) | Pause pipeline for manual inspection |
| [Send](./steps/send.md) | Upload data to FHIR server or DSF transfer |

## Typical Pipelines

### Basic (TORCH + DIMP)

```yaml
pipeline:
  enabled_steps:
    - torch
    - dimp
```

### With Validation

```yaml
pipeline:
  enabled_steps:
    - local_import
    - validation    # Validate before pseudonymization
    - dimp
```

### Local Data with Manual Review

```yaml
pipeline:
  enabled_steps:
    - local_import
    - wait          # Review imported data
    - dimp
    - wait          # Review pseudonymized data
    - flattening
```

### Full Pipeline with Send

```yaml
pipeline:
  enabled_steps:
    - torch
    - validation
    - dimp
    - flattening
    - send
```

## Monitoring

```bash
# List all jobs
aether job list aether.yaml

# Check specific job
aether pipeline status aether.yaml <job-id>
```

## Resuming

```bash
# Resume failed or paused job
aether pipeline continue aether.yaml <job-id>
```

Completed steps are not re-run. To re-run a completed step manually, use
`aether job run <config> <job-id> --step <step> --force`. Forcing a re-run also
resets every step after it to pending — its output is now stale — so the job
does not become `completed` until you re-run those downstream steps (or resume
the pipeline). The state file holds `in_progress`, but no process runs the job
after the command exits, thus `job list` shows the job as `stopped`.
