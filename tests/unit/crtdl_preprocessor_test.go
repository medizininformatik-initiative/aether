package unit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

// --- Test Fixtures ---

// createTestCRTDLDocument creates a sample CRTDL document for testing
func createTestCRTDLDocument() models.CRTDLDocument {
	return models.CRTDLDocument{
		DataExtraction: models.DataExtraction{
			AttributeGroups: []models.AttributeGroup{
				{
					ID:             "patient-group",
					Name:           "Patient",
					GroupReference: "https://www.medizininformatik-initiative.de/fhir/core/modul-person/StructureDefinition/Patient",
					Attributes: []models.Attribute{
						{AttributeRef: "Patient.id", MustHave: true},
						{AttributeRef: "Patient.gender", MustHave: false},
					},
				},
				{
					ID:             "encounter-group",
					Name:           "Encounter",
					GroupReference: "https://www.medizininformatik-initiative.de/fhir/core/modul-fall/StructureDefinition/KontaktGesundheitseinrichtung",
					Attributes: []models.Attribute{
						{AttributeRef: "Encounter.id", MustHave: true},
					},
				},
			},
		},
	}
}

// --- EnrichCRTDL Tests ---

func TestEnrichCRTDL_AddAttributesToExistingGroup(t *testing.T) {
	doc := createTestCRTDLDocument()
	enrichments := []models.GroupEnrichment{
		{
			GroupReference: "https://www.medizininformatik-initiative.de/fhir/core/modul-person/StructureDefinition/Patient",
			AttributesToAdd: []models.EnrichmentAttribute{
				{AttributeRef: "Patient.identifier:PseudonymisierterIdentifier", MustHave: true},
			},
		},
	}

	result, err := services.EnrichCRTDL(doc, enrichments)

	require.NoError(t, err)
	assert.Len(t, result.DataExtraction.AttributeGroups, 2, "should preserve existing groups")

	// Find patient group
	patientGroup := findGroupByID(result, "patient-group")
	require.NotNil(t, patientGroup, "patient group should exist")
	assert.Len(t, patientGroup.Attributes, 3, "should have added one attribute")

	// Verify new attribute was added
	newAttr := findAttributeByRef(patientGroup.Attributes, "Patient.identifier:PseudonymisierterIdentifier")
	require.NotNil(t, newAttr, "new attribute should exist")
	// Enrichment attributes always have mustHave=false to avoid filtering out
	// resources that don't have the enriched attribute (e.g., when test data
	// doesn't have the specific identifier slice)
	assert.False(t, newAttr.MustHave, "new enrichment attribute should have mustHave=false")
}

func TestEnrichCRTDL_UpdateExistingAttribute_MustHave(t *testing.T) {
	doc := createTestCRTDLDocument()
	enrichments := []models.GroupEnrichment{
		{
			GroupReference: "https://www.medizininformatik-initiative.de/fhir/core/modul-person/StructureDefinition/Patient",
			AttributesToAdd: []models.EnrichmentAttribute{
				{AttributeRef: "Patient.gender", MustHave: true}, // Update existing attribute
			},
		},
	}

	result, err := services.EnrichCRTDL(doc, enrichments)

	require.NoError(t, err)

	patientGroup := findGroupByID(result, "patient-group")
	require.NotNil(t, patientGroup)
	assert.Len(t, patientGroup.Attributes, 2, "should not duplicate attribute")

	genderAttr := findAttributeByRef(patientGroup.Attributes, "Patient.gender")
	require.NotNil(t, genderAttr)
	assert.True(t, genderAttr.MustHave, "mustHave should be updated to true")
}

func TestEnrichCRTDL_CreateNewGroup_WhenCreateIfNotExists(t *testing.T) {
	doc := createTestCRTDLDocument()
	enrichments := []models.GroupEnrichment{
		{
			GroupReference:    "https://www.example.org/fhir/StructureDefinition/NewResource",
			CreateIfNotExists: &models.CreateGroupConfig{GroupName: "NewResource"},
			AttributesToAdd: []models.EnrichmentAttribute{
				{AttributeRef: "NewResource.id", MustHave: true},
			},
		},
	}

	result, err := services.EnrichCRTDL(doc, enrichments)

	require.NoError(t, err)
	assert.Len(t, result.DataExtraction.AttributeGroups, 3, "should have added new group")

	// Find new group by groupReference
	var newGroup *models.AttributeGroup
	for i := range result.DataExtraction.AttributeGroups {
		if result.DataExtraction.AttributeGroups[i].GroupReference == "https://www.example.org/fhir/StructureDefinition/NewResource" {
			newGroup = &result.DataExtraction.AttributeGroups[i]
			break
		}
	}

	require.NotNil(t, newGroup, "new group should exist")
	assert.Equal(t, "NewResource", newGroup.Name)
	assert.Len(t, newGroup.Attributes, 1)
}

func TestEnrichCRTDL_SkipNewGroup_WhenCreateIfNotExistsNil(t *testing.T) {
	doc := createTestCRTDLDocument()
	enrichments := []models.GroupEnrichment{
		{
			GroupReference:    "https://www.example.org/fhir/StructureDefinition/NonExistent",
			CreateIfNotExists: nil, // Should not create
			AttributesToAdd: []models.EnrichmentAttribute{
				{AttributeRef: "NonExistent.id", MustHave: true},
			},
		},
	}

	result, err := services.EnrichCRTDL(doc, enrichments)

	require.NoError(t, err)
	assert.Len(t, result.DataExtraction.AttributeGroups, 2, "should NOT have added new group")
}

func TestEnrichCRTDL_ResolveLinkedGroups(t *testing.T) {
	doc := createTestCRTDLDocument()
	enrichments := []models.GroupEnrichment{
		{
			GroupReference: "https://www.medizininformatik-initiative.de/fhir/core/modul-person/StructureDefinition/Patient",
			AttributesToAdd: []models.EnrichmentAttribute{
				{
					AttributeRef: "Patient.someRef",
					MustHave:     true,
					LinkedGroups: []string{
						"https://www.medizininformatik-initiative.de/fhir/core/modul-fall/StructureDefinition/KontaktGesundheitseinrichtung",
					},
				},
			},
		},
	}

	result, err := services.EnrichCRTDL(doc, enrichments)

	require.NoError(t, err)

	patientGroup := findGroupByID(result, "patient-group")
	require.NotNil(t, patientGroup)

	someRefAttr := findAttributeByRef(patientGroup.Attributes, "Patient.someRef")
	require.NotNil(t, someRefAttr, "new attribute should exist")
	require.Len(t, someRefAttr.LinkedGroups, 1, "should have one linked group")
	assert.Equal(t, "encounter-group", someRefAttr.LinkedGroups[0], "linkedGroups should be resolved to group IDs")
}

// TestEnrichCRTDL_ResolveLinkedGroups_ForwardReference verifies that a rule can
// link a group that a later rule creates. Resolution runs after all rules apply,
// so the order of the rules does not change the result.
func TestEnrichCRTDL_ResolveLinkedGroups_ForwardReference(t *testing.T) {
	doc := createTestCRTDLDocument()
	consentProfile := "https://www.example.org/fhir/StructureDefinition/Consent"

	enrichments := []models.GroupEnrichment{
		{
			GroupReference: "https://www.medizininformatik-initiative.de/fhir/core/modul-person/StructureDefinition/Patient",
			AttributesToAdd: []models.EnrichmentAttribute{
				{AttributeRef: "Patient.consent", LinkedGroups: []string{consentProfile}},
			},
		},
		{
			GroupReference:    consentProfile,
			CreateIfNotExists: &models.CreateGroupConfig{GroupName: "Consent"},
			AttributesToAdd: []models.EnrichmentAttribute{
				{AttributeRef: "Consent.id"},
			},
		},
	}

	result, err := services.EnrichCRTDL(doc, enrichments)

	require.NoError(t, err)

	consentGroup := findGroupByReference(result, consentProfile)
	require.NotNil(t, consentGroup, "the second rule should create the Consent group")

	patientGroup := findGroupByID(result, "patient-group")
	require.NotNil(t, patientGroup)
	consentAttr := findAttributeByRef(patientGroup.Attributes, "Patient.consent")
	require.NotNil(t, consentAttr)
	require.Len(t, consentAttr.LinkedGroups, 1)
	assert.Equal(t, consentGroup.ID, consentAttr.LinkedGroups[0],
		"the profile URL should resolve to the id of the group that the later rule creates")
}

// TestEnrichCRTDL_UnresolvedLinkedGroup_ReturnsError verifies that a profile URL
// which no attribute group matches stops the enrichment. Silent removal of the
// entry gives a document that looks correct but has lost the link.
func TestEnrichCRTDL_UnresolvedLinkedGroup_ReturnsError(t *testing.T) {
	doc := createTestCRTDLDocument()
	unknownProfile := "https://www.example.org/fhir/StructureDefinition/Unknown"

	enrichments := []models.GroupEnrichment{
		{
			GroupReference: "https://www.medizininformatik-initiative.de/fhir/core/modul-person/StructureDefinition/Patient",
			AttributesToAdd: []models.EnrichmentAttribute{
				{AttributeRef: "Patient.unknownRef", LinkedGroups: []string{unknownProfile}},
			},
		},
	}

	_, err := services.EnrichCRTDL(doc, enrichments)

	require.Error(t, err)
	assert.Contains(t, err.Error(), unknownProfile, "the error should name the profile URL")
	assert.Contains(t, err.Error(), "Patient.unknownRef", "the error should name the attribute")
	assert.Contains(t, err.Error(), "patient-group", "the error should name the group")
}

func TestEnrichCRTDL_MultipleEnrichments(t *testing.T) {
	doc := createTestCRTDLDocument()
	enrichments := []models.GroupEnrichment{
		{
			GroupReference: "https://www.medizininformatik-initiative.de/fhir/core/modul-person/StructureDefinition/Patient",
			AttributesToAdd: []models.EnrichmentAttribute{
				{AttributeRef: "Patient.identifier:PseudonymisierterIdentifier", MustHave: true},
			},
		},
		{
			GroupReference: "https://www.medizininformatik-initiative.de/fhir/core/modul-fall/StructureDefinition/KontaktGesundheitseinrichtung",
			AttributesToAdd: []models.EnrichmentAttribute{
				{AttributeRef: "Encounter.identifier:Aufnahmenummer", MustHave: true},
			},
		},
	}

	result, err := services.EnrichCRTDL(doc, enrichments)

	require.NoError(t, err)

	patientGroup := findGroupByID(result, "patient-group")
	require.NotNil(t, patientGroup)
	assert.Len(t, patientGroup.Attributes, 3)

	encounterGroup := findGroupByID(result, "encounter-group")
	require.NotNil(t, encounterGroup)
	assert.Len(t, encounterGroup.Attributes, 2)
}

func TestEnrichCRTDL_EmptyEnrichments_ReturnsUnchanged(t *testing.T) {
	doc := createTestCRTDLDocument()
	enrichments := []models.GroupEnrichment{}

	result, err := services.EnrichCRTDL(doc, enrichments)

	require.NoError(t, err)
	assert.Equal(t, len(doc.DataExtraction.AttributeGroups), len(result.DataExtraction.AttributeGroups))
}

func TestEnrichCRTDL_PreservesOriginalDocument(t *testing.T) {
	doc := createTestCRTDLDocument()
	originalPatientAttrCount := len(doc.DataExtraction.AttributeGroups[0].Attributes)

	enrichments := []models.GroupEnrichment{
		{
			GroupReference: "https://www.medizininformatik-initiative.de/fhir/core/modul-person/StructureDefinition/Patient",
			AttributesToAdd: []models.EnrichmentAttribute{
				{AttributeRef: "Patient.newAttr", MustHave: true},
			},
		},
	}

	_, err := services.EnrichCRTDL(doc, enrichments)
	require.NoError(t, err)

	// Original document should be unchanged (immutability)
	assert.Equal(t, originalPatientAttrCount, len(doc.DataExtraction.AttributeGroups[0].Attributes),
		"original document should not be modified")
}

