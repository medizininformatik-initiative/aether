package unit

import (
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

// TestResolveGrandchildren tests the resolveGrandchildren function specifically
func TestResolveGrandchildren(t *testing.T) {
	t.Run("grandchild with forEach gets converted to select clause", func(t *testing.T) {
		// Setup: parent -> child (with forEach) -> grandchild
		// This specifically tests resolveGrandchildren logic
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
		// Test resolveGrandchildren path where child has no root forEach
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
		childForEachPaths := make(map[string]bool)
		for _, childSel := range codeSelectClause.Select {
			if childSel.ForEachOrNull != "" {
				childForEachPaths[childSel.ForEachOrNull] = true
			}
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

// TestCloneSelectClause tests the cloneSelectClause function
func TestCloneSelectClause(t *testing.T) {
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

	t.Run("deep cloning of nested select clauses", func(t *testing.T) {
		// Test that cloneSelectClause properly deep clones nested structures
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

		// Check that children have their forEachOrNull preserved
		var foundSct, foundIcd10 bool
		for _, childSel := range codeSelectClause.Select {
			if childSel.ForEachOrNull == "coding.where(system = 'http://snomed.info/sct')" {
				foundSct = true
				// Verify the child has the correct column
				require.NotEmpty(t, childSel.Select, "sct child should have select with columns")
			}
			if childSel.ForEachOrNull == "coding.where(system = 'http://fhir.de/CodeSystem/bfarm/icd-10-gm')" {
				foundIcd10 = true
				require.NotEmpty(t, childSel.Select, "icd10 child should have select with columns")
			}
		}

		assert.True(t, foundSct, "Should have child with forEachOrNull for SNOMED CT")
		assert.True(t, foundIcd10, "Should have child with forEachOrNull for ICD-10-GM")

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

		assertedDateCount := 0
		relatedCount := 0
		for _, sel := range viewDef.Select {
			fe := sel.ForEach
			if fe == "" {
				fe = sel.ForEachOrNull
			}
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
