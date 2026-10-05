package unit

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

// Test helpers for ViewDefinition builder tests

func newLookupTable(url, resourceType string, elements map[string]models.LookupElement) models.LookupTable {
	return models.LookupTable{
		URL:          url,
		ResourceType: resourceType,
		Elements:     elements,
	}
}

func newAttributeGroup(name, groupRef string, attributeRefs ...string) models.AttributeGroup {
	attrs := make([]models.Attribute, len(attributeRefs))
	for i, ref := range attributeRefs {
		attrs[i] = models.Attribute{AttributeRef: ref, MustHave: true}
	}
	return models.AttributeGroup{
		Name:           name,
		GroupReference: groupRef,
		Attributes:     attrs,
	}
}

func newSelectClause(columnName, path string) models.SelectClause {
	return models.SelectClause{
		Column: []models.ColumnDefinition{{Name: columnName, Path: path}},
	}
}

func newViewDefSnippet(selects ...models.SelectClause) models.ViewDefSnippet {
	return models.ViewDefSnippet{Select: selects}
}

func buildAndAssertViewDef(t *testing.T, lookupTables []models.LookupTable, group models.AttributeGroup) *models.ViewDefinition {
	t.Helper()
	builder := services.NewViewDefinitionBuilder(lookupTables)
	viewDef, err := builder.BuildViewDefinition(group)
	require.NoError(t, err)
	require.NotNil(t, viewDef)
	return viewDef
}

func TestBuildViewDefinition(t *testing.T) {
	t.Run("basic Patient ViewDefinition", func(t *testing.T) {
		lookupTables := []models.LookupTable{
			newLookupTable("https://example.com/Patient", "Patient", map[string]models.LookupElement{
				"Patient.birthDate": {
					ViewDefinition: newViewDefSnippet(newSelectClause("birthDate", "birthDate")),
				},
			}),
		}
		group := newAttributeGroup("Patients", "https://example.com/Patient", "Patient.birthDate")

		viewDef := buildAndAssertViewDef(t, lookupTables, group)

		assert.Equal(t, "https://sql-on-fhir.org/ig/StructureDefinition/ViewDefinition", viewDef.ResourceType)
		assert.Equal(t, "Patients", viewDef.Name)
		assert.Equal(t, "draft", viewDef.Status)
		assert.Equal(t, "Patient", viewDef.Resource)
		require.NotEmpty(t, viewDef.Select)
	})

	t.Run("patient compartment resource includes patient column with correct path", func(t *testing.T) {
		lookupTables := []models.LookupTable{
			newLookupTable("https://example.com/Condition", "Condition", map[string]models.LookupElement{
				"Condition.code": {
					ViewDefinition: newViewDefSnippet(newSelectClause("code", "code.coding[0].code")),
				},
			}),
		}
		group := newAttributeGroup("Conditions", "https://example.com/Condition", "Condition.code")

		viewDef := buildAndAssertViewDef(t, lookupTables, group)

		assert.Equal(t, "Condition", viewDef.Resource)
		require.NotEmpty(t, viewDef.Select)
		assert.Len(t, viewDef.Select[0].Column, 2) // id and patient
		assert.Equal(t, "patient", viewDef.Select[0].Column[1].Name)
		assert.Equal(t, "subject.reference", viewDef.Select[0].Column[1].Path)
	})

	t.Run("patient compartment resource with patient.reference path", func(t *testing.T) {
		lookupTables := []models.LookupTable{
			newLookupTable("https://example.com/AllergyIntolerance", "AllergyIntolerance", map[string]models.LookupElement{
				"AllergyIntolerance.code": {
					ViewDefinition: newViewDefSnippet(newSelectClause("code", "code.coding[0].code")),
				},
			}),
		}
		group := newAttributeGroup("Allergies", "https://example.com/AllergyIntolerance", "AllergyIntolerance.code")

		viewDef := buildAndAssertViewDef(t, lookupTables, group)

		assert.Equal(t, "AllergyIntolerance", viewDef.Resource)
		require.NotEmpty(t, viewDef.Select)
		assert.Len(t, viewDef.Select[0].Column, 2) // id and patient
		assert.Equal(t, "patient", viewDef.Select[0].Column[1].Name)
		assert.Equal(t, "patient.reference", viewDef.Select[0].Column[1].Path)
	})

	t.Run("non-compartment resource does not include patient column", func(t *testing.T) {
		lookupTables := []models.LookupTable{
			newLookupTable("https://example.com/Organization", "Organization", map[string]models.LookupElement{
				"Organization.name": {
					ViewDefinition: newViewDefSnippet(newSelectClause("name", "name")),
				},
			}),
		}
		group := newAttributeGroup("Organizations", "https://example.com/Organization", "Organization.name")

		viewDef := buildAndAssertViewDef(t, lookupTables, group)

		assert.Equal(t, "Organization", viewDef.Resource)
		require.NotEmpty(t, viewDef.Select)
		assert.Len(t, viewDef.Select[0].Column, 1) // only id, no patient
		assert.Equal(t, "id", viewDef.Select[0].Column[0].Name)
	})

	t.Run("missing lookup profile", func(t *testing.T) {
		group := newAttributeGroup("Patients", "https://example.com/Patient", "Patient.id")

		builder := services.NewViewDefinitionBuilder([]models.LookupTable{})
		_, err := builder.BuildViewDefinition(group)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "no lookup table found")
	})

	t.Run("missing element in lookup skips gracefully", func(t *testing.T) {
		lookupTables := []models.LookupTable{
			newLookupTable("https://example.com/Patient", "Patient", map[string]models.LookupElement{}),
		}
		group := newAttributeGroup("Patients", "https://example.com/Patient", "Patient.unknown")

		viewDef := buildAndAssertViewDef(t, lookupTables, group)
		require.NotNil(t, viewDef)
	})
}

func TestExtractColumnNames(t *testing.T) {
	t.Run("simple columns", func(t *testing.T) {
		viewDef := models.ViewDefinition{
			Select: []models.SelectClause{
				{Column: []models.ColumnDefinition{{Name: "id"}, {Name: "name"}}},
				{Column: []models.ColumnDefinition{{Name: "birthDate"}}},
			},
		}
		assert.Equal(t, []string{"id", "name", "birthDate"}, services.ExtractColumnNames(viewDef))
	})

	t.Run("nested selects", func(t *testing.T) {
		viewDef := models.ViewDefinition{
			Select: []models.SelectClause{
				{Column: []models.ColumnDefinition{{Name: "id"}}},
				{
					ForEach: "name",
					Select: []models.SelectClause{
						{Column: []models.ColumnDefinition{{Name: "family"}, {Name: "given"}}},
					},
				},
			},
		}
		assert.Equal(t, []string{"id", "family", "given"}, services.ExtractColumnNames(viewDef))
	})

	t.Run("unionAll columns appear once at the position of the clause", func(t *testing.T) {
		viewDef := models.ViewDefinition{
			Select: []models.SelectClause{
				{Column: []models.ColumnDefinition{{Name: "id"}}},
				{
					Column: []models.ColumnDefinition{{Name: "own"}},
					Select: []models.SelectClause{{Column: []models.ColumnDefinition{{Name: "nested"}}}},
					UnionAll: []models.SelectClause{
						{Select: []models.SelectClause{
							{Column: []models.ColumnDefinition{{Name: "a"}}},
							{Column: []models.ColumnDefinition{{Name: "b"}}},
						}},
						{Select: []models.SelectClause{
							{Column: []models.ColumnDefinition{{Name: "a"}}},
							{Column: []models.ColumnDefinition{{Name: "b"}}},
						}},
					},
				},
				{Column: []models.ColumnDefinition{{Name: "last"}}},
			},
		}
		assert.Equal(t, []string{"id", "own", "nested", "a", "b", "last"}, services.ExtractColumnNames(viewDef))
	})

	t.Run("empty viewDefinition", func(t *testing.T) {
		viewDef := models.ViewDefinition{Select: []models.SelectClause{}}
		assert.Empty(t, services.ExtractColumnNames(viewDef))
	})
}

func TestValidateViewDefinition(t *testing.T) {
	validSelect := []models.SelectClause{{Column: []models.ColumnDefinition{{Name: "id"}}}}

	t.Run("valid viewDefinition", func(t *testing.T) {
		viewDef := &models.ViewDefinition{
			ResourceType: "https://sql-on-fhir.org/ig/StructureDefinition/ViewDefinition",
			Name:         "TestView",
			Status:       "draft",
			Resource:     "Patient",
			Select:       validSelect,
		}
		assert.NoError(t, services.ValidateViewDefinition(viewDef))
	})

	t.Run("nil viewDefinition", func(t *testing.T) {
		err := services.ValidateViewDefinition(nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "nil")
	})

	t.Run("missing name", func(t *testing.T) {
		viewDef := &models.ViewDefinition{Resource: "Patient", Select: validSelect}
		err := services.ValidateViewDefinition(viewDef)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "name is required")
	})

	t.Run("missing resource", func(t *testing.T) {
		viewDef := &models.ViewDefinition{Name: "TestView", Select: validSelect}
		err := services.ValidateViewDefinition(viewDef)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "resource is required")
	})

	t.Run("empty select", func(t *testing.T) {
		viewDef := &models.ViewDefinition{Name: "TestView", Resource: "Patient", Select: []models.SelectClause{}}
		err := services.ValidateViewDefinition(viewDef)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "at least one select clause")
	})
}

func TestBuildViewDefinitionWithChildren(t *testing.T) {
	t.Run("element with children resolved recursively", func(t *testing.T) {
		lookupTables := []models.LookupTable{
			newLookupTable("https://example.com/Patient", "Patient", map[string]models.LookupElement{
				"Patient.name": {
					Children:       []string{"Patient.name.family", "Patient.name.given"},
					ViewDefinition: models.ViewDefSnippet{ForEach: "name", Select: []models.SelectClause{}},
				},
				"Patient.name.family": {
					Parent:         "Patient.name",
					ViewDefinition: newViewDefSnippet(newSelectClause("family", "family")),
				},
				"Patient.name.given": {
					Parent:         "Patient.name",
					ViewDefinition: newViewDefSnippet(newSelectClause("given", "given")),
				},
			}),
		}
		group := newAttributeGroup("Patients", "https://example.com/Patient", "Patient.name")

		viewDef := buildAndAssertViewDef(t, lookupTables, group)
		require.NotEmpty(t, viewDef.Select)
	})

	t.Run("element with nested forEach and children", func(t *testing.T) {
		lookupTables := []models.LookupTable{
			newLookupTable("https://example.com/Observation", "Observation", map[string]models.LookupElement{
				"Observation.component": {
					Children: []string{"Observation.component.code", "Observation.component.value"},
					ViewDefinition: models.ViewDefSnippet{
						ForEachOrNull: "component",
						Select:        []models.SelectClause{newSelectClause("componentIdx", "$index")},
					},
				},
				"Observation.component.code": {
					Parent:         "Observation.component",
					ViewDefinition: newViewDefSnippet(newSelectClause("componentCode", "code.coding[0].code")),
				},
				"Observation.component.value": {
					Parent:         "Observation.component",
					ViewDefinition: newViewDefSnippet(newSelectClause("componentValue", "valueQuantity.value")),
				},
			}),
		}
		group := newAttributeGroup("Observations", "https://example.com/Observation", "Observation.component")

		viewDef := buildAndAssertViewDef(t, lookupTables, group)
		assert.Equal(t, "Observation", viewDef.Resource)
	})

	t.Run("deeply nested children", func(t *testing.T) {
		lookupTables := []models.LookupTable{
			newLookupTable("https://example.com/Patient", "Patient", map[string]models.LookupElement{
				"Patient.address": {
					Children: []string{"Patient.address.line"},
					ViewDefinition: models.ViewDefSnippet{
						ForEach: "address",
						Select:  []models.SelectClause{newSelectClause("city", "city")},
					},
				},
				"Patient.address.line": {
					Parent:         "Patient.address",
					Children:       []string{},
					ViewDefinition: newViewDefSnippet(newSelectClause("line", "line[0]")),
				},
			}),
		}
		group := newAttributeGroup("Patients", "https://example.com/Patient", "Patient.address")

		buildAndAssertViewDef(t, lookupTables, group)
	})
}

func TestBuildAllViewDefinitions(t *testing.T) {
	lookupTables := []models.LookupTable{
		newLookupTable("https://example.com/Patient", "Patient", map[string]models.LookupElement{
			"Patient.id": {ViewDefinition: newViewDefSnippet(newSelectClause("id", "id"))},
		}),
		newLookupTable("https://example.com/Condition", "Condition", map[string]models.LookupElement{
			"Condition.code": {ViewDefinition: newViewDefSnippet(newSelectClause("code", "code.coding[0].code"))},
		}),
	}

	doc := &models.CRTDLDocument{
		DataExtraction: models.DataExtraction{
			AttributeGroups: []models.AttributeGroup{
				newAttributeGroup("Patients", "https://example.com/Patient", "Patient.id"),
				newAttributeGroup("Conditions", "https://example.com/Condition", "Condition.code"),
			},
		},
	}

	builder := services.NewViewDefinitionBuilder(lookupTables)
	viewDefs, err := builder.BuildAllViewDefinitions(doc)

	require.NoError(t, err)
	assert.Len(t, viewDefs, 2)
	assert.Contains(t, viewDefs, "Patients")
	assert.Contains(t, viewDefs, "Conditions")
	assert.Equal(t, "Patient", viewDefs["Patients"].Resource)
	assert.Equal(t, "Condition", viewDefs["Conditions"].Resource)
}

