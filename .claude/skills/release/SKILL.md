---
name: release
description: Cut a new aether release — create and push a version tag, wait for the release CI, write the GitHub release notes, close the milestone, and open the next one so two future milestones stay ahead. Use when the user asks to "create a release", "cut a release", "do the next release", "tag a release", or "release vX.Y.Z".
---

# Release

Cut the next aether release. Version is derived from the git tag via `git describe`
(`Makefile`), so there is **no version file to bump** — the tag is the source of truth.
Pushing a `v*.*.*` tag triggers `.github/workflows/ci.yaml` → `_release.yaml`, which
builds, signs, and **auto-creates the GitHub release** with generated notes. You then
replace those notes with structured ones, close the milestone, and open the next one so
two future milestones always stay ahead.

## Workflow (do in order)

1. **Determine the version.** Latest tag: `git tag --sort=-v:refname | head`. The next
   version normally matches the lowest open milestone (`gh api repos/{owner}/aether/milestones --jq '.[]|{title,number,open_issues,closed_issues,state}'`).
   Confirm with the user if ambiguous.

2. **Handle unfinished milestone issues.** List them:
   `gh issue list --milestone "vX.Y.Z" --state open`. If any are open, the milestone
   can't close cleanly — **ask the user** whether to move them to the next milestone
   (`gh issue edit <n> --milestone "vNEXT"`), drop them, or close as-is.

3. **Verify the working tree is synced** before tagging:
   `git rev-parse HEAD` must equal `git rev-parse origin/main` (run `git fetch --tags` first).
   Tag the commit that's actually on `origin/main`.

4. **Create and push the annotated tag** (match existing convention: annotated, message `Release vX.Y.Z`):
   ```
   git tag -a vX.Y.Z -m "Release vX.Y.Z"
   git push origin vX.Y.Z
   ```
   Bypass the sandbox for the push.

5. **Wait for CI.** Find the run: `gh run list --workflow=ci.yaml --event=push --limit 5`.
   Watch it: `gh run watch <id> --exit-status --interval 30`.
   The `Release` job runs only after lint/tests/codeql/vuln pass.
   - **E2E Validation Tests fail transiently** (Docker image pull/network). CI retries the
     job automatically — keep watching. If it lands failed, re-run failed jobs:
     `gh run rerun <id> --failed` (errors with "already running" if a retry is in flight — just keep watching).
     Only investigate as a real failure if logs show a code/test error, not a pull/infra error.

6. **Write the release notes.** Get the auto-generated PR list:
   `gh release view vX.Y.Z --json body --jq .body`. Reshape it into the house format —
   **orient on the previous release**: `gh release view <prev-tag>`. Sections, in order,
   omitting any that are empty:
   - `## What's Changed` then a one-paragraph summary intro (above the heading, like prior releases)
   - `### Breaking Changes` — `!`-marked / behavior-changing PRs; note migration impact, `(closes #nnn)`
   - `### Features` — `feat:` PRs
   - `### Bug Fixes` — `fix:` PRs (exclude `fix(deps)` — those are dependencies)
   - `### Internal & CI` — refactor/docs/ci/test/chore (non-dep)
   - `### Dependency Updates` — group renovate PRs by component, list PR numbers
   - `[Full Changelog](.../compare/vPREV...vX.Y.Z)`

   Write to a file, then apply: `gh release edit vX.Y.Z --notes-file <file>`.
   See [REFERENCE.md](REFERENCE.md) for a worked example and PR-categorization rules.

7. **Close the milestone.** Confirm `open_issues == 0`
   (`gh api repos/{owner}/aether/milestones/<number> --jq '{title,open_issues}'`), then:
   `gh api -X PATCH repos/{owner}/aether/milestones/<number> -f state=closed`.

8. **Keep two future milestones open.** After closing, list open milestones:
   `gh api repos/{owner}/aether/milestones --jq '.[]|select(.state=="open")|.title'`.
   The invariant: **exactly two open milestones ahead of the released version** should
   remain. Closing the released one normally leaves one open (e.g. released `v0.5.0`,
   `v0.6.0` still open), so create the next: take the **highest open milestone** and bump
   its minor by one (`v0.6.0` → `v0.7.0`), then:
   `gh api -X POST repos/{owner}/aether/milestones -f title="vX.Y.Z"`.
   This restores two open milestones (`v0.6.0`, `v0.7.0`). If two (or more) are already
   open, skip — the invariant already holds.

## Notes

- Owner/repo: `medizininformatik-initiative/aether`.
- Release artifacts (signed binaries, SBOM, attestations) are produced by CI — never build/upload them by hand.
- `alpha`/`beta`/`rc` tags are auto-marked prerelease by the workflow.
