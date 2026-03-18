package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/pipeline"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

// makeProvenanceMultiTarget builds a Provenance resource targeting multiple resources
func makeProvenanceMultiTarget(id string, targetRefs []string, groupID string) map[string]any {
	targets := make([]any, len(targetRefs))
	for i, ref := range targetRefs {
		targets[i] = map[string]any{"reference": ref}
	}
	return map[string]any{
		"resourceType": "Provenance",
		"id":           id,
		"target":       targets,
		"entity": []any{
			map[string]any{
				"role": "source",
				"what": map[string]any{
					"identifier": map[string]any{
						"system": "https://www.medizininformatik-initiative.de/fhir/fdpg/NamingSystem/attribute_group",
						"value":  groupID,
					},
				},
			},
		},
	}
}

// TestMultiGroupSameResourceType_ProvenanceRouting tests that when the same resource type
// (Procedure) appears in two different CRTDL attribute groups, both groups receive their
// resources via Provenance-based routing.
//
// Regression test for: two attribute groups with same groupReference URL but different
// group IDs both target the same resources via separate Provenance entries.
func TestMultiGroupSameResourceType_ProvenanceRouting(t *testing.T) {
	tmpDir := t.TempDir()
	logger := lib.NewLogger(lib.LogLevelDebug)

	proc1 := map[string]any{
		"resourceType": "Procedure",
		"id":           "prozedur-1",
		"status":       "completed",
		"code": map[string]any{
			"coding": []any{
				map[string]any{"system": "http://fhir.de/CodeSystem/bfarm/ops", "code": "5-323.51"},
			},
		},
		"subject": map[string]any{"reference": "Patient/patient-1"},
	}
	proc2 := map[string]any{
		"resourceType": "Procedure",
		"id":           "prozedur-2",
		"status":       "completed",
		"code": map[string]any{
			"coding": []any{
				map[string]any{"system": "http://fhir.de/CodeSystem/bfarm/ops", "code": "6-00g.33"},
			},
		},
		"subject": map[string]any{"reference": "Patient/patient-1"},
	}

	groupIDOne := "group-proc-one"
	groupIDTwo := "group-proc-two"

	// Two Provenance resources targeting the SAME Procedures but with DIFFERENT group IDs
	provOne := makeProvenanceMultiTarget("prov-one",
		[]string{"Procedure/prozedur-1", "Procedure/prozedur-2"},
		groupIDOne,
	)
	provTwo := makeProvenanceMultiTarget("prov-two",
		[]string{"Procedure/prozedur-1", "Procedure/prozedur-2"},
		groupIDTwo,
	)

	bundle := makeBundle("test-bundle", proc1, proc2, provOne, provTwo)

	filePath := filepath.Join(tmpDir, "fhir-data.ndjson")
	data, err := json.Marshal(bundle)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filePath, append(data, '\n'), 0644))

	resources, index, err := pipeline.LoadResourcesFromFile(filePath, logger, lib.DefaultMaxNDJSONLineSize)
	require.NoError(t, err)

	assert.Len(t, resources, 2, "expected 2 Procedure resources (Provenance filtered out)")

	groupOneMatches := pipeline.FilterResourcesByProvenance(resources, index, groupIDOne)
	groupTwoMatches := pipeline.FilterResourcesByProvenance(resources, index, groupIDTwo)

	assert.Len(t, groupOneMatches, 2,
		"Procedure group one should match both Procedures")
	assert.Len(t, groupTwoMatches, 2,
		"Procedure group two should match both Procedures")
}