func TestBuildAllViewDefinitionsError(t *testing.T) {
	lookupTables := []models.LookupTable{
		newLookupTable("https://example.com/Patient", "Patient", map[string]models.LookupElement{
			"Patient.id": {ViewDefinition: newViewDefSnippet(newSelectClause("id", "id"))},
		}),
	}

	doc := &models.CRTDLDocument{
		DataExtraction: models.DataExtraction{
			AttributeGroups: []models.AttributeGroup{
				newAttributeGroup("Patients", "https://example.com/Patient", "Patient.id"),
				newAttributeGroup("Unknown", "https://example.com/Unknown", "Unknown.field"), // Not in lookup tables
			},
		},
	}

	builder := services.NewViewDefinitionBuilder(lookupTables)
	_, err := builder.BuildAllViewDefinitions(doc)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to build ViewDefinition")
	assert.Contains(t, err.Error(), "Unknown")
}

// TestDownwardTraversal tests the resolveWithChildren logic for various scenarios
func TestDownwardTraversal(t *testing.T) {
	t.Run("placeholder parent with empty select array returns children directly", func(t *testing.T) {
		// This tests the bug fix: when parent has children but empty Select array,
		// children should be returned directly
		lookupTables := []models.LookupTable{
			{
				URL:          "https://example.com/Condition",
				ResourceType: "Condition",
				Elements: map[string]models.LookupElement{
					"Condition.code": {
						Children: []string{"Condition.code.icd10", "Condition.code.snomed"},
						ViewDefinition: models.ViewDefSnippet{
							Select: []models.SelectClause{}, // Empty placeholder
						},
					},
					"Condition.code.icd10": {
						Parent: "Condition.code",
						ViewDefinition: models.ViewDefSnippet{
							ForEach: "code.coding.where(system='http://fhir.de/CodeSystem/bfarm/icd-10-gm')", // At viewDefinition level
							Select: []models.SelectClause{
								{Column: []models.ColumnDefinition{{Name: "icd10_code", Path: "code"}}},
							},
						},
					},
					"Condition.code.snomed": {
						Parent: "Condition.code",
						ViewDefinition: models.ViewDefSnippet{
							ForEach: "code.coding.where(system='http://snomed.info/sct')", // At viewDefinition level
							Select: []models.SelectClause{
								{Column: []models.ColumnDefinition{{Name: "snomed_code", Path: "code"}}},
							},
						},
					},
				},
			},
		}

		group := models.AttributeGroup{
			Name:           "Conditions",
			GroupReference: "https://example.com/Condition",
			Attributes: []models.Attribute{
				{AttributeRef: "Condition.code"},
			},
		}

		builder := services.NewViewDefinitionBuilder(lookupTables)
		viewDef, err := builder.BuildViewDefinition(group)

		require.NoError(t, err)
		require.NotNil(t, viewDef)

		// Should have fixed columns + both icd10 and snomed selects
		columnNames := services.ExtractColumnNames(*viewDef)
		assert.Contains(t, columnNames, "id")
		assert.Contains(t, columnNames, "patient")
		assert.Contains(t, columnNames, "icd10_code")
		assert.Contains(t, columnNames, "snomed_code")
	})

	t.Run("parent without forEach appends children directly", func(t *testing.T) {
		// When parent has select clauses but no forEach, children should be appended directly
		lookupTables := []models.LookupTable{
			{
				URL:          "https://example.com/Patient",
				ResourceType: "Patient",
				Elements: map[string]models.LookupElement{
					"Patient.identifier": {
						Children: []string{"Patient.identifier.value", "Patient.identifier.system"},
						ViewDefinition: models.ViewDefSnippet{
							Select: []models.SelectClause{
								// No forEach - just direct columns
								{Column: []models.ColumnDefinition{{Name: "hasIdentifier", Path: "identifier.exists()"}}},
							},
						},
					},
					"Patient.identifier.value": {
						Parent: "Patient.identifier",
						ViewDefinition: models.ViewDefSnippet{
							Select: []models.SelectClause{
								{Column: []models.ColumnDefinition{{Name: "identifierValue", Path: "identifier[0].value"}}},
							},
						},
					},
					"Patient.identifier.system": {
						Parent: "Patient.identifier",
						ViewDefinition: models.ViewDefSnippet{
							Select: []models.SelectClause{
								{Column: []models.ColumnDefinition{{Name: "identifierSystem", Path: "identifier[0].system"}}},
							},
						},
					},
				},
			},
		}

		group := models.AttributeGroup{
			Name:           "Patients",
			GroupReference: "https://example.com/Patient",
			Attributes: []models.Attribute{
				{AttributeRef: "Patient.identifier"},
			},
		}

		builder := services.NewViewDefinitionBuilder(lookupTables)
		viewDef, err := builder.BuildViewDefinition(group)

		require.NoError(t, err)
		require.NotNil(t, viewDef)

		columnNames := services.ExtractColumnNames(*viewDef)
		assert.Contains(t, columnNames, "hasIdentifier")
		assert.Contains(t, columnNames, "identifierValue")
		assert.Contains(t, columnNames, "identifierSystem")
	})

	t.Run("grandchildren resolved through hierarchy", func(t *testing.T) {
		// Test multiple levels of hierarchy
		lookupTables := []models.LookupTable{
			{
				URL:          "https://example.com/Patient",
				ResourceType: "Patient",
				Elements: map[string]models.LookupElement{
					"Patient.contact": {
						Children: []string{"Patient.contact.name"},
						ViewDefinition: models.ViewDefSnippet{
							ForEach: "contact", // At viewDefinition level
							Select:  []models.SelectClause{},
						},
					},
					"Patient.contact.name": {
						Parent:   "Patient.contact",
						Children: []string{"Patient.contact.name.family"},
						ViewDefinition: models.ViewDefSnippet{
							Select: []models.SelectClause{}, // Placeholder
						},
					},
					"Patient.contact.name.family": {
						Parent: "Patient.contact.name",
						ViewDefinition: models.ViewDefSnippet{
							Select: []models.SelectClause{
								{Column: []models.ColumnDefinition{{Name: "contactFamily", Path: "name.family"}}},
							},
						},
					},
				},
			},
		}

		group := models.AttributeGroup{
			Name:           "Patients",
			GroupReference: "https://example.com/Patient",
			Attributes: []models.Attribute{
				{AttributeRef: "Patient.contact"},
			},
		}

		builder := services.NewViewDefinitionBuilder(lookupTables)
		viewDef, err := builder.BuildViewDefinition(group)

		require.NoError(t, err)
		require.NotNil(t, viewDef)

		columnNames := services.ExtractColumnNames(*viewDef)
		assert.Contains(t, columnNames, "contactFamily")
	})
}

// TestUpwardTraversal tests the resolveWithParent logic for various scenarios
func TestUpwardTraversal(t *testing.T) {
	t.Run("child element wrapped in parent forEach context", func(t *testing.T) {
		// When referencing a child element directly, it should be wrapped in parent's forEach
		lookupTables := []models.LookupTable{
			{
				URL:          "https://example.com/Patient",
				ResourceType: "Patient",
				Elements: map[string]models.LookupElement{
					"Patient.name": {
						Children: []string{"Patient.name.family"},
						ViewDefinition: models.ViewDefSnippet{
							ForEach: "name", // At viewDefinition level
							Select:  []models.SelectClause{},
						},
					},
					"Patient.name.family": {
						Parent: "Patient.name",
						ViewDefinition: models.ViewDefSnippet{
							Select: []models.SelectClause{
								{Column: []models.ColumnDefinition{{Name: "family", Path: "family"}}},
							},
						},
					},
				},
			},
		}

		group := models.AttributeGroup{
			Name:           "Patients",
			GroupReference: "https://example.com/Patient",
			Attributes: []models.Attribute{
				{AttributeRef: "Patient.name.family"}, // Referencing child directly
			},
		}

		builder := services.NewViewDefinitionBuilder(lookupTables)
		viewDef, err := builder.BuildViewDefinition(group)

		require.NoError(t, err)
		require.NotNil(t, viewDef)

		// Find the select with forEach: "name" - it should have nested family column
		var foundForEachName bool
		for _, sel := range viewDef.Select {
			if sel.ForEach == "name" {
				foundForEachName = true
				// Should have nested select with family column
				assert.NotEmpty(t, sel.Select, "forEach 'name' should have nested selects")
			}
		}
		assert.True(t, foundForEachName, "Should have a select with forEach='name'")

		columnNames := services.ExtractColumnNames(*viewDef)
		assert.Contains(t, columnNames, "family")
	})

	t.Run("grandchild wrapped through placeholder parent", func(t *testing.T) {
		// When grandchild is referenced and parent is placeholder, should still find grandparent forEach
		lookupTables := []models.LookupTable{
			{
				URL:          "https://example.com/Patient",
				ResourceType: "Patient",
				Elements: map[string]models.LookupElement{
					"Patient.contact": {
						Children: []string{"Patient.contact.name"},
						ViewDefinition: models.ViewDefSnippet{
							ForEach: "contact", // At viewDefinition level
							Select:  []models.SelectClause{},
						},
					},
					"Patient.contact.name": {
						Parent:   "Patient.contact",
						Children: []string{"Patient.contact.name.given"},
						ViewDefinition: models.ViewDefSnippet{
							Select: []models.SelectClause{}, // Placeholder
						},
					},
					"Patient.contact.name.given": {
						Parent: "Patient.contact.name",
						ViewDefinition: models.ViewDefSnippet{
							Select: []models.SelectClause{
								{Column: []models.ColumnDefinition{{Name: "contactGiven", Path: "name.given[0]"}}},
							},
						},
					},
				},
			},
		}

		group := models.AttributeGroup{
			Name:           "Patients",
			GroupReference: "https://example.com/Patient",
			Attributes: []models.Attribute{
				{AttributeRef: "Patient.contact.name.given"}, // Referencing grandchild directly
			},
		}

		builder := services.NewViewDefinitionBuilder(lookupTables)
		viewDef, err := builder.BuildViewDefinition(group)

		require.NoError(t, err)
		require.NotNil(t, viewDef)

		// Should find the contact forEach context
		var foundContactForEach bool
		for _, sel := range viewDef.Select {
			if sel.ForEach == "contact" {
				foundContactForEach = true
			}
		}
		assert.True(t, foundContactForEach, "Should have forEach='contact' from grandparent")

		columnNames := services.ExtractColumnNames(*viewDef)
		assert.Contains(t, columnNames, "contactGiven")
	})

	t.Run("missing parent element returns element selects directly", func(t *testing.T) {
		// Edge case: parent field set but parent not in lookup table
		lookupTables := []models.LookupTable{
			{
				URL:          "https://example.com/Patient",
				ResourceType: "Patient",
				Elements: map[string]models.LookupElement{
					"Patient.orphan": {
						Parent: "Patient.missing", // Parent doesn't exist
						ViewDefinition: models.ViewDefSnippet{
							Select: []models.SelectClause{
								{Column: []models.ColumnDefinition{{Name: "orphan", Path: "orphan"}}},
							},
						},
					},
				},
			},
		}

		group := models.AttributeGroup{
			Name:           "Patients",
			GroupReference: "https://example.com/Patient",
			Attributes: []models.Attribute{
				{AttributeRef: "Patient.orphan"},
			},
		}

		builder := services.NewViewDefinitionBuilder(lookupTables)
		viewDef, err := builder.BuildViewDefinition(group)

		require.NoError(t, err)
		require.NotNil(t, viewDef)

		// Should still have the orphan column
		columnNames := services.ExtractColumnNames(*viewDef)
		assert.Contains(t, columnNames, "orphan")
	})
}

// TestBidirectionalTraversal tests scenarios involving both parent and children
func TestBidirectionalTraversal(t *testing.T) {
	t.Run("element with both parent and children", func(t *testing.T) {
		// Middle element in hierarchy that has both parent (needs wrapping) and children (needs resolution)
		lookupTables := []models.LookupTable{
			{
				URL:          "https://example.com/Observation",
				ResourceType: "Observation",
				Elements: map[string]models.LookupElement{
					"Observation.component": {
						Children: []string{"Observation.component.code"},
						ViewDefinition: models.ViewDefSnippet{
							ForEach: "component", // At viewDefinition level
							Select:  []models.SelectClause{},
						},
					},
					"Observation.component.code": {
						Parent:   "Observation.component",
						Children: []string{"Observation.component.code.coding"},
						ViewDefinition: models.ViewDefSnippet{
							Select: []models.SelectClause{}, // Placeholder - has children
						},
					},
					"Observation.component.code.coding": {
						Parent: "Observation.component.code",
						ViewDefinition: models.ViewDefSnippet{
							Select: []models.SelectClause{
								{Column: []models.ColumnDefinition{{Name: "componentCodeSystem", Path: "code.coding[0].system"}}},
							},
						},
					},
				},
			},
		}

		group := models.AttributeGroup{
			Name:           "Observations",
			GroupReference: "https://example.com/Observation",
			Attributes: []models.Attribute{
				{AttributeRef: "Observation.component.code"}, // Middle of hierarchy
			},
		}

		builder := services.NewViewDefinitionBuilder(lookupTables)
		viewDef, err := builder.BuildViewDefinition(group)

		require.NoError(t, err)
		require.NotNil(t, viewDef)

		// Should have forEach: "component" from parent
		var foundComponentForEach bool
		for _, sel := range viewDef.Select {
			if sel.ForEach == "component" {
				foundComponentForEach = true
			}
		}
		assert.True(t, foundComponentForEach, "Should be wrapped in component forEach")

		// Should also have the child's coding column
		columnNames := services.ExtractColumnNames(*viewDef)
		assert.Contains(t, columnNames, "componentCodeSystem")
	})
}

// TestNestedDescendants tests the resolution of elements below a child element
func TestNestedDescendants(t *testing.T) {
	t.Run("grandchild with forEach gets converted to select clause", func(t *testing.T) {
		// Setup: parent -> child (with forEach) -> grandchild
		lookupTables := []models.LookupTable{
			{
				URL:          "https://example.com/Observation",
				ResourceType: "Observation",
				Elements: map[string]models.LookupElement{
					"Observation.component": {
						Children: []string{"Observation.component.interpretation"},
						ViewDefinition: models.ViewDefSnippet{
							ForEach: "component",
							Select:  []models.SelectClause{},
						},
					},
					"Observation.component.interpretation": {
						Parent:   "Observation.component",
						Children: []string{"Observation.component.interpretation.coding"},
						ViewDefinition: models.ViewDefSnippet{
							ForEachOrNull: "interpretation", // Has forEach at viewDefinition level
							Select:        []models.SelectClause{},
						},
					},
					"Observation.component.interpretation.coding": {
						Parent: "Observation.component.interpretation",
						ViewDefinition: models.ViewDefSnippet{
							ForEach: "coding",
							Select: []models.SelectClause{
								{Column: []models.ColumnDefinition{{Name: "interpretationCode", Path: "code"}}},
							},
						},
					},
				},
			},
		}

		group := models.AttributeGroup{
			Name:           "Observations",
			GroupReference: "https://example.com/Observation",
			Attributes: []models.Attribute{
				{AttributeRef: "Observation.component"},
			},
		}

		builder := services.NewViewDefinitionBuilder(lookupTables)
		viewDef, err := builder.BuildViewDefinition(group)

		require.NoError(t, err)
		require.NotNil(t, viewDef)

		// The grandchild column should be included
		columnNames := services.ExtractColumnNames(*viewDef)
		assert.Contains(t, columnNames, "interpretationCode")
	})

	t.Run("grandchild without forEach resolved recursively", func(t *testing.T) {
		// The child has no root forEach
		lookupTables := []models.LookupTable{
			{
				URL:          "https://example.com/Patient",
				ResourceType: "Patient",
				Elements: map[string]models.LookupElement{
					"Patient.extension": {
						Children: []string{"Patient.extension.value"},
						ViewDefinition: models.ViewDefSnippet{
							ForEach: "extension",
							Select:  []models.SelectClause{},
						},
					},
					"Patient.extension.value": {
						Parent:   "Patient.extension",
						Children: []string{"Patient.extension.value.nested"},
						ViewDefinition: models.ViewDefSnippet{
							// No forEach at root level - placeholder
							Select: []models.SelectClause{},
						},
					},
					"Patient.extension.value.nested": {
						Parent: "Patient.extension.value",
						ViewDefinition: models.ViewDefSnippet{
							Select: []models.SelectClause{
								{Column: []models.ColumnDefinition{{Name: "nestedValue", Path: "value"}}},
							},
						},
					},
				},
			},
		}

		group := models.AttributeGroup{
			Name:           "Patients",
			GroupReference: "https://example.com/Patient",
			Attributes: []models.Attribute{
				{AttributeRef: "Patient.extension"},
			},
		}

		builder := services.NewViewDefinitionBuilder(lookupTables)
		viewDef, err := builder.BuildViewDefinition(group)

		require.NoError(t, err)
		require.NotNil(t, viewDef)

		columnNames := services.ExtractColumnNames(*viewDef)
		assert.Contains(t, columnNames, "nestedValue")
	})
}

// TestBuildViewDefinitionWithOverlappingAttributes tests that when both parent and children
// are in the CRTDL attributes list, children are skipped (parent's downward traversal includes them)
func TestBuildViewDefinitionWithOverlappingAttributes(t *testing.T) {
	t.Run("parent and children both in CRTDL should not duplicate", func(t *testing.T) {
		// Use the exact structure from the bug report
		lookupTables := []models.LookupTable{
			{
				URL:          "https://www.medizininformatik-initiative.de/fhir/core/modul-diagnose/StructureDefinition/Diagnose",
				ResourceType: "Condition",
				Elements: map[string]models.LookupElement{
					"Condition.code": {
						Children: []string{
							"Condition.code.coding:sct",
							"Condition.code.coding:icd10-gm",
							"Condition.code.coding:alpha-id",
							"Condition.code.coding:orphanet",
						},
						ViewDefinition: models.ViewDefSnippet{
							ForEachOrNull: "code",
							Select:        []models.SelectClause{},
						},
					},
					"Condition.code.coding:sct": {
						Parent: "Condition.code",
						ViewDefinition: models.ViewDefSnippet{
							ForEachOrNull: "coding.where(system = 'http://snomed.info/sct')",
							Select: []models.SelectClause{
								{Column: []models.ColumnDefinition{{Name: "code_sct", Path: "code"}}},
							},
						},
					},
					"Condition.code.coding:icd10-gm": {
						Parent: "Condition.code",
						ViewDefinition: models.ViewDefSnippet{
							ForEachOrNull: "coding.where(system = 'http://fhir.de/CodeSystem/bfarm/icd-10-gm')",
							Select: []models.SelectClause{
								{Column: []models.ColumnDefinition{{Name: "code_icd10gm", Path: "code"}}},
							},
						},
					},
					"Condition.code.coding:alpha-id": {
						Parent: "Condition.code",
						ViewDefinition: models.ViewDefSnippet{
							ForEachOrNull: "coding.where(system = 'http://fhir.de/CodeSystem/bfarm/alpha-id')",
							Select: []models.SelectClause{
								{Column: []models.ColumnDefinition{{Name: "code_alphaid", Path: "code"}}},
							},
						},
					},
					"Condition.code.coding:orphanet": {
						Parent: "Condition.code",
						ViewDefinition: models.ViewDefSnippet{
							ForEachOrNull: "coding.where(system = 'http://www.orpha.net')",
							Select: []models.SelectClause{
								{Column: []models.ColumnDefinition{{Name: "code_orphanet", Path: "code"}}},
							},
						},
					},
					"Condition.recordedDate": {
						ViewDefinition: models.ViewDefSnippet{
							Select: []models.SelectClause{
								{Column: []models.ColumnDefinition{{Name: "recorded_date", Path: "recordedDate"}}},
							},
						},
					},
				},
			},
		}

		// CRTDL with BOTH parent AND some children specified
		group := models.AttributeGroup{
			Name:           "Diagnosis",
			GroupReference: "https://www.medizininformatik-initiative.de/fhir/core/modul-diagnose/StructureDefinition/Diagnose",
			Attributes: []models.Attribute{
				{AttributeRef: "Condition.code"},                 // Parent
				{AttributeRef: "Condition.code.coding:icd10-gm"}, // Child - should be skipped
				{AttributeRef: "Condition.code.coding:alpha-id"}, // Child - should be skipped
				{AttributeRef: "Condition.recordedDate"},
			},
		}

		builder := services.NewViewDefinitionBuilder(lookupTables)
		viewDef, err := builder.BuildViewDefinition(group)

		require.NoError(t, err)
		require.NotNil(t, viewDef)

		// Count how many times forEachOrNull: "code" appears at the top level
		codeForEachCount := 0
		for _, sel := range viewDef.Select {
			if sel.ForEachOrNull == "code" {
				codeForEachCount++
			}
		}

		// Should have exactly ONE forEachOrNull: "code" (not duplicated)
		assert.Equal(t, 1, codeForEachCount, "Should have exactly one forEachOrNull: 'code' at top level")

		// Find the code select clause
		var codeSelectClause *models.SelectClause
		for i, sel := range viewDef.Select {
			if sel.ForEachOrNull == "code" {
				codeSelectClause = &viewDef.Select[i]
				break
			}
		}
		require.NotNil(t, codeSelectClause, "Should have forEachOrNull: 'code'")

		// All 4 children should be inside the code wrapper (from downward traversal of parent)
		// The slices are the branches of one unionAll, after the fallback branch.
		require.Len(t, codeSelectClause.Select, 1)
		require.Len(t, codeSelectClause.Select[0].UnionAll, 5)
		childForEachPaths := make(map[string]bool)
		for _, branch := range codeSelectClause.Select[0].UnionAll[1:] {
			childForEachPaths[branch.ForEach] = true
		}

		assert.True(t, childForEachPaths["coding.where(system = 'http://snomed.info/sct')"], "Should have sct child")
		assert.True(t, childForEachPaths["coding.where(system = 'http://fhir.de/CodeSystem/bfarm/icd-10-gm')"], "Should have icd10-gm child")
		assert.True(t, childForEachPaths["coding.where(system = 'http://fhir.de/CodeSystem/bfarm/alpha-id')"], "Should have alpha-id child")
		assert.True(t, childForEachPaths["coding.where(system = 'http://www.orpha.net')"], "Should have orphanet child")

		// Verify no duplicate columns
		columnNames := services.ExtractColumnNames(*viewDef)

		// Count occurrences of each column
		columnCounts := make(map[string]int)
		for _, name := range columnNames {
			columnCounts[name]++
		}

		// Each column should appear exactly once
		for name, count := range columnCounts {
			assert.Equal(t, 1, count, "Column '%s' should appear exactly once, got %d", name, count)
		}
	})

	t.Run("only children in CRTDL (no parent) should include only those children", func(t *testing.T) {
		// Same lookup table structure
		lookupTables := []models.LookupTable{
			{
				URL:          "https://www.medizininformatik-initiative.de/fhir/core/modul-diagnose/StructureDefinition/Diagnose",
				ResourceType: "Condition",
				Elements: map[string]models.LookupElement{
					"Condition.code": {
						Children: []string{
							"Condition.code.coding:sct",
							"Condition.code.coding:icd10-gm",
							"Condition.code.coding:alpha-id",
						},
						ViewDefinition: models.ViewDefSnippet{
							ForEachOrNull: "code",
							Select:        []models.SelectClause{},
						},
					},
					"Condition.code.coding:sct": {
						Parent: "Condition.code",
						ViewDefinition: models.ViewDefSnippet{
							ForEachOrNull: "coding.where(system = 'http://snomed.info/sct')",
							Select: []models.SelectClause{
								{Column: []models.ColumnDefinition{{Name: "code_sct", Path: "code"}}},
							},
						},
					},
					"Condition.code.coding:icd10-gm": {
						Parent: "Condition.code",
						ViewDefinition: models.ViewDefSnippet{
							ForEachOrNull: "coding.where(system = 'http://fhir.de/CodeSystem/bfarm/icd-10-gm')",
							Select: []models.SelectClause{
								{Column: []models.ColumnDefinition{{Name: "code_icd10gm", Path: "code"}}},
							},
						},
					},
					"Condition.code.coding:alpha-id": {
						Parent: "Condition.code",
						ViewDefinition: models.ViewDefSnippet{
							ForEachOrNull: "coding.where(system = 'http://fhir.de/CodeSystem/bfarm/alpha-id')",
							Select: []models.SelectClause{
								{Column: []models.ColumnDefinition{{Name: "code_alphaid", Path: "code"}}},
							},
						},
					},
				},
			},
		}

		// CRTDL with ONLY children (no parent)
		group := models.AttributeGroup{
			Name:           "Diagnosis",
			GroupReference: "https://www.medizininformatik-initiative.de/fhir/core/modul-diagnose/StructureDefinition/Diagnose",
			Attributes: []models.Attribute{
				{AttributeRef: "Condition.code.coding:icd10-gm"}, // Only icd10-gm
				{AttributeRef: "Condition.code.coding:alpha-id"}, // Only alpha-id
			},
		}

		builder := services.NewViewDefinitionBuilder(lookupTables)
		viewDef, err := builder.BuildViewDefinition(group)

		require.NoError(t, err)
		require.NotNil(t, viewDef)

		// Should have forEachOrNull: "code" wrapper (from upward traversal)
		var codeSelectClause *models.SelectClause
		for i, sel := range viewDef.Select {
			if sel.ForEachOrNull == "code" {
				codeSelectClause = &viewDef.Select[i]
				break
			}
		}
		require.NotNil(t, codeSelectClause, "Should have forEachOrNull: 'code' wrapper from parent")

		// Verify columns - should have icd10-gm and alpha-id, NOT sct
		columnNames := services.ExtractColumnNames(*viewDef)
		assert.Contains(t, columnNames, "code_icd10gm")
		assert.Contains(t, columnNames, "code_alphaid")
		// sct should NOT be included since parent wasn't requested
		assert.NotContains(t, columnNames, "code_sct")
	})

	t.Run("grandchild with parent in list should be skipped", func(t *testing.T) {
		// Test multi-level hierarchy: grandparent -> parent -> child
		lookupTables := []models.LookupTable{
			{
				URL:          "https://example.com/Patient",
				ResourceType: "Patient",
				Elements: map[string]models.LookupElement{
					"Patient.contact": {
						Children: []string{"Patient.contact.name"},
						ViewDefinition: models.ViewDefSnippet{
							ForEach: "contact",
							Select:  []models.SelectClause{},
						},
					},
					"Patient.contact.name": {
						Parent:   "Patient.contact",
						Children: []string{"Patient.contact.name.family"},
						ViewDefinition: models.ViewDefSnippet{
							Select: []models.SelectClause{}, // Placeholder
						},
					},
					"Patient.contact.name.family": {
						Parent: "Patient.contact.name",
						ViewDefinition: models.ViewDefSnippet{
							Select: []models.SelectClause{
								{Column: []models.ColumnDefinition{{Name: "contact_family", Path: "name.family"}}},
							},
						},
					},
				},
			},
		}

		// CRTDL with grandparent AND grandchild
		group := models.AttributeGroup{
			Name:           "Patients",
			GroupReference: "https://example.com/Patient",
			Attributes: []models.Attribute{
				{AttributeRef: "Patient.contact"},             // Grandparent
				{AttributeRef: "Patient.contact.name.family"}, // Grandchild - should be skipped
			},
		}

		builder := services.NewViewDefinitionBuilder(lookupTables)
		viewDef, err := builder.BuildViewDefinition(group)

		require.NoError(t, err)
		require.NotNil(t, viewDef)

		// Verify no duplicate columns
		columnNames := services.ExtractColumnNames(*viewDef)
		familyCount := 0
		for _, name := range columnNames {
			if name == "contact_family" {
				familyCount++
			}
		}
		assert.Equal(t, 1, familyCount, "contact_family should appear exactly once")
	})
}

