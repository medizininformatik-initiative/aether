package unit

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadLookupTables(t *testing.T) {
	t.Run("valid lookup file", func(t *testing.T) {
		// Create temp file with valid lookup data
		tempDir := t.TempDir()
		lookupPath := filepath.Join(tempDir, "lookup.json")
		lookupJSON := `[
			{
				"url": "https://example.com/Patient",
				"resourceType": "Patient",
				"elements": {
					"Patient.id": {
						"viewDefinition": {
							"select": [{"column": [{"name": "id", "path": "id"}]}]
						}
					}
				}
			}
		]`
		err := os.WriteFile(lookupPath, []byte(lookupJSON), 0644)
		require.NoError(t, err)

		tables, err := services.LoadLookupTables(lookupPath)
		require.NoError(t, err)
		assert.Len(t, tables, 1)
		assert.Equal(t, "https://example.com/Patient", tables[0].URL)
		assert.Equal(t, "Patient", tables[0].ResourceType)
		assert.Contains(t, tables[0].Elements, "Patient.id")
	})

	t.Run("multiple profiles", func(t *testing.T) {
		tempDir := t.TempDir()
		lookupPath := filepath.Join(tempDir, "lookup.json")
		lookupJSON := `[
			{
				"url": "https://example.com/Patient",
				"resourceType": "Patient",
				"elements": {"Patient.id": {"viewDefinition": {"select": []}}}
			},
			{
				"url": "https://example.com/Condition",
				"resourceType": "Condition",
				"elements": {"Condition.code": {"viewDefinition": {"select": []}}}
			}
		]`
		err := os.WriteFile(lookupPath, []byte(lookupJSON), 0644)
		require.NoError(t, err)

		tables, err := services.LoadLookupTables(lookupPath)
		require.NoError(t, err)
		assert.Len(t, tables, 2)
	})

	t.Run("missing url field", func(t *testing.T) {
		tempDir := t.TempDir()
		lookupPath := filepath.Join(tempDir, "lookup.json")
		lookupJSON := `[{"resourceType": "Patient", "elements": {}}]`
		err := os.WriteFile(lookupPath, []byte(lookupJSON), 0644)
		require.NoError(t, err)

		_, err = services.LoadLookupTables(lookupPath)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "missing 'url' field")
	})

	t.Run("missing resourceType field", func(t *testing.T) {
		tempDir := t.TempDir()
		lookupPath := filepath.Join(tempDir, "lookup.json")
		lookupJSON := `[{"url": "https://example.com/Patient", "elements": {}}]`
		err := os.WriteFile(lookupPath, []byte(lookupJSON), 0644)
		require.NoError(t, err)

		_, err = services.LoadLookupTables(lookupPath)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "missing 'resourceType' field")
	})

	t.Run("file not found", func(t *testing.T) {
		_, err := services.LoadLookupTables("/nonexistent/path/lookup.json")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to read lookup file")
	})

	t.Run("invalid JSON", func(t *testing.T) {
		tempDir := t.TempDir()
		lookupPath := filepath.Join(tempDir, "lookup.json")
		err := os.WriteFile(lookupPath, []byte("not valid json"), 0644)
		require.NoError(t, err)

		_, err = services.LoadLookupTables(lookupPath)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse lookup file")
	})
}

func TestGetProfileLookup(t *testing.T) {
	tables := []models.LookupTable{
		{URL: "https://example.com/Patient", ResourceType: "Patient"},
		{URL: "https://example.com/Condition", ResourceType: "Condition"},
	}

	t.Run("found", func(t *testing.T) {
		result := services.GetProfileLookup(tables, "https://example.com/Patient")
		require.NotNil(t, result)
		assert.Equal(t, "Patient", result.ResourceType)
	})

	t.Run("not found", func(t *testing.T) {
		result := services.GetProfileLookup(tables, "https://example.com/Unknown")
		assert.Nil(t, result)
	})

	t.Run("empty tables", func(t *testing.T) {
		result := services.GetProfileLookup([]models.LookupTable{}, "https://example.com/Patient")
		assert.Nil(t, result)
	})
}

