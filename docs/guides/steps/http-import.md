# HTTP Import

Downloads FHIR NDJSON files from an HTTP/HTTPS URL.

## Configuration

```yaml
pipeline:
  enabled_steps:
    - http_import
```

## Usage

```bash
aether pipeline start aether.yaml crtdl.json https://example.com/fhir/Patient.ndjson --allow-http-crtdl
```

## Notes

- Every `aether pipeline start` invocation requires a CRTDL JSON as the second positional argument, even when the import URL already points at the data — the CRTDL is used by downstream steps such as flattening.
- The HTTP(S) URL is provided as the third positional argument and overrides any local-import configuration.
- HTTP import bypasses TORCH's CRTDL guarantees, so aether refuses the run unless you opt in with `--allow-http-crtdl`. Pass the flag to acknowledge that the downloaded data may not match the CRTDL query.
- Both HTTP and HTTPS URLs are supported; downloads land in the job directory.