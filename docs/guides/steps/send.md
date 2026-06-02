# Send

Uploads pipeline output to a destination. Supports three modes selected via `send_as`:

- `direct_resource_load` — POST FHIR resources to a FHIR server using transaction bundles
- `transfer_load` — wrap files in `Binary` / `DocumentReference` resources and POST them to a DSF transfer server
- `s3_upload` — upload files directly to an S3-compatible object store (AWS S3, MinIO, Ceph)

## Direct Resource Load

Sends NDJSON directly to a FHIR server using transaction bundles.

```yaml
services:
  send:
    send_as: "direct_resource_load"
    url: "https://fhir-server.example.com"  # server root; /fhir appended by client
    batch_size: 100  # resources per transaction (0-1000, default 100)
    auth:
      username: "${FHIR_USER}"
      password: "${FHIR_PASSWORD}"

pipeline:
  enabled_steps:
    - local_import
    - dimp
    - send
```

### How it Works

1. Reads NDJSON files from input directory
2. Batches resources into transaction bundles
3. Uploads each bundle to the FHIR server
4. Uses PUT for resources with IDs, POST for resources without

## Transfer Load

Packages files for DSF-based transfer using Binary/DocumentReference resources.

```yaml
services:
  send:
    send_as: "transfer_load"
    url: "https://transfer-server.example.com"  # server root; /fhir appended by client
    auth:
      oauth_issuer_uri: "${OAUTH_ISSUER}"
      oauth_client_id: "${OAUTH_CLIENT_ID}"
      oauth_client_secret: "${OAUTH_CLIENT_SECRET}"
    transfer:
      project_identifier: "MII-PROJECT"
      organization_identifier: "your-org.example.de"

pipeline:
  enabled_steps:
    - local_import
    - dimp
    - send
```

### How it Works

1. Reads all files from input directory
2. Zips each file and wraps in FHIR Binary resource
3. Creates DocumentReference linking all Binary resources
4. Uploads to transfer server

## S3 Upload

Uploads pipeline output files directly to an S3-compatible bucket. Works with AWS S3, MinIO, Ceph, and any other S3-API-compatible store.

```yaml
services:
  send:
    send_as: "s3_upload"
    s3:
      bucket: "${S3_BUCKET}"
      region: "eu-central-1"
      access_key_id: "${AWS_ACCESS_KEY_ID}"
      secret_access_key: "${AWS_SECRET_ACCESS_KEY}"

      # Optional: custom endpoint for non-AWS stores (MinIO, Ceph)
      # endpoint: "http://minio.example.com:9000"

      # Optional: required for MinIO and most non-AWS stores
      # use_path_style: true

      # Optional: per-upload timeout (default PT30M)
      # timeout: PT30M

pipeline:
  enabled_steps:
    - local_import
    - dimp
    - send
```

`url` is **not** required in `s3_upload` mode — it is only used by the FHIR send modes.

### How it Works

1. Reads all files from input directory (recursively)
2. Uploads each file to the configured bucket using AWS SDK v2
3. Records the returned ETag per upload

### Authentication

`access_key_id` and `secret_access_key` authenticate to the S3 API itself.

The optional top-level `auth` block is reused as **proxy authentication** for environments where the S3 endpoint sits behind an HTTP proxy that requires its own credentials. When `auth.username` and `auth.password` are set in `s3_upload` mode, aether attaches a `Proxy-Authorization: Basic …` header to every S3 request without disturbing the AWS SDK's own `Authorization` signing header.

```yaml
services:
  send:
    send_as: "s3_upload"
    auth:                              # only used as proxy auth in s3_upload mode
      username: "${PROXY_USER}"
      password: "${PROXY_PASS}"
    s3:
      bucket: "${S3_BUCKET}"
      region: "eu-central-1"
      access_key_id: "${AWS_ACCESS_KEY_ID}"
      secret_access_key: "${AWS_SECRET_ACCESS_KEY}"
```

### Transient Errors

The S3 client classifies AWS responses such as `SlowDown`, `ServiceUnavailable`, `InternalError`, `RequestTimeout`, `TooManyRequests`, and connection-level failures (`connection reset`, `connection refused`, generic timeouts) as transient. These are retried according to the top-level `retry` settings; all other errors fail the step immediately.

## Configuration Options

### Common

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `send_as` | string | — | `direct_resource_load`, `transfer_load`, or `s3_upload` (required) |
| `url` | string | — | FHIR server root URL. Required for `direct_resource_load` and `transfer_load`. Do not include `/fhir` — the client appends it. Ignored for `s3_upload`. |
| `batch_size` | int | 100 | Resources per transaction (`direct_resource_load` only, 0–1000) |
| `auth` | object | — | Authentication (see below) |

### `transfer` (transfer_load only)

| Option | Description |
|--------|-------------|
| `transfer.project_identifier` | MII project identifier (used in `DocumentReference.masterIdentifier`) |
| `transfer.organization_identifier` | Organization identifier (used in `DocumentReference.author`) |

### `s3` (s3_upload only)

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `s3.bucket` | string | — | Target bucket name (required) |
| `s3.region` | string | — | AWS region (required, e.g. `eu-central-1`) |
| `s3.access_key_id` | string | — | S3 access key (required) |
| `s3.secret_access_key` | string | — | S3 secret key (required) |
| `s3.endpoint` | string | — | Custom endpoint URL for S3-compatible stores. Leave empty for AWS S3. Must be `http://` or `https://`. |
| `s3.use_path_style` | bool | `false` | Use path-style addressing (required for MinIO and many S3-compatible stores) |
| `s3.timeout` | duration | `PT30M` | Per-request timeout (ISO 8601 or Go duration format) |

## Authentication

For FHIR send modes (`direct_resource_load`, `transfer_load`), choose one method:

**Basic Auth:**
```yaml
auth:
  username: "${FHIR_USER}"
  password: "${FHIR_PASSWORD}"
```

**OAuth2 Client Credentials:**
```yaml
auth:
  oauth_issuer_uri: "${OAUTH_ISSUER}"
  oauth_client_id: "${OAUTH_CLIENT_ID}"
  oauth_client_secret: "${OAUTH_CLIENT_SECRET}"
```

For `s3_upload`, the `auth` block is optional and only used for upstream proxy auth (see [S3 Upload → Authentication](#authentication-1)). The S3 API itself is always authenticated via the keys under `s3.*`.
