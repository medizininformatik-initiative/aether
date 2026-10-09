# Local Import

Imports FHIR NDJSON files from a local directory. The step is made for the output directory of a TORCH extraction: you can set `dir` to that directory directly.

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

## TORCH Output

`local_import` selects the files to import by filename:

| Filename | Result |
|----------|--------|
| `*.ndjson`, `*.ndjson.zst` | Imported |
| `*_consent.ndjson`, `*_consent.ndjson.zst` | Ignored. TORCH writes these consent diagnosis files. They are not FHIR result data. |
| All other files | Ignored |

The match ignores case. For each ignored consent file, the step writes a debug log entry. If the directory contains only consent files, the step stops with the error `no FHIR NDJSON files found`.

## Notes

- The `--dir` flag takes precedence over the config file setting; passing the directory as a positional argument still works but is deprecated.
- A CRTDL JSON file is always required as the second positional argument — downstream steps such as flattening and CRTDL preprocessing depend on it.
- `local_import` copies every matched file into a single, flat destination directory keyed by filename. Leave `recursive` at its default `false` unless your source directory is deliberately organized across subdirectories with unique filenames throughout — scanning subdirectories that a producer (for example, TORCH) uses for its own internal working files can otherwise pick up unrelated data that happens to share a filename with a real result file.