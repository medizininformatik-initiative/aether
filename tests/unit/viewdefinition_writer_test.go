package unit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildViewDefinitionFilename(t *testing.T) {
	tests := []struct {
		name      string
		groupName string
		expected  string
	}{
		{
			name:      "simple name",
			groupName: "Patient",
			expected:  "Patient_viewdefinition.json",
		},
		{
			name:      "name with spaces",
			groupName: "standard pat",
			expected:  "standard pat_viewdefinition.json",
		},
		{
			name:      "name with special characters",
			groupName: "Condition/Diagnosis",
			expected:  "Condition_Diagnosis_viewdefinition.json",
		},
		{
			name:      "complex name",
			groupName: "MII_PR_Person:Patient<Test>",
			expected:  "MII_PR_Person_Patient_Test__viewdefinition.json",
		},
		{
			name:      "empty name",
			groupName: "",
			expected:  "_viewdefinition.json",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := services.BuildViewDefinitionFilename(tc.groupName)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestWriteViewDefinition_Success(t *testing.T) {
	tmpDir := t.TempDir()
	writer := services.NewViewDefinitionWriter(tmpDir)

	viewDef := models.NewBaseViewDefinition("TestGroup", "Patient")
	viewDef.Select = []models.SelectClause{
		{
			Column: []models.ColumnDefinition{
				{Name: "id", Path: "id"},
				{Name: "gender", Path: "gender"},
			},
		},
	}

	filename := services.BuildViewDefinitionFilename("TestGroup")
	err := writer.WriteViewDefinition(filename, viewDef)
	require.NoError(t, err)

	// Verify file exists
	outputPath := filepath.Join(tmpDir, filename)
	assert.FileExists(t, outputPath)

	// Verify content is valid JSON
	data, err := os.ReadFile(outputPath)
	require.NoError(t, err)

	var parsed models.ViewDefinition
	err = json.Unmarshal(data, &parsed)
	require.NoError(t, err)

	assert.Equal(t, "TestGroup", parsed.Name)
	assert.Equal(t, "Patient", parsed.Resource)
	assert.Equal(t, "draft", parsed.Status)
	assert.Len(t, parsed.Select, 1)
	assert.Len(t, parsed.Select[0].Column, 2)
}

func TestWriteViewDefinition_CreatesDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	nestedDir := filepath.Join(tmpDir, "nested", "viewdefinitions")
	writer := services.NewViewDefinitionWriter(nestedDir)

	viewDef := models.NewBaseViewDefinition("Test", "Patient")
	filename := services.BuildViewDefinitionFilename("Test")
	err := writer.WriteViewDefinition(filename, viewDef)
	require.NoError(t, err)

	// Verify directory was created
	assert.DirExists(t, nestedDir)
	assert.FileExists(t, filepath.Join(nestedDir, filename))
}

func TestWriteViewDefinition_PrettyPrintedJSON(t *testing.T) {
	tmpDir := t.TempDir()
	writer := services.NewViewDefinitionWriter(tmpDir)

	viewDef := models.NewBaseViewDefinition("Test", "Patient")
	viewDef.Select = []models.SelectClause{
		{Column: []models.ColumnDefinition{{Name: "id", Path: "id"}}},
	}

	filename := services.BuildViewDefinitionFilename("Test")
	err := writer.WriteViewDefinition(filename, viewDef)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(tmpDir, filename))
	require.NoError(t, err)

	// Check that JSON is indented (not minified)
	content := string(data)
	assert.Contains(t, content, "\n")
	assert.Contains(t, content, "  ") // 2-space indentation
}

func TestWriteViewDefinition_OverwritesExisting(t *testing.T) {
	tmpDir := t.TempDir()
	writer := services.NewViewDefinitionWriter(tmpDir)

	// Write first version
	viewDef1 := models.NewBaseViewDefinition("Test", "Patient")
	filename := services.BuildViewDefinitionFilename("Test")
	err := writer.WriteViewDefinition(filename, viewDef1)
	require.NoError(t, err)

	// Write second version (different resource type)
	viewDef2 := models.NewBaseViewDefinition("Test", "Observation")
	err = writer.WriteViewDefinition(filename, viewDef2)
	require.NoError(t, err)

	// Verify it's the second version
	data, err := os.ReadFile(filepath.Join(tmpDir, filename))
	require.NoError(t, err)

	var parsed models.ViewDefinition
	err = json.Unmarshal(data, &parsed)
	require.NoError(t, err)

	assert.Equal(t, "Observation", parsed.Resource)
}

func TestWriteViewDefinition_MultipleGroups(t *testing.T) {
	tmpDir := t.TempDir()
	writer := services.NewViewDefinitionWriter(tmpDir)

	groups := []string{"Patient", "Condition", "Observation"}

	for _, group := range groups {
		viewDef := models.NewBaseViewDefinition(group, group)
		filename := services.BuildViewDefinitionFilename(group)
		err := writer.WriteViewDefinition(filename, viewDef)
		require.NoError(t, err, "Failed to write ViewDefinition for group: %s", group)
	}

	// Verify all files exist
	for _, group := range groups {
		filename := services.BuildViewDefinitionFilename(group)
		assert.FileExists(t, filepath.Join(tmpDir, filename))
	}
}

func TestWriteViewDefinition_DirectoryCreationError(t *testing.T) {
	// Use a path that can't be created (file as parent directory)
	tmpDir := t.TempDir()

	// Create a file where we need a directory
	blockingFile := filepath.Join(tmpDir, "blocking_file")
	err := os.WriteFile(blockingFile, []byte("content"), 0644)
	require.NoError(t, err)

	// Try to write ViewDefinition to a path that requires creating dir inside the file
	invalidDir := filepath.Join(blockingFile, "viewdefinitions")
	writer := services.NewViewDefinitionWriter(invalidDir)

	viewDef := models.NewBaseViewDefinition("Test", "Patient")
	filename := services.BuildViewDefinitionFilename("Test")
	err = writer.WriteViewDefinition(filename, viewDef)

	// Should fail because can't create directory inside a file
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create viewdefinitions directory")
}

func TestWriteViewDefinition_WriteFileError(t *testing.T) {
	// Create a read-only directory
	tmpDir := t.TempDir()
	readOnlyDir := filepath.Join(tmpDir, "readonly")
	err := os.MkdirAll(readOnlyDir, 0755)
	require.NoError(t, err)

	// Make directory read-only (no write permission)
	err = os.Chmod(readOnlyDir, 0555)
	require.NoError(t, err)

	// Ensure we restore permissions after test for cleanup
	t.Cleanup(func() {
		_ = os.Chmod(readOnlyDir, 0755)
	})

	writer := services.NewViewDefinitionWriter(readOnlyDir)

	viewDef := models.NewBaseViewDefinition("Test", "Patient")
	filename := services.BuildViewDefinitionFilename("Test")
	err = writer.WriteViewDefinition(filename, viewDef)

	// Should fail because can't write to read-only directory
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to write ViewDefinition file")
}
