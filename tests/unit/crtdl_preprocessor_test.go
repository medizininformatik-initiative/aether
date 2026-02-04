package unit

import (
	"encoding/json"
	"testing"

	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test camelCase validation

func TestGroupEnrichment_UnmarshalJSON_SnakeCaseReturnsError(t *testing.T) {
	jsonData := `{
		"group_reference": "https://example.com/Patient",
		"create_if_not_exists": {"group_name": "Patient"},
		"attributes_to_add": [{"attribute_ref": "Patient.id", "must_have": true}]
	}`

	var enrichment models.GroupEnrichment
	err := json.Unmarshal([]byte(jsonData), &enrichment)

	require.Error(t, err)
	// Map iteration order is non-deterministic, so we check for any snake_case field
	assert.Contains(t, err.Error(), "invalid field name")
	assert.Contains(t, err.Error(), "_") // snake_case uses underscores
	assert.Contains(t, err.Error(), "JSON enrichment files must use camelCase")
}

func TestGroupEnrichment_UnmarshalJSON_PascalCaseReturnsError(t *testing.T) {
	jsonData := `{
		"GroupReference": "https://example.com/Patient",
		"attributesToAdd": [{"attributeRef": "Patient.id", "mustHave": true}]
	}`

	var enrichment models.GroupEnrichment
	err := json.Unmarshal([]byte(jsonData), &enrichment)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid field name 'GroupReference'")
	assert.Contains(t, err.Error(), "JSON enrichment files must use camelCase")
}

func TestGroupEnrichment_UnmarshalJSON_KebabCaseReturnsError(t *testing.T) {
	jsonData := `{
		"group-reference": "https://example.com/Patient",
		"attributes-to-add": [{"attribute-ref": "Patient.id", "must-have": true}]
	}`

	var enrichment models.GroupEnrichment
	err := json.Unmarshal([]byte(jsonData), &enrichment)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid field name 'group-reference'")
	assert.Contains(t, err.Error(), "JSON enrichment files must use camelCase")
}

func TestGroupEnrichment_UnmarshalJSON_CamelCaseWorks(t *testing.T) {
	jsonData := `{
		"groupReference": "https://example.com/Patient",
		"createIfNotExists": {"groupName": "Patient"},
		"attributesToAdd": [{"attributeRef": "Patient.id", "mustHave": true}]
	}`

	var enrichment models.GroupEnrichment
	err := json.Unmarshal([]byte(jsonData), &enrichment)

	require.NoError(t, err)
	assert.Equal(t, "https://example.com/Patient", enrichment.GroupReference)
	assert.NotNil(t, enrichment.CreateIfNotExists)
	assert.Equal(t, "Patient", enrichment.CreateIfNotExists.GroupName)
	assert.Len(t, enrichment.AttributesToAdd, 1)
	assert.Equal(t, "Patient.id", enrichment.AttributesToAdd[0].AttributeRef)
	assert.True(t, enrichment.AttributesToAdd[0].MustHave)
}

func TestCreateGroupConfig_UnmarshalJSON_SnakeCaseReturnsError(t *testing.T) {
	jsonData := `{
		"groupReference": "https://example.com/Patient",
		"createIfNotExists": {"group_name": "Patient"},
		"attributesToAdd": [{"attributeRef": "Patient.id", "mustHave": true}]
	}`

	var enrichment models.GroupEnrichment
	err := json.Unmarshal([]byte(jsonData), &enrichment)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid field name 'group_name'")
	assert.Contains(t, err.Error(), "createIfNotExists")
}

func TestEnrichmentAttribute_UnmarshalJSON_SnakeCaseReturnsError(t *testing.T) {
	jsonData := `{
		"groupReference": "https://example.com/Patient",
		"attributesToAdd": [{"attribute_ref": "Patient.id", "must_have": true}]
	}`

	var enrichment models.GroupEnrichment
	err := json.Unmarshal([]byte(jsonData), &enrichment)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid field name 'attribute_ref'")
	assert.Contains(t, err.Error(), "EnrichmentAttribute")
}

// Test validation

func TestGroupEnrichment_Validate_MissingGroupReference(t *testing.T) {
	enrichment := models.GroupEnrichment{
		GroupReference: "",
		AttributesToAdd: []models.EnrichmentAttribute{
			{AttributeRef: "Patient.id", MustHave: true},
		},
	}

	err := enrichment.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "groupReference is required")
}

func TestGroupEnrichment_Validate_MissingGroupNameInCreateIfNotExists(t *testing.T) {
	enrichment := models.GroupEnrichment{
		GroupReference:    "https://example.com/Patient",
		CreateIfNotExists: &models.CreateGroupConfig{GroupName: ""},
		AttributesToAdd: []models.EnrichmentAttribute{
			{AttributeRef: "Patient.id", MustHave: true},
		},
	}

	err := enrichment.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "groupName is required in createIfNotExists")
}

func TestGroupEnrichment_Validate_EmptyAttributesToAdd(t *testing.T) {
	enrichment := models.GroupEnrichment{
		GroupReference:  "https://example.com/Patient",
		AttributesToAdd: []models.EnrichmentAttribute{},
	}

	err := enrichment.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "attributesToAdd must contain at least one attribute")
}

func TestGroupEnrichment_Validate_InvalidAttribute(t *testing.T) {
	enrichment := models.GroupEnrichment{
		GroupReference: "https://example.com/Patient",
		AttributesToAdd: []models.EnrichmentAttribute{
			{AttributeRef: "", MustHave: true},
		},
	}

	err := enrichment.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "attributesToAdd[0]")
	assert.Contains(t, err.Error(), "attributeRef is required")
}

func TestGroupEnrichment_Validate_ValidWithCreateIfNotExists(t *testing.T) {
	enrichment := models.GroupEnrichment{
		GroupReference:    "https://example.com/Encounter",
		CreateIfNotExists: &models.CreateGroupConfig{GroupName: "Encounter"},
		AttributesToAdd: []models.EnrichmentAttribute{
			{AttributeRef: "Encounter.class", MustHave: false},
			{AttributeRef: "Encounter.status", MustHave: false},
		},
	}

	err := enrichment.Validate()
	require.NoError(t, err)
}

func TestGroupEnrichment_Validate_ValidWithoutCreateIfNotExists(t *testing.T) {
	enrichment := models.GroupEnrichment{
		GroupReference: "https://example.com/Patient",
		AttributesToAdd: []models.EnrichmentAttribute{
			{AttributeRef: "Patient.identifier", MustHave: true},
		},
	}

	err := enrichment.Validate()
	require.NoError(t, err)
}

// Test helper methods

func TestGroupEnrichment_ShouldCreateIfNotExists(t *testing.T) {
	t.Run("returns true when CreateIfNotExists is set", func(t *testing.T) {
		enrichment := models.GroupEnrichment{
			CreateIfNotExists: &models.CreateGroupConfig{GroupName: "Test"},
		}
		assert.True(t, enrichment.ShouldCreateIfNotExists())
	})

	t.Run("returns false when CreateIfNotExists is nil", func(t *testing.T) {
		enrichment := models.GroupEnrichment{}
		assert.False(t, enrichment.ShouldCreateIfNotExists())
	})
}

func TestGroupEnrichment_GetGroupName(t *testing.T) {
	t.Run("returns group name when CreateIfNotExists is set", func(t *testing.T) {
		enrichment := models.GroupEnrichment{
			CreateIfNotExists: &models.CreateGroupConfig{GroupName: "Encounter"},
		}
		assert.Equal(t, "Encounter", enrichment.GetGroupName())
	})

	t.Run("returns empty string when CreateIfNotExists is nil", func(t *testing.T) {
		enrichment := models.GroupEnrichment{}
		assert.Equal(t, "", enrichment.GetGroupName())
	})
}

// Test EnrichCRTDL function

func TestEnrichCRTDL_NilDocument(t *testing.T) {
	_, err := services.EnrichCRTDL(nil, []models.GroupEnrichment{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "CRTDL document cannot be nil")
}

func TestEnrichCRTDL_AddAttributesToExistingGroup(t *testing.T) {
	doc := &models.CRTDLDocument{
		DataExtraction: models.DataExtraction{
			AttributeGroups: []models.AttributeGroup{
				{
					Name:           "Patient",
					GroupReference: "https://example.com/Patient",
					Attributes: []models.Attribute{
						{AttributeRef: "Patient.id", MustHave: true},
					},
				},
			},
		},
	}

	enrichments := []models.GroupEnrichment{
		{
			GroupReference: "https://example.com/Patient",
			AttributesToAdd: []models.EnrichmentAttribute{
				{AttributeRef: "Patient.identifier", MustHave: true},
				{AttributeRef: "Patient.name", MustHave: false},
			},
		},
	}

	result, err := services.EnrichCRTDL(doc, enrichments)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Original document should be unchanged
	assert.Len(t, doc.DataExtraction.AttributeGroups[0].Attributes, 1)

	// Result should have enriched attributes
	assert.Len(t, result.DataExtraction.AttributeGroups, 1)
	group := result.DataExtraction.AttributeGroups[0]
	assert.Equal(t, "Patient", group.Name)
	assert.Len(t, group.Attributes, 3)
	assert.Equal(t, "Patient.id", group.Attributes[0].AttributeRef)
	assert.Equal(t, "Patient.identifier", group.Attributes[1].AttributeRef)
	assert.Equal(t, "Patient.name", group.Attributes[2].AttributeRef)
}

func TestEnrichCRTDL_CreateNewGroup(t *testing.T) {
	doc := &models.CRTDLDocument{
		DataExtraction: models.DataExtraction{
			AttributeGroups: []models.AttributeGroup{
				{
					Name:           "Patient",
					GroupReference: "https://example.com/Patient",
					Attributes: []models.Attribute{
						{AttributeRef: "Patient.id", MustHave: true},
					},
				},
			},
		},
	}

	enrichments := []models.GroupEnrichment{
		{
			GroupReference:    "https://example.com/Encounter",
			CreateIfNotExists: &models.CreateGroupConfig{GroupName: "Encounter"},
			AttributesToAdd: []models.EnrichmentAttribute{
				{AttributeRef: "Encounter.class", MustHave: false},
				{AttributeRef: "Encounter.status", MustHave: false},
			},
		},
	}

	result, err := services.EnrichCRTDL(doc, enrichments)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Original document should be unchanged
	assert.Len(t, doc.DataExtraction.AttributeGroups, 1)

	// Result should have new group
	assert.Len(t, result.DataExtraction.AttributeGroups, 2)

	newGroup := result.DataExtraction.AttributeGroups[1]
	assert.Equal(t, "Encounter", newGroup.Name)
	assert.Equal(t, "https://example.com/Encounter", newGroup.GroupReference)
	assert.Len(t, newGroup.Attributes, 2)
	assert.Equal(t, "Encounter.class", newGroup.Attributes[0].AttributeRef)
	assert.Equal(t, "Encounter.status", newGroup.Attributes[1].AttributeRef)
	assert.Contains(t, newGroup.ID, "generated-")
}

func TestEnrichCRTDL_SkipsNonExistentGroupWithoutCreateIfNotExists(t *testing.T) {
	doc := &models.CRTDLDocument{
		DataExtraction: models.DataExtraction{
			AttributeGroups: []models.AttributeGroup{
				{
					Name:           "Patient",
					GroupReference: "https://example.com/Patient",
					Attributes: []models.Attribute{
						{AttributeRef: "Patient.id", MustHave: true},
					},
				},
			},
		},
	}

	enrichments := []models.GroupEnrichment{
		{
			GroupReference: "https://example.com/Condition",
			// No CreateIfNotExists - should skip
			AttributesToAdd: []models.EnrichmentAttribute{
				{AttributeRef: "Condition.code", MustHave: true},
			},
		},
	}

	result, err := services.EnrichCRTDL(doc, enrichments)
	require.NoError(t, err)
	require.NotNil(t, result)

	// No new groups should be added
	assert.Len(t, result.DataExtraction.AttributeGroups, 1)
	assert.Equal(t, "Patient", result.DataExtraction.AttributeGroups[0].Name)
}

func TestEnrichCRTDL_DoesNotAddDuplicateAttributes(t *testing.T) {
	doc := &models.CRTDLDocument{
		DataExtraction: models.DataExtraction{
			AttributeGroups: []models.AttributeGroup{
				{
					Name:           "Patient",
					GroupReference: "https://example.com/Patient",
					Attributes: []models.Attribute{
						{AttributeRef: "Patient.id", MustHave: true},
					},
				},
			},
		},
	}

	enrichments := []models.GroupEnrichment{
		{
			GroupReference: "https://example.com/Patient",
			AttributesToAdd: []models.EnrichmentAttribute{
				{AttributeRef: "Patient.id", MustHave: false},    // Duplicate
				{AttributeRef: "Patient.name", MustHave: false}, // New
			},
		},
	}

	result, err := services.EnrichCRTDL(doc, enrichments)
	require.NoError(t, err)

	// Should have 2 attributes, not 3 (Patient.id not duplicated)
	assert.Len(t, result.DataExtraction.AttributeGroups[0].Attributes, 2)
	assert.Equal(t, "Patient.id", result.DataExtraction.AttributeGroups[0].Attributes[0].AttributeRef)
	assert.Equal(t, "Patient.name", result.DataExtraction.AttributeGroups[0].Attributes[1].AttributeRef)
}

func TestEnrichCRTDL_InvalidEnrichmentReturnsError(t *testing.T) {
	doc := &models.CRTDLDocument{
		DataExtraction: models.DataExtraction{
			AttributeGroups: []models.AttributeGroup{},
		},
	}

	enrichments := []models.GroupEnrichment{
		{
			GroupReference:  "",  // Invalid - empty
			AttributesToAdd: []models.EnrichmentAttribute{},
		},
	}

	_, err := services.EnrichCRTDL(doc, enrichments)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "enrichment[0]")
}

