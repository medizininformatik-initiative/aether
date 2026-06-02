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
aether pipeline start aether.yaml crtdl.json

# Override directory via flag
aether pipeline start aether.yaml crtdl.json --dir /other/path

# Override directory as positional (deprecated, prints warning)
aether pipeline start aether.yaml crtdl.json /other/path
```

## Configuration Options

| Option | Type | Description |
|--------|------|-------------|
| `dir` | string | Default import directory (overridable with `--dir` flag or as the third positional argument) |

## Notes

- The `--dir` flag takes precedence over the config file setting; passing the directory as a positional argument still works but is deprecated.
- A CRTDL JSON file is always required as the second positional argument — downstream steps such as flattening and CRTDL preprocessing depend on it.