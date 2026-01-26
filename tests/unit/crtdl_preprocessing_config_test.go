package unit

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/models"
)

// --- CRTDLPreprocessingConfig.Validate Tests ---

func TestCRTDLPreprocessingConfig_Validate_Disabled(t *testing.T) {
	config := models.CRTDLPreprocessingConfig{
		Enabled: false,
	}

	err := config.Validate()

	assert.NoError(t, err, "disabled config should not require validation")
}

func TestCRTDLPreprocessingConfig_Validate_BothPathAndInline(t *testing.T) {
	config := models.CRTDLPreprocessingConfig{
		Enabled:         true,
		EnrichmentsPath: "/path/to/enrichments.json",
		Enrichments: []models.GroupEnrichment{
			{
				GroupReference: "https://example.org/Patient",
				AttributesToAdd: []models.EnrichmentAttribute{
					{AttributeRef: "Patient.id", MustHave: true},
				},
			},
		},
	}

	err := config.Validate()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot specify both enrichments_path and enrichments")
}

func TestCRTDLPreprocessingConfig_Validate_NoEnrichments(t *testing.T) {
	config := models.CRTDLPreprocessingConfig{
		Enabled: true,
	}

	err := config.Validate()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "enabled but no enrichments configured")
}

func TestCRTDLPreprocessingConfig_Validate_WithPath(t *testing.T) {
	config := models.CRTDLPreprocessingConfig{
		Enabled:         true,
		EnrichmentsPath: "/path/to/enrichments.json",
	}

	err := config.Validate()

	assert.NoError(t, err)
}

func TestCRTDLPreprocessingConfig_Validate_WithInlineEnrichments(t *testing.T) {
	config := models.CRTDLPreprocessingConfig{
		Enabled: true,
		Enrichments: []models.GroupEnrichment{
			{
				GroupReference: "https://example.org/Patient",
				AttributesToAdd: []models.EnrichmentAttribute{
					{AttributeRef: "Patient.id", MustHave: true},
				},
			},
		},
	}

	err := config.Validate()

	assert.NoError(t, err)
}

func TestCRTDLPreprocessingConfig_Validate_InvalidInlineEnrichment(t *testing.T) {
	config := models.CRTDLPreprocessingConfig{
		Enabled: true,
		Enrichments: []models.GroupEnrichment{
			{
				GroupReference:  "", // Invalid: empty groupReference
				AttributesToAdd: []models.EnrichmentAttribute{},
			},
		},
	}

	err := config.Validate()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "enrichments[0]")
	assert.Contains(t, err.Error(), "groupReference is required")
}

// --- GroupEnrichment.Validate Tests ---

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

func TestGroupEnrichment_Validate_CreateIfNotExists_MissingGroupName(t *testing.T) {
	enrichment := models.GroupEnrichment{
		GroupReference:    "https://example.org/Patient",
		CreateIfNotExists: &models.CreateGroupConfig{GroupName: ""}, // Invalid: empty GroupName
		AttributesToAdd: []models.EnrichmentAttribute{
			{AttributeRef: "Patient.id", MustHave: true},
		},
	}

	err := enrichment.Validate()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "groupName is required in createIfNotExists")
}

func TestGroupEnrichment_Validate_EmptyAttributes(t *testing.T) {
	enrichment := models.GroupEnrichment{
		GroupReference:  "https://example.org/Patient",
		AttributesToAdd: []models.EnrichmentAttribute{},
	}

	err := enrichment.Validate()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "attributesToAdd must contain at least one attribute")
}

func TestGroupEnrichment_Validate_InvalidAttribute(t *testing.T) {
	enrichment := models.GroupEnrichment{
		GroupReference: "https://example.org/Patient",
		AttributesToAdd: []models.EnrichmentAttribute{
			{AttributeRef: "", MustHave: true}, // Invalid: empty attributeRef
		},
	}

	err := enrichment.Validate()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "attributesToAdd[0]")
	assert.Contains(t, err.Error(), "attributeRef is required")
}

func TestGroupEnrichment_Validate_Valid(t *testing.T) {
	enrichment := models.GroupEnrichment{
		GroupReference: "https://example.org/Patient",
		AttributesToAdd: []models.EnrichmentAttribute{
			{AttributeRef: "Patient.id", MustHave: true},
		},
	}

	err := enrichment.Validate()

	assert.NoError(t, err)
}

func TestGroupEnrichment_Validate_ValidWithCreateIfNotExists(t *testing.T) {
	enrichment := models.GroupEnrichment{
		GroupReference:    "https://example.org/Patient",
		CreateIfNotExists: &models.CreateGroupConfig{GroupName: "Patient"},
		AttributesToAdd: []models.EnrichmentAttribute{
			{AttributeRef: "Patient.id", MustHave: true},
		},
	}

	err := enrichment.Validate()

	assert.NoError(t, err)
}

// --- EnrichmentAttribute.Validate Tests ---

func TestEnrichmentAttribute_Validate_MissingAttributeRef(t *testing.T) {
	attr := models.EnrichmentAttribute{
		AttributeRef: "",
		MustHave:     true,
	}

	err := attr.Validate()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "attributeRef is required")
}

func TestEnrichmentAttribute_Validate_Valid(t *testing.T) {
	attr := models.EnrichmentAttribute{
		AttributeRef: "Patient.id",
		MustHave:     true,
	}

	err := attr.Validate()

	assert.NoError(t, err)
}

func TestEnrichmentAttribute_Validate_ValidWithLinkedGroups(t *testing.T) {
	attr := models.EnrichmentAttribute{
		AttributeRef: "Patient.managingOrganization",
		MustHave:     false,
		LinkedGroups: []string{"organization-group"},
	}

	err := attr.Validate()

	assert.NoError(t, err)
}

// --- DefaultCRTDLPreprocessingConfig Tests ---

func TestDefaultCRTDLPreprocessingConfig(t *testing.T) {
	config := models.DefaultCRTDLPreprocessingConfig()

	assert.False(t, config.Enabled, "default should be disabled")
	assert.Empty(t, config.EnrichmentsPath)
	assert.Nil(t, config.Enrichments)
}