// TestMultiGroupSameResourceType_WithTorchOutput tests with actual torch output containing
// Provenance resources that route the same Procedures to two different CRTDL attribute groups.
// Uses real NDJSON bundle and CRTDL from a reported bug where one Procedure group had no matches.
func TestMultiGroupSameResourceType_WithTorchOutput(t *testing.T) {
	ndjsonPath := filepath.Join("..", "..", ".github", "test", "b4a838f8-55e9-4d4b-8551-e530fc7d9f3e.ndjson")
	crtdlPath := filepath.Join("..", "..", ".github", "test", "example-crtdl-multi-procedure.json")

	logger := lib.NewLogger(lib.LogLevelDebug)

	resources, index, err := pipeline.LoadResourcesFromFile(ndjsonPath, logger, lib.DefaultMaxNDJSONLineSize)
	require.NoError(t, err)

	// Torch output: 2 Conditions, 2 Encounters, 1 Patient, 2 Procedures (+ 5 Provenances excluded)
	assert.Len(t, resources, 7, "expected 7 clinical resources (2 Condition + 2 Encounter + 1 Patient + 2 Procedure)")

	crtdl, err := services.ParseCRTDL(crtdlPath)
	require.NoError(t, err)

	groups := services.GetAttributeGroups(crtdl)
	require.Len(t, groups, 4, "CRTDL should have 4 attribute groups")

	t.Logf("Provenance index (%d entries):", len(index))
	for ref, gid := range index {
		t.Logf("  %s -> %s", ref, gid)
	}

	// Patient group
	patientMatches := pipeline.FilterResourcesByProvenance(resources, index, "42b72307-d33b-4fa1-95c8-e01390a9ec8a")
	assert.Len(t, patientMatches, 1, "Patient group should match 1 resource")

	// Diagnosis group
	diagnosisMatches := pipeline.FilterResourcesByProvenance(resources, index, "d20a1423-4188-4c3f-98c8-725b0f09746b")
	assert.Len(t, diagnosisMatches, 2, "Diagnosis group should match 2 resources")

	// Procedure group one (d49d96a5) — both Procedures
	procOneMatches := pipeline.FilterResourcesByProvenance(resources, index, "d49d96a5-53af-4069-862b-2ff804eac084")
	assert.Len(t, procOneMatches, 2, "Procedure one group should match 2 Procedures")

	// Procedure group two (bobd96a5) — both Procedures
	procTwoMatches := pipeline.FilterResourcesByProvenance(resources, index, "bobd96a5-53af-4069-862b-2ff804eac084")
	assert.Len(t, procTwoMatches, 2, "Procedure two group should match 2 Procedures")
}

