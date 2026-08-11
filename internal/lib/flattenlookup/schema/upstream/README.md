# Upstream schema (reference only)

`LookupFileSchema.json` is a byte-exact copy of the schema from the
[fhir-ontology-generator](https://github.com/medizininformatik-initiative/fhir-ontology-generator)
repository:

- Source: `flattening/schema/LookupFileSchema.json` on branch `main`
- Vendored from upstream commit `bd0c0cfa3669` (2026-05-04)

The generator produces this schema from its Pydantic models. It describes
one lookup table as a single object and keeps only weak constraints. This
tool does **not** validate against it. Validation uses the strict schema in
`schema/flatten-lookup.schema.json`.

We vendor the file for two reasons:

1. It documents the format that the generator emits.
2. CI compares it with the current upstream file and fails on drift
   (see `.github/workflows/upstream-schema-drift.yml`). A failure tells
   us that the upstream format changed and that our strict schema may
   need an update.

To update after intentional drift: replace the file with the current
upstream content and update the commit reference above.
