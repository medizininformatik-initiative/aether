# CLI Commands

Reference for Aether CLI commands.

## Global Options

```bash
aether [global-options] <command> [command-options]
```

- `--config FILE` - Path to configuration file (default: ./aether.yaml)
- `--verbose, -v` - Enable verbose logging
- `--help, -h` - Show help
- `--version` - Show version

## Pipeline Commands

### aether pipeline start

Start a new pipeline job.

```bash
aether pipeline start [input] [options]
```

**Arguments:**
- `[input]` - CRTDL file, TORCH URL, or HTTP URL. CRTDL file required if flattening enabled. TORCH URLs (containing `/fhir/extraction/` or `/fhir/result/`) skip extraction submission.

**Options:**
- `--no-progress` - Disable progress indicators
- `--dir PATH` - Directory for local import (overrides config)

**Examples:**
```bash
# TORCH extraction with CRTDL
aether pipeline start query.crtdl

# Direct TORCH URL (skip extraction, poll and download results)
aether pipeline start "https://torch.example.com/fhir/extraction/result-123"

# Local import with CRTDL for flattening
aether pipeline start query.crtdl --dir /path/to/data

# Local import without CRTDL (uses config directory)
aether pipeline start

# Local import with directory override
aether pipeline start --dir /path/to/data

# HTTP download (single file)
aether pipeline start https://example.com/fhir/data.ndjson

# Disable progress bars
aether pipeline start query.crtdl --no-progress
```

### aether pipeline status

Check pipeline job status.

```bash
aether pipeline status <job-id>
```

**Output:**
- Job ID and status
- Current step
- Progress per step (files, bytes)
- Error messages if failed

**Example:**
```bash
aether pipeline status abc-123-def
```

### aether pipeline continue

Resume a paused or failed pipeline.

```bash
aether pipeline continue <job-id>
```

**Use cases:**
- Resume after terminal close
- Continue after fixing errors
- Resume from wait step after placing files in wait directory

**Examples:**
```bash
# Resume failed job
aether pipeline continue abc-123-def

# Resume from wait step (after placing files)
aether pipeline continue abc-123-def
```

## Job Commands

### aether job list

List all pipeline jobs.

```bash
aether job list
```

**Output columns:**
- JOB ID - Full UUID
- STATUS - completed/in_progress/failed/pending
- STEP - Current step
- FILES - Total files processed
- AGE - Time since creation

**Status symbols:**
- `✓` - Completed
- `→` - In progress
- `✗` - Failed
- `○` - Pending

**Example:**
```bash
aether job list
```

### aether job run

Execute a specific pipeline step manually.

```bash
aether job run <job-id> --step <step-name>
```

**Options:**
- `--step` - Step to execute (required)

**Valid steps:** `torch`, `local_import`, `http_import`, `dimp`, `validation`, `csv_conversion`, `parquet_conversion`

**Examples:**
```bash
# Run DIMP step manually
aether job run abc-123-def --step dimp

# Run import step manually
aether job run abc-123-def --step local_import
```

## Other Commands

### aether completion

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

### aether version

Show version information.

```bash
aether version
```

### aether help

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

Aether loads configuration in this order:
1. `--config` flag
2. `./aether.yaml` (current directory)
3. `~/.config/aether/aether.yaml` (user config)

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