func TestParentChildRefsFromChildrenListOnly(t *testing.T) {
	t.Run("child ref is skipped when the parent ref is also in the group", func(t *testing.T) {
		// The lookup declares the child only through the parent's children list.
		// Normalization backfills the Parent link, so the parent-based skip
		// detects the overlap.
		lookupTables := []models.LookupTable{
			newLookupTable("https://example.com/Encounter", "Encounter", map[string]models.LookupElement{
				"Encounter.extension:Aufnahmegrund": {
					Children: []string{"Encounter.extension:Aufnahmegrund.extension:ErsteUndZweiteStelle"},
					ViewDefinition: models.ViewDefSnippet{
						ForEachOrNull: "extension.where(url = 'aufnahmegrund')",
					},
				},
				"Encounter.extension:Aufnahmegrund.extension:ErsteUndZweiteStelle": {
					ViewDefinition: models.ViewDefSnippet{
						ForEachOrNull: "extension.where(url = 'ErsteUndZweiteStelle')",
						Column: []models.ColumnDefinition{
							{Name: "aufnahmegrund_stelle12_code", Path: "value.code"},
						},
					},
				},
			}),
		}
		services.NormalizeLookupTables(lookupTables)

		group := newAttributeGroup("Encounter", "https://example.com/Encounter",
			"Encounter.extension:Aufnahmegrund",
			"Encounter.extension:Aufnahmegrund.extension:ErsteUndZweiteStelle",
		)

		viewDef := buildAndAssertViewDef(t, lookupTables, group)

		columnNames := services.ExtractColumnNames(*viewDef)
		assert.Equal(t, 1, countOccurrences(columnNames, "aufnahmegrund_stelle12_code"),
			"aufnahmegrund_stelle12_code should appear exactly once")
	})

	t.Run("child-only ref gets the parent forEach wrap after normalization", func(t *testing.T) {
		// The lookup fills only the children direction. Without normalization the
		// child resolves at the resource root and its columns stay empty. After
		// normalization the backfilled Parent link wraps it in the parent context.
		lookupTables := []models.LookupTable{
			newLookupTable("https://example.com/Encounter", "Encounter", map[string]models.LookupElement{
				"Encounter.extension:Aufnahmegrund": {
					Children: []string{"Encounter.extension:Aufnahmegrund.extension:ErsteUndZweiteStelle"},
					ViewDefinition: models.ViewDefSnippet{
						ForEachOrNull: "extension.where(url = 'aufnahmegrund')",
					},
				},
				"Encounter.extension:Aufnahmegrund.extension:ErsteUndZweiteStelle": {
					ViewDefinition: models.ViewDefSnippet{
						ForEachOrNull: "extension.where(url = 'ErsteUndZweiteStelle')",
						Column: []models.ColumnDefinition{
							{Name: "aufnahmegrund_stelle12_code", Path: "value.code"},
						},
					},
				},
			}),
		}
		services.NormalizeLookupTables(lookupTables)

		group := newAttributeGroup("Encounter", "https://example.com/Encounter",
			"Encounter.extension:Aufnahmegrund.extension:ErsteUndZweiteStelle",
		)

		viewDef := buildAndAssertViewDef(t, lookupTables, group)

		require.Len(t, viewDef.Select, 2)
		wrapper := viewDef.Select[1]
		assert.Equal(t, "extension.where(url = 'aufnahmegrund')", wrapper.ForEachOrNull,
			"child must be wrapped in the parent forEach context")
		require.Len(t, wrapper.Select, 1)
		assert.Equal(t, "extension.where(url = 'ErsteUndZweiteStelle')", wrapper.Select[0].ForEachOrNull)
	})

	t.Run("child ref is kept when the parent ref is missing from the lookup", func(t *testing.T) {
		// The parent produces no columns, so skipping the child would lose
		// its data entirely.
		lookupTables := []models.LookupTable{
			newLookupTable("https://example.com/Encounter", "Encounter", map[string]models.LookupElement{
				"Encounter.extension:Aufnahmegrund.extension:ErsteUndZweiteStelle": {
					ViewDefinition: models.ViewDefSnippet{
						ForEachOrNull: "extension.where(url = 'ErsteUndZweiteStelle')",
						Column: []models.ColumnDefinition{
							{Name: "aufnahmegrund_stelle12_code", Path: "value.code"},
						},
					},
				},
			}),
		}

		group := newAttributeGroup("Encounter", "https://example.com/Encounter",
			"Encounter.extension:Aufnahmegrund",
			"Encounter.extension:Aufnahmegrund.extension:ErsteUndZweiteStelle",
		)

		viewDef := buildAndAssertViewDef(t, lookupTables, group)

		columnNames := services.ExtractColumnNames(*viewDef)
		assert.Contains(t, columnNames, "aufnahmegrund_stelle12_code")
	})

	t.Run("sibling ref sharing a string prefix is not skipped", func(t *testing.T) {
		// "Encounter.classHistory" starts with "Encounter.class" as a string,
		// but it is a sibling, not a child, so both must produce columns.
		lookupTables := []models.LookupTable{
			newLookupTable("https://example.com/Encounter", "Encounter", map[string]models.LookupElement{
				"Encounter.class": {
					ViewDefinition: newViewDefSnippet(newSelectClause("class_code", "class.code")),
				},
				"Encounter.classHistory": {
					ViewDefinition: newViewDefSnippet(newSelectClause("class_history_code", "classHistory.class.code")),
				},
			}),
		}

		group := newAttributeGroup("Encounter", "https://example.com/Encounter",
			"Encounter.class",
			"Encounter.classHistory",
		)

		viewDef := buildAndAssertViewDef(t, lookupTables, group)

		columnNames := services.ExtractColumnNames(*viewDef)
		assert.Contains(t, columnNames, "class_code")
		assert.Contains(t, columnNames, "class_history_code")
	})
}

// TestParentSelectForEach tests the wrap of a child in the forEach select of its parent
func TestParentSelectForEach(t *testing.T) {
	t.Run("parent with forEach in select clause wraps child correctly", func(t *testing.T) {
		// This test exercises the path where parent's select clauses have forEach
		// (not at root viewDefinition level but inside SelectClause)
		lookupTables := []models.LookupTable{
			{
				URL:          "https://example.com/MedicationRequest",
				ResourceType: "MedicationRequest",
				Elements: map[string]models.LookupElement{
					"MedicationRequest.dosageInstruction": {
						Children: []string{"MedicationRequest.dosageInstruction.doseAndRate"},
						ViewDefinition: models.ViewDefSnippet{
							// forEach inside select clause, not at viewDefinition level
							Select: []models.SelectClause{
								{
									ForEach: "dosageInstruction",
									Column:  []models.ColumnDefinition{{Name: "dosageText", Path: "text"}},
								},
							},
						},
					},
					"MedicationRequest.dosageInstruction.doseAndRate": {
						Parent: "MedicationRequest.dosageInstruction",
						ViewDefinition: models.ViewDefSnippet{
							Select: []models.SelectClause{
								{Column: []models.ColumnDefinition{{Name: "doseValue", Path: "doseQuantity.value"}}},
							},
						},
					},
				},
			},
		}

		group := models.AttributeGroup{
			Name:           "Medications",
			GroupReference: "https://example.com/MedicationRequest",
			Attributes: []models.Attribute{
				{AttributeRef: "MedicationRequest.dosageInstruction.doseAndRate"},
			},
		}

		builder := services.NewViewDefinitionBuilder(lookupTables)
		viewDef, err := builder.BuildViewDefinition(group)

		require.NoError(t, err)
		require.NotNil(t, viewDef)

		columnNames := services.ExtractColumnNames(*viewDef)
		assert.Contains(t, columnNames, "doseValue")
	})

	t.Run("forEach select with nested selects wraps the child", func(t *testing.T) {
		// The forEach select of the parent has nested selects of its own
		lookupTables := []models.LookupTable{
			{
				URL:          "https://example.com/Bundle",
				ResourceType: "Bundle",
				Elements: map[string]models.LookupElement{
					"Bundle.entry": {
						Children: []string{"Bundle.entry.resource"},
						ViewDefinition: models.ViewDefSnippet{
							Select: []models.SelectClause{
								{
									ForEach: "entry",
									Column:  []models.ColumnDefinition{{Name: "entryIdx", Path: "$index"}},
									Select: []models.SelectClause{
										{Column: []models.ColumnDefinition{{Name: "fullUrl", Path: "fullUrl"}}},
									},
								},
							},
						},
					},
					"Bundle.entry.resource": {
						Parent: "Bundle.entry",
						ViewDefinition: models.ViewDefSnippet{
							Select: []models.SelectClause{
								{Column: []models.ColumnDefinition{{Name: "resourceType", Path: "resource.resourceType"}}},
							},
						},
					},
				},
			},
		}

		group := models.AttributeGroup{
			Name:           "Bundles",
			GroupReference: "https://example.com/Bundle",
			Attributes: []models.Attribute{
				{AttributeRef: "Bundle.entry.resource"},
			},
		}

		builder := services.NewViewDefinitionBuilder(lookupTables)
		viewDef, err := builder.BuildViewDefinition(group)

		require.NoError(t, err)
		require.NotNil(t, viewDef)

		columnNames := services.ExtractColumnNames(*viewDef)
		assert.Contains(t, columnNames, "resourceType")
	})
}

// TestResolveWithParentEdgeCases tests additional edge cases in resolveWithParent
func TestResolveWithParentEdgeCases(t *testing.T) {
	t.Run("parent without forEach returns element selects directly", func(t *testing.T) {
		// Test case where parent has select clauses but none have forEach
		lookupTables := []models.LookupTable{
			{
				URL:          "https://example.com/Encounter",
				ResourceType: "Encounter",
				Elements: map[string]models.LookupElement{
					"Encounter.period": {
						Children: []string{"Encounter.period.start"},
						ViewDefinition: models.ViewDefSnippet{
							// No forEach - direct selects
							Select: []models.SelectClause{
								{Column: []models.ColumnDefinition{{Name: "hasPeriod", Path: "period.exists()"}}},
							},
						},
					},
					"Encounter.period.start": {
						Parent: "Encounter.period",
						ViewDefinition: models.ViewDefSnippet{
							Select: []models.SelectClause{
								{Column: []models.ColumnDefinition{{Name: "periodStart", Path: "period.start"}}},
							},
						},
					},
				},
			},
		}

		group := models.AttributeGroup{
			Name:           "Encounters",
			GroupReference: "https://example.com/Encounter",
			Attributes: []models.Attribute{
				{AttributeRef: "Encounter.period.start"}, // Child referenced directly
			},
		}

		builder := services.NewViewDefinitionBuilder(lookupTables)
		viewDef, err := builder.BuildViewDefinition(group)

		require.NoError(t, err)
		require.NotNil(t, viewDef)

		// Since parent has no forEach, child's selects should be returned directly
		columnNames := services.ExtractColumnNames(*viewDef)
		assert.Contains(t, columnNames, "periodStart")
	})

	t.Run("parent with forEach but result wrapped recursively", func(t *testing.T) {
		// Test the recursive wrapping path: element -> parent (forEach) -> grandparent (forEach)
		lookupTables := []models.LookupTable{
			{
				URL:          "https://example.com/DiagnosticReport",
				ResourceType: "DiagnosticReport",
				Elements: map[string]models.LookupElement{
					"DiagnosticReport.result": {
						Children: []string{"DiagnosticReport.result.display"},
						ViewDefinition: models.ViewDefSnippet{
							ForEach: "result",
							Select:  []models.SelectClause{},
						},
					},
					"DiagnosticReport.result.display": {
						Parent: "DiagnosticReport.result",
						ViewDefinition: models.ViewDefSnippet{
							Select: []models.SelectClause{
								{Column: []models.ColumnDefinition{{Name: "resultDisplay", Path: "display"}}},
							},
						},
					},
				},
			},
		}

		group := models.AttributeGroup{
			Name:           "Reports",
			GroupReference: "https://example.com/DiagnosticReport",
			Attributes: []models.Attribute{
				{AttributeRef: "DiagnosticReport.result.display"},
			},
		}

		builder := services.NewViewDefinitionBuilder(lookupTables)
		viewDef, err := builder.BuildViewDefinition(group)

		require.NoError(t, err)
		require.NotNil(t, viewDef)

		// Should have forEach: "result" wrapper
		var foundResultForEach bool
		for _, sel := range viewDef.Select {
			if sel.ForEach == "result" {
				foundResultForEach = true
			}
		}
		assert.True(t, foundResultForEach, "Should be wrapped in result forEach")

		columnNames := services.ExtractColumnNames(*viewDef)
		assert.Contains(t, columnNames, "resultDisplay")
	})
}

