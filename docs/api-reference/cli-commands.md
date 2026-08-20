# CLI Commands

Reference for Aether CLI commands.

## Global Options

```bash
aether [global-options] <command> ...
```

- `--verbose, -v` - Enable verbose logging
- `--help, -h` - Show help
- `--version` - Show version

Every subcommand that needs configuration takes the path to `aether.yaml` as
its first positional argument. There is no `--config` flag and no implicit
discovery of `./aether.yaml` or `~/.config/aether/aether.yaml`.

## Pipeline Commands

### `aether pipeline start`

Start a new pipeline job.

```bash
aether pipeline start <config> <crtdl> [input] [options]
```

**Arguments:**
- `<config>` - Path to `aether.yaml` (required, first positional)
- `<crtdl>` - CRTDL JSON file (required, second positional)
- `[input]` - Optional import-step input:
  - omitted: torch_import submits the CRTDL; local_import uses `--dir`/config
  - local directory (local_import)
  - HTTP(S) URL (http_import)
  - TORCH result URL (torch_import; auto-detected from URL shape)

**Options:**
- `--no-progress` - Disable progress indicators
- `--dir PATH` - Directory for local import (overrides config)
- `--allow-http-crtdl` - Acknowledge that HTTP data may not match the CRTDL query (required when `http_import` is enabled)

**Examples:**
```bash
# TORCH extraction with CRTDL
aether pipeline start aether.yaml crtdl.json

# Direct TORCH URL (skip extraction, poll and download results)
aether pipeline start aether.yaml crtdl.json "https://torch.example.com/fhir/extraction/result-123"

# Local import with CRTDL for flattening (dir from flag)
aether pipeline start aether.yaml crtdl.json --dir /path/to/data

# Local import with CRTDL for flattening (dir as positional)
aether pipeline start aether.yaml crtdl.json /path/to/data

# HTTP download piped through flattening
aether pipeline start aether.yaml crtdl.json https://example.com/fhir/data.ndjson --allow-http-crtdl

# Disable progress bars
aether pipeline start aether.yaml crtdl.json --no-progress
```

### `aether pipeline status`

Check pipeline job status.

```bash
aether pipeline status <config> <job-id>
```

**Output:**
- Job ID and status
- Current step
- Progress per step (files, bytes)
- Error messages if failed

**Example:**
```bash
aether pipeline status aether.yaml abc-123-def
```

### `aether pipeline continue`

Resume a paused or failed pipeline.

```bash
aether pipeline continue <config> <job-id> [options]
```

**Use cases:**
- Resume after terminal close
- Continue after fixing errors
- Resume from wait step after placing files in wait directory

**Options:**
- `--no-progress` - Disable progress indicators

**Examples:**
```bash
# Resume failed job
aether pipeline continue aether.yaml abc-123-def

# Resume from wait step (after placing files)
aether pipeline continue aether.yaml abc-123-def

# Resume without progress bars
aether pipeline continue aether.yaml abc-123-def --no-progress
```

## Job Commands

### `aether job list`

List all pipeline jobs.

```bash
aether job list <config>
```

**Output columns:**
- JOB ID - Job ID (`YYYYMMDD_HHMM_UUID`)
- STATUS - completed/in_progress/failed/pending/stopped/waiting
- STEP - Current step
- FILES - Total files processed
- AGE - Time since creation

**Status symbols:**
- `✓` - Completed
- `→` - In progress
- `✗` - Failed
- `○` - Pending
- `■` - Stopped
- `‖` - Waiting at the wait step

**Stopped jobs:**

A job becomes `stopped` when its process ends before the pipeline is complete.
Aether finds this in two ways:

1. If you press Ctrl+C, or the process receives SIGTERM, Aether writes the
   `stopped` status to the state file before it exits.
2. If the process dies without a chance to write, for example from SIGKILL, a
   crash, or a power loss, the state file keeps `in_progress`. `job list` and
   `pipeline status` then test the job lock. If no process holds the lock, they
   show the job as `stopped`.

Both commands only read. They do not change the state file in the second case.

A `stopped` job is not terminal. Continue it:

```bash
aether pipeline continue aether.yaml <job-id>
```

A `waiting` job also has no live process, but that state is intended: the wait
step paused the job. Continue it with the same command.

::: warning State file compatibility
The `stopped` and `waiting` job statuses are new. An older Aether binary rejects
a state file that holds one of them, and `job list` skips that job with a
warning. Do not change to an older version while jobs are in these states.
:::

**Example:**
```bash
aether job list aether.yaml
```

### `aether job run`

Execute a specific pipeline step manually.

```bash
aether job run <config> <job-id> --step <step-name>
```

**Options:**
- `--step` - Step to execute (required)
- `--force` - Re-run the step even if it has already completed

**Valid steps:** `torch`, `local_import`, `http_import`, `dimp`, `validation`, `flattening`, `send`

**Re-running completed steps:** An already-completed step is not re-run unless
`--force` is passed. Without `--force`, the command fails fast with `step
'<step>' already completed; pass --force to re-run` and a non-zero exit. This is
a pre-execution guard — nothing runs, so job state is left untouched.

With `--force`, the step is reset to pending and re-run. **Every step ordered
after it is also reset to pending**, because re-running a step invalidates the
output the later steps consumed (outputs are not diffed, so this is
conservative). The command itself re-runs only the target step; the invalidated
downstream steps stay pending until you re-run them — either with another `job
run --step` or by resuming the pipeline.

**Job status after a manual run:** The job-level status is always derived from
its steps. While a `--force` re-run runs, the job reads `in_progress` (visible in
`aether pipeline status`). After it finishes, the status is re-derived: the job
returns to `completed` only if every step is completed — so re-running the last
step completes the job, while re-running an earlier step leaves it `in_progress`
until the invalidated downstream steps re-run. Likewise, a manual run that
finishes the last outstanding step sets the job to `completed`.

After the command exits, no process runs the job. Thus a job left mid-pipeline
holds `in_progress` in the state file, but `job list` shows it as `stopped`.

**Examples:**
```bash
# Run DIMP step manually
aether job run aether.yaml abc-123-def --step dimp

# Run import step manually
aether job run aether.yaml abc-123-def --step local_import

# Re-run an already-completed step
aether job run aether.yaml abc-123-def --step dimp --force
```

## Other Commands

### `aether completion`

Generate shell completion scripts.

```bash
aether completion <shell>
```

**Shells:** `bash`, `zsh`, `fish`, `powershell`

**Examples:**
```bash
# Generate bash completions
aether completion bash > /etc/bash_completion.d/aether

# Generate zsh completions
aether completion zsh > "${fpath[1]}/_aether"
```

### `aether help`

Show help information.

```bash
aether help [command]
```

**Examples:**
```bash
aether help
aether help pipeline
aether help pipeline start
```

## Configuration Loading

The path to `aether.yaml` is the first positional argument of every command
that needs configuration; Aether does not auto-discover it. CLI flags and
`AETHER_*` environment variables still override individual values from the
file.

## Environment Variables

Configuration values support environment variable substitution:

```yaml
services:
  torch:
    username: "${TORCH_USERNAME}"
    password: "${TORCH_PASSWORD}"
```

## Next Steps

- [Configuration Reference](./config-reference.md) - All configuration options
- [Pipeline Steps](../guides/pipeline-steps.md) - Step details
