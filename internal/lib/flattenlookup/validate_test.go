package flattenlookup_test

import (
	"os"
	"path/filepath"
	"testing"

	flattenlookup "github.com/medizininformatik-initiative/aether/internal/lib/flattenlookup"
)

// TestRealLookupFilesHaveNoErrors validates real lookup files from aether.
// Warnings are allowed, errors are not. Both files are clean because the
// parent link is derived from the children lists: a single stored direction
// cannot disagree with itself.
func TestRealLookupFilesHaveNoErrors(t *testing.T) {
	for _, name := range []string{"aether-example-flattening.json", "aether-torch.json"} {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", name))
			if err != nil {
				t.Fatal(err)
			}
			result := flattenlookup.Validate(data)
			if !result.Valid() {
				t.Fatalf("expected no errors, got: %v", result.Findings)
			}
			t.Logf("findings: %v", result.Findings)
		})
	}
}

const validLookup = `[
  {
    "url": "https://example.org/StructureDefinition/Patient",
    "resourceType": "Patient",
    "elements": {
      "Patient.name": {
        "children": ["Patient.name.family"],
        "viewDefinition": {"forEachOrNull": "name", "select": []}
      },
      "Patient.name.family": {
        "viewDefinition": {
          "select": [
            {"column": [{"name": "family", "path": "family", "type": "string"}]}
          ]
        }
      }
    }
  }
]`

func TestMalformedJSONIsAnError(t *testing.T) {
	result := flattenlookup.Validate([]byte(`{not json`))

	if result.Valid() {
		t.Fatal("expected invalid result for malformed JSON")
	}
	if len(result.Findings) != 1 {
		t.Fatalf("expected one finding, got: %v", result.Findings)
	}
	f := result.Findings[0]
	if f.Severity != flattenlookup.SeverityError || f.Code != "malformed-json" {
		t.Fatalf("expected malformed-json error, got: %+v", f)
	}
}

func TestMissingURLIsASchemaError(t *testing.T) {
	result := flattenlookup.Validate([]byte(`[
	  {"resourceType": "Patient", "elements": {}}
	]`))

	if result.Valid() {
		t.Fatal("expected invalid result for a table without url")
	}
	found := false
	for _, f := range result.Findings {
		if f.Severity == flattenlookup.SeverityError && f.Code == "schema-violation" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a schema-violation error, got: %v", result.Findings)
	}
}

func TestUnresolvedChildIsAWarning(t *testing.T) {
	result := flattenlookup.Validate([]byte(`[
	  {
	    "url": "https://example.org/StructureDefinition/Patient",
	    "resourceType": "Patient",
	    "elements": {
	      "Patient.name": {
	        "children": ["Patient.name.family"],
	        "viewDefinition": {"forEachOrNull": "name", "select": []}
	      }
	    }
	  }
	]`))

	if !result.Valid() {
		t.Fatalf("an unresolved child must not make the file invalid, got: %v", result.Findings)
	}
	if len(result.Findings) != 1 {
		t.Fatalf("expected one finding, got: %v", result.Findings)
	}
	f := result.Findings[0]
	if f.Code != "unresolved-child" || f.Severity != flattenlookup.SeverityWarning {
		t.Fatalf("expected unresolved-child warning, got: %+v", f)
	}
	if f.Table != "https://example.org/StructureDefinition/Patient" || f.Element != "Patient.name" {
		t.Fatalf("expected table and element set, got: %+v", f)
	}
}

// storedParentLookup carries the deprecated parent field, in agreement with the
// parent that the children list of Patient.name derives.
const storedParentLookup = `[
  {
    "url": "https://example.org/StructureDefinition/Patient",
    "resourceType": "Patient",
    "elements": {
      "Patient.name": {
        "children": ["Patient.name.family"],
        "viewDefinition": {"forEachOrNull": "name", "select": []}
      },
      "Patient.name.family": {
        "parent": "Patient.name",
        "viewDefinition": {"select": []}
      }
    }
  }
]`