// TestElementWithNoChildrenAndForEach tests element with forEach but no children
func TestElementWithNoChildrenAndForEach(t *testing.T) {
	t.Run("element with root forEach and no children returns select clause", func(t *testing.T) {
		// Tests lines 99-101: element has hasRootForEach but no children
		lookupTables := []models.LookupTable{
			{
				URL:          "https://example.com/Observation",
				ResourceType: "Observation",
				Elements: map[string]models.LookupElement{
					"Observation.note": {
						ViewDefinition: models.ViewDefSnippet{
							ForEach: "note", // Has root forEach
							Select: []models.SelectClause{
								{Column: []models.ColumnDefinition{{Name: "noteText", Path: "text"}}},
							},
						},
						// No children
					},
				},
			},
		}

		group := models.AttributeGroup{
			Name:           "Observations",
			GroupReference: "https://example.com/Observation",
			Attributes: []models.Attribute{
				{AttributeRef: "Observation.note"},
			},
		}

		builder := services.NewViewDefinitionBuilder(lookupTables)
		viewDef, err := builder.BuildViewDefinition(group)

		require.NoError(t, err)
		require.NotNil(t, viewDef)

		// Should have forEach: "note"
		var foundNoteForEach bool
		for _, sel := range viewDef.Select {
			if sel.ForEach == "note" {
				foundNoteForEach = true
			}
		}
		assert.True(t, foundNoteForEach, "Should have forEach='note'")

		columnNames := services.ExtractColumnNames(*viewDef)
		assert.Contains(t, columnNames, "noteText")
	})
}

// TestBuildViewDefinitionWithRealLookupStructure tests the exact structure from flatten-lookup.json
// This test mimics the Condition.code with coding children structure
func TestBuildViewDefinitionWithRealLookupStructure(t *testing.T) {
	t.Run("Condition.code with coding children", func(t *testing.T) {
		// This test case mimics the exact structure from flatten-lookup.json
		// where forEach/forEachOrNull is at the viewDefinition level, not inside SelectClause
		lookupTables := []models.LookupTable{
			{
				URL:          "https://www.medizininformatik-initiative.de/fhir/core/modul-diagnose/StructureDefinition/Diagnose",
				ResourceType: "Condition",
				Elements: map[string]models.LookupElement{
					"Condition.id": {
						ViewDefinition: models.ViewDefSnippet{
							Select: []models.SelectClause{
								{Column: []models.ColumnDefinition{{Name: "id", Path: "id", Type: "string"}}},
							},
						},
					},
					"Condition.subject": {
						ViewDefinition: models.ViewDefSnippet{
							Select: []models.SelectClause{
								{Column: []models.ColumnDefinition{{Name: "patient", Path: "subject.reference", Type: "string"}}},
							},
						},
					},
					"Condition.code": {
						Children: []string{"Condition.code.coding:sct", "Condition.code.coding:icd10-gm"},
						ViewDefinition: models.ViewDefSnippet{
							ForEachOrNull: "code", // At viewDefinition level
							Select:        []models.SelectClause{},
						},
					},
					"Condition.code.coding:sct": {
						Parent: "Condition.code",
						ViewDefinition: models.ViewDefSnippet{
							ForEachOrNull: "coding.where(system = 'http://snomed.info/sct')", // At viewDefinition level
							Select: []models.SelectClause{
								{Column: []models.ColumnDefinition{{Name: "code_system_sct", Path: "system", Type: "string"}}},
							},
						},
					},
					"Condition.code.coding:icd10-gm": {
						Parent: "Condition.code",
						ViewDefinition: models.ViewDefSnippet{
							ForEachOrNull: "coding.where(system = 'http://fhir.de/CodeSystem/bfarm/icd-10-gm')", // At viewDefinition level
							Select: []models.SelectClause{
								{Column: []models.ColumnDefinition{{Name: "code_system_icd10gm", Path: "system", Type: "string"}}},
							},
						},
					},
					"Condition.recordedDate": {
						ViewDefinition: models.ViewDefSnippet{
							Select: []models.SelectClause{
								{Column: []models.ColumnDefinition{{Name: "recorded_date", Path: "recordedDate", Type: "string"}}},
							},
						},
					},
				},
			},
		}

		group := models.AttributeGroup{
			Name:           "Diagnose",
			GroupReference: "https://www.medizininformatik-initiative.de/fhir/core/modul-diagnose/StructureDefinition/Diagnose",
			Attributes: []models.Attribute{
				{AttributeRef: "Condition.id"},
				{AttributeRef: "Condition.subject"},
				{AttributeRef: "Condition.code"}, // Parent with children
				{AttributeRef: "Condition.recordedDate"},
			},
		}

		builder := services.NewViewDefinitionBuilder(lookupTables)
		viewDef, err := builder.BuildViewDefinition(group)

		require.NoError(t, err)
		require.NotNil(t, viewDef)

		// Verify basic ViewDefinition structure
		assert.Equal(t, "Diagnose", viewDef.Name)
		assert.Equal(t, "Condition", viewDef.Resource)
		assert.Equal(t, "draft", viewDef.Status)

		// Verify the nested structure with forEachOrNull preserved
		// We should have:
		// - Fixed columns (id, patient)
		// - id column
		// - subject (patient) column
		// - Condition.code wrapper with forEachOrNull: "code" containing children
		// - recordedDate column

		// Find the select clause with forEachOrNull: "code"
		var codeSelectClause *models.SelectClause
		for i, sel := range viewDef.Select {
			if sel.ForEachOrNull == "code" {
				codeSelectClause = &viewDef.Select[i]
				break
			}
		}

		require.NotNil(t, codeSelectClause, "Should have a select clause with forEachOrNull='code'")
		assert.Equal(t, "code", codeSelectClause.ForEachOrNull)

		// The code select clause should have nested select clauses (children)
		require.NotEmpty(t, codeSelectClause.Select, "code select clause should have nested selects for children")

		// The coding slices go into one unionAll: a fallback branch, then one branch for each slice
		require.Len(t, codeSelectClause.Select, 1)
		union := codeSelectClause.Select[0].UnionAll
		require.Len(t, union, 3, "the code select clause should hold one unionAll with a fallback and two slice branches")
		assert.Equal(t, "coding.where(system = 'http://snomed.info/sct')", union[1].ForEach)
		assert.Equal(t, "coding.where(system = 'http://fhir.de/CodeSystem/bfarm/icd-10-gm')", union[2].ForEach)

		// Verify all column names are extractable
		columnNames := services.ExtractColumnNames(*viewDef)
		assert.Contains(t, columnNames, "id")
		assert.Contains(t, columnNames, "patient")
		assert.Contains(t, columnNames, "code_system_sct")
		assert.Contains(t, columnNames, "code_system_icd10gm")
		assert.Contains(t, columnNames, "recorded_date")
	})
}

// TestIssue142EmbedChildrenIntoParents tests the exact scenario from GitHub issue #142:
// When a CRTDL references an element that has a parent AND children (a 3-level hierarchy),
// the children should be resolved first, embedded into the element, then wrapped in parent context.
// Specifically: column definitions at the viewDefinition level (not inside select) should be supported.
func TestIssue142EmbedChildrenIntoParents(t *testing.T) {
	t.Run("child with column at viewDefinition level embedded in parent", func(t *testing.T) {
		// Exact structure from issue #142
		lookupTables := []models.LookupTable{
			{
				URL:          "https://www.medizininformatik-initiative.de/fhir/core/modul-diagnose/StructureDefinition/Diagnose",
				ResourceType: "Condition",
				Elements: map[string]models.LookupElement{
					"Condition.code": {
						Children: []string{
							"Condition.code.coding:sct",
							"Condition.code.coding:icd10-gm",
							"Condition.code.coding:alpha-id",
							"Condition.code.coding:orphanet",
						},
						ViewDefinition: models.ViewDefSnippet{
							ForEachOrNull: "code",
							Select:        []models.SelectClause{},
						},
					},
					"Condition.code.coding:icd10-gm": {
						Parent:   "Condition.code",
						Children: []string{"Condition.code.coding:icd10-gm.system", "Condition.code.coding:icd10-gm.code"},
						ViewDefinition: models.ViewDefSnippet{
							ForEachOrNull: "coding.where(system = 'http://fhir.de/CodeSystem/bfarm/icd-10-gm')",
							Select:        []models.SelectClause{},
						},
					},
					"Condition.code.coding:icd10-gm.system": {
						Parent: "Condition.code.coding:icd10-gm",
						ViewDefinition: models.ViewDefSnippet{
							// Column at viewDefinition level (not inside select) - this is the key scenario
							Column: []models.ColumnDefinition{
								{Name: "Condition_code_codingicd10gm_system", Path: "system", Type: "string"},
							},
						},
					},
					"Condition.code.coding:icd10-gm.code": {
						Parent: "Condition.code.coding:icd10-gm",
						ViewDefinition: models.ViewDefSnippet{
							// Column at viewDefinition level
							Column: []models.ColumnDefinition{
								{Name: "Condition_code_codingicd10gm_code", Path: "code", Type: "code"},
							},
						},
					},
				},
			},
		}

		// CRTDL references only the middle-tier element
		group := models.AttributeGroup{
			Name:           "Diagnosis",
			GroupReference: "https://www.medizininformatik-initiative.de/fhir/core/modul-diagnose/StructureDefinition/Diagnose",
			Attributes: []models.Attribute{
				{AttributeRef: "Condition.code.coding:icd10-gm"},
			},
		}

		builder := services.NewViewDefinitionBuilder(lookupTables)
		viewDef, err := builder.BuildViewDefinition(group)

		require.NoError(t, err)
		require.NotNil(t, viewDef)

		// Expected structure (from issue #142):
		// {
		//   "select": [
		//     { "column": [id, patient] },
		//     {
		//       "forEachOrNull": "code",
		//       "select": [
		//         {
		//           "forEachOrNull": "coding.where(system = '...')",
		//           "select": [
		//             { "column": [system] },
		//             { "column": [code] }
		//           ]
		//         }
		//       ]
		//     }
		//   ]
		// }

		// Find the outer forEachOrNull: "code" wrapper (from parent)
		var codeSelectClause *models.SelectClause
		for i, sel := range viewDef.Select {
			if sel.ForEachOrNull == "code" {
				codeSelectClause = &viewDef.Select[i]
				break
			}
		}
		require.NotNil(t, codeSelectClause, "Should have forEachOrNull: 'code' wrapper from parent")

		// Inside should have the icd10-gm forEachOrNull
		var icd10gmSelectClause *models.SelectClause
		for i, sel := range codeSelectClause.Select {
			if sel.ForEachOrNull == "coding.where(system = 'http://fhir.de/CodeSystem/bfarm/icd-10-gm')" {
				icd10gmSelectClause = &codeSelectClause.Select[i]
				break
			}
		}
		require.NotNil(t, icd10gmSelectClause, "Should have forEachOrNull for icd10-gm inside code wrapper")

		// Inside icd10-gm should have the children's columns
		columnNames := services.ExtractColumnNames(*viewDef)
		assert.Contains(t, columnNames, "id")
		assert.Contains(t, columnNames, "patient")
		assert.Contains(t, columnNames, "Condition_code_codingicd10gm_system")
		assert.Contains(t, columnNames, "Condition_code_codingicd10gm_code")
	})

	t.Run("element with column at viewDefinition level resolves correctly", func(t *testing.T) {
		// Test that Column at viewDefinition level (no Select, no children) works
		lookupTables := []models.LookupTable{
			{
				URL:          "https://example.com/Patient",
				ResourceType: "Patient",
				Elements: map[string]models.LookupElement{
					"Patient.birthDate": {
						ViewDefinition: models.ViewDefSnippet{
							Column: []models.ColumnDefinition{
								{Name: "birth_date", Path: "birthDate", Type: "date"},
							},
						},
					},
				},
			},
		}

		group := models.AttributeGroup{
			Name:           "Patients",
			GroupReference: "https://example.com/Patient",
			Attributes: []models.Attribute{
				{AttributeRef: "Patient.birthDate"},
			},
		}

		builder := services.NewViewDefinitionBuilder(lookupTables)
		viewDef, err := builder.BuildViewDefinition(group)

		require.NoError(t, err)
		require.NotNil(t, viewDef)

		columnNames := services.ExtractColumnNames(*viewDef)
		assert.Contains(t, columnNames, "id")
		assert.Contains(t, columnNames, "birth_date")
	})
}