func TestEnrichCRTDL_PreservesFullDocumentStructure(t *testing.T) {
	// Create document with all top-level fields (like real CRTDL files)
	cohortDef := json.RawMessage(`{"version":"test","inclusionCriteria":[]}`)
	doc := models.CRTDLDocument{
		Display:          "Test Display",
		Version:          "http://test.com/schema#",
		CohortDefinition: cohortDef,
		DataExtraction: models.DataExtraction{
			AttributeGroups: []models.AttributeGroup{
				{
					ID:             "test-group",
					Name:           "Test",
					GroupReference: "https://example.org/Patient",
					Attributes: []models.Attribute{
						{AttributeRef: "Patient.id", MustHave: true},
					},
				},
			},
		},
	}

	enrichments := []models.GroupEnrichment{
		{
			GroupReference: "https://example.org/Patient",
			AttributesToAdd: []models.EnrichmentAttribute{
				{AttributeRef: "Patient.newAttr", MustHave: true},
			},
		},
	}

	result, err := services.EnrichCRTDL(doc, enrichments)
	require.NoError(t, err)

	// Verify all top-level fields are preserved
	assert.Equal(t, "Test Display", result.Display, "Display should be preserved")
	assert.Equal(t, "http://test.com/schema#", result.Version, "Version should be preserved")
	assert.NotNil(t, result.CohortDefinition, "CohortDefinition should be preserved")
	assert.JSONEq(t, string(cohortDef), string(result.CohortDefinition), "CohortDefinition content should match")

	// Verify enrichment was applied
	assert.Len(t, result.DataExtraction.AttributeGroups[0].Attributes, 2, "should have added new attribute")
}

// --- ResolveLinkedGroups Tests ---

func TestResolveLinkedGroups_SingleMatch(t *testing.T) {
	doc := createTestCRTDLDocument()
	profileURLs := []string{
		"https://www.medizininformatik-initiative.de/fhir/core/modul-fall/StructureDefinition/KontaktGesundheitseinrichtung",
	}

	result := services.ResolveLinkedGroups(doc, profileURLs)

	assert.Len(t, result, 1)
	assert.Equal(t, "encounter-group", result[0])
}

func TestResolveLinkedGroups_MultipleMatches(t *testing.T) {
	doc := createTestCRTDLDocument()
	profileURLs := []string{
		"https://www.medizininformatik-initiative.de/fhir/core/modul-person/StructureDefinition/Patient",
		"https://www.medizininformatik-initiative.de/fhir/core/modul-fall/StructureDefinition/KontaktGesundheitseinrichtung",
	}

	result := services.ResolveLinkedGroups(doc, profileURLs)

	assert.Len(t, result, 2)
	assert.Contains(t, result, "patient-group")
	assert.Contains(t, result, "encounter-group")
}

// TestResolveLinkedGroups_NoMatch_KeepsURL verifies that an unmatched profile
// URL stays in the result. A dropped URL makes the lost link invisible to the
// caller and to the cross-reference check.
func TestResolveLinkedGroups_NoMatch_KeepsURL(t *testing.T) {
	doc := createTestCRTDLDocument()
	profileURLs := []string{
		"https://www.example.org/fhir/StructureDefinition/NonExistent",
	}

	result := services.ResolveLinkedGroups(doc, profileURLs)

	assert.Equal(t, profileURLs, result, "an unmatched profile URL must stay in place")
}

func TestResolveLinkedGroups_EmptyInput(t *testing.T) {
	doc := createTestCRTDLDocument()

	result := services.ResolveLinkedGroups(doc, []string{})

	assert.Empty(t, result)
}

// --- CreateNewGroup Tests ---

func TestCreateNewGroup_BasicCreation(t *testing.T) {
	enrichment := models.GroupEnrichment{
		GroupReference:    "https://www.example.org/fhir/StructureDefinition/TestResource",
		CreateIfNotExists: &models.CreateGroupConfig{GroupName: "TestResource"},
		AttributesToAdd: []models.EnrichmentAttribute{
			{AttributeRef: "TestResource.id", MustHave: true},
			{AttributeRef: "TestResource.code", MustHave: false},
		},
	}

	result := services.CreateNewGroup(enrichment)

	assert.NotEmpty(t, result.ID, "should generate an ID")
	assert.Equal(t, "TestResource", result.Name)
	assert.Equal(t, "https://www.example.org/fhir/StructureDefinition/TestResource", result.GroupReference)
	assert.Len(t, result.Attributes, 2)
	assert.Equal(t, "TestResource.id", result.Attributes[0].AttributeRef)
	// Enrichment attributes always have mustHave=false regardless of config
	assert.False(t, result.Attributes[0].MustHave, "enrichment attributes should have mustHave=false")
}

// --- AddAttributesToGroup Tests ---

func TestAddAttributesToGroup_AddNew(t *testing.T) {
	group := models.AttributeGroup{
		ID:             "test-group",
		Name:           "Test",
		GroupReference: "https://example.org/Test",
		Attributes: []models.Attribute{
			{AttributeRef: "Test.existing", MustHave: false},
		},
	}
	doc := models.CRTDLDocument{}
	attrs := []models.EnrichmentAttribute{
		{AttributeRef: "Test.new", MustHave: true},
	}

	result := services.AddAttributesToGroup(group, attrs, doc)

	assert.Len(t, result.Attributes, 2)
	newAttr := findAttributeByRef(result.Attributes, "Test.new")
	require.NotNil(t, newAttr)
	// Enrichment attributes always have mustHave=false regardless of config
	assert.False(t, newAttr.MustHave, "enrichment attributes should have mustHave=false")
}

func TestAddAttributesToGroup_UpdateExisting(t *testing.T) {
	group := models.AttributeGroup{
		ID:             "test-group",
		Name:           "Test",
		GroupReference: "https://example.org/Test",
		Attributes: []models.Attribute{
			{AttributeRef: "Test.existing", MustHave: false},
		},
	}
	doc := models.CRTDLDocument{}
	attrs := []models.EnrichmentAttribute{
		{AttributeRef: "Test.existing", MustHave: true}, // Update mustHave
	}

	result := services.AddAttributesToGroup(group, attrs, doc)

	assert.Len(t, result.Attributes, 1, "should not duplicate")
	assert.True(t, result.Attributes[0].MustHave, "mustHave should be updated")
}

