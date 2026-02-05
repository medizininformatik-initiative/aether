# Local Import

Imports FHIR NDJSON files from a local directory.

## Configuration

```yaml
services:
  local_import:
    dir: "/path/to/fhir/data"

pipeline:
  enabled_steps:
    - local_import
```

## Usage

```bash
# Use directory from config
aether pipeline start

# Override directory via flag
aether pipeline start --dir /other/path

# With CRTDL for flattening
aether pipeline start query.crtdl --dir /path/to/data
```

## Configuration Options

| Option | Type | Description |
|--------|------|-------------|
| `dir` | string | Default import directory (overridable with `--dir` flag) |

## Notes

- The `--dir` flag takes precedence over the config file setting
- When using flattening step, a CRTDL file is still required as input
