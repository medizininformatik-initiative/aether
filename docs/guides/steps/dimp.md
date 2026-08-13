# DIMP (Pseudonymization)

De-identifies FHIR data using a DIMP service.

## What it does

- Sends FHIR Bundles to DIMP service
- Automatically splits large Bundles (>10 MB by default)
- Saves pseudonymized data

## Configuration

```yaml
services:
  dimp:
    url: "http://your-dimp-server:32861"  # server root; /fhir appended by client
    bundle_split_threshold_mb: 10  # 1-100 MB, default: 10
    auth:                          # optional; use one scheme only
      api_key: "${DIMP_API_KEY}"

pipeline:
  enabled_steps:
    - local_import
    - dimp
```

## Configuration Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `url` | string | - | DIMP server root URL (required). Do not include `/fhir` — the client appends it. |
| `bundle_split_threshold_mb` | int | 10 | Split Bundles larger than this (1-100 MB) |
| `auth.username` | string | - | Basic Auth username |
| `auth.password` | string | - | Basic Auth password |
| `auth.oauth_issuer_uri` | string | - | OAuth 2.0 issuer URI. When set, Aether fetches client-credentials bearer tokens. |
| `auth.oauth_client_id` | string | - | OAuth 2.0 client ID |
| `auth.oauth_client_secret` | string | - | OAuth 2.0 client secret |
| `auth.api_key` | string | - | API key. Sent in its own header, not in `Authorization`. |
| `auth.api_key_header` | string | `x-api-key` | Header that carries `api_key`. Change it only for a service or proxy that expects a different header. |

## Authentication

The `auth` block is optional. Set it when the DIMP service, or a reverse proxy in
front of it, requires credentials. It accepts Basic Auth, OAuth 2.0 client
credentials, and an API key.

Basic Auth and OAuth 2.0 both set the `Authorization` header. Aether refuses a
config that sets both. The API key uses its own header, thus you can set it
together with Basic Auth or OAuth 2.0. Aether also refuses an incomplete set of
fields for one scheme.

Keep credentials out of the config file with environment variables:

```bash
export AETHER_SERVICES_DIMP_AUTH_API_KEY="your-api-key"
export AETHER_SERVICES_DIMP_AUTH_PASSWORD="your-password"
```

## Bundle Splitting

Large FHIR Bundles are automatically split to prevent HTTP 413 errors:

- Bundles exceeding the threshold are partitioned into smaller chunks
- Each chunk is sent separately to DIMP
- Results are reassembled after processing
- 100% data preservation during split-reassemble

## Output

Pseudonymized files are saved to `jobs/<job-id>/dimp/`
