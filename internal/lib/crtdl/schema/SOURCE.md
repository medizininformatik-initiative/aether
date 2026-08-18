# Where these schemas come from

`crtdl-schema.json` and `ccdl-schema.json` are unmodified copies of the schemas
that the dataportal-backend enforces. aether embeds them (`../schema.go`) and
validates each CRTDL document against them.

- Repository: [medizininformatik-initiative/dataportal-backend](https://github.com/medizininformatik-initiative/dataportal-backend)
- Directory: `src/main/resources/de/medizininformatikinitiative/dataportal/backend/query/api/validation`

`upstream.json` holds the pin: the repository, the directory, the ref, and the
sha256 of each file. It is the only place that records the checksums.

The placeholder `$id` `http://example.com/schema/data-extraction-schema.json` in
`crtdl-schema.json` is upstream text. Do not correct it. A change here makes the
copy different from upstream and breaks the check.

## Checks

`.github/scripts/verify-crtdl-schemas.sh` compares the copies with the pin and
with upstream. The `Schemas` job in CI runs it on every push.

The pin and upstream at the pinned ref never change, so that comparison cannot
show that upstream moved on. The script therefore does a second comparison,
against the ref in `drift_ref`. In CI it prints a warning. The
`Schema Drift` workflow runs the same comparison each week and opens an issue.

## How to update the pin

1. Read the upstream changes and decide whether aether must follow.
2. Copy both files from the new ref into this directory. Do not edit them.
3. Set `ref` in `upstream.json` to the new ref.
4. Put the new checksums in `upstream.json`:

   ```sh
   sha256sum internal/lib/crtdl/schema/*-schema.json
   ```

5. Run the check and the tests:

   ```sh
   .github/scripts/verify-crtdl-schemas.sh
   go test ./internal/lib/crtdl/...
   ```

6. If the new schema rejects a test fixture, correct the fixture. Do not relax
   the schema.

Renovate cannot do this update. Its `*_VERSION`/`*_CHECKSUM` manager reads
release attachments, and the dataportal-backend releases a Java artifact, not
these files.
