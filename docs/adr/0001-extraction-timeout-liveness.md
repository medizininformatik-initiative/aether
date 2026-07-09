---
status: accepted
---

# `extraction_timeout` is a liveness (no-progress) timeout, not a total cap

TORCH extractions for large cohorts (100k+ patients) legitimately run for hours, but the
`extraction_timeout` config (default 30 min) was a hard total cap measured from the start of each
poll run — so a healthy long job was killed for being long, and resuming only restarted the same
30-minute clock. We redefine `extraction_timeout` as the maximum time aether will wait **since last
contact with TORCH**, reset on every `202`/`200` poll response. A healthy job (polling at least every
`max_polling_interval`) never trips it; aether only gives up when TORCH goes silent.

## Considered options

- **Redefine the existing `extraction_timeout` key (chosen)** — no new config surface; the new
  meaning is what operators actually want. Cost: a silent behavior change for anyone who relied on it
  as a total-cost ceiling (rare; called out in the config comment and changelog).
- **Add a new `poll_liveness_timeout` key, keep `extraction_timeout` as a total cap** — cleaner
  naming but adds config surface and a deprecation story for a meaning almost nobody wants.
- **Liveness timeout plus a large absolute ceiling** — rejected for now; the only case it guards is a
  TORCH job stuck returning `202` forever (a TORCH bug), which an operator can interrupt manually.

## Consequences

- A TORCH job that stays responsive (`202`) but never completes will be polled indefinitely until
  manually interrupted. Accepted as a TORCH-side fault, not aether's to bound.

## Applied again: `download_stall_timeout` (#530)

The same liveness principle governs the NDJSON *download*, for the same reason. Downloads shared
the HTTP client's 30 s whole-request timeout, which capped the entire body read — so any extraction
that could not stream within 30 s was impossible to import. A healthy but large download was killed
for being large: exactly the failure this ADR rejects for polling, one layer down at the byte
stream.

Downloads now use a dedicated client with no whole-request deadline (`Timeout: 0`); a
`download_stall_timeout` (default `PT1M`) bounds *inactivity* instead, re-armed on every read of the
response body. A large but progressing download completes regardless of size, while a connection
that goes silent fails fast with a clear "download stalled" error. This mirrors the polling liveness
window (reset on each `202`/`200`) at the download stream.

- The stall window spans each body read together with the write of the chunk that read returned, so
  it bounds read+write inactivity, not a pure network stall. At the default 60 s the distinction is
  academic (a 32 KiB chunk write never approaches it); keeping the write inside the window also means
  a wedged local disk cannot hang the download forever, which narrowing to read-only would give up.