// TestAddAttributesToGroup_EnrichmentAttributesAlwaysMustHaveFalse verifies that
// new enrichment attributes always have mustHave=false regardless of config.
// This is critical because mustHave=true would filter out resources that don't have
// the enriched attribute (e.g., when test data doesn't have the specific identifier
// slice like Patient.identifier:PseudonymisierterIdentifier).
func TestAddAttributesToGroup_EnrichmentAttributesAlwaysMustHaveFalse(t *testing.T) {
	group := models.AttributeGroup{
		ID:             "test-group",
		Name:           "Test",
		GroupReference: "https://example.org/Test",
		Attributes: []models.Attribute{
			{AttributeRef: "Test.existing", MustHave: false},
		},
	}
	doc := models.CRTDLDocument{}

	// Even with mustHave=true in the enrichment config...
	attrs := []models.EnrichmentAttribute{
		{AttributeRef: "Test.identifier:SlicedIdentifier", MustHave: true},
	}

	result := services.AddAttributesToGroup(group, attrs, doc)

	assert.Len(t, result.Attributes, 2)
	newAttr := findAttributeByRef(result.Attributes, "Test.identifier:SlicedIdentifier")
	require.NotNil(t, newAttr)
	// ...the resulting attribute should have mustHave=false
	assert.False(t, newAttr.MustHave, "enrichment attributes should always have mustHave=false to avoid filtering")
}

// --- LoadEnrichments Tests ---

func TestLoadEnrichments_FromInlineConfig(t *testing.T) {
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

	result, err := services.LoadEnrichments(config)

	require.NoError(t, err)
	assert.Len(t, result, 1)
	assert.Equal(t, "https://example.org/Patient", result[0].GroupReference)
}

func TestLoadEnrichments_FromFile(t *testing.T) {
	// Create temp file with enrichment rules (must use camelCase in JSON)
	tempDir := t.TempDir()
	enrichmentsFile := filepath.Join(tempDir, "enrichments.json")

	// JSON file must use camelCase field names
	jsonContent := `[{
		"groupReference": "https://example.org/Patient",
		"attributesToAdd": [
			{"attributeRef": "Patient.identifier:Pseudonym", "mustHave": true}
		]
	}]`
	err := os.WriteFile(enrichmentsFile, []byte(jsonContent), 0644)
	require.NoError(t, err)

	config := models.CRTDLPreprocessingConfig{
		Enabled:         true,
		EnrichmentsPath: enrichmentsFile,
	}

	result, err := services.LoadEnrichments(config)

	require.NoError(t, err)
	assert.Len(t, result, 1)
	assert.Equal(t, "https://example.org/Patient", result[0].GroupReference)
}

func TestLoadEnrichments_FileNotFound(t *testing.T) {
	config := models.CRTDLPreprocessingConfig{
		Enabled:         true,
		EnrichmentsPath: "/nonexistent/path/enrichments.json",
	}

	_, err := services.LoadEnrichments(config)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read enrichments file")
}

func TestLoadEnrichments_InvalidJSON(t *testing.T) {
	tempDir := t.TempDir()
	enrichmentsFile := filepath.Join(tempDir, "invalid.json")
	err := os.WriteFile(enrichmentsFile, []byte("not valid json"), 0644)
	require.NoError(t, err)

	config := models.CRTDLPreprocessingConfig{
		Enabled:         true,
		EnrichmentsPath: enrichmentsFile,
	}

	_, err = services.LoadEnrichments(config)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse enrichments file")
}

func TestLoadEnrichments_EmptyConfig(t *testing.T) {
	config := models.CRTDLPreprocessingConfig{
		Enabled: true,
	}

	result, err := services.LoadEnrichments(config)

	require.NoError(t, err)
	assert.Empty(t, result)
}

// --- Additional Coverage Tests ---

// TestAddAttributesToGroup_UpdateExistingLinkedGroups tests updating linkedGroups for existing attributes
// (covers crtdl_preprocessor.go lines 111-113)
func TestAddAttributesToGroup_UpdateExistingLinkedGroups(t *testing.T) {
	doc := createTestCRTDLDocument()
	group := models.AttributeGroup{
		ID:             "test-group",
		Name:           "Test",
		GroupReference: "https://example.org/Test",
		Attributes: []models.Attribute{
			{AttributeRef: "Test.existing", MustHave: false, LinkedGroups: []string{}},
		},
	}

	// Enrichment that updates existing attribute with linkedGroups
	attrs := []models.EnrichmentAttribute{
		{
			AttributeRef: "Test.existing",
			MustHave:     false,
			LinkedGroups: []string{
				"https://www.medizininformatik-initiative.de/fhir/core/modul-fall/StructureDefinition/KontaktGesundheitseinrichtung",
			},
		},
	}

	result := services.AddAttributesToGroup(group, attrs, doc)

	require.Len(t, result.Attributes, 1, "should not duplicate")
	assert.Len(t, result.Attributes[0].LinkedGroups, 1, "should have updated linkedGroups")
	assert.Equal(t, "encounter-group", result.Attributes[0].LinkedGroups[0], "linkedGroups should be resolved to group ID")
}

// TestEnrichCRTDL_UpdateExistingAttributeLinkedGroups tests updating linkedGroups on existing attribute
// (covers crtdl_preprocessor.go lines 111-113 via EnrichCRTDL)
func TestEnrichCRTDL_UpdateExistingAttributeLinkedGroups(t *testing.T) {
	doc := createTestCRTDLDocument()
	// Add an existing attribute with no linkedGroups
	doc.DataExtraction.AttributeGroups[0].Attributes = append(
		doc.DataExtraction.AttributeGroups[0].Attributes,
		models.Attribute{AttributeRef: "Patient.link", MustHave: false, LinkedGroups: []string{}},
	)

	enrichments := []models.GroupEnrichment{
		{
			GroupReference: "https://www.medizininformatik-initiative.de/fhir/core/modul-person/StructureDefinition/Patient",
			AttributesToAdd: []models.EnrichmentAttribute{
				{
					AttributeRef: "Patient.link", // existing attribute
					MustHave:     false,
					LinkedGroups: []string{
						"https://www.medizininformatik-initiative.de/fhir/core/modul-fall/StructureDefinition/KontaktGesundheitseinrichtung",
					},
				},
			},
		},
	}

	result, err := services.EnrichCRTDL(doc, enrichments)

	require.NoError(t, err)
	patientGroup := findGroupByID(result, "patient-group")
	require.NotNil(t, patientGroup)

	linkAttr := findAttributeByRef(patientGroup.Attributes, "Patient.link")
	require.NotNil(t, linkAttr, "attribute should exist")
	require.Len(t, linkAttr.LinkedGroups, 1, "should have one linked group")
	assert.Equal(t, "encounter-group", linkAttr.LinkedGroups[0], "linkedGroups should be resolved to group ID")
}

