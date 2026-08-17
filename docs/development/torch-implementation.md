# TORCH Extraction Implementation

Developer reference for how Aether drives a TORCH data-extraction job end to end:
submitting a CRTDL query, polling for completion, and downloading the resulting
FHIR NDJSON files.

This page documents the *implementation* — the types, functions, and control
flow — behind Aether's use of TORCH's asynchronous Bulk Data extraction API. For
operator-facing usage see the [TORCH Integration guide](../guides/torch-integration.md)
and the [TORCH Import step](../guides/steps/torch-import.md).

## TORCH's two APIs

TORCH exposes **two** REST APIs, and Aether uses only the first:

- **[FHIR controller](https://medizininformatik-initiative.github.io/torch/api/api.html#fhir-controller)**
  — the extraction API, built on the
  [FHIR Asynchronous Bulk Data Request Pattern](http://hl7.org/fhir/R5/async-bulk.html):
  the `$extract-data` kick-off operation plus the `GET /fhir/__status/{jobId}`
  status/manifest endpoint. 
- **[Task API](https://medizininformatik-initiative.github.io/torch/api/api.html#task-controller)**
  (task-controller) — controls extraction jobs *in execution* (inspect, pause,
  resume, cancel), exposing each job as a FHIR `Task`. Aether does **not** use it
  today; job control and `Task` reads are out of scope
  (see [PR #474](https://github.com/medizininformatik-initiative/aether/pull/474)).

### The asynchronous bulk extraction flow

Aether drives the FHIR controller through three phases:

1. **Kick-off** — `POST $extract-data` creates the extraction job. TORCH returns
   the job's status URL (`.../fhir/__status/{jobId}`) in the `Content-Location`
   response header.
2. **Poll** — Aether polls the status URL. `202 Accepted` means the job is still
   running; `200 OK` means it completed and the response body is the async-bulk
   **manifest** carrying the output file URLs.
3. **Download** — Aether downloads each output file.

The extraction job is identified by a **TORCH job ID** (a UUID, distinct from the
Aether pipeline job ID). Aether persists a **job handle** — the job ID plus its
status URL — so it can re-attach to an in-flight job after a crash or restart
instead of re-submitting (see *Handle persistence and resume* below).
The `CONTEXT.md` glossary at the repository root defines the domain terms
(extraction job, job handle, re-attach, orphan, re-roll).

### Extraction job status (the "double status")

Each extraction job moves through a status lifecycle (coding system
`https://medizininformatik-initiative.de/torch/job-status`):

| Status | Meaning |
|--------|---------|
| `PENDING` | Accepted, not yet started |
| `RUNNING_GET_COHORT` | Selecting the cohort |
| `RUNNING_PROCESS_BATCH` | Extracting patient batches |
| `RUNNING_PROCESS_CORE` | Assembling shared (non-patient) `core` resources |
| `PAUSED` | Paused, resumable |
| `TEMP_FAILED` | Transient failure — **not terminal**; TORCH retries |
| `COMPLETED` / `FAILED` / `CANCELLED` | Terminal |

This status appears in **two** places — an artifact of the async-bulk design that
carries job state alongside the Task model: the Task API exposes it as a `Task`
business status, and the completion manifest from `__status` embeds the same job
state in a `torch-job` extension. Aether reads neither representation. It branches
purely on the async-bulk **HTTP** status the `__status` endpoint returns (see the
*Polling* stage below): `TEMP_FAILED` surfaces as `202`/`503`, terminal failure as
`500`, and completion as `200`.

## Source map

| File | Responsibility |
|------|----------------|
| `internal/pipeline/import.go` | Orchestration: submit → persist handle → poll → download, resume, error classification |
| `internal/pipeline/crtdl_prep.go` | `PrepareCRTDL` — copies/enriches the CRTDL into the job directory before submission |
| `internal/services/torch_client.go` | `TORCHClient`: submit, download, result parsing, file-availability checks, URL resolution, request/response types |
| `internal/services/torch_poller.go` | `PollConfig` and `handlePollResponse`: status interpretation, liveness window, exponential backoff |
| `internal/models/config.go` | `TORCHConfig` — the tunable knobs |
| `internal/models/job.go` | `CRTDLPath`, `TORCHExtractionURL`, `TORCHJobID` — the persisted resume handle |
| `internal/models/step.go` | `StepTorchImport` step constant |

## Lifecycle

```
                          internal/pipeline/import.go
┌──────────────────────────────────────────────────────────────────────┐
│ importStep.Run  (step == torch)                                      │
│   InputTypeTORCHURL ──► executeTORCHDownload  (poll URL directly)    │
│   InputTypeCRTDL    ──► executeTORCHExtraction                       │
└───────────────────────────────┬──────────────────────────────────────┘
                                 ▼
        job.TORCHJobID set? ──yes──► re-attach to in-flight job
                 │ no
                 ▼
   submitAndPersistTORCHHandle
     SubmitExtraction ─► POST {base_url}/fhir/$extract-data  (DoOnce, no retry)
        └─ 200 + Content-Location header ─► status URL
     persist job.TORCHExtractionURL + job.TORCHJobID to state.json
                 │
                 ▼
   PollExtractionStatus(statusURL)                 internal/services/torch_poller.go
     loop:
       GET statusURL (DoOnce)
       handlePollResponse:
         202 / 102 ─► in-progress; log OperationOutcome diagnostics; backoff
         200       ─► parseExtractionResult ─► [fileURLs]  ✔ done
         404 / 410 ─► ErrHandleDead ─► clear handle, re-submit, re-poll
         500       ─► terminal job failure (non-transient)
         408/429/other 5xx ─► transient; sleep + backoff; retry
       RecordContact() on every 200/202 (resets liveness window)
       CheckTimeout(): give up only after extraction_timeout of *silence*
                 │
                 ▼
   DownloadExtractionFiles([fileURLs], importDir)  internal/services/torch_client.go
     per file:
       waitForFileAvailability (HEAD, fallback Range GET)
       downloadFile ─► stall-guarded stream ─► {job}/import/<name>.ndjson[.zst]
```

## Stage reference

### 1. CRTDL preparation

Before the import step runs, `PrepareCRTDL` (`internal/pipeline/crtdl_prep.go`)
copies the input CRTDL into the job directory as `crtdl.json`, or — when CRTDL
preprocessing is enabled with at least one enrichment — writes an enriched
`enriched-crtdl.json`. It repoints `job.CRTDLPath` at that file so every
downstream step shares one effective CRTDL. Enrichment adds attributes DIMP
needs (for example, `Patient.identifier`) that the original query may omit.

### 2. Submission

`TORCHClient.SubmitExtraction(crtdlPath)`:

- Reads the CRTDL, validates it is JSON, and base64-encodes it
  (`encodeCRTDLToBase64`).
- Wraps it in a FHIR `Parameters` resource with a single `crtdl` parameter
  carrying `valueBase64Binary` (`TORCHExtractionRequest` / `TORCHParameter`).
- `POST {base_url}/fhir/$extract-data` with `Content-Type: application/fhir+json`
  and auth applied via `HTTPClient.ApplyAuth`.
- **Sends with `DoOnce` — deliberately no retry.** `$extract-data` is
  non-idempotent job creation; a retried timeout could spawn a duplicate
  extraction on the server.
- Reads the `Content-Location` header (the status URL) and normalizes it to an
  absolute URL (`makeAbsoluteURL`). A missing header is a hard error.

`SubmitExtractionWithContent(crtdlContent []byte)` is the in-memory variant for
already-enriched documents; it is otherwise identical (it sends
`Content-Type: application/json`).

### 3. Handle persistence and resume

`submitAndPersistTORCHHandle` stores the extraction handle **before** the long
poll begins:

- `job.TORCHExtractionURL` — the status URL to poll.
- `job.TORCHJobID` — the URL's trailing path segment (`JobIDFromStatusURL`),
  for example `.../fhir/__status/{jobId}`.

Both are written to `state.json` via `UpdateJob`, so a crash mid-extraction
leaves a recoverable handle. On resume, `executeTORCHExtraction` sees a non-empty
`job.TORCHJobID` and **re-attaches** to the running job instead of submitting a
new one.

If polling later reports the handle is dead (`ErrHandleDead`, from a `404`/`410`),
the orchestrator clears both fields, re-submits a fresh extraction, and resumes
polling.

### 4. Polling

`TORCHClient.PollExtractionStatus(statusURL, showProgress)` runs the poll loop;
`internal/services/torch_poller.go` owns the per-response logic and timing state
(`PollConfig`).

Each iteration issues a single `GET` (`DoOnce` — the loop, not the HTTP client,
owns retry cadence) with `Accept: application/json`, then `handlePollResponse`
interprets the status:

| Status | Meaning | Action |
|--------|---------|--------|
| `202 Accepted`, `102 Processing` | Still running | Read `OperationOutcome` `information` diagnostics for progress display; back off and poll again |
| `200 OK` | Complete | `parseExtractionResult` → file URLs → return |
| `404 Not Found`, `410 Gone` | Handle gone | Return `ErrHandleDead` (caller re-submits) |
| `500 Internal Server Error` | Terminal job failure | Non-transient error, stop |
| `408`, `429`, other `5xx` | Transient | Sleep + backoff, retry |
| other `4xx` | Terminal | Stop |

> **Note:** `500` is treated as terminal here even though 5xx is normally
> retryable, because TORCH surfaces a temporary failure (`TEMP_FAILED`) as
> `202`/`503` and reserves `500` for a genuinely failed job.

**Liveness window.** `extraction_timeout` is a *no-progress* window, not a total
cap. `CheckTimeout` returns true only when `time.Since(LastContact) > Timeout`,
and `RecordContact` resets `LastContact` on every `200`/`202`. A healthy job that
keeps answering — even one running for hours — never trips the timeout; Aether
gives up only when TORCH goes silent. See
[ADR 0001](../adr/0001-extraction-timeout-liveness.md) for the rationale.

**Exponential backoff.** The poll interval starts at `polling_interval` and
doubles after each in-progress poll (`CalculateNextPollInterval` /
`UpdateInterval`), capped at `max_polling_interval`.

Transient network failures on the GET itself (timeouts, connection resets) are
swallowed and retried — the extraction may still be running server-side, and the
liveness window is the safety net.

### 5. Result parsing

`parseExtractionResult` accepts two shapes and extracts absolute file URLs:

- **FHIR `Parameters`** (`TORCHExtractionResult`): reads `output` parameters and
  their `url` parts (`extractURLsFromFHIRFormat`).
- **Async-bulk manifest** (`TORCHSimpleResponse`): `{ "requiresAccessToken": …,
  "output": [ { "type": …, "url": … } ], "extension": [ … ] }`
  (`extractURLsFromSimpleFormat`) — the format the server actually returns today.
  Aether reads only the `output` URLs; the manifest's `torch-job` /
  diagnostics extensions are ignored.

An empty `output` is not silently accepted: if TORCH reports errors the parser
surfaces them; otherwise it returns a "no matching data" message explaining the
likely CRTDL causes (criteria matched nothing, out-of-range time period, unknown
cohort).

**URL resolution.** `makeAbsoluteURL` is a pure function that dispatches on the
URL shape: absolute URLs pass through, path-relative URLs
(`/output/x.ndjson`) resolve against `base_url` (`resolvePathRelativeURL`), and
scheme-less host-prefixed URLs (`host:8080/…`, a known TORCH misconfiguration)
inherit `base_url`'s scheme (`prependBaseScheme`).

### 6. Download

`TORCHClient.DownloadExtractionFiles` writes each output file into the job's
`import/` directory. Per file:

- Derive the filename from the URL's base, forcing an `.ndjson` suffix, then add
  the compression extension via `GetCompressedFilename`.
- **`waitForFileAvailability`** polls the file before downloading (TORCH results
  are often served through a proxy with eventual consistency). It uses a `HEAD`
  request, falling back to a `Range: bytes=0-0` GET when the server answers `403`,
  `404`, or `405`. It retries up to `file_ready_retries` times spaced by
  `file_ready_interval`; setting `file_ready_retries <= 0` disables the check.
- **`downloadFile` / `downloadFileOnce`** stream the body with
  `Accept: application/fhir+ndjson` and optional zstd compression on write.

**Stall watchdog.** Downloads use a dedicated `http.Client` (`downloadClient`)
with **no whole-request deadline**, so an arbitrarily large but steadily flowing
NDJSON completes regardless of total size. Inactivity is bounded instead by a
`stallGuardReader`: each read re-arms a `time.AfterFunc` timer, and if no bytes
arrive within `download_stall_timeout` the timer cancels the request context. The
cancellation is reported as `errDownloadStalled` rather than a generic
"context canceled". The watchdog is armed before even an error body is read, so a
proxy that flushes `4xx`/`5xx` headers then goes silent is bounded too.

**Retry.** `downloadFile` retries only a retryable `*TORCHError` (a transient
one), using the shared client's backoff config. A stall or a mid-body write error
is *not* a `*TORCHError` and is not retried — restarting from scratch would only
repeat the stall or waste a full re-transfer.

Each downloaded file yields a `models.FHIRDataFile` with size, resource
(`LineCount`) count, and `SourceStep: StepTorchImport`.

### 7. Error classification

`classifyImportError` decides whether a failure is transient (pipeline may retry
/ pause and resume) or terminal:

- It unwraps a `*services.TORCHError` (via `errors.As`, so wrapped
  submit/poll/download errors are caught) and returns its `ErrorType`.
- For `torch`/`http` import steps, a bare network error is transient.
- Everything else defaults to non-transient.

`TORCHError` carries `Operation` (`submit`/`poll`/`download`), `StatusCode`,
`Message`, and `ErrorType`; `IsRetryable()` is true only for
`models.ErrorTypeTransient`.

### Direct TORCH result URL

When the input is an already-complete TORCH result URL
(`InputType == InputTypeTORCHURL`), `executeTORCHDownload` skips submission
entirely: it polls the URL directly (expecting an immediate `200`) and downloads
the listed files.

## Data types

Defined in `internal/services/torch_client.go`:

| Type | Purpose |
|------|---------|
| `TORCHExtractionRequest` / `TORCHParameter` | Kickoff body: FHIR `Parameters` with a base64 `crtdl` parameter |
| `TORCHExtractionResult` / `TORCHResultParameter` / `TORCHResultPart` | FHIR `Parameters` result format |
| `TORCHSimpleResponse` / `TORCHSimpleOutput` | Simplified `{ output: [ { type, url } ] }` result format |
| `OperationOutcome` / `OperationOutcomeIssue` | Progress diagnostics parsed from `202` bodies |
| `TORCHError` | Structured submit/poll/download error with retryability |

Sentinel errors: `ErrExtractionTimeout`, `ErrHandleDead`, `ErrInvalidCRTDL`.

## Configuration

`TORCHConfig` (`internal/models/config.go`) tunes the flow. Defaults are set in
`DefaultConfig` and range-checked in `TORCHConfig.Validate`:

| Field | Default | Effect |
|-------|---------|--------|
| `base_url` | — | TORCH server base URL (required when the torch step is enabled) |
| `auth` | — | `models.AuthConfig`, the same block as `dimp` and `send`. `TORCHConfig.EffectiveAuth` reads it. |
| `extraction_timeout` | `30m` | Liveness window — max time to wait **without** a response; reset on every `200`/`202` |
| `polling_interval` | `5s` | Initial status poll interval (must be ≥ `1s`) |
| `max_polling_interval` | `30s` | Backoff cap (must be ≥ `polling_interval`) |
| `file_ready_retries` | `10` | Availability checks before download; `0` disables the check |
| `file_ready_interval` | `10s` | Delay between availability checks |
| `download_stall_timeout` | `60s` | Inactivity window while streaming a file; `0` uses the built-in default |

`TORCHConfig` also keeps the deprecated flat auth fields (`Username`,
`Password`, `OAuthIssuerURI`, `OAuthClientID`, `OAuthClientSecret`) for older
configuration files. `EffectiveAuth` maps them to an `AuthConfig`, thus the
client reads one shape only. `Validate` refuses a config that sets both shapes,
and `LoadConfig` writes a deprecation warning for the flat fields.

See the [Configuration Reference](../api-reference/config-reference.md#torch) for
the operator-facing table.

## Persisted job handle

For resume, the following `PipelineJob` fields (`internal/models/job.go`) are
written to `state.json`:

| Field | Purpose |
|-------|---------|
| `CRTDLPath` | The effective CRTDL used for submission (decoupled from the raw input source) |
| `TORCHExtractionURL` | The `Content-Location` status URL to re-poll |
| `TORCHJobID` | The extraction job ID (handle) used to re-attach to an in-flight job |

## Testing seams

The import step depends on the `Extractor` interface — `SubmitExtraction`,
`PollExtractionStatus`, `DownloadExtractionFiles` — which `TORCHClient`
satisfies. `extractorFactory` (`internal/pipeline/import.go`) builds it, and
`SetExtractorFactoryForTesting` / `ResetExtractorFactory` swap in a fake so tests
exercise submit/poll/download without a live TORCH server. Pure helpers such as
`makeAbsoluteURL`, `CalculateNextPollInterval`, and the `PollConfig` liveness
methods are unit-tested directly.

## Next Steps

- [Architecture](./architecture.md) — system-wide design and data flow
- [TORCH Integration](../guides/torch-integration.md) — operator-facing guide
- [ADR 0001: Extraction timeout liveness](../adr/0001-extraction-timeout-liveness.md)
- [Configuration Reference](../api-reference/config-reference.md#torch) — TORCH config knobs
