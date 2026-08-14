package unit

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

func TestParseCRTDL(t *testing.T) {
	t.Run("valid CRTDL", func(t *testing.T) {
		doc, err := services.ParseCRTDL(validCRTDLFixture)
		require.NoError(t, err)
		require.NotNil(t, doc)
		require.Len(t, doc.DataExtraction.AttributeGroups, 2)
		assert.Equal(t, "1234", doc.DataExtraction.AttributeGroups[0].ID)
		assert.Equal(t, "TyphusDiagnosis", doc.DataExtraction.AttributeGroups[0].Name)
		assert.NotEmpty(t, doc.DataExtraction.AttributeGroups[0].GroupReference)
		assert.Len(t, doc.DataExtraction.AttributeGroups[0].Attributes, 2)
	})

	t.Run("file not found", func(t *testing.T) {
		_, err := services.ParseCRTDL("/nonexistent/path/test.json")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to read CRTDL file")
	})

	t.Run("invalid JSON", func(t *testing.T) {
		tempDir := t.TempDir()
		crtdlPath := filepath.Join(tempDir, "test.json")
		err := os.WriteFile(crtdlPath, []byte("not valid json"), 0644)
		require.NoError(t, err)

		_, err = services.ParseCRTDL(crtdlPath)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "malformed-json")
	})

	t.Run("invalid document", func(t *testing.T) {
		tempDir := t.TempDir()
		crtdlPath := filepath.Join(tempDir, "test.json")
		crtdlJSON := `{"dataExtraction": {"attributeGroups": []}}`
		err := os.WriteFile(crtdlPath, []byte(crtdlJSON), 0644)
		require.NoError(t, err)

		_, err = services.ParseCRTDL(crtdlPath)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "schema-violation")
	})
}

func TestGetAttributeGroups(t *testing.T) {
	doc := &models.CRTDLDocument{
		DataExtraction: models.DataExtraction{
			AttributeGroups: []models.AttributeGroup{
				{Name: "Group1"},
				{Name: "Group2"},
			},
		},
	}

	groups := services.GetAttributeGroups(doc)
	assert.Len(t, groups, 2)
	assert.Equal(t, "Group1", groups[0].Name)
	assert.Equal(t, "Group2", groups[1].Name)
}

func TestGetAttributeGroups_NilDoc(t *testing.T) {
	groups := services.GetAttributeGroups(nil)
	assert.Nil(t, groups)
}

func TestGetAttributeGroupByName(t *testing.T) {
	doc := &models.CRTDLDocument{
		DataExtraction: models.DataExtraction{
			AttributeGroups: []models.AttributeGroup{
				{Name: "Patients", GroupReference: "https://example.com/Patient"},
				{Name: "Conditions", GroupReference: "https://example.com/Condition"},
			},
		},
	}

	t.Run("found", func(t *testing.T) {
		group := services.GetAttributeGroupByName(doc, "Patients")
		require.NotNil(t, group)
		assert.Equal(t, "https://example.com/Patient", group.GroupReference)
	})

	t.Run("not found", func(t *testing.T) {
		group := services.GetAttributeGroupByName(doc, "Unknown")
		assert.Nil(t, group)
	})

	t.Run("nil doc", func(t *testing.T) {
		group := services.GetAttributeGroupByName(nil, "Patients")
		assert.Nil(t, group)
	})
}

func TestGetAttributeGroupByReference(t *testing.T) {
	doc := &models.CRTDLDocument{
		DataExtraction: models.DataExtraction{
			AttributeGroups: []models.AttributeGroup{
				{Name: "Patients", GroupReference: "https://example.com/Patient"},
				{Name: "Conditions", GroupReference: "https://example.com/Condition"},
			},
		},
	}

	t.Run("found", func(t *testing.T) {
		group := services.GetAttributeGroupByReference(doc, "https://example.com/Patient")
		require.NotNil(t, group)
		assert.Equal(t, "Patients", group.Name)
	})

	t.Run("not found", func(t *testing.T) {
		group := services.GetAttributeGroupByReference(doc, "https://example.com/Unknown")
		assert.Nil(t, group)
	})
}

func TestGetMustHaveAttributes(t *testing.T) {
	group := &models.AttributeGroup{
		Attributes: []models.Attribute{
			{AttributeRef: "Patient.id", MustHave: true},
			{AttributeRef: "Patient.name", MustHave: false},
			{AttributeRef: "Patient.birthDate", MustHave: true},
		},
	}

	mustHave := services.GetMustHaveAttributes(group)
	assert.Len(t, mustHave, 2)
	assert.Equal(t, "Patient.id", mustHave[0].AttributeRef)
	assert.Equal(t, "Patient.birthDate", mustHave[1].AttributeRef)
}

func TestGetMustHaveAttributes_NilGroup(t *testing.T) {
	mustHave := services.GetMustHaveAttributes(nil)
	assert.Nil(t, mustHave)
}

func TestIsCRTDLFile(t *testing.T) {
	tests := []struct {
		path     string
		expected bool
	}{
		{"/path/to/file.json", true},
		{"crtdl.json", true},
		{"/path/to/file.jso", false},
		{".json", false},
		{"", false},
		{"json", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := services.IsCRTDLFile(tt.path)
			assert.Equal(t, tt.expected, result)
		})
	}
}