// TestEnrichCRTDL_PreservesExistingLinkedGroups tests deep copy preserves linkedGroups
// (covers crtdl_preprocessor.go lines 267-270 deepCopyAttribute and 302-305 resolveLinkedGroupsForGroup)
func TestEnrichCRTDL_PreservesExistingLinkedGroups(t *testing.T) {
	// Create document with attributes that already have linkedGroups
	doc := models.CRTDLDocument{
		DataExtraction: models.DataExtraction{
			AttributeGroups: []models.AttributeGroup{
				{
					ID:             "patient-group",
					Name:           "Patient",
					GroupReference: "https://www.medizininformatik-initiative.de/fhir/core/modul-person/StructureDefinition/Patient",
					Attributes: []models.Attribute{
						{
							AttributeRef: "Patient.link",
							MustHave:     true,
							LinkedGroups: []string{"encounter-group"}, // Already has linkedGroups
						},
					},
				},
				{
					ID:             "encounter-group",
					Name:           "Encounter",
					GroupReference: "https://www.medizininformatik-initiative.de/fhir/core/modul-fall/StructureDefinition/KontaktGesundheitseinrichtung",
					Attributes: []models.Attribute{
						{AttributeRef: "Encounter.id", MustHave: true},
					},
				},
			},
		},
	}

	// Add unrelated enrichment to trigger deep copy
	enrichments := []models.GroupEnrichment{
		{
			GroupReference: "https://www.medizininformatik-initiative.de/fhir/core/modul-fall/StructureDefinition/KontaktGesundheitseinrichtung",
			AttributesToAdd: []models.EnrichmentAttribute{
				{AttributeRef: "Encounter.status", MustHave: false},
			},
		},
	}

	result, err := services.EnrichCRTDL(doc, enrichments)

	require.NoError(t, err)

	// Verify original linkedGroups are preserved through deep copy
	patientGroup := findGroupByID(result, "patient-group")
	require.NotNil(t, patientGroup)

	linkAttr := findAttributeByRef(patientGroup.Attributes, "Patient.link")
	require.NotNil(t, linkAttr)
	require.Len(t, linkAttr.LinkedGroups, 1, "linkedGroups should be preserved")
	assert.Equal(t, "encounter-group", linkAttr.LinkedGroups[0])

	// Verify original document is unchanged (immutability)
	assert.Len(t, doc.DataExtraction.AttributeGroups[0].Attributes[0].LinkedGroups, 1,
		"original document should not be modified")
}

// TestEnrichCRTDL_DeepCopyWithMultipleLinkedGroups tests deep copy with multiple linkedGroups
// (covers crtdl_preprocessor.go lines 267-270 deepCopyAttribute with slice copy)
func TestEnrichCRTDL_DeepCopyWithMultipleLinkedGroups(t *testing.T) {
	// Create document with multiple linkedGroups
	doc := models.CRTDLDocument{
		DataExtraction: models.DataExtraction{
			AttributeGroups: []models.AttributeGroup{
				{
					ID:             "main-group",
					Name:           "Main",
					GroupReference: "https://example.org/Main",
					Attributes: []models.Attribute{
						{
							AttributeRef: "Main.reference",
							MustHave:     true,
							LinkedGroups: []string{"group-a", "group-b", "group-c"},
						},
					},
				},
				{
					ID:             "group-a",
					Name:           "GroupA",
					GroupReference: "https://example.org/GroupA",
					Attributes:     []models.Attribute{},
				},
				{
					ID:             "group-b",
					Name:           "GroupB",
					GroupReference: "https://example.org/GroupB",
					Attributes:     []models.Attribute{},
				},
				{
					ID:             "group-c",
					Name:           "GroupC",
					GroupReference: "https://example.org/GroupC",
					Attributes:     []models.Attribute{},
				},
			},
		},
	}

	// Trigger deep copy with any enrichment
	enrichments := []models.GroupEnrichment{
		{
			GroupReference: "https://example.org/GroupA",
			AttributesToAdd: []models.EnrichmentAttribute{
				{AttributeRef: "GroupA.newAttr", MustHave: true},
			},
		},
	}

	result, err := services.EnrichCRTDL(doc, enrichments)

	require.NoError(t, err)

	// Verify all linkedGroups are preserved
	var mainGroup *models.AttributeGroup
	for i := range result.DataExtraction.AttributeGroups {
		if result.DataExtraction.AttributeGroups[i].ID == "main-group" {
			mainGroup = &result.DataExtraction.AttributeGroups[i]
			break
		}
	}
	require.NotNil(t, mainGroup)

	refAttr := findAttributeByRef(mainGroup.Attributes, "Main.reference")
	require.NotNil(t, refAttr)
	require.Len(t, refAttr.LinkedGroups, 3, "all linkedGroups should be preserved")
	assert.Contains(t, refAttr.LinkedGroups, "group-a")
	assert.Contains(t, refAttr.LinkedGroups, "group-b")
	assert.Contains(t, refAttr.LinkedGroups, "group-c")

	// Verify deep copy (modifying result doesn't affect original)
	result.DataExtraction.AttributeGroups[0].Attributes[0].LinkedGroups[0] = "modified"
	assert.Equal(t, "group-a", doc.DataExtraction.AttributeGroups[0].Attributes[0].LinkedGroups[0],
		"original should not be modified (deep copy)")
}

// --- Helper Functions ---

