# DIMP

DIMP (**D**e-identify, **M**inimize, **P**seudonymize) provides de-identification, minimization, and pseudonymization for FHIR data, protecting patient privacy while keeping the data useful for research.

## What DIMP Does

- Removes or masks identifying information (names, addresses, etc.)
- Generates consistent pseudonyms for patient identifiers
- Preserves clinical data (diagnoses, procedures, lab values)

## Configuration

Add DIMP to your `aether.yaml`:

```yaml
services:
  dimp:
    url: "http://your-dimp-server:32861"

pipeline:
  enabled_steps:
    - torch   # or local_import
    - dimp    # Pseudonymize after import

jobs_dir: "./jobs"
```

## Running Pseudonymization

```bash
aether pipeline start aether.yaml your-crtdl.json
```

Aether will:
1. Extract data from TORCH (or import from files)
2. Send it to DIMP for dimping
3. Save the protected data in the jobs folder

## Output

Results are saved in:

```
jobs/<job-id>/
├── state.json                    # Job state and step status
└── dimp/
    └── dimped_<name>.ndjson.zst  # Pseudonymized data (one file per input; .ndjson when compression is disabled)
```

## CRTDL Preprocessing

DIMP requires certain attributes (like `Patient.identifier`) to be present in the extracted FHIR data. If your CRTDL query doesn't include these attributes, DIMP pseudonymization will fail.

CRTDL preprocessing automatically enriches your CRTDL with the required attributes before sending it to TORCH:

```yaml
services:
  crtdl_preprocessing:
    enabled: true
    enrichments:
      - group_reference: "https://www.medizininformatik-initiative.de/fhir/core/modul-person/StructureDefinition/PatientPseudonymisiert"
        create_if_not_exists:
          group_name: "PatientPseudonymisiert"
        attributes_to_add:
          - attribute_ref: "Patient.identifier"
            must_have: false
      - group_reference: "https://www.medizininformatik-initiative.de/fhir/core/modul-fall/StructureDefinition/KontaktGesundheitseinrichtung"
        attributes_to_add:
          - attribute_ref: "Encounter.identifier"
            must_have: false
```

The `create_if_not_exists` option creates the group in the CRTDL if it doesn't already exist. This is useful for groups like `PatientPseudonymisiert` that may not be part of the original research query but are needed by DIMP.

Enrichment rules can also be loaded from an external JSON file. See [CRTDL Preprocessing](../api-reference/config-reference.md#crtdl-preprocessing) in the configuration reference for details.

### Linked Groups

An added attribute can link other attribute groups with `linked_groups`. Give the
profile URL of each group. Aether changes each profile URL into the id of the
attribute group that has this `group_reference`:

```yaml
attributes_to_add:
  - attribute_ref: "Patient.identifier"
    must_have: false
    linked_groups:
      - "https://www.medizininformatik-initiative.de/fhir/core/modul-fall/StructureDefinition/KontaktGesundheitseinrichtung"
```

Aether does this after it applies all the rules. Thus a rule can link a group
that a subsequent rule creates, and the sequence of the rules does not change
the result.

If a profile URL agrees with no attribute group, the pipeline stops with an
error that gives the group, the attribute, and the URL. Aether writes no
`enriched-crtdl.json`. Correct the URL, or add a rule that creates the group.

## Large Bundles

For large datasets, Aether automatically splits bundles before sending to DIMP:

```yaml
services:
  dimp:
    url: "http://your-dimp-server:32861"
    bundle_split_threshold_mb: 10   # Split bundles larger than 10MB
```

