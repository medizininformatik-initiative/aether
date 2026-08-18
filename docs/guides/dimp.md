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

## Experimental v3 Endpoint

The FHIR-Pseudonymizer has an experimental `v3alpha1` endpoint that receives the anonymization configuration with each request. With this endpoint, you can change the anonymization rules without a restart of the service.

This endpoint needs FHIR-Pseudonymizer v2.31.0 or later.

```yaml
services:
  dimp:
    url: "http://your-dimp-server:32861"
    experimental_v3:
      anonymization_config: "/path/to/anonymization.yaml"
```

The `anonymization_config` path selects the endpoint. If you give a path, Aether
uses the `v3alpha1` endpoint. If you give no path, Aether uses the default
endpoint, and the service reads its anonymization configuration from its own
file.

Aether reads the anonymization YAML at the start of the dimp step and sends it with each request. The upstream endpoint is experimental and can change.

## Key Derivation

The `cryptoHash` method of the FHIR-Pseudonymizer needs a key. A different key
for each project keeps the pseudonyms of the projects separate. But then you
must generate and store one secret for each project.

The FHIR-Pseudonymizer can instead derive the key of each project from one
master key. It uses HKDF (RFC 5869) with a derivation context. Key derivation
needs FHIR-Pseudonymizer v2.30.0 or later.

Generate one master key for all projects:

```bash
openssl rand -hex 32
```

Set this master key on the FHIR-Pseudonymizer server:

```yaml
# compose.yaml of the FHIR-Pseudonymizer
environment:
  Anonymization__CryptoHashKey: "<master key>"
```

Then give each project a different context in the anonymization YAML:

```yaml
fhirVersion: R4
fhirPathRules:
  - path: Resource.id
    method: cryptoHash
parameters:
  keyDerivationContext: "project-a"
```

The FHIR-Pseudonymizer derives the hash key from the master key and the
context. The same master key with the same context always gives the same key.
Two different contexts give two independent keys. Thus one master key is
sufficient for many projects.

Keep the master key on the server. Do not set `parameters.cryptoHashKey` in the
anonymization YAML. A key in `parameters` has precedence over
`Anonymization__CryptoHashKey`, and the secret then moves with each request to
the service.

You can also set a context on the server with
`Anonymization__KeyDerivationContext`. A `parameters.keyDerivationContext` in
the YAML has precedence over it. With the experimental v3 endpoint, Aether
sends the YAML with each request. Thus the anonymization YAML of the project
selects the key, and the server keeps only the master key.

The pseudonyms change if the master key or the context changes. Data that you
send after a change does not agree with data from before the change. Use a
stable context for each project.

The `encrypt` method uses the same mechanism for `encryptKey`. The
FHIR-Pseudonymizer derives the hash key and the encryption key independently of
each other.

## Large Bundles

For large datasets, Aether automatically splits bundles before sending to DIMP:

```yaml
services:
  dimp:
    url: "http://your-dimp-server:32861"
    bundle_split_threshold_mb: 10   # Split bundles larger than 10MB
```