func findGroupByID(doc models.CRTDLDocument, id string) *models.AttributeGroup {
	for i := range doc.DataExtraction.AttributeGroups {
		if doc.DataExtraction.AttributeGroups[i].ID == id {
			return &doc.DataExtraction.AttributeGroups[i]
		}
	}
	return nil
}

func findGroupByReference(doc models.CRTDLDocument, groupReference string) *models.AttributeGroup {
	for i := range doc.DataExtraction.AttributeGroups {
		if doc.DataExtraction.AttributeGroups[i].GroupReference == groupReference {
			return &doc.DataExtraction.AttributeGroups[i]
		}
	}
	return nil
}

func findAttributeByRef(attrs []models.Attribute, ref string) *models.Attribute {
	for i := range attrs {
		if attrs[i].AttributeRef == ref {
			return &attrs[i]
		}
	}
	return nil
}

// --- camelCase Validation Tests ---

func TestGroupEnrichment_UnmarshalJSON_CamelCaseValid(t *testing.T) {
	// Valid camelCase JSON should unmarshal successfully
	jsonData := `{
		"groupReference": "https://example.org/Patient",
		"createIfNotExists": {"groupName": "Patient"},
		"attributesToAdd": [
			{"attributeRef": "Patient.id", "mustHave": true}
		]
	}`

	var enrichment models.GroupEnrichment
	err := json.Unmarshal([]byte(jsonData), &enrichment)

	require.NoError(t, err)
	assert.Equal(t, "https://example.org/Patient", enrichment.GroupReference)
	assert.NotNil(t, enrichment.CreateIfNotExists)
	assert.Equal(t, "Patient", enrichment.CreateIfNotExists.GroupName)
	require.Len(t, enrichment.AttributesToAdd, 1)
	assert.Equal(t, "Patient.id", enrichment.AttributesToAdd[0].AttributeRef)
	assert.True(t, enrichment.AttributesToAdd[0].MustHave)
}

func TestGroupEnrichment_UnmarshalJSON_SnakeCaseRejected(t *testing.T) {
	// snake_case JSON should be rejected with helpful error message
	testCases := []struct {
		name    string
		json    string
		errPart string
	}{
		{
			name: "group_reference snake_case",
			json: `{
				"group_reference": "https://example.org/Patient",
				"attributesToAdd": [{"attributeRef": "Patient.id", "mustHave": true}]
			}`,
			errPart: "group_reference",
		},
		{
			name: "create_if_not_exists snake_case",
			json: `{
				"groupReference": "https://example.org/Patient",
				"create_if_not_exists": {"groupName": "Patient"},
				"attributesToAdd": [{"attributeRef": "Patient.id", "mustHave": true}]
			}`,
			errPart: "create_if_not_exists",
		},
		{
			name: "attributes_to_add snake_case",
			json: `{
				"groupReference": "https://example.org/Patient",
				"attributes_to_add": [{"attributeRef": "Patient.id", "mustHave": true}]
			}`,
			errPart: "attributes_to_add",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var enrichment models.GroupEnrichment
			err := json.Unmarshal([]byte(tc.json), &enrichment)

			require.Error(t, err, "snake_case field should be rejected")
			assert.Contains(t, err.Error(), tc.errPart, "error should mention the invalid field")
			assert.Contains(t, err.Error(), "snake_case", "error should mention snake_case")
			assert.Contains(t, err.Error(), "camelCase", "error should mention camelCase")
		})
	}
}

func TestEnrichmentAttribute_UnmarshalJSON_CamelCaseValid(t *testing.T) {
	// Valid camelCase JSON should unmarshal successfully
	jsonData := `{
		"attributeRef": "Patient.identifier",
		"mustHave": true,
		"linkedGroups": ["group-1", "group-2"]
	}`

	var attr models.EnrichmentAttribute
	err := json.Unmarshal([]byte(jsonData), &attr)

	require.NoError(t, err)
	assert.Equal(t, "Patient.identifier", attr.AttributeRef)
	assert.True(t, attr.MustHave)
	assert.Len(t, attr.LinkedGroups, 2)
}

func TestEnrichmentAttribute_UnmarshalJSON_SnakeCaseRejected(t *testing.T) {
	testCases := []struct {
		name    string
		json    string
		errPart string
	}{
		{
			name:    "attribute_ref snake_case",
			json:    `{"attribute_ref": "Patient.id", "mustHave": true}`,
			errPart: "attribute_ref",
		},
		{
			name:    "must_have snake_case",
			json:    `{"attributeRef": "Patient.id", "must_have": true}`,
			errPart: "must_have",
		},
		{
			name:    "linked_groups snake_case",
			json:    `{"attributeRef": "Patient.id", "mustHave": true, "linked_groups": ["g1"]}`,
			errPart: "linked_groups",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var attr models.EnrichmentAttribute
			err := json.Unmarshal([]byte(tc.json), &attr)

			require.Error(t, err, "snake_case field should be rejected")
			assert.Contains(t, err.Error(), tc.errPart, "error should mention the invalid field")
			assert.Contains(t, err.Error(), "snake_case", "error should mention snake_case")
		})
	}
}

func TestCreateGroupConfig_UnmarshalJSON_CamelCaseValid(t *testing.T) {
	jsonData := `{"groupName": "TestGroup"}`

	var config models.CreateGroupConfig
	err := json.Unmarshal([]byte(jsonData), &config)

	require.NoError(t, err)
	assert.Equal(t, "TestGroup", config.GroupName)
}

func TestCreateGroupConfig_UnmarshalJSON_SnakeCaseRejected(t *testing.T) {
	jsonData := `{"group_name": "TestGroup"}`

	var config models.CreateGroupConfig
	err := json.Unmarshal([]byte(jsonData), &config)

	require.Error(t, err, "snake_case field should be rejected")
	assert.Contains(t, err.Error(), "group_name", "error should mention the invalid field")
	assert.Contains(t, err.Error(), "snake_case", "error should mention snake_case")
}