// TestSiblingsUnderPlaceholderParent is a regression test for issue #300.
// Two sibling attributes sharing a placeholder parent must produce exactly one
// block each, not duplicates. The placeholder parent has no root forEach and an
// empty select, mirroring the structure of auto-generated MII lookup tables
// where extension leaves share a common abstract ancestor.
func TestSiblingsUnderPlaceholderParent(t *testing.T) {
	t.Run("two siblings under placeholder parent produce one block each", func(t *testing.T) {
		lookupTables := []models.LookupTable{
			{
				URL:          "https://example.com/Condition",
				ResourceType: "Condition",
				Elements: map[string]models.LookupElement{
					"Condition.extension": {
						Children: []string{
							"Condition.extension:Feststellungsdatum",
							"Condition.extension:ReferenzPrimaerdiagnose",
						},
						ViewDefinition: models.ViewDefSnippet{
							Select: []models.SelectClause{}, // placeholder
						},
					},
					"Condition.extension:Feststellungsdatum": {
						Parent: "Condition.extension",
						ViewDefinition: models.ViewDefSnippet{
							ForEachOrNull: "extension.where(url = 'http://hl7.org/fhir/StructureDefinition/condition-assertedDate')",
							Column: []models.ColumnDefinition{
								{Name: "Feststellungsdatum", Path: "value.ofType(dateTime)", Type: "dateTime"},
							},
						},
					},
					"Condition.extension:ReferenzPrimaerdiagnose": {
						Parent: "Condition.extension",
						ViewDefinition: models.ViewDefSnippet{
							ForEachOrNull: "extension.where(url = 'http://hl7.org/fhir/StructureDefinition/condition-related')",
							Column: []models.ColumnDefinition{
								{Name: "ReferenzPrimaerdiagnose", Path: "value.ofType(Reference).reference", Type: "string"},
							},
						},
					},
				},
			},
		}

		group := models.AttributeGroup{
			Name:           "MII PR Diagnose Condition",
			GroupReference: "https://example.com/Condition",
			Attributes: []models.Attribute{
				{AttributeRef: "Condition.extension:Feststellungsdatum"},
				{AttributeRef: "Condition.extension:ReferenzPrimaerdiagnose"},
			},
		}

		builder := services.NewViewDefinitionBuilder(lookupTables)
		viewDef, err := builder.BuildViewDefinition(group)

		require.NoError(t, err)
		require.NotNil(t, viewDef)

		// The extension slices are the branches of one unionAll, after the fallback branch.
		require.Len(t, viewDef.Select, 2)
		require.Len(t, viewDef.Select[1].UnionAll, 3)
		assertedDateCount := 0
		relatedCount := 0
		for _, branch := range viewDef.Select[1].UnionAll[1:] {
			fe := branch.ForEach
			switch fe {
			case "extension.where(url = 'http://hl7.org/fhir/StructureDefinition/condition-assertedDate')":
				assertedDateCount++
			case "extension.where(url = 'http://hl7.org/fhir/StructureDefinition/condition-related')":
				relatedCount++
			}
		}

		assert.Equal(t, 1, assertedDateCount, "assertedDate extension block must appear exactly once")
		assert.Equal(t, 1, relatedCount, "condition-related extension block must appear exactly once")

		columnNames := services.ExtractColumnNames(*viewDef)
		assert.Equal(t, 1, countOccurrences(columnNames, "Feststellungsdatum"), "Feststellungsdatum column must appear exactly once")
		assert.Equal(t, 1, countOccurrences(columnNames, "ReferenzPrimaerdiagnose"), "ReferenzPrimaerdiagnose column must appear exactly once")
	})
}

func countOccurrences(names []string, target string) int {
	n := 0
	for _, s := range names {
		if s == target {
			n++
		}
	}
	return n
}

// assertAttributeSelectsJSON compares the selects after the fixed-column clause as JSON,
// because JSON is the form that the flattener receives.
func assertAttributeSelectsJSON(t *testing.T, lookup models.LookupTable, attributeRef, expected string) {
	t.Helper()
	assertGroupSelectsJSON(t, lookup, expected, attributeRef)
}

// assertGroupSelectsJSON is assertAttributeSelectsJSON for a group with more than one attribute.
func assertGroupSelectsJSON(t *testing.T, lookup models.LookupTable, expected string, attributeRefs ...string) {
	t.Helper()
	viewDef := buildAndAssertViewDef(t, []models.LookupTable{lookup}, newAttributeGroup("G", lookup.URL, attributeRefs...))
	require.NotEmpty(t, viewDef.Select)
	actual, err := json.Marshal(viewDef.Select[1:])
	require.NoError(t, err)
	assert.JSONEq(t, expected, string(actual))
}

func TestViewDefinitionSelectStructure(t *testing.T) {
	t.Run("leaf with only select clauses keeps them unwrapped", func(t *testing.T) {
		lookup := newLookupTable("https://example.com/Observation", "Observation", map[string]models.LookupElement{
			"Observation.status": {ViewDefinition: newViewDefSnippet(newSelectClause("status", "status"))},
		})

		assertAttributeSelectsJSON(t, lookup, "Observation.status",
			`[{"column":[{"name":"status","path":"status"}]}]`)
	})

	t.Run("child without forEach adds its selects next to the parent selects", func(t *testing.T) {
		lookup := newLookupTable("https://example.com/Observation", "Observation", map[string]models.LookupElement{
			"Observation.code": {
				Children:       []string{"Observation.code.text"},
				ViewDefinition: newViewDefSnippet(newSelectClause("code", "code.coding.code")),
			},
			"Observation.code.text": {
				Parent:         "Observation.code",
				ViewDefinition: newViewDefSnippet(newSelectClause("text", "code.text")),
			},
		})

		assertAttributeSelectsJSON(t, lookup, "Observation.code", `[
			{"column":[{"name":"code","path":"code.coding.code"}]},
			{"column":[{"name":"text","path":"code.text"}]}
		]`)
	})

	t.Run("descendants of a forEach child nest inside its forEach context", func(t *testing.T) {
		lookup := newLookupTable("https://example.com/Observation", "Observation", map[string]models.LookupElement{
			"Observation.component": {
				Children: []string{"Observation.component.item"},
			},
			"Observation.component.item": {
				Parent:         "Observation.component",
				Children:       []string{"Observation.component.item.text", "Observation.component.item.coding"},
				ViewDefinition: models.ViewDefSnippet{ForEach: "component"},
			},
			"Observation.component.item.text": {
				Parent:         "Observation.component.item",
				ViewDefinition: newViewDefSnippet(newSelectClause("text", "code.text")),
			},
			"Observation.component.item.coding": {
				Parent:         "Observation.component.item",
				Children:       []string{"Observation.component.item.coding.code"},
				ViewDefinition: models.ViewDefSnippet{ForEachOrNull: "code.coding"},
			},
			"Observation.component.item.coding.code": {
				Parent:         "Observation.component.item.coding",
				ViewDefinition: newViewDefSnippet(newSelectClause("code", "code")),
			},
		})

		assertAttributeSelectsJSON(t, lookup, "Observation.component", `[
			{"forEach":"component","select":[
				{"column":[{"name":"text","path":"code.text"}]},
				{"forEachOrNull":"code.coding","select":[
					{"column":[{"name":"code","path":"code"}]}
				]}
			]}
		]`)
	})

	t.Run("child is wrapped in the forEach select of its parent", func(t *testing.T) {
		lookup := newLookupTable("https://example.com/MedicationRequest", "MedicationRequest", map[string]models.LookupElement{
			"MedicationRequest.dosageInstruction": {
				Children: []string{"MedicationRequest.dosageInstruction.doseAndRate"},
				ViewDefinition: newViewDefSnippet(
					newSelectClause("hasDosage", "dosageInstruction.exists()"),
					models.SelectClause{
						ForEach: "dosageInstruction",
						Column:  []models.ColumnDefinition{{Name: "dosageText", Path: "text"}},
					},
				),
			},
			"MedicationRequest.dosageInstruction.doseAndRate": {
				Parent:         "MedicationRequest.dosageInstruction",
				ViewDefinition: newViewDefSnippet(newSelectClause("dose", "doseAndRate.doseQuantity.value")),
			},
		})

		assertAttributeSelectsJSON(t, lookup, "MedicationRequest.dosageInstruction.doseAndRate", `[
			{"forEach":"dosageInstruction",
			 "column":[{"name":"dosageText","path":"text"}],
			 "select":[{"column":[{"name":"dose","path":"doseAndRate.doseQuantity.value"}]}]}
		]`)
	})

	t.Run("placeholder parent passes the child up to a forEachOrNull grandparent", func(t *testing.T) {
		lookup := newLookupTable("https://example.com/Observation", "Observation", map[string]models.LookupElement{
			"Observation.component": {
				Children:       []string{"Observation.component.value"},
				ViewDefinition: models.ViewDefSnippet{ForEachOrNull: "component"},
			},
			"Observation.component.value": {
				Parent:   "Observation.component",
				Children: []string{"Observation.component.value.unit"},
			},
			"Observation.component.value.unit": {
				Parent:         "Observation.component.value",
				ViewDefinition: newViewDefSnippet(newSelectClause("unit", "valueQuantity.unit")),
			},
		})

		assertAttributeSelectsJSON(t, lookup, "Observation.component.value.unit", `[
			{"forEachOrNull":"component","select":[
				{"column":[{"name":"unit","path":"valueQuantity.unit"}]}
			]}
		]`)
	})

	t.Run("leaf with a root column becomes one select clause", func(t *testing.T) {
		lookup := newLookupTable("https://example.com/Observation", "Observation", map[string]models.LookupElement{
			"Observation.status": {ViewDefinition: models.ViewDefSnippet{
				Column: []models.ColumnDefinition{{Name: "status", Path: "status"}},
			}},
		})

		assertAttributeSelectsJSON(t, lookup, "Observation.status",
			`[{"column":[{"name":"status","path":"status"}]}]`)
	})
}

func TestBuildViewDefinitionDoesNotWriteIntoLookupSelect(t *testing.T) {
	ownSelect := make([]models.SelectClause, 1, 2)
	ownSelect[0] = newSelectClause("code", "code")
	lookupTables := []models.LookupTable{
		newLookupTable("https://example.com/Condition", "Condition", map[string]models.LookupElement{
			"Condition.code": {
				Children: []string{"Condition.code.display"},
				ViewDefinition: models.ViewDefSnippet{
					ForEach: "code.coding",
					Select:  ownSelect,
				},
			},
			"Condition.code.display": {
				Parent: "Condition.code",
				ViewDefinition: models.ViewDefSnippet{
					Column: []models.ColumnDefinition{{Name: "display", Path: "display"}},
				},
			},
		}),
	}

	builder := services.NewViewDefinitionBuilder(lookupTables)
	viewDef, err := builder.BuildViewDefinition(newAttributeGroup("Conditions", "https://example.com/Condition", "Condition.code"))

	require.NoError(t, err)
	assert.Contains(t, services.ExtractColumnNames(*viewDef), "display")
	assert.Equal(t, models.SelectClause{}, ownSelect[:2][1], "the build must not write into the spare capacity of the lookup table's Select")
}

