# CSV Import

Imports CSV files that are already flattened from a local directory. Use this step to send CSV data that an earlier run or an external tool produced. The pipeline does not need FHIR data.

A typical flow is: a first run extracts, pseudonymizes, and flattens the data. You then analyze the CSV files with an external tool. A second run sends the files.

## Configuration

```yaml
pipeline:
  enabled_steps:
    - csv_import
    - send

services:
  csv_import:
    dir: ./output/2026-11-02_09-40-53
  send:
    send_as: transfer_load
    url: "https://transfer-server.example.com"
    auth:
      oauth_issuer_uri: "${OAUTH_ISSUER}"
      oauth_client_id: "${OAUTH_CLIENT_ID}"
      oauth_client_secret: "${OAUTH_CLIENT_SECRET}"
    transfer:
      project_identifier: "MII-PROJECT"
      organization_identifier: "your-org.example.de"
```

## Usage

```bash
# Use directory from config
aether pipeline start aether.yaml crtdl.json

# Override directory via flag
aether pipeline start aether.yaml crtdl.json --dir ./output/2026-11-02_09-40-53

# Directory as positional argument
aether pipeline start aether.yaml crtdl.json ./output/2026-11-02_09-40-53
```

The `--dir` flag has precedence over `services.csv_import.dir`. The positional argument is only used when neither is set.

## Configuration Options

| Option | Type | Description |
|--------|------|-------------|
| `dir` | string | Source directory (overridable with the `--dir` flag or as the third positional argument) |

## How it Works

1. The step deletes the contents of the `csv/` directory of the job. Thus, if `continue` runs the step again, the job holds only the files that are now in `dir`.
2. The step scans `dir` and all its subdirectories.
3. It copies each file with the extension `.csv` to the `csv/` directory of the job. The match ignores case.
4. It keeps the relative path. The file `dir/a/x.csv` becomes `csv/a/x.csv`.
5. It ignores all other files.

If `dir` is the `csv/` directory of the job, or lies in it, the step stops with an error and deletes nothing.

The step does not compress the files, because `send` zips each file itself. The relative path stays with the file: in the mode `transfer_load`, the name of the file in the zip is `a/x.csv`; in the mode `s3_upload`, the object key ends with `a/x.csv`. If the directory has no CSV file, the step stops with an error that names the directory.

The `csv/` directory is the same directory that `flattening` writes to. The next step reads from it.

## Restrictions

- `csv_import` must be the first step. No other import step can be enabled.
- After `csv_import`, only `wait` and `send` are allowed. The steps `dimp`, `validation`, and `flattening` need FHIR data, so the configuration check rejects them.
- `send_as` must be `transfer_load` or `s3_upload`. The mode `direct_resource_load` uploads FHIR NDJSON only, so the configuration check rejects it.

## Notes

- A CRTDL JSON file is always required as the second positional argument.
- The scan is always recursive. Put only the files to send in the source directory.