// TestEnrichCRTDL_AddGroupIfNotExists_BooleanJSON tests that the simpler
// "addGroupIfNotExists": true syntax creates groups when they don't exist in the CRTDL.
// Regression test for the bug where this field was silently ignored.
func TestEnrichCRTDL_AddGroupIfNotExists_BooleanJSON(t *testing.T) {
	enrichmentJSON := `[
		{
			"groupReference": "https://www.medizininformatik-initiative.de/fhir/core/modul-person/StructureDefinition/PatientPseudonymisiert",
			"addGroupIfNotExists": true,
			"attributesToAdd": [
				{
					"attributeRef": "Patient.identifier",
					"mustHave": false
				}
			]
		},
		{
			"groupReference": "https://www.medizininformatik-initiative.de/fhir/core/modul-fall/StructureDefinition/KontaktGesundheitseinrichtung",
			"addGroupIfNotExists": true,
			"attributesToAdd": [
				{
					"attributeRef": "Encounter.identifier",
					"mustHave": false
				}
			]
		}
	]`

	tempDir := t.TempDir()
	enrichmentsFile := filepath.Join(tempDir, "enrichments.json")
	err := os.WriteFile(enrichmentsFile, []byte(enrichmentJSON), 0644)
	require.NoError(t, err)

	config := models.CRTDLPreprocessingConfig{
		Enabled:         true,
		EnrichmentsPath: enrichmentsFile,
	}

	enrichments, err := services.LoadEnrichments(config)
	require.NoError(t, err)
	require.Len(t, enrichments, 2)

	// addGroupIfNotExists: true should populate CreateIfNotExists with a derived group name
	for _, e := range enrichments {
		assert.True(t, e.ShouldCreateIfNotExists(),
			"addGroupIfNotExists: true should enable group creation")
	}

	// Group name should be derived from the last path segment of the profile URL
	assert.Equal(t, "PatientPseudonymisiert", enrichments[0].GetGroupName())
	assert.Equal(t, "KontaktGesundheitseinrichtung", enrichments[1].GetGroupName())

	// Full pipeline: CRTDL that does NOT contain these groups
	doc := models.CRTDLDocument{
		DataExtraction: models.DataExtraction{
			AttributeGroups: []models.AttributeGroup{
				{
					ID:             "observation-group",
					Name:           "Observation",
					GroupReference: "https://example.org/Observation",
					Attributes: []models.Attribute{
						{AttributeRef: "Observation.id", MustHave: true},
					},
				},
			},
		},
	}

	result, err := services.EnrichCRTDL(doc, enrichments)
	require.NoError(t, err)

	assert.Len(t, result.DataExtraction.AttributeGroups, 3,
		"expected 1 original + 2 new groups from addGroupIfNotExists")
}

// TestGroupEnrichment_UnmarshalJSON_AddGroupIfNotExists_False tests that
// addGroupIfNotExists: false does NOT create the group.
func TestGroupEnrichment_UnmarshalJSON_AddGroupIfNotExists_False(t *testing.T) {
	jsonData := `{
		"groupReference": "https://example.org/Patient",
		"addGroupIfNotExists": false,
		"attributesToAdd": [
			{"attributeRef": "Patient.id", "mustHave": true}
		]
	}`

	var enrichment models.GroupEnrichment
	err := json.Unmarshal([]byte(jsonData), &enrichment)

	require.NoError(t, err)
	assert.False(t, enrichment.ShouldCreateIfNotExists(),
		"addGroupIfNotExists: false should not enable group creation")
}

// TestGroupEnrichment_UnmarshalJSON_BothAddGroupAndCreateIfNotExists_Error tests that
// specifying both addGroupIfNotExists and createIfNotExists is an error.
func TestGroupEnrichment_UnmarshalJSON_BothAddGroupAndCreateIfNotExists_Error(t *testing.T) {
	jsonData := `{
		"groupReference": "https://example.org/Patient",
		"addGroupIfNotExists": true,
		"createIfNotExists": {"groupName": "Patient"},
		"attributesToAdd": [
			{"attributeRef": "Patient.id", "mustHave": true}
		]
	}`

	var enrichment models.GroupEnrichment
	err := json.Unmarshal([]byte(jsonData), &enrichment)

	require.Error(t, err, "both addGroupIfNotExists and createIfNotExists should be rejected")
	assert.Contains(t, err.Error(), "addGroupIfNotExists")
	assert.Contains(t, err.Error(), "createIfNotExists")
}

// TestGroupEnrichment_UnmarshalJSON_UnknownFieldRejected tests that truly unknown JSON fields
// are rejected with a clear error, preventing silent data loss.
func TestGroupEnrichment_UnmarshalJSON_UnknownFieldRejected(t *testing.T) {
	testCases := []struct {
		name    string
		json    string
		errPart string
	}{
		{
			name: "typo in field name",
			json: `{
				"groupReference": "https://example.org/Patient",
				"atributesToAdd": [{"attributeRef": "Patient.id", "mustHave": true}]
			}`,
			errPart: "atributesToAdd",
		},
		{
			name: "completely unknown field",
			json: `{
				"groupReference": "https://example.org/Patient",
				"foo": "bar",
				"attributesToAdd": [{"attributeRef": "Patient.id", "mustHave": true}]
			}`,
			errPart: "foo",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var enrichment models.GroupEnrichment
			err := json.Unmarshal([]byte(tc.json), &enrichment)

			require.Error(t, err, "unknown fields should be rejected")
			assert.Contains(t, err.Error(), tc.errPart)
			assert.Contains(t, err.Error(), "unknown")
		})
	}
}

// TestGroupEnrichment_UnmarshalJSON_AddGroupIfNotExists_NonBoolean_Error tests that
// addGroupIfNotExists with a non-boolean value produces a clear error.
func TestGroupEnrichment_UnmarshalJSON_AddGroupIfNotExists_NonBoolean_Error(t *testing.T) {
	jsonData := `{
		"groupReference": "https://example.org/Patient",
		"addGroupIfNotExists": "yes",
		"attributesToAdd": [
			{"attributeRef": "Patient.id", "mustHave": true}
		]
	}`

	var enrichment models.GroupEnrichment
	err := json.Unmarshal([]byte(jsonData), &enrichment)

	require.Error(t, err, "non-boolean addGroupIfNotExists should be rejected")
	assert.Contains(t, err.Error(), "addGroupIfNotExists")
	assert.Contains(t, err.Error(), "boolean")
}