func TestSharedAncestorsMergeIntoOneClause(t *testing.T) {
	t.Run("attributes with a shared root forEach parent share one clause", func(t *testing.T) {
		lookup := newLookupTable("https://example.com/Medication", "Medication", map[string]models.LookupElement{
			"Medication.ingredient": {
				Children:       []string{"Medication.ingredient.item[x]", "Medication.ingredient.strength"},
				ViewDefinition: models.ViewDefSnippet{ForEachOrNull: "ingredient"},
			},
			"Medication.ingredient.item[x]": {
				Parent: "Medication.ingredient",
				ViewDefinition: models.ViewDefSnippet{
					ForEachOrNull: "item.ofType(CodeableConcept).coding",
					Column:        []models.ColumnDefinition{{Name: "code", Path: "code"}},
				},
			},
			"Medication.ingredient.strength": {
				Parent: "Medication.ingredient",
				ViewDefinition: models.ViewDefSnippet{
					ForEachOrNull: "strength",
					Column:        []models.ColumnDefinition{{Name: "numerator", Path: "numerator.value"}},
				},
			},
		})

		assertGroupSelectsJSON(t, lookup, `[{
			"forEachOrNull": "ingredient",
			"select": [
				{"forEachOrNull": "item.ofType(CodeableConcept).coding", "column": [{"name": "code", "path": "code"}]},
				{"forEachOrNull": "strength", "column": [{"name": "numerator", "path": "numerator.value"}]}
			]
		}]`, "Medication.ingredient.item[x]", "Medication.ingredient.strength")
	})

	t.Run("attributes with a shared select-level forEach parent show its columns once", func(t *testing.T) {
		lookup := newLookupTable("https://example.com/Observation", "Observation", map[string]models.LookupElement{
			"Observation.component": {
				Children: []string{"Observation.component.code", "Observation.component.value[x]"},
				ViewDefinition: newViewDefSnippet(models.SelectClause{
					ForEach: "component",
					Column:  []models.ColumnDefinition{{Name: "component_id", Path: "id"}},
				}),
			},
			"Observation.component.code": {
				Parent:         "Observation.component",
				ViewDefinition: newViewDefSnippet(newSelectClause("code", "code.coding.code")),
			},
			"Observation.component.value[x]": {
				Parent:         "Observation.component",
				ViewDefinition: newViewDefSnippet(newSelectClause("value", "value.ofType(string)")),
			},
		})

		assertGroupSelectsJSON(t, lookup, `[{
			"forEach": "component",
			"column": [{"name": "component_id", "path": "id"}],
			"select": [
				{"column": [{"name": "code", "path": "code.coding.code"}]},
				{"column": [{"name": "value", "path": "value.ofType(string)"}]}
			]
		}]`, "Observation.component.code", "Observation.component.value[x]")
	})

	t.Run("a shared grandparent clause holds one clause for each distinct parent", func(t *testing.T) {
		lookup := newLookupTable("https://example.com/Encounter", "Encounter", map[string]models.LookupElement{
			"Encounter.diagnosis": {
				Children:       []string{"Encounter.diagnosis.condition", "Encounter.diagnosis.use"},
				ViewDefinition: models.ViewDefSnippet{ForEachOrNull: "diagnosis"},
			},
			"Encounter.diagnosis.condition": {
				Parent:         "Encounter.diagnosis",
				Children:       []string{"Encounter.diagnosis.condition.reference"},
				ViewDefinition: models.ViewDefSnippet{ForEachOrNull: "condition"},
			},
			"Encounter.diagnosis.condition.reference": {
				Parent:         "Encounter.diagnosis.condition",
				ViewDefinition: newViewDefSnippet(newSelectClause("condition_ref", "reference")),
			},
			"Encounter.diagnosis.use": {
				Parent:         "Encounter.diagnosis",
				Children:       []string{"Encounter.diagnosis.use.text"},
				ViewDefinition: models.ViewDefSnippet{ForEachOrNull: "use"},
			},
			"Encounter.diagnosis.use.text": {
				Parent:         "Encounter.diagnosis.use",
				ViewDefinition: newViewDefSnippet(newSelectClause("use_text", "text")),
			},
		})

		assertGroupSelectsJSON(t, lookup, `[{
			"forEachOrNull": "diagnosis",
			"select": [
				{"forEachOrNull": "condition", "select": [{"column": [{"name": "condition_ref", "path": "reference"}]}]},
				{"forEachOrNull": "use", "select": [{"column": [{"name": "use_text", "path": "text"}]}]}
			]
		}]`, "Encounter.diagnosis.condition.reference", "Encounter.diagnosis.use.text")
	})

	t.Run("a shared ancestor clause takes the position of its first attribute", func(t *testing.T) {
		lookup := newLookupTable("https://example.com/Observation", "Observation", map[string]models.LookupElement{
			"Observation.x": {
				Children:       []string{"Observation.x.a", "Observation.x.c"},
				ViewDefinition: models.ViewDefSnippet{ForEachOrNull: "x"},
			},
			"Observation.x.a": {
				Parent:         "Observation.x",
				ViewDefinition: models.ViewDefSnippet{Column: []models.ColumnDefinition{{Name: "a", Path: "a"}}},
			},
			"Observation.x.c": {
				Parent:         "Observation.x",
				ViewDefinition: models.ViewDefSnippet{Column: []models.ColumnDefinition{{Name: "c", Path: "c"}}},
			},
			"Observation.b": {
				ViewDefinition: models.ViewDefSnippet{Column: []models.ColumnDefinition{{Name: "b", Path: "b"}}},
			},
		})

		assertGroupSelectsJSON(t, lookup, `[
			{"forEachOrNull": "x", "select": [
				{"column": [{"name": "a", "path": "a"}]},
				{"column": [{"name": "c", "path": "c"}]}
			]},
			{"column": [{"name": "b", "path": "b"}]}
		]`, "Observation.x.a", "Observation.b", "Observation.x.c")

		assertGroupSelectsJSON(t, lookup, `[
			{"column": [{"name": "b", "path": "b"}]},
			{"forEachOrNull": "x", "select": [
				{"column": [{"name": "a", "path": "a"}]},
				{"column": [{"name": "c", "path": "c"}]}
			]}
		]`, "Observation.b", "Observation.x.a", "Observation.x.c")
	})

	t.Run("a selected ancestor includes its selected descendant once in the shared clause", func(t *testing.T) {
		lookup := newLookupTable("https://example.com/Observation", "Observation", map[string]models.LookupElement{
			"Observation.g": {
				Children:       []string{"Observation.g.p", "Observation.g.q"},
				ViewDefinition: models.ViewDefSnippet{ForEachOrNull: "g"},
			},
			"Observation.g.p": {
				Parent:         "Observation.g",
				Children:       []string{"Observation.g.p.c"},
				ViewDefinition: models.ViewDefSnippet{ForEachOrNull: "p"},
			},
			"Observation.g.p.c": {
				Parent:         "Observation.g.p",
				ViewDefinition: models.ViewDefSnippet{Column: []models.ColumnDefinition{{Name: "c", Path: "c"}}},
			},
			"Observation.g.q": {
				Parent:         "Observation.g",
				ViewDefinition: models.ViewDefSnippet{Column: []models.ColumnDefinition{{Name: "q", Path: "q"}}},
			},
		})

		assertGroupSelectsJSON(t, lookup, `[
			{"forEachOrNull": "g", "select": [
				{"column": [{"name": "q", "path": "q"}]},
				{"forEachOrNull": "p", "select": [{"column": [{"name": "c", "path": "c"}]}]}
			]}
		]`, "Observation.g.p.c", "Observation.g.q", "Observation.g.p")
	})

	t.Run("attributes merge only below an ancestor that the lookup does not have", func(t *testing.T) {
		lookup := newLookupTable("https://example.com/Observation", "Observation", map[string]models.LookupElement{
			"Observation.p": {
				Parent:         "Observation.missing",
				Children:       []string{"Observation.p.c", "Observation.p.d"},
				ViewDefinition: models.ViewDefSnippet{ForEachOrNull: "p"},
			},
			"Observation.p.c": {
				Parent:         "Observation.p",
				ViewDefinition: models.ViewDefSnippet{Column: []models.ColumnDefinition{{Name: "c", Path: "c"}}},
			},
			"Observation.p.d": {
				Parent:         "Observation.p",
				ViewDefinition: models.ViewDefSnippet{Column: []models.ColumnDefinition{{Name: "d", Path: "d"}}},
			},
			"Observation.e": {
				Parent:         "Observation.missing",
				ViewDefinition: models.ViewDefSnippet{Column: []models.ColumnDefinition{{Name: "e", Path: "e"}}},
			},
		})

		assertGroupSelectsJSON(t, lookup, `[
			{"forEachOrNull": "p", "select": [
				{"column": [{"name": "c", "path": "c"}]},
				{"column": [{"name": "d", "path": "d"}]}
			]},
			{"column": [{"name": "e", "path": "e"}]}
		]`, "Observation.p.c", "Observation.e", "Observation.p.d")
	})
}

// codingSliceElement returns a lookup element for a coding slice that iterates with forEachOrNull.
func codingSliceElement(parent, filter, columnName string) models.LookupElement {
	return models.LookupElement{
		Parent: parent,
		ViewDefinition: models.ViewDefSnippet{
			ForEachOrNull: "coding.where(" + filter + ")",
			Column:        []models.ColumnDefinition{{Name: columnName, Path: "code", Type: "string"}},
		},
	}
}