// TestMultiGroupSameResourceType_WithTesterCRTDL uses an inline copy of the tester's CRTDL
// with two Procedure attribute groups sharing the same groupReference URL, ensuring
// the test is self-contained and doesn't depend on external files.
func TestMultiGroupSameResourceType_WithTesterCRTDL(t *testing.T) {
	tmpDir := t.TempDir()
	logger := lib.NewLogger(lib.LogLevelDebug)

	crtdlContent := `{
		"display": "",
		"version": "http://json-schema.org/to-be-done/schema#",
		"dataExtraction": {
			"attributeGroups": [
				{
					"id": "42b72307-d33b-4fa1-95c8-e01390a9ec8a",
					"groupReference": "https://www.medizininformatik-initiative.de/fhir/core/modul-person/StructureDefinition/PatientPseudonymisiert",
					"name": "MII PR Person Patient (Pseudonymisiert)",
					"attributes": [
						{"attributeRef": "Patient.gender", "mustHave": false},
						{"attributeRef": "Patient.birthDate", "mustHave": false}
					]
				},
				{
					"id": "d20a1423-4188-4c3f-98c8-725b0f09746b",
					"groupReference": "https://www.medizininformatik-initiative.de/fhir/core/modul-diagnose/StructureDefinition/Diagnose",
					"name": "Diagnosis",
					"attributes": [
						{"attributeRef": "Condition.code", "mustHave": false},
						{"attributeRef": "Condition.recordedDate", "mustHave": false}
					]
				},
				{
					"id": "d49d96a5-53af-4069-862b-2ff804eac084",
					"groupReference": "https://www.medizininformatik-initiative.de/fhir/core/modul-prozedur/StructureDefinition/Procedure",
					"name": "Procedure one",
					"attributes": [
						{"attributeRef": "Procedure.code", "mustHave": false},
						{"attributeRef": "Procedure.status", "mustHave": false},
						{"attributeRef": "Procedure.performed[x]", "mustHave": false},
						{"attributeRef": "Procedure.extension:Dokumentationsdatum", "mustHave": false}
					]
				},
				{
					"id": "bobd96a5-53af-4069-862b-2ff804eac084",
					"groupReference": "https://www.medizininformatik-initiative.de/fhir/core/modul-prozedur/StructureDefinition/Procedure",
					"name": "Procedure two",
					"attributes": [
						{"attributeRef": "Procedure.code", "mustHave": false},
						{"attributeRef": "Procedure.status", "mustHave": false}
					]
				}
			]
		}
	}`
	crtdlPath := filepath.Join(tmpDir, "example-crtdl.json")
	require.NoError(t, os.WriteFile(crtdlPath, []byte(crtdlContent), 0644))

	patient := map[string]any{
		"resourceType": "Patient", "id": "patient-1",
		"gender": "male", "birthDate": "1977-05-24",
	}
	cond1 := map[string]any{
		"resourceType": "Condition", "id": "diagnose-1",
		"code": map[string]any{"coding": []any{
			map[string]any{"system": "http://fhir.de/CodeSystem/bfarm/icd-10-gm", "code": "B05.3"},
		}},
		"subject": map[string]any{"reference": "Patient/patient-1"},
	}
	cond2 := map[string]any{
		"resourceType": "Condition", "id": "diagnose-2",
		"code": map[string]any{"coding": []any{
			map[string]any{"system": "http://fhir.de/CodeSystem/bfarm/icd-10-gm", "code": "H67.1"},
		}},
		"subject": map[string]any{"reference": "Patient/patient-1"},
	}
	proc1 := map[string]any{
		"resourceType": "Procedure", "id": "prozedur-1",
		"status": "completed",
		"code": map[string]any{"coding": []any{
			map[string]any{"system": "http://fhir.de/CodeSystem/bfarm/ops", "code": "5-323.51"},
		}},
		"subject": map[string]any{"reference": "Patient/patient-1"},
	}
	proc2 := map[string]any{
		"resourceType": "Procedure", "id": "prozedur-2",
		"status": "completed",
		"code": map[string]any{"coding": []any{
			map[string]any{"system": "http://fhir.de/CodeSystem/bfarm/ops", "code": "6-00g.33"},
		}},
		"subject": map[string]any{"reference": "Patient/patient-1"},
	}

	provPatient := makeProvenanceMultiTarget("prov-patient",
		[]string{"Patient/patient-1"},
		"42b72307-d33b-4fa1-95c8-e01390a9ec8a",
	)
	provConditions := makeProvenanceMultiTarget("prov-conditions",
		[]string{"Condition/diagnose-1", "Condition/diagnose-2"},
		"d20a1423-4188-4c3f-98c8-725b0f09746b",
	)
	provProcGroupOne := makeProvenanceMultiTarget("prov-proc-one",
		[]string{"Procedure/prozedur-1", "Procedure/prozedur-2"},
		"d49d96a5-53af-4069-862b-2ff804eac084",
	)
	provProcGroupTwo := makeProvenanceMultiTarget("prov-proc-two",
		[]string{"Procedure/prozedur-1", "Procedure/prozedur-2"},
		"bobd96a5-53af-4069-862b-2ff804eac084",
	)

	bundle := makeBundle("test-bundle",
		patient, cond1, cond2, proc1, proc2,
		provPatient, provConditions, provProcGroupOne, provProcGroupTwo,
	)

	filePath := filepath.Join(tmpDir, "fhir-data.ndjson")
	bundleData, err := json.Marshal(bundle)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filePath, append(bundleData, '\n'), 0644))

	resources, index, err := pipeline.LoadResourcesFromFile(filePath, logger, lib.DefaultMaxNDJSONLineSize)
	require.NoError(t, err)

	assert.Len(t, resources, 5, "expected 5 clinical resources")

	crtdl, err := services.ParseCRTDL(crtdlPath)
	require.NoError(t, err)
	groups := services.GetAttributeGroups(crtdl)
	require.Len(t, groups, 4, "CRTDL should have 4 attribute groups")

	t.Logf("Provenance index (%d entries):", len(index))
	for ref, gid := range index {
		t.Logf("  %s -> %s", ref, gid)
	}

	patientMatches := pipeline.FilterResourcesByProvenance(resources, index, "42b72307-d33b-4fa1-95c8-e01390a9ec8a")
	assert.Len(t, patientMatches, 1, "Patient group should match 1 resource")

	diagnosisMatches := pipeline.FilterResourcesByProvenance(resources, index, "d20a1423-4188-4c3f-98c8-725b0f09746b")
	assert.Len(t, diagnosisMatches, 2, "Diagnosis group should match 2 resources")

	procOneMatches := pipeline.FilterResourcesByProvenance(resources, index, "d49d96a5-53af-4069-862b-2ff804eac084")
	assert.Len(t, procOneMatches, 2, "Procedure one group should match 2 Procedures")

	procTwoMatches := pipeline.FilterResourcesByProvenance(resources, index, "bobd96a5-53af-4069-862b-2ff804eac084")
	assert.Len(t, procTwoMatches, 2, "Procedure two group should match 2 Procedures")
}
