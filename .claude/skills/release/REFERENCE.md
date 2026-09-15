# Release — Reference

## PR categorization rules

Map the auto-generated `gh release view --json body` PR list to sections by conventional-commit prefix:

| PR title prefix | Section |
|---|---|
| `feat!:` / `feat(x)!:` / any `!` / removes a flag / renames output | **Breaking Changes** |
| `feat:` / `feat(x):` | **Features** |
| `fix:` / `fix(x):` (NOT `fix(deps)`) | **Bug Fixes** |
| `refactor:` `docs:` `ci:` `test:` `chore:` (NOT `chore(deps)`) | **Internal & CI** |
| `chore(deps):` `fix(deps):` | **Dependency Updates** |

- A title without a prefix — judge by content (e.g. "Rename DIMP step output directory" → Breaking).
- Append `(closes #nnn)` / `(relates to #nnn)` where a PR resolves a milestone issue.
- Collapse repeated renovate PRs for the same component into one bullet listing all PR numbers
  (e.g. `aws-sdk-go-v2 monorepo #397 #402 #403`).
- Lead with the highest-impact change in the summary paragraph; mention the headline dependency bump (e.g. TORCH version).

## Worked example (v0.10.0)

Intro paragraph sits **above** `## What's Changed`:

```markdown
This release makes `aether.yaml` a required positional argument (dropping the `--config`
flag), renames the DIMP step output directory from `pseudonymized` to `dimp`, adds per-job
log files written into the job directory, fixes TORCH URL handling and extraction
reporting, refactors config loading to be struct-driven, and pulls in TORCH
`v1.0.0-beta.3` plus the usual dependency refresh.

## What's Changed

### Breaking Changes

- **`aether.yaml` is now a required first positional argument** — The `--config` flag is gone; pass the config path as the first argument instead. #333 (closes #329)
- **DIMP step output directory renamed `pseudonymized` → `dimp`** — Downstream steps reading pseudonymized output must use the new `dimp/` path. #412 (closes #408)

### Features

- **Per-job logs stored in the job directory** — Each job writes its own log file for easier debugging. #411

### Bug Fixes

- **Handle scheme-less host-prefixed URLs from TORCH** — Fixes double-host URLs. #352 (closes #351)
- **Show extraction summary instead of stale cohort diagnostic** #386 (closes #384)
- **Use Scorecard-recognized extensions for release artifacts** #376 (closes #375)

### Internal & CI

- **Struct-driven config loading** #428 (closes #422)
- **Remove dead exported flattening loaders** #339 (closes #336)
- **Replace MinIO with SeaweedFS for the S3 upload E2E** #434
- **Prune stale `go.sum` entries via `go mod tidy`** #440

### Dependency Updates

- TORCH `v1.0.0-beta.3` #380 #381 #437
- fhir-pseudonymizer `v2.26.1` #401 #436
- aws-sdk-go-v2 monorepo #397 #402 #403 #405 #417 #418 #432
- Go toolchain `v1.26.4` #416
- Vue `v3.5.35` #406, Vite `v8.0.16` #398 #431

[Full Changelog](https://github.com/medizininformatik-initiative/aether/compare/v0.9.0...v0.10.0)
```

## CI internals (for debugging a stuck release)

- `ci.yaml` triggers on `push: tags: ["v*.*.*"]`; the `release` job runs only `if startsWith(github.ref, 'refs/tags/')` and `needs` the test/lint/scan jobs.
- `_release.yaml` jobs: `build` → (`sign`, `sbom`, `attestation`) → `publish`. `publish` uses
  `softprops/action-gh-release` with `generate_release_notes: true`, `draft: false`,
  `prerelease` true for alpha/beta/rc tags.
- If `publish` succeeded but you need to re-edit notes, just re-run step 6 — `gh release edit` is idempotent.
- If the tag was pushed to the wrong commit: delete remote+local tag
  (`git push origin :refs/tags/vX.Y.Z` and `git tag -d vX.Y.Z`), re-tag, re-push.
