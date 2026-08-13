package crtdl

import "testing"

func TestDateFilterEndBeforeStart(t *testing.T) {
	doc := `{
  "version": "https://example.org/crtdl",
  "cohortDefinition": ` + validCohortDefinition + `,
  "dataExtraction": {
    "attributeGroups": [
      {
        "id": "diagnosis",
        "groupReference": "https://example.org/StructureDefinition/Diagnose",
        "attributes": [{"attributeRef": "Condition.code", "mustHave": true}],
        "filter": [
          {"type": "date", "name": "recorded-date", "start": "2021-10-09", "end": "2021-05-01"}
        ]
      }
    ]
  }
}`
	result := Validate([]byte(doc))

	if result.Valid() {
		t.Error("end before start must not be valid")
	}
	if len(result.Findings) != 1 {
		t.Fatalf("got %d findings, want 1: %+v", len(result.Findings), result.Findings)
	}
	f := result.Findings[0]
	if f.Code != "filter-date-order" {
		t.Errorf("got code %q, want %q", f.Code, "filter-date-order")
	}
	if f.Group != "diagnosis" {
		t.Errorf("got group %q, want %q", f.Group, "diagnosis")
	}
}

func TestDuplicateGroupID(t *testing.T) {
	doc := `{
  "version": "https://example.org/crtdl",
  "cohortDefinition": ` + validCohortDefinition + `,
  "dataExtraction": {
    "attributeGroups": [
      {
        "id": "diagnosis",
        "groupReference": "https://example.org/StructureDefinition/Diagnose",
        "attributes": [{"attributeRef": "Condition.code", "mustHave": true}]
      },
      {
        "id": "diagnosis",
        "groupReference": "https://example.org/StructureDefinition/Encounter",
        "attributes": [{"attributeRef": "Encounter.status", "mustHave": true}]
      }
    ]
  }
}`
	result := Validate([]byte(doc))

	if result.Valid() {
		t.Error("duplicate group id must not be valid")
	}
	if len(result.Findings) != 1 {
		t.Fatalf("got %d findings, want 1: %+v", len(result.Findings), result.Findings)
	}
	f := result.Findings[0]
	if f.Code != "duplicate-group-id" {
		t.Errorf("got code %q, want %q", f.Code, "duplicate-group-id")
	}
	if f.Group != "diagnosis" {
		t.Errorf("got group %q, want %q", f.Group, "diagnosis")
	}
}

func TestLinkedGroupNotFound(t *testing.T) {
	doc := `{
  "version": "https://example.org/crtdl",
  "cohortDefinition": ` + validCohortDefinition + `,
  "dataExtraction": {
    "attributeGroups": [
      {
        "id": "diagnosis",
        "groupReference": "https://example.org/StructureDefinition/Diagnose",
        "attributes": [
          {"attributeRef": "Condition.encounter", "mustHave": false, "linkedGroups": ["encounter"]}
        ]
      }
    ]
  }
}`
	result := Validate([]byte(doc))

	if result.Valid() {
		t.Error("unresolved linked group must not be valid")
	}
	if len(result.Findings) != 1 {
		t.Fatalf("got %d findings, want 1: %+v", len(result.Findings), result.Findings)
	}
	f := result.Findings[0]
	if f.Code != "linked-group-not-found" {
		t.Errorf("got code %q, want %q", f.Code, "linked-group-not-found")
	}
	if f.Group != "diagnosis" {
		t.Errorf("got group %q, want %q", f.Group, "diagnosis")
	}
}