func TestLoadEnrichments_FromFile_SnakeCaseRejected(t *testing.T) {
	// Create temp file with snake_case fields (should be rejected)
	tempDir := t.TempDir()
	enrichmentsFile := filepath.Join(tempDir, "enrichments.json")

	// Use snake_case field names (invalid)
	jsonContent := `[{
		"group_reference": "https://example.org/Patient",
		"attributes_to_add": [
			{"attribute_ref": "Patient.id", "must_have": true}
		]
	}]`
	err := os.WriteFile(enrichmentsFile, []byte(jsonContent), 0644)
	require.NoError(t, err)

	config := models.CRTDLPreprocessingConfig{
		Enabled:         true,
		EnrichmentsPath: enrichmentsFile,
	}

	_, err = services.LoadEnrichments(config)

	require.Error(t, err, "snake_case fields in JSON file should be rejected")
	assert.Contains(t, err.Error(), "snake_case", "error should mention snake_case")
}

// --- Unknown Field Preservation (Issue #356) ---

// TestEnrichCRTDL_PreservesUnknownAttributeGroupField verifies that fields
// not modeled by the AttributeGroup struct (e.g. includeReferenceOnly) survive
// the unmarshal → enrich → marshal round-trip.
func TestEnrichCRTDL_PreservesUnknownAttributeGroupField(t *testing.T) {
	rawCRTDL := `{
		"version": "http://json-schema.org/to-be-done/schema#",
		"display": "",
		"dataExtraction": {
			"attributeGroups": [
				{
					"id": "med-group",
					"name": "Medication",
					"groupReference": "https://example.org/Medication",
					"includeReferenceOnly": true,
					"attributes": [
						{"attributeRef": "Medication.code", "mustHave": false}
					]
				}
			]
		}
	}`

	var doc models.CRTDLDocument
	require.NoError(t, json.Unmarshal([]byte(rawCRTDL), &doc))

	enrichments := []models.GroupEnrichment{
		{
			GroupReference: "https://example.org/Medication",
			AttributesToAdd: []models.EnrichmentAttribute{
				{AttributeRef: "Medication.ingredient", MustHave: false},
			},
		},
	}

	enriched, err := services.EnrichCRTDL(doc, enrichments)
	require.NoError(t, err)

	out, err := json.Marshal(enriched)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, json.Unmarshal(out, &got))
	groups := got["dataExtraction"].(map[string]any)["attributeGroups"].([]any)
	require.Len(t, groups, 1)
	medGroup := groups[0].(map[string]any)
	assert.Equal(t, true, medGroup["includeReferenceOnly"],
		"includeReferenceOnly must be preserved through enrichment")

	attrs := medGroup["attributes"].([]any)
	assert.Len(t, attrs, 2, "enrichment should still have added attribute")
}

// TestEnrichCRTDL_PreservesUnknownAttributeField verifies that unknown fields
// at the Attribute level survive enrichment.
func TestEnrichCRTDL_PreservesUnknownAttributeField(t *testing.T) {
	rawCRTDL := `{
		"dataExtraction": {
			"attributeGroups": [
				{
					"id": "p",
					"name": "Patient",
					"groupReference": "https://example.org/Patient",
					"attributes": [
						{
							"attributeRef": "Patient.id",
							"mustHave": true,
							"customExtension": "preserveMe"
						}
					]
				}
			]
		}
	}`

	var doc models.CRTDLDocument
	require.NoError(t, json.Unmarshal([]byte(rawCRTDL), &doc))

	enriched, err := services.EnrichCRTDL(doc, []models.GroupEnrichment{
		{
			GroupReference: "https://example.org/Patient",
			AttributesToAdd: []models.EnrichmentAttribute{
				{AttributeRef: "Patient.gender", MustHave: false},
			},
		},
	})
	require.NoError(t, err)

	out, err := json.Marshal(enriched)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, json.Unmarshal(out, &got))
	attrs := got["dataExtraction"].(map[string]any)["attributeGroups"].([]any)[0].(map[string]any)["attributes"].([]any)
	var patientID map[string]any
	for _, a := range attrs {
		m := a.(map[string]any)
		if m["attributeRef"] == "Patient.id" {
			patientID = m
			break
		}
	}
	require.NotNil(t, patientID, "Patient.id attribute should exist")
	assert.Equal(t, "preserveMe", patientID["customExtension"],
		"unknown attribute-level field must be preserved")
}

// TestCRTDLDocument_RoundTripPreservesUnknownFieldsAllLevels verifies a plain
// JSON round-trip preserves unknown fields at all four document levels.
func TestCRTDLDocument_RoundTripPreservesUnknownFieldsAllLevels(t *testing.T) {
	raw := `{
		"display": "x",
		"version": "v1",
		"topLevelExtra": "doc-level",
		"dataExtraction": {
			"sectionExtra": "section-level",
			"attributeGroups": [
				{
					"id": "g",
					"name": "G",
					"groupReference": "https://example.org/G",
					"groupExtra": "group-level",
					"includeReferenceOnly": true,
					"attributes": [
						{
							"attributeRef": "G.id",
							"mustHave": true,
							"attrExtra": "attr-level"
						}
					]
				}
			]
		}
	}`

	var doc models.CRTDLDocument
	require.NoError(t, json.Unmarshal([]byte(raw), &doc))

	out, err := json.Marshal(doc)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, json.Unmarshal(out, &got))

	assert.Equal(t, "doc-level", got["topLevelExtra"], "doc-level unknown field preserved")
	de := got["dataExtraction"].(map[string]any)
	assert.Equal(t, "section-level", de["sectionExtra"], "dataExtraction-level unknown field preserved")
	group := de["attributeGroups"].([]any)[0].(map[string]any)
	assert.Equal(t, "group-level", group["groupExtra"], "group-level unknown field preserved")
	assert.Equal(t, true, group["includeReferenceOnly"], "includeReferenceOnly preserved")
	attr := group["attributes"].([]any)[0].(map[string]any)
	assert.Equal(t, "attr-level", attr["attrExtra"], "attribute-level unknown field preserved")
}