func TestGetElement(t *testing.T) {
	table := &models.LookupTable{
		URL:          "https://example.com/Patient",
		ResourceType: "Patient",
		Elements: map[string]models.LookupElement{
			"Patient.id": {
				ViewDefinition: models.ViewDefSnippet{
					Select: []models.SelectClause{
						{Column: []models.ColumnDefinition{{Name: "id", Path: "id"}}},
					},
				},
			},
		},
	}

	t.Run("found", func(t *testing.T) {
		result := services.GetElement(table, "Patient.id")
		require.NotNil(t, result)
		assert.Len(t, result.ViewDefinition.Select, 1)
	})

	t.Run("not found", func(t *testing.T) {
		result := services.GetElement(table, "Patient.unknown")
		assert.Nil(t, result)
	})

	t.Run("nil table", func(t *testing.T) {
		result := services.GetElement(nil, "Patient.id")
		assert.Nil(t, result)
	})
}

func TestGetElementChildren(t *testing.T) {
	table := &models.LookupTable{
		URL:          "https://example.com/Patient",
		ResourceType: "Patient",
		Elements: map[string]models.LookupElement{
			"Patient.name": {
				Children: []string{"Patient.name.family", "Patient.name.given"},
			},
			"Patient.name.family": {
				ViewDefinition: models.ViewDefSnippet{},
			},
			"Patient.name.given": {
				ViewDefinition: models.ViewDefSnippet{},
			},
		},
	}

	t.Run("element with children", func(t *testing.T) {
		children := services.GetElementChildren(table, "Patient.name")
		assert.Len(t, children, 2)
	})

	t.Run("element without children", func(t *testing.T) {
		children := services.GetElementChildren(table, "Patient.name.family")
		assert.Nil(t, children)
	})

	t.Run("nonexistent element", func(t *testing.T) {
		children := services.GetElementChildren(table, "Patient.unknown")
		assert.Nil(t, children)
	})
}

func TestLoadLookupTablesMissingElements(t *testing.T) {
	t.Run("missing elements field", func(t *testing.T) {
		tempDir := t.TempDir()
		lookupPath := filepath.Join(tempDir, "lookup.json")
		lookupJSON := `[{"url": "https://example.com/Patient", "resourceType": "Patient"}]`
		err := os.WriteFile(lookupPath, []byte(lookupJSON), 0644)
		require.NoError(t, err)

		_, err = services.LoadLookupTables(lookupPath)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "missing 'elements' field")
	})
}

func TestValidateLookupTables(t *testing.T) {
	t.Run("valid tables", func(t *testing.T) {
		tables := []models.LookupTable{
			{URL: "https://example.com/Patient", ResourceType: "Patient", Elements: map[string]models.LookupElement{}},
			{URL: "https://example.com/Condition", ResourceType: "Condition", Elements: map[string]models.LookupElement{}},
		}
		err := services.ValidateLookupTables(tables)
		assert.NoError(t, err)
	})

	t.Run("duplicate URLs", func(t *testing.T) {
		tables := []models.LookupTable{
			{URL: "https://example.com/Patient", ResourceType: "Patient", Elements: map[string]models.LookupElement{}},
			{URL: "https://example.com/Patient", ResourceType: "Patient", Elements: map[string]models.LookupElement{}},
		}
		err := services.ValidateLookupTables(tables)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "duplicate profile URL")
	})

	t.Run("invalid child reference", func(t *testing.T) {
		tables := []models.LookupTable{
			{
				URL:          "https://example.com/Patient",
				ResourceType: "Patient",
				Elements: map[string]models.LookupElement{
					"Patient.name": {Children: []string{"Patient.name.nonexistent"}},
				},
			},
		}
		err := services.ValidateLookupTables(tables)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "non-existent child")
	})
}