func TestSlicesOfOneElementGoIntoUnionAll(t *testing.T) {
	t.Run("two slices of one element become one unionAll with a fallback branch first", func(t *testing.T) {
		lookup := newLookupTable("https://example.com/Condition", "Condition", map[string]models.LookupElement{
			"Condition.code": {
				Children:       []string{"Condition.code.coding:icd10-gm", "Condition.code.coding:alpha-id"},
				ViewDefinition: models.ViewDefSnippet{ForEachOrNull: "code"},
			},
			"Condition.code.coding:icd10-gm": codingSliceElement("Condition.code", "system='icd'", "icd10"),
			"Condition.code.coding:alpha-id": codingSliceElement("Condition.code", "system='alpha'", "alpha"),
		})

		assertGroupSelectsJSON(t, lookup, `[{
			"forEachOrNull": "code",
			"select": [{"unionAll": [
				{
					"forEach": "$this.where((coding.where(system='icd')).empty() and (coding.where(system='alpha')).empty())",
					"select": [
						{"forEachOrNull": "coding.where(system='icd')", "column": [{"name": "icd10", "path": "code", "type": "string"}]},
						{"forEachOrNull": "coding.where(system='alpha')", "column": [{"name": "alpha", "path": "code", "type": "string"}]}
					]
				},
				{
					"forEach": "coding.where(system='icd')",
					"select": [
						{"column": [{"name": "icd10", "path": "code", "type": "string"}]},
						{"column": [{"name": "alpha", "path": "{}", "type": "string"}]}
					]
				},
				{
					"forEach": "coding.where(system='alpha')",
					"select": [
						{"column": [{"name": "icd10", "path": "{}", "type": "string"}]},
						{"column": [{"name": "alpha", "path": "code", "type": "string"}]}
					]
				}
			]}]
		}]`, "Condition.code")
	})

	t.Run("slices selected as separate attributes under a common parent share one unionAll", func(t *testing.T) {
		lookup := newLookupTable("https://example.com/Condition", "Condition", map[string]models.LookupElement{
			"Condition.code": {
				Children:       []string{"Condition.code.coding:icd10-gm", "Condition.code.coding:alpha-id"},
				ViewDefinition: models.ViewDefSnippet{ForEachOrNull: "code"},
			},
			"Condition.code.coding:icd10-gm": codingSliceElement("Condition.code", "system='icd'", "icd10"),
			"Condition.code.coding:alpha-id": codingSliceElement("Condition.code", "system='alpha'", "alpha"),
		})

		assertGroupSelectsJSON(t, lookup, `[{
			"forEachOrNull": "code",
			"select": [{"unionAll": [
				{
					"forEach": "$this.where((coding.where(system='icd')).empty() and (coding.where(system='alpha')).empty())",
					"select": [
						{"forEachOrNull": "coding.where(system='icd')", "column": [{"name": "icd10", "path": "code", "type": "string"}]},
						{"forEachOrNull": "coding.where(system='alpha')", "column": [{"name": "alpha", "path": "code", "type": "string"}]}
					]
				},
				{
					"forEach": "coding.where(system='icd')",
					"select": [
						{"column": [{"name": "icd10", "path": "code", "type": "string"}]},
						{"column": [{"name": "alpha", "path": "{}", "type": "string"}]}
					]
				},
				{
					"forEach": "coding.where(system='alpha')",
					"select": [
						{"column": [{"name": "icd10", "path": "{}", "type": "string"}]},
						{"column": [{"name": "alpha", "path": "code", "type": "string"}]}
					]
				}
			]}]
		}]`, "Condition.code.coding:icd10-gm", "Condition.code.coding:alpha-id")
	})

	t.Run("three slices join all expressions in the fallback and pad around the middle slice", func(t *testing.T) {
		lookup := newLookupTable("https://example.com/Condition", "Condition", map[string]models.LookupElement{
			"Condition.code": {
				Children:       []string{"Condition.code.coding:a", "Condition.code.coding:b", "Condition.code.coding:c"},
				ViewDefinition: models.ViewDefSnippet{ForEachOrNull: "code"},
			},
			"Condition.code.coding:a": codingSliceElement("Condition.code", "s='a'", "col_a"),
			"Condition.code.coding:b": codingSliceElement("Condition.code", "s='b'", "col_b"),
			"Condition.code.coding:c": codingSliceElement("Condition.code", "s='c'", "col_c"),
		})

		assertGroupSelectsJSON(t, lookup, `[{
			"forEachOrNull": "code",
			"select": [{"unionAll": [
				{
					"forEach": "$this.where((coding.where(s='a')).empty() and (coding.where(s='b')).empty() and (coding.where(s='c')).empty())",
					"select": [
						{"forEachOrNull": "coding.where(s='a')", "column": [{"name": "col_a", "path": "code", "type": "string"}]},
						{"forEachOrNull": "coding.where(s='b')", "column": [{"name": "col_b", "path": "code", "type": "string"}]},
						{"forEachOrNull": "coding.where(s='c')", "column": [{"name": "col_c", "path": "code", "type": "string"}]}
					]
				},
				{"forEach": "coding.where(s='a')", "select": [
					{"column": [{"name": "col_a", "path": "code", "type": "string"}]},
					{"column": [{"name": "col_b", "path": "{}", "type": "string"}]},
					{"column": [{"name": "col_c", "path": "{}", "type": "string"}]}
				]},
				{"forEach": "coding.where(s='b')", "select": [
					{"column": [{"name": "col_a", "path": "{}", "type": "string"}]},
					{"column": [{"name": "col_b", "path": "code", "type": "string"}]},
					{"column": [{"name": "col_c", "path": "{}", "type": "string"}]}
				]},
				{"forEach": "coding.where(s='c')", "select": [
					{"column": [{"name": "col_a", "path": "{}", "type": "string"}]},
					{"column": [{"name": "col_b", "path": "{}", "type": "string"}]},
					{"column": [{"name": "col_c", "path": "code", "type": "string"}]}
				]}
			]}]
		}]`, "Condition.code")
	})

	t.Run("a single slice stays as it is", func(t *testing.T) {
		lookup := newLookupTable("https://example.com/Condition", "Condition", map[string]models.LookupElement{
			"Condition.code": {
				Children:       []string{"Condition.code.coding:icd10-gm"},
				ViewDefinition: models.ViewDefSnippet{ForEachOrNull: "code"},
			},
			"Condition.code.coding:icd10-gm": codingSliceElement("Condition.code", "system='icd'", "icd10"),
		})

		assertGroupSelectsJSON(t, lookup, `[{
			"forEachOrNull": "code",
			"select": [
				{"forEachOrNull": "coding.where(system='icd')", "column": [{"name": "icd10", "path": "code", "type": "string"}]}
			]
		}]`, "Condition.code")
	})

	t.Run("forEach siblings that are not slices of one element stay cross-joined", func(t *testing.T) {
		lookup := newLookupTable("https://example.com/Condition", "Condition", map[string]models.LookupElement{
			"Condition.clinicalStatus": {
				ViewDefinition: models.ViewDefSnippet{
					ForEachOrNull: "clinicalStatus.coding",
					Column:        []models.ColumnDefinition{{Name: "status", Path: "code"}},
				},
			},
			"Condition.code": {
				ViewDefinition: models.ViewDefSnippet{
					ForEachOrNull: "code.coding",
					Column:        []models.ColumnDefinition{{Name: "code", Path: "code"}},
				},
			},
		})

		assertGroupSelectsJSON(t, lookup, `[
			{"forEachOrNull": "clinicalStatus.coding", "column": [{"name": "status", "path": "code"}]},
			{"forEachOrNull": "code.coding", "column": [{"name": "code", "path": "code"}]}
		]`, "Condition.clinicalStatus", "Condition.code")
	})

	t.Run("a slice with forEach becomes forEachOrNull in the fallback branch", func(t *testing.T) {
		sliceA := codingSliceElement("Condition.code", "s='a'", "col_a")
		sliceA.ViewDefinition.ForEach, sliceA.ViewDefinition.ForEachOrNull = sliceA.ViewDefinition.ForEachOrNull, ""
		lookup := newLookupTable("https://example.com/Condition", "Condition", map[string]models.LookupElement{
			"Condition.code": {
				Children:       []string{"Condition.code.coding:a", "Condition.code.coding:b"},
				ViewDefinition: models.ViewDefSnippet{ForEachOrNull: "code"},
			},
			"Condition.code.coding:a": sliceA,
			"Condition.code.coding:b": codingSliceElement("Condition.code", "s='b'", "col_b"),
		})

		assertGroupSelectsJSON(t, lookup, `[{
			"forEachOrNull": "code",
			"select": [{"unionAll": [
				{
					"forEach": "$this.where((coding.where(s='a')).empty() and (coding.where(s='b')).empty())",
					"select": [
						{"forEachOrNull": "coding.where(s='a')", "column": [{"name": "col_a", "path": "code", "type": "string"}]},
						{"forEachOrNull": "coding.where(s='b')", "column": [{"name": "col_b", "path": "code", "type": "string"}]}
					]
				},
				{"forEach": "coding.where(s='a')", "select": [
					{"column": [{"name": "col_a", "path": "code", "type": "string"}]},
					{"column": [{"name": "col_b", "path": "{}", "type": "string"}]}
				]},
				{"forEach": "coding.where(s='b')", "select": [
					{"column": [{"name": "col_a", "path": "{}", "type": "string"}]},
					{"column": [{"name": "col_b", "path": "code", "type": "string"}]}
				]}
			]}]
		}]`, "Condition.code")
	})

	t.Run("the union takes the position of the first slice before a sibling that is not a slice", func(t *testing.T) {
		lookup := newLookupTable("https://example.com/Condition", "Condition", map[string]models.LookupElement{
			"Condition.code": {
				Children:       []string{"Condition.code.coding:a", "Condition.code.text", "Condition.code.coding:b"},
				ViewDefinition: models.ViewDefSnippet{ForEachOrNull: "code"},
			},
			"Condition.code.coding:a": codingSliceElement("Condition.code", "s='a'", "col_a"),
			"Condition.code.text": {
				Parent:         "Condition.code",
				ViewDefinition: models.ViewDefSnippet{Column: []models.ColumnDefinition{{Name: "text", Path: "text"}}},
			},
			"Condition.code.coding:b": codingSliceElement("Condition.code", "s='b'", "col_b"),
		})

		viewDef, err := services.NewViewDefinitionBuilder([]models.LookupTable{lookup}).BuildViewDefinition(models.AttributeGroup{
			Name:           "g",
			GroupReference: lookup.URL,
			Attributes:     []models.Attribute{{AttributeRef: "Condition.code"}},
		})
		require.NoError(t, err)
		code := viewDef.Select[1].Select
		require.Len(t, code, 2)
		assert.Len(t, code[0].UnionAll, 3)
		assert.Equal(t, []models.ColumnDefinition{{Name: "text", Path: "text"}}, code[1].Column)
		assert.Equal(t, []string{"id", "patient", "col_a", "col_b", "text"}, services.ExtractColumnNames(*viewDef))
	})

	t.Run("a slice that is an ancestor of a selected attribute joins the union", func(t *testing.T) {
		sliceA := models.LookupElement{
			Parent:         "Condition.code",
			Children:       []string{"Condition.code.coding:a.display"},
			ViewDefinition: models.ViewDefSnippet{ForEachOrNull: "coding.where(s='a')"},
		}
		lookup := newLookupTable("https://example.com/Condition", "Condition", map[string]models.LookupElement{
			"Condition.code": {
				Children:       []string{"Condition.code.coding:a", "Condition.code.coding:b"},
				ViewDefinition: models.ViewDefSnippet{ForEachOrNull: "code"},
			},
			"Condition.code.coding:a": sliceA,
			"Condition.code.coding:a.display": {
				Parent:         "Condition.code.coding:a",
				ViewDefinition: models.ViewDefSnippet{Column: []models.ColumnDefinition{{Name: "display", Path: "display"}}},
			},
			"Condition.code.coding:b": codingSliceElement("Condition.code", "s='b'", "col_b"),
		})

		assertGroupSelectsJSON(t, lookup, `[{
			"forEachOrNull": "code",
			"select": [{"unionAll": [
				{
					"forEach": "$this.where((coding.where(s='a')).empty() and (coding.where(s='b')).empty())",
					"select": [
						{"forEachOrNull": "coding.where(s='a')", "select": [{"column": [{"name": "display", "path": "display"}]}]},
						{"forEachOrNull": "coding.where(s='b')", "column": [{"name": "col_b", "path": "code", "type": "string"}]}
					]
				},
				{"forEach": "coding.where(s='a')", "select": [
					{"select": [{"column": [{"name": "display", "path": "display"}]}]},
					{"column": [{"name": "col_b", "path": "{}", "type": "string"}]}
				]},
				{"forEach": "coding.where(s='b')", "select": [
					{"column": [{"name": "display", "path": "{}"}]},
					{"column": [{"name": "col_b", "path": "code", "type": "string"}]}
				]}
			]}]
		}]`, "Condition.code.coding:a.display", "Condition.code.coding:b")
	})

	t.Run("forEach siblings with ids without path or slice separator stay cross-joined", func(t *testing.T) {
		lookup := newLookupTable("https://example.com/Condition", "Condition", map[string]models.LookupElement{
			"code": {
				ViewDefinition: models.ViewDefSnippet{
					ForEachOrNull: "code.coding",
					Column:        []models.ColumnDefinition{{Name: "code", Path: "code"}},
				},
			},
			"category": {
				ViewDefinition: models.ViewDefSnippet{
					ForEachOrNull: "category.coding",
					Column:        []models.ColumnDefinition{{Name: "category", Path: "code"}},
				},
			},
		})

		assertGroupSelectsJSON(t, lookup, `[
			{"forEachOrNull": "code.coding", "column": [{"name": "code", "path": "code"}]},
			{"forEachOrNull": "category.coding", "column": [{"name": "category", "path": "code"}]}
		]`, "code", "category")
	})

	t.Run("slices of different elements form separate unions and keep their order", func(t *testing.T) {
		lookup := newLookupTable("https://example.com/Condition", "Condition", map[string]models.LookupElement{
			"Condition.code.coding:a":     codingSliceElement("", "s='a'", "col_a"),
			"Condition.category.coding:x": codingSliceElement("", "s='x'", "col_x"),
			"Condition.code.coding:b":     codingSliceElement("", "s='b'", "col_b"),
			"Condition.category.coding:y": codingSliceElement("", "s='y'", "col_y"),
		})

		assertGroupSelectsJSON(t, lookup, `[
			{"unionAll": [
				{
					"forEach": "$this.where((coding.where(s='a')).empty() and (coding.where(s='b')).empty())",
					"select": [
						{"forEachOrNull": "coding.where(s='a')", "column": [{"name": "col_a", "path": "code", "type": "string"}]},
						{"forEachOrNull": "coding.where(s='b')", "column": [{"name": "col_b", "path": "code", "type": "string"}]}
					]
				},
				{"forEach": "coding.where(s='a')", "select": [
					{"column": [{"name": "col_a", "path": "code", "type": "string"}]},
					{"column": [{"name": "col_b", "path": "{}", "type": "string"}]}
				]},
				{"forEach": "coding.where(s='b')", "select": [
					{"column": [{"name": "col_a", "path": "{}", "type": "string"}]},
					{"column": [{"name": "col_b", "path": "code", "type": "string"}]}
				]}
			]},
			{"unionAll": [
				{
					"forEach": "$this.where((coding.where(s='x')).empty() and (coding.where(s='y')).empty())",
					"select": [
						{"forEachOrNull": "coding.where(s='x')", "column": [{"name": "col_x", "path": "code", "type": "string"}]},
						{"forEachOrNull": "coding.where(s='y')", "column": [{"name": "col_y", "path": "code", "type": "string"}]}
					]
				},
				{"forEach": "coding.where(s='x')", "select": [
					{"column": [{"name": "col_x", "path": "code", "type": "string"}]},
					{"column": [{"name": "col_y", "path": "{}", "type": "string"}]}
				]},
				{"forEach": "coding.where(s='y')", "select": [
					{"column": [{"name": "col_x", "path": "{}", "type": "string"}]},
					{"column": [{"name": "col_y", "path": "code", "type": "string"}]}
				]}
			]}
		]`, "Condition.code.coding:a", "Condition.category.coding:x", "Condition.code.coding:b", "Condition.category.coding:y")
	})

	t.Run("a slice without forEach passes through and one forEach slice is not combined", func(t *testing.T) {
		lookup := newLookupTable("https://example.com/Condition", "Condition", map[string]models.LookupElement{
			"Condition.code": {
				Children:       []string{"Condition.code.coding:a", "Condition.code.coding:b"},
				ViewDefinition: models.ViewDefSnippet{ForEachOrNull: "code"},
			},
			"Condition.code.coding:a": {
				Parent:         "Condition.code",
				ViewDefinition: newViewDefSnippet(newSelectClause("col_a", "coding.code")),
			},
			"Condition.code.coding:b": codingSliceElement("Condition.code", "s='b'", "col_b"),
		})

		assertGroupSelectsJSON(t, lookup, `[{
			"forEachOrNull": "code",
			"select": [
				{"column": [{"name": "col_a", "path": "coding.code"}]},
				{"forEachOrNull": "coding.where(s='b')", "column": [{"name": "col_b", "path": "code", "type": "string"}]}
			]
		}]`, "Condition.code")
	})

	t.Run("padding of a slice with nested selects lists all its nested columns", func(t *testing.T) {
		lookup := newLookupTable("https://example.com/Condition", "Condition", map[string]models.LookupElement{
			"Condition.code": {
				Children:       []string{"Condition.code.coding:a", "Condition.code.coding:b"},
				ViewDefinition: models.ViewDefSnippet{ForEachOrNull: "code"},
			},
			"Condition.code.coding:a": {
				Parent:   "Condition.code",
				Children: []string{"Condition.code.coding:a.ext"},
				ViewDefinition: models.ViewDefSnippet{
					ForEachOrNull: "coding.where(s='a')",
					Column:        []models.ColumnDefinition{{Name: "col_a", Path: "code", Type: "string"}},
				},
			},
			"Condition.code.coding:a.ext": {
				Parent: "Condition.code.coding:a",
				ViewDefinition: models.ViewDefSnippet{
					ForEach: "extension",
					Column:  []models.ColumnDefinition{{Name: "col_ext", Path: "url", Type: "uri"}},
				},
			},
			"Condition.code.coding:b": codingSliceElement("Condition.code", "s='b'", "col_b"),
		})

		assertGroupSelectsJSON(t, lookup, `[{
			"forEachOrNull": "code",
			"select": [{"unionAll": [
				{
					"forEach": "$this.where((coding.where(s='a')).empty() and (coding.where(s='b')).empty())",
					"select": [
						{
							"forEachOrNull": "coding.where(s='a')",
							"column": [{"name": "col_a", "path": "code", "type": "string"}],
							"select": [{"forEach": "extension", "column": [{"name": "col_ext", "path": "url", "type": "uri"}]}]
						},
						{"forEachOrNull": "coding.where(s='b')", "column": [{"name": "col_b", "path": "code", "type": "string"}]}
					]
				},
				{"forEach": "coding.where(s='a')", "select": [
					{
						"column": [{"name": "col_a", "path": "code", "type": "string"}],
						"select": [{"forEach": "extension", "column": [{"name": "col_ext", "path": "url", "type": "uri"}]}]
					},
					{"column": [{"name": "col_b", "path": "{}", "type": "string"}]}
				]},
				{"forEach": "coding.where(s='b')", "select": [
					{"column": [{"name": "col_a", "path": "{}", "type": "string"}, {"name": "col_ext", "path": "{}", "type": "uri"}]},
					{"column": [{"name": "col_b", "path": "code", "type": "string"}]}
				]}
			]}]
		}]`, "Condition.code")
	})

	t.Run("the build does not write into the lookup elements", func(t *testing.T) {
		a := codingSliceElement("Condition.code", "s='a'", "col_a")
		b := codingSliceElement("Condition.code", "s='b'", "col_b")
		lookup := newLookupTable("https://example.com/Condition", "Condition", map[string]models.LookupElement{
			"Condition.code":          {Children: []string{"Condition.code.coding:a", "Condition.code.coding:b"}, ViewDefinition: models.ViewDefSnippet{ForEachOrNull: "code"}},
			"Condition.code.coding:a": a,
			"Condition.code.coding:b": b,
		})

		viewDef := buildAndAssertViewDef(t, []models.LookupTable{lookup}, newAttributeGroup("G", lookup.URL, "Condition.code"))
		require.NotEmpty(t, viewDef.Select)

		assert.Equal(t, "coding.where(s='a')", a.ViewDefinition.ForEachOrNull)
		assert.Empty(t, a.ViewDefinition.ForEach)
		assert.Equal(t, "coding.where(s='b')", b.ViewDefinition.ForEachOrNull)
		assert.Empty(t, b.ViewDefinition.ForEach)
	})
}

func TestSelectClauseUnionAllJSON(t *testing.T) {
	t.Run("a clause with UnionAll marshals to the key unionAll", func(t *testing.T) {
		data, err := json.Marshal(models.SelectClause{UnionAll: []models.SelectClause{newSelectClause("a", "a")}})
		require.NoError(t, err)
		assert.JSONEq(t, `{"unionAll": [{"column": [{"name": "a", "path": "a"}]}]}`, string(data))
	})

	t.Run("a clause without UnionAll has no unionAll key", func(t *testing.T) {
		data, err := json.Marshal(newSelectClause("a", "a"))
		require.NoError(t, err)
		assert.NotContains(t, string(data), "unionAll")
	})
}
