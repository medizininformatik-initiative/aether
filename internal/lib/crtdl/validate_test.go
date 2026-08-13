package crtdl

import (
	"os"
	"path/filepath"
	"testing"
)

// validCohortDefinition is the smallest cohortDefinition the CCDL schema
// accepts: a version and one inclusion criterion.
const validCohortDefinition = `{
    "version": "http://to_be_decided.com/draft-1/schema#",
    "inclusionCriteria": [
      [
        {
          "termCodes": [
            {"code": "263495000", "system": "http://snomed.info/sct", "display": "Geschlecht"}
          ],
          "context": {"code": "Patient", "system": "fdpg.mii.cds", "display": "Patient"}
        }
      ]
    ]
  }`

const validCRTDL = `{
  "version": "http://json-schema.org/to-be-done/schema#",
  "display": "",
  "cohortDefinition": ` + validCohortDefinition + `,
  "dataExtraction": {
    "attributeGroups": [
      {
        "id": "diagnosis",
        "groupReference": "https://example.org/StructureDefinition/Diagnose",
        "attributes": [
          {"attributeRef": "Condition.code", "mustHave": true}
        ],
        "filter": [
          {"type": "date", "name": "recorded-date", "start": "2021-05-01", "end": "2021-10-09"}
        ]
      }
    ]
  }
}`

func TestValidateValidDocument(t *testing.T) {
	result := Validate([]byte(validCRTDL))

	if !result.Valid() {
		t.Error("valid document reported as invalid")
	}
	for _, f := range result.Findings {
		t.Errorf("unexpected finding: %+v", f)
	}
}

// TestTestdata pins the verdicts for the upstream example files. The three
// upstream examples nest dataExtraction inside cohortDefinition and contradict
// the upstream schema; the corrected copy must validate without findings.
func TestTestdata(t *testing.T) {
	cases := map[string]bool{
		"CRTDL_diagnosis.json":                           false,
		"CRTDL_observation.json":                         false,
		"CRTDL_Diagnosis_linked_with_Encounter.json":     false,
		"diagnosis_linked_with_encounter_corrected.json": true,
	}
	for file, wantValid := range cases {
		t.Run(file, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", file))
			if err != nil {
				t.Fatal(err)
			}
			result := Validate(data)
			if result.Valid() != wantValid {
				t.Errorf("Valid() = %v, want %v; findings: %+v",
					result.Valid(), wantValid, result.Findings)
			}
		})
	}
}

func TestValidateCohortDefinitionAgainstCCDLSchema(t *testing.T) {
	// An empty cohortDefinition misses version and inclusionCriteria.
	doc := `{
  "version": "https://example.org/crtdl",
  "cohortDefinition": {},
  "dataExtraction": {
    "attributeGroups": [
      {
        "id": "diagnosis",
        "groupReference": "https://example.org/StructureDefinition/Diagnose",
        "attributes": [{"attributeRef": "Condition.code", "mustHave": true}]
      }
    ]
  }
}`
	result := Validate([]byte(doc))

	if result.Valid() {
		t.Error("empty cohortDefinition must not be valid")
	}
	for _, f := range result.Findings {
		if f.Code != "schema-violation" {
			t.Errorf("got code %q, want %q", f.Code, "schema-violation")
		}
	}
}

func TestValidateSchemaViolation(t *testing.T) {
	// The top level requires version, cohortDefinition, and dataExtraction.
	result := Validate([]byte(`{}`))

	if result.Valid() {
		t.Error("empty object must not be valid")
	}
	if len(result.Findings) == 0 {
		t.Fatal("got no findings, want schema violations")
	}
	for _, f := range result.Findings {
		if f.Code != "schema-violation" {
			t.Errorf("got code %q, want %q", f.Code, "schema-violation")
		}
	}
}

func TestValidateMalformedJSON(t *testing.T) {
	result := Validate([]byte("{"))

	if result.Valid() {
		t.Error("malformed JSON must not be valid")
	}
	if len(result.Findings) != 1 {
		t.Fatalf("got %d findings, want 1", len(result.Findings))
	}
	if result.Findings[0].Code != "malformed-json" {
		t.Errorf("got code %q, want %q", result.Findings[0].Code, "malformed-json")
	}
}
