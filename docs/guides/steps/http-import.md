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
aether pipeline start https://example.com/fhir/Patient.ndjson
```

## Notes

- The URL is provided as the argument to `pipeline start`
- Supports both HTTP and HTTPS URLs
- Downloads files to the job directory
