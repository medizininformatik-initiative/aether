package unit

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/services"
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

		tables, err := services.LoadLookupTables(lookupPath, nil)
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

		tables, err := services.LoadLookupTables(lookupPath, nil)
		require.NoError(t, err)
		assert.Len(t, tables, 2)
	})

	t.Run("missing url field", func(t *testing.T) {
		tempDir := t.TempDir()
		lookupPath := filepath.Join(tempDir, "lookup.json")
		lookupJSON := `[{"resourceType": "Patient", "elements": {}}]`
		err := os.WriteFile(lookupPath, []byte(lookupJSON), 0644)
		require.NoError(t, err)

		_, err = services.LoadLookupTables(lookupPath, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "missing 'url' field")
	})

	t.Run("missing resourceType field", func(t *testing.T) {
		tempDir := t.TempDir()
		lookupPath := filepath.Join(tempDir, "lookup.json")
		lookupJSON := `[{"url": "https://example.com/Patient", "elements": {}}]`
		err := os.WriteFile(lookupPath, []byte(lookupJSON), 0644)
		require.NoError(t, err)

		_, err = services.LoadLookupTables(lookupPath, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "missing 'resourceType' field")
	})

	t.Run("file not found", func(t *testing.T) {
		_, err := services.LoadLookupTables("/nonexistent/path/lookup.json", nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to read lookup file")
	})

	t.Run("invalid JSON", func(t *testing.T) {
		tempDir := t.TempDir()
		lookupPath := filepath.Join(tempDir, "lookup.json")
		err := os.WriteFile(lookupPath, []byte("not valid json"), 0644)
		require.NoError(t, err)

		_, err = services.LoadLookupTables(lookupPath, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse lookup file")
	})

	t.Run("rejects circular children references", func(t *testing.T) {
		tempDir := t.TempDir()
		lookupPath := filepath.Join(tempDir, "lookup.json")
		lookupJSON := `[
			{
				"url": "https://example.com/Patient",
				"resourceType": "Patient",
				"elements": {
					"Patient.a": {"children": ["Patient.b"]},
					"Patient.b": {"children": ["Patient.a"]}
				}
			}
		]`
		err := os.WriteFile(lookupPath, []byte(lookupJSON), 0644)
		require.NoError(t, err)

		_, err = services.LoadLookupTables(lookupPath, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "circular")
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

		_, err = services.LoadLookupTables(lookupPath, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "missing 'elements' field")
	})
}

func TestLoadLookupTablesNormalizesParentLinks(t *testing.T) {
	t.Run("backfills missing parent link from children list", func(t *testing.T) {
		tempDir := t.TempDir()
		lookupPath := filepath.Join(tempDir, "lookup.json")
		lookupJSON := `[
			{
				"url": "https://example.com/Encounter",
				"resourceType": "Encounter",
				"elements": {
					"Encounter.extension:A": {
						"children": ["Encounter.extension:A.extension:B"],
						"viewDefinition": {"forEachOrNull": "extension.where(url = 'A')"}
					},
					"Encounter.extension:A.extension:B": {
						"viewDefinition": {
							"forEachOrNull": "extension.where(url = 'B')",
							"column": [{"name": "b_code", "path": "value.code"}]
						}
					}
				}
			}
		]`
		err := os.WriteFile(lookupPath, []byte(lookupJSON), 0644)
		require.NoError(t, err)

		tables, err := services.LoadLookupTables(lookupPath, nil)
		require.NoError(t, err)
		child := tables[0].Elements["Encounter.extension:A.extension:B"]
		assert.Equal(t, "Encounter.extension:A", child.Parent)
	})

	t.Run("overwrites authored parent link from children list", func(t *testing.T) {
		tempDir := t.TempDir()
		lookupPath := filepath.Join(tempDir, "lookup.json")
		lookupJSON := `[
			{
				"url": "https://example.com/Encounter",
				"resourceType": "Encounter",
				"elements": {
					"Encounter.extension:A": {
						"children": ["Encounter.extension:A.extension:B"],
						"viewDefinition": {"forEachOrNull": "extension.where(url = 'A')"}
					},
					"Encounter.extension:A.extension:B": {
						"parent": "Encounter.extension:Other",
						"viewDefinition": {"forEachOrNull": "extension.where(url = 'B')"}
					}
				}
			}
		]`
		err := os.WriteFile(lookupPath, []byte(lookupJSON), 0644)
		require.NoError(t, err)

		tables, err := services.LoadLookupTables(lookupPath, nil)
		require.NoError(t, err)
		child := tables[0].Elements["Encounter.extension:A.extension:B"]
		assert.Equal(t, "Encounter.extension:A", child.Parent)
	})

	t.Run("clears authored parent link that no children list confirms", func(t *testing.T) {
		tempDir := t.TempDir()
		lookupPath := filepath.Join(tempDir, "lookup.json")
		lookupJSON := `[
			{
				"url": "https://example.com/Encounter",
				"resourceType": "Encounter",
				"elements": {
					"Encounter.extension:A": {
						"viewDefinition": {"forEachOrNull": "extension.where(url = 'A')"}
					},
					"Encounter.extension:A.extension:B": {
						"parent": "Encounter.extension:A",
						"viewDefinition": {"forEachOrNull": "extension.where(url = 'B')"}
					}
				}
			}
		]`
		err := os.WriteFile(lookupPath, []byte(lookupJSON), 0644)
		require.NoError(t, err)

		tables, err := services.LoadLookupTables(lookupPath, nil)
		require.NoError(t, err)
		child := tables[0].Elements["Encounter.extension:A.extension:B"]
		assert.Empty(t, child.Parent)
	})

	t.Run("skips a child ID that is not in the elements map", func(t *testing.T) {
		tables := []models.LookupTable{
			{
				URL:          "https://example.com/Encounter",
				ResourceType: "Encounter",
				Elements: map[string]models.LookupElement{
					"Encounter.extension:A": {
						Children: []string{"Encounter.extension:A.extension:Missing", "Encounter.extension:A.extension:B"},
					},
					"Encounter.extension:A.extension:B": {},
				},
			},
		}

		err := services.NormalizeLookupTables(tables, nil)
		require.NoError(t, err)
		assert.Equal(t, "Encounter.extension:A", tables[0].Elements["Encounter.extension:A.extension:B"].Parent)
		assert.NotContains(t, tables[0].Elements, "Encounter.extension:A.extension:Missing")
	})

	t.Run("rejects a child claimed by two parents", func(t *testing.T) {
		tempDir := t.TempDir()
		lookupPath := filepath.Join(tempDir, "lookup.json")
		lookupJSON := `[
			{
				"url": "https://example.com/Encounter",
				"resourceType": "Encounter",
				"elements": {
					"Encounter.extension:A": {
						"children": ["Encounter.extension:A.extension:C"],
						"viewDefinition": {"forEachOrNull": "extension.where(url = 'A')"}
					},
					"Encounter.extension:B": {
						"children": ["Encounter.extension:A.extension:C"],
						"viewDefinition": {"forEachOrNull": "extension.where(url = 'B')"}
					},
					"Encounter.extension:A.extension:C": {
						"viewDefinition": {"forEachOrNull": "extension.where(url = 'C')"}
					}
				}
			}
		]`
		err := os.WriteFile(lookupPath, []byte(lookupJSON), 0644)
		require.NoError(t, err)

		_, err = services.LoadLookupTables(lookupPath, nil)
		require.Error(t, err)
		// Map order decides which parent backfills first and which one the
		// error names, so assert only the child ID.
		assert.Contains(t, err.Error(), "Encounter.extension:A.extension:C")
	})

	t.Run("rejects a children list that names the same child twice", func(t *testing.T) {
		tempDir := t.TempDir()
		lookupPath := filepath.Join(tempDir, "lookup.json")
		lookupJSON := `[
			{
				"url": "https://example.com/Encounter",
				"resourceType": "Encounter",
				"elements": {
					"Encounter.extension:A": {
						"children": ["Encounter.extension:A.extension:B", "Encounter.extension:A.extension:B"],
						"viewDefinition": {"forEachOrNull": "extension.where(url = 'A')"}
					},
					"Encounter.extension:A.extension:B": {
						"viewDefinition": {"forEachOrNull": "extension.where(url = 'B')"}
					}
				}
			}
		]`
		err := os.WriteFile(lookupPath, []byte(lookupJSON), 0644)
		require.NoError(t, err)

		_, err = services.LoadLookupTables(lookupPath, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Encounter.extension:A.extension:B")
		assert.Contains(t, err.Error(), "listed more than once")
	})
}

func TestLoadLookupTablesWarnsOnAuthoredParentFields(t *testing.T) {
	t.Run("warns once per profile that sets deprecated parent fields", func(t *testing.T) {
		tempDir := t.TempDir()
		lookupPath := filepath.Join(tempDir, "lookup.json")
		lookupJSON := `[
			{
				"url": "https://example.com/Encounter",
				"resourceType": "Encounter",
				"elements": {
					"Encounter.extension:A": {
						"children": ["Encounter.extension:A.extension:B"],
						"viewDefinition": {"forEachOrNull": "extension.where(url = 'A')"}
					},
					"Encounter.extension:A.extension:B": {
						"parent": "Encounter.extension:A",
						"viewDefinition": {"forEachOrNull": "extension.where(url = 'B')"}
					}
				}
			}
		]`
		err := os.WriteFile(lookupPath, []byte(lookupJSON), 0644)
		require.NoError(t, err)

		var logBuf bytes.Buffer
		logger := lib.NewLoggerWithWriter(lib.LogLevelWarn, &logBuf)

		_, err = services.LoadLookupTables(lookupPath, logger)
		require.NoError(t, err)

		logOutput := logBuf.String()
		assert.Contains(t, logOutput, "deprecated")
		assert.Contains(t, logOutput, "https://example.com/Encounter")
		assert.Contains(t, logOutput, "Encounter.extension:A.extension:B")
	})

	t.Run("stays silent when no parent fields are set", func(t *testing.T) {
		tempDir := t.TempDir()
		lookupPath := filepath.Join(tempDir, "lookup.json")
		lookupJSON := `[
			{
				"url": "https://example.com/Encounter",
				"resourceType": "Encounter",
				"elements": {
					"Encounter.extension:A": {
						"children": ["Encounter.extension:A.extension:B"],
						"viewDefinition": {"forEachOrNull": "extension.where(url = 'A')"}
					},
					"Encounter.extension:A.extension:B": {
						"viewDefinition": {"forEachOrNull": "extension.where(url = 'B')"}
					}
				}
			}
		]`
		err := os.WriteFile(lookupPath, []byte(lookupJSON), 0644)
		require.NoError(t, err)

		var logBuf bytes.Buffer
		logger := lib.NewLoggerWithWriter(lib.LogLevelWarn, &logBuf)

		_, err = services.LoadLookupTables(lookupPath, logger)
		require.NoError(t, err)
		assert.Empty(t, logBuf.String())
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

	t.Run("valid nested children hierarchy", func(t *testing.T) {
		tables := []models.LookupTable{
			{
				URL:          "https://example.com/Patient",
				ResourceType: "Patient",
				Elements: map[string]models.LookupElement{
					"Patient.a":     {Children: []string{"Patient.a.b", "Patient.a.c"}},
					"Patient.a.b":   {Children: []string{"Patient.a.b.d"}},
					"Patient.a.c":   {},
					"Patient.a.b.d": {},
				},
			},
		}
		err := services.ValidateLookupTables(tables)
		assert.NoError(t, err)
	})

	t.Run("circular children reference", func(t *testing.T) {
		tables := []models.LookupTable{
			{
				URL:          "https://example.com/Patient",
				ResourceType: "Patient",
				Elements: map[string]models.LookupElement{
					"Patient.a": {Children: []string{"Patient.b"}},
					"Patient.b": {Children: []string{"Patient.a"}},
				},
			},
		}
		err := services.ValidateLookupTables(tables)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "circular")
	})

	t.Run("self-referencing children entry", func(t *testing.T) {
		tables := []models.LookupTable{
			{
				URL:          "https://example.com/Patient",
				ResourceType: "Patient",
				Elements: map[string]models.LookupElement{
					"Patient.a": {Children: []string{"Patient.a"}},
				},
			},
		}
		err := services.ValidateLookupTables(tables)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "circular")
	})

}