// TestStoredParentIsSilentByDefault pins the migration path: the parent link is
// derived from children, so a stored parent is redundant but still accepted.
func TestStoredParentIsSilentByDefault(t *testing.T) {
	result := flattenlookup.Validate([]byte(storedParentLookup))

	if len(result.Findings) != 0 {
		t.Fatalf("expected no findings without verbose mode, got: %v", result.Findings)
	}
}

func TestStoredParentIsDeprecatedInVerboseMode(t *testing.T) {
	result := flattenlookup.Validate([]byte(storedParentLookup), flattenlookup.Verbose())

	if !result.Valid() {
		t.Fatalf("a stored parent must not make the file invalid, got: %v", result.Findings)
	}
	if len(result.Findings) != 1 {
		t.Fatalf("expected one finding, got: %v", result.Findings)
	}
	f := result.Findings[0]
	if f.Code != "deprecated-parent" || f.Severity != flattenlookup.SeverityWarning {
		t.Fatalf("expected deprecated-parent warning, got: %+v", f)
	}
	if f.Table != "https://example.org/StructureDefinition/Patient" || f.Element != "Patient.name.family" {
		t.Fatalf("expected table and element set, got: %+v", f)
	}
}

// TestStoredParentThatDisagreesIsAWarning covers the defect that the deprecated
// field can still carry: the stored link and the derived link name different
// elements. This is a defect, not a note about style, so verbose mode is not
// necessary for it.
func TestStoredParentThatDisagreesIsAWarning(t *testing.T) {
	result := flattenlookup.Validate([]byte(`[
	  {
	    "url": "https://example.org/StructureDefinition/Patient",
	    "resourceType": "Patient",
	    "elements": {
	      "Patient.name": {
	        "children": ["Patient.name.family"],
	        "viewDefinition": {"forEachOrNull": "name", "select": []}
	      },
	      "Patient.contact": {
	        "viewDefinition": {"forEachOrNull": "contact", "select": []}
	      },
	      "Patient.name.family": {
	        "parent": "Patient.contact",
	        "viewDefinition": {"select": []}
	      }
	    }
	  }
	]`))

	if !result.Valid() {
		t.Fatalf("a stored parent must not make the file invalid, got: %v", result.Findings)
	}
	if len(result.Findings) != 1 {
		t.Fatalf("expected one finding, got: %v", result.Findings)
	}
	f := result.Findings[0]
	if f.Code != "parent-mismatch" || f.Severity != flattenlookup.SeverityWarning {
		t.Fatalf("expected parent-mismatch warning, got: %+v", f)
	}
	if f.Element != "Patient.name.family" {
		t.Fatalf("expected element set to the child, got: %+v", f)
	}
}

func TestChildCycleIsAnError(t *testing.T) {
	result := flattenlookup.Validate([]byte(`[
	  {
	    "url": "https://example.org/StructureDefinition/Patient",
	    "resourceType": "Patient",
	    "elements": {
	      "Patient.a": {
	        "children": ["Patient.b"],
	        "viewDefinition": {"select": []}
	      },
	      "Patient.b": {
	        "children": ["Patient.a"],
	        "viewDefinition": {"select": []}
	      }
	    }
	  }
	]`))

	if result.Valid() {
		t.Fatal("expected invalid result for a cycle in the children graph")
	}
	var cycles []flattenlookup.Finding
	for _, f := range result.Findings {
		if f.Code == "child-cycle" {
			cycles = append(cycles, f)
		}
	}
	if len(cycles) != 1 {
		t.Fatalf("expected one child-cycle finding, got: %v", result.Findings)
	}
	if cycles[0].Severity != flattenlookup.SeverityError {
		t.Fatalf("expected child-cycle error, got: %+v", cycles[0])
	}
}

