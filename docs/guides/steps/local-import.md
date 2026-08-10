# Local Import

Imports FHIR NDJSON files from a local directory.

## Configuration

```yaml
services:
  local_import:
    dir: "/path/to/fhir/data"
    recursive: false

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
| `recursive` | bool | Scan subdirectories of `dir` for NDJSON files. Default `false` — only the top-level directory is scanned. |

## Notes

- The `--dir` flag takes precedence over the config file setting; passing the directory as a positional argument still works but is deprecated.
- A CRTDL JSON file is always required as the second positional argument — downstream steps such as flattening and CRTDL preprocessing depend on it.
- `local_import` copies every matched file into a single, flat destination directory keyed by filename. Leave `recursive` at its default `false` unless your source directory is deliberately organized across subdirectories with unique filenames throughout — scanning subdirectories that a producer (for example, TORCH) uses for its own internal working files can otherwise pick up unrelated data that happens to share a filename with a real result file.