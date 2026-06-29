# Context

Glossary of domain language for aether. Terms only — no implementation detail.

## Pipeline

- **Pipeline job** — one execution of the Data Use Process in aether, identified by an aether job ID, persisted on disk under `jobs/<job-id>/`. Has an ordered list of steps and a status.
- **Import step** — the pipeline step that brings FHIR data into the job. One of three modes: local import, HTTP import, or **TORCH extraction**.

## TORCH extraction

- **TORCH** — external FHIR extraction service. Given a CRTDL query it selects a cohort and extracts the requested resources.
- **Extraction job** — the server-side unit of work TORCH runs in response to a kick-off. Identified by a **TORCH job ID** (a UUID, distinct from the aether pipeline job ID). Long-running for large cohorts (can run for hours at 100k+ patients).
- **Job handle** — the identifier aether persists so it can reconnect to an extraction job it already started: the TORCH job ID (and the status URL derived from / returned with it). Without a persisted handle, a crashed aether run cannot tell TORCH "I already started this."
- **Re-attach** — on resume, reconnecting to an existing extraction job (polling its status) instead of submitting a new kick-off. The opposite of re-submitting, which would start the extraction over from scratch.
- **Orphan** — a server-side extraction job that is still running (or holding results) but for which aether has lost or never persisted the handle. Causes duplicate load on the source FHIR server when aether re-submits (each kick-off mints a new TORCH job ID — there is no dedup). TORCH has no time-based expiry: jobs and results persist until an explicit `DELETE`, after which a garbage collector reclaims the directory. Orphans do not self-clean.
- **Re-roll** — TORCH's own recovery on restart: in-progress extraction jobs are reset to a reprocessable state, so a persisted **job handle** stays valid and **re-attach** works across a TORCH restart.

## Extraction job status

The lifecycle a TORCH extraction job moves through (coding system `https://medizininformatik-initiative.de/torch/job-status`):

- **PENDING** — accepted, not yet started.
- **RUNNING_GET_COHORT** — selecting the cohort.
- **RUNNING_PROCESS_BATCH** — extracting patient batches.
- **RUNNING_PROCESS_CORE** — assembling the shared (non-patient) `core` resources.
- **PAUSED** — paused, resumable.
- **TEMP_FAILED** — failed transiently; **not terminal**, TORCH will retry.
- **COMPLETED** / **FAILED** / **CANCELLED** — terminal. (`DELETED` is internal bookkeeping and never appears in a Task response.)