// TestChildListedByTwoParentsIsAnError covers the risk the derived parent link
// introduces: with only the children direction stored, an element that two
// elements claim as a child has no single parent.
func TestChildListedByTwoParentsIsAnError(t *testing.T) {
	result := flattenlookup.Validate([]byte(`[
	  {
	    "url": "https://example.org/StructureDefinition/Patient",
	    "resourceType": "Patient",
	    "elements": {
	      "Patient.name": {
	        "children": ["Patient.name.family"],
	        "viewDefinition": {"forEachOrNull": "name", "select": []}
	      },
	      "Patient.contact": {
	        "children": ["Patient.name.family"],
	        "viewDefinition": {"forEachOrNull": "contact", "select": []}
	      },
	      "Patient.name.family": {
	        "viewDefinition": {"select": []}
	      }
	    }
	  }
	]`))

	if result.Valid() {
		t.Fatal("expected invalid result for an element with two parents")
	}
	if len(result.Findings) != 1 {
		t.Fatalf("expected one finding, got: %v", result.Findings)
	}
	f := result.Findings[0]
	if f.Code != "multiple-parents" || f.Severity != flattenlookup.SeverityError {
		t.Fatalf("expected multiple-parents error, got: %+v", f)
	}
	if f.Element != "Patient.name.family" {
		t.Fatalf("expected element set to the child, got: %+v", f)
	}
}

// TestAmbiguousParentSuppressesTheMismatchWarning keeps the report on one
// defect: an element that two elements claim as a child has no derived parent,
// and the deprecated field must not add a second finding about the same defect.
func TestAmbiguousParentSuppressesTheMismatchWarning(t *testing.T) {
	result := flattenlookup.Validate([]byte(`[
	  {
	    "url": "https://example.org/StructureDefinition/Patient",
	    "resourceType": "Patient",
	    "elements": {
	      "Patient.name": {
	        "children": ["Patient.name.family"],
	        "viewDefinition": {"forEachOrNull": "name", "select": []}
	      },
	      "Patient.contact": {
	        "children": ["Patient.name.family"],
	        "viewDefinition": {"forEachOrNull": "contact", "select": []}
	      },
	      "Patient.name.family": {
	        "parent": "Patient.name",
	        "viewDefinition": {"select": []}
	      }
	    }
	  }
	]`))

	if len(result.Findings) != 1 {
		t.Fatalf("expected only the multiple-parents finding, got: %v", result.Findings)
	}
	if result.Findings[0].Code != "multiple-parents" {
		t.Fatalf("expected multiple-parents error, got: %+v", result.Findings[0])
	}
}

func TestParentIDNotAPrefixOfChildIDIsAWarning(t *testing.T) {
	result := flattenlookup.Validate([]byte(`[
	  {
	    "url": "https://example.org/StructureDefinition/Patient",
	    "resourceType": "Patient",
	    "elements": {
	      "Patient.name": {
	        "children": ["Patient.telecom.value"],
	        "viewDefinition": {"forEachOrNull": "name", "select": []}
	      },
	      "Patient.telecom.value": {
	        "viewDefinition": {"select": []}
	      }
	    }
	  }
	]`))

	if !result.Valid() {
		t.Fatalf("warnings must not make the file invalid, got: %v", result.Findings)
	}
	if len(result.Findings) != 1 {
		t.Fatalf("expected one finding, got: %v", result.Findings)
	}
	f := result.Findings[0]
	if f.Code != "parent-not-prefix" || f.Severity != flattenlookup.SeverityWarning {
		t.Fatalf("expected parent-not-prefix warning, got: %+v", f)
	}
	if f.Element != "Patient.telecom.value" {
		t.Fatalf("expected element set to the child, got: %+v", f)
	}
}

func TestSlicedElementIDsFollowThePrefixConvention(t *testing.T) {
	result := flattenlookup.Validate([]byte(`[
	  {
	    "url": "https://example.org/StructureDefinition/Diagnose",
	    "resourceType": "Condition",
	    "elements": {
	      "Condition.code": {
	        "children": ["Condition.code.coding:sct"],
	        "viewDefinition": {"forEachOrNull": "code", "select": []}
	      },
	      "Condition.code.coding:sct": {
	        "viewDefinition": {"select": []}
	      }
	    }
	  }
	]`))

	if len(result.Findings) != 0 {
		t.Fatalf("expected no findings for a sliced child ID, got: %v", result.Findings)
	}
}

func TestValidFileHasNoFindings(t *testing.T) {
	result := flattenlookup.Validate([]byte(validLookup))

	if !result.Valid() {
		t.Fatalf("expected valid, got findings: %v", result.Findings)
	}
	if len(result.Findings) != 0 {
		t.Fatalf("expected no findings, got: %v", result.Findings)
	}
}
