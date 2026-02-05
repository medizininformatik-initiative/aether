# Send

Uploads pipeline output to a destination server. Supports two modes.

## Direct Resource Load

Sends NDJSON directly to a FHIR server using transaction bundles.

```yaml
services:
  send:
    send_as: "direct_resource_load"
    url: "https://fhir-server.example.com/fhir"
    batch_size: 100  # resources per transaction (1-1000)
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
    url: "https://transfer-server.example.com/fhir"
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

## Configuration Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `send_as` | string | - | `direct_resource_load` or `transfer_load` |
| `url` | string | - | FHIR server base URL |
| `batch_size` | int | 100 | Resources per transaction (direct mode, 1-1000) |

## Authentication

Choose one method:

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

## Transfer Settings (transfer_load only)

| Option | Description |
|--------|-------------|
| `transfer.project_identifier` | MII project identifier |
| `transfer.organization_identifier` | Organization identifier |
