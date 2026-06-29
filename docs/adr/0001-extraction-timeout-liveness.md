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