// Test CreateNewGroup function

func TestCreateNewGroup(t *testing.T) {
	enrichment := models.GroupEnrichment{
		GroupReference:    "https://example.com/Observation",
		CreateIfNotExists: &models.CreateGroupConfig{GroupName: "Observation"},
		AttributesToAdd: []models.EnrichmentAttribute{
			{AttributeRef: "Observation.code", MustHave: true},
			{AttributeRef: "Observation.value", MustHave: false},
		},
	}

	group := services.CreateNewGroup(enrichment)

	assert.Equal(t, "Observation", group.Name)
	assert.Equal(t, "https://example.com/Observation", group.GroupReference)
	assert.Contains(t, group.ID, "generated-")
	assert.Len(t, group.Attributes, 2)
	assert.Equal(t, "Observation.code", group.Attributes[0].AttributeRef)
	assert.True(t, group.Attributes[0].MustHave)
	assert.Equal(t, "Observation.value", group.Attributes[1].AttributeRef)
	assert.False(t, group.Attributes[1].MustHave)
}

// Test CRTDLPreprocessingConfig

func TestCRTDLPreprocessingConfig_Validate_WhenDisabled(t *testing.T) {
	config := models.CRTDLPreprocessingConfig{
		Enabled:     false,
		Enrichments: []models.GroupEnrichment{},
	}

	err := config.Validate()
	require.NoError(t, err)
}

func TestCRTDLPreprocessingConfig_Validate_InvalidEnrichment(t *testing.T) {
	config := models.CRTDLPreprocessingConfig{
		Enabled: true,
		Enrichments: []models.GroupEnrichment{
			{
				GroupReference:  "",
				AttributesToAdd: []models.EnrichmentAttribute{},
			},
		},
	}

	err := config.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "enrichments[0]")
}

// Test full JSON parsing example from plan verification

func TestEnrichCRTDL_FullExampleFromPlan(t *testing.T) {
	// This is the exact JSON structure from the plan verification section
	jsonData := `[
		{
			"groupReference": "https://www.medizininformatik-initiative.de/fhir/core/modul-fall/StructureDefinition/KontaktGesundheitseinrichtung",
			"createIfNotExists": {
				"groupName": "Encounter"
			},
			"attributesToAdd": [
				{"attributeRef": "Encounter.class", "mustHave": false},
				{"attributeRef": "Encounter.status", "mustHave": false}
			]
		}
	]`

	var enrichments []models.GroupEnrichment
	err := json.Unmarshal([]byte(jsonData), &enrichments)
	require.NoError(t, err)
	require.Len(t, enrichments, 1)

	assert.Equal(t, "https://www.medizininformatik-initiative.de/fhir/core/modul-fall/StructureDefinition/KontaktGesundheitseinrichtung", enrichments[0].GroupReference)
	assert.NotNil(t, enrichments[0].CreateIfNotExists)
	assert.Equal(t, "Encounter", enrichments[0].CreateIfNotExists.GroupName)
	assert.Len(t, enrichments[0].AttributesToAdd, 2)

	// Verify it passes validation
	err = enrichments[0].Validate()
	require.NoError(t, err)

	// Test with a document
	doc := &models.CRTDLDocument{
		DataExtraction: models.DataExtraction{
			AttributeGroups: []models.AttributeGroup{
				{
					Name:           "Patient",
					GroupReference: "https://example.com/Patient",
					Attributes: []models.Attribute{
						{AttributeRef: "Patient.id", MustHave: true},
					},
				},
			},
		},
	}

	result, err := services.EnrichCRTDL(doc, enrichments)
	require.NoError(t, err)
	require.Len(t, result.DataExtraction.AttributeGroups, 2)

	encounterGroup := result.DataExtraction.AttributeGroups[1]
	assert.Equal(t, "Encounter", encounterGroup.Name)
	assert.Len(t, encounterGroup.Attributes, 2)
}
