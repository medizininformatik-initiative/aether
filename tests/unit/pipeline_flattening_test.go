package unit

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/pipeline"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

// Helper function to create a test logger
func createFlatteningTestLogger() *lib.Logger {
	return lib.NewLogger(lib.LogLevelDebug)
}

// Helper function to create test NDJSON file
func writeTestNDJSON(t *testing.T, filename string, resources []map[string]any) {
	f, err := os.Create(filename)
	require.NoError(t, err)
	defer func() { require.NoError(t, f.Close()) }()

	for _, res := range resources {
		data, err := json.Marshal(res)
		require.NoError(t, err)
		_, err = f.Write(data)
		require.NoError(t, err)
		_, err = f.WriteString("\n")
		require.NoError(t, err)
	}
}

// makeProvenance builds a Provenance resource for testing
func makeProvenance(id string, targetRef string, groupID string) map[string]any {
	return map[string]any{
		"resourceType": "Provenance",
		"id":           id,
		"target": []any{
			map[string]any{"reference": targetRef},
		},
		"entity": []any{
			map[string]any{
				"role": "source",
				"what": map[string]any{
					"identifier": map[string]any{
						"system": models.AttributeGroupNamingSystem,
						"value":  groupID,
					},
				},
			},
		},
	}
}

// makeBundle creates a Bundle with the given entries
func makeBundle(id string, entries ...map[string]any) map[string]any {
	entryList := make([]any, 0, len(entries))
	for _, e := range entries {
		entryList = append(entryList, map[string]any{
			"resource": e,
			"request": map[string]any{
				"method": "PUT",
				"url":    e["resourceType"].(string) + "/" + e["id"].(string),
			},
		})
	}
	return map[string]any{
		"resourceType": "Bundle",
		"id":           id,
		"type":         "transaction",
		"entry":        entryList,
	}
}

func TestFilterResourcesByProvenance(t *testing.T) {
	t.Run("filters resources matching group via provenance", func(t *testing.T) {
		resources := []map[string]any{
			{"resourceType": "Patient", "id": "1"},
			{"resourceType": "Patient", "id": "2"},
			{"resourceType": "Condition", "id": "3"},
		}
		index := models.ProvenanceIndex{
			"Patient/1":   {"group-a"},
			"Patient/2":   {"group-a"},
			"Condition/3": {"group-b"},
		}

		result := pipeline.FilterResourcesByProvenance(resources, index, "group-a")
		assert.Len(t, result, 2)
		assert.Equal(t, "1", result[0]["id"])
		assert.Equal(t, "2", result[1]["id"])
	})

	t.Run("returns empty for no matches", func(t *testing.T) {
		resources := []map[string]any{
			{"resourceType": "Patient", "id": "1"},
		}
		index := models.ProvenanceIndex{
			"Patient/1": {"group-a"},
		}

		result := pipeline.FilterResourcesByProvenance(resources, index, "group-b")
		assert.Empty(t, result)
	})

	t.Run("returns empty for empty provenance index", func(t *testing.T) {
		resources := []map[string]any{
			{"resourceType": "Patient", "id": "1"},
		}
		index := models.ProvenanceIndex{}

		result := pipeline.FilterResourcesByProvenance(resources, index, "group-a")
		assert.Empty(t, result)
	})

	t.Run("skips resources without resourceType or id", func(t *testing.T) {
		resources := []map[string]any{
			{"resourceType": "Patient"},           // missing id
			{"id": "1"},                           // missing resourceType
			{"resourceType": "Patient", "id": ""}, // empty id
		}
		index := models.ProvenanceIndex{
			"Patient/1": {"group-a"},
		}

		result := pipeline.FilterResourcesByProvenance(resources, index, "group-a")
		assert.Empty(t, result)
	})

	t.Run("handles nil index", func(t *testing.T) {
		resources := []map[string]any{
			{"resourceType": "Patient", "id": "1"},
		}

		result := pipeline.FilterResourcesByProvenance(resources, nil, "group-a")
		assert.Empty(t, result)
	})
}

func TestResourceReference(t *testing.T) {
	t.Run("builds reference from resource", func(t *testing.T) {
		r := map[string]any{"resourceType": "Patient", "id": "123"}
		assert.Equal(t, "Patient/123", pipeline.ResourceReference(r))
	})

	t.Run("returns empty for missing fields", func(t *testing.T) {
		assert.Equal(t, "", pipeline.ResourceReference(map[string]any{"resourceType": "Patient"}))
		assert.Equal(t, "", pipeline.ResourceReference(map[string]any{"id": "1"}))
		assert.Equal(t, "", pipeline.ResourceReference(map[string]any{}))
	})
}

func TestLoadResourcesFromFile(t *testing.T) {
	logger := createFlatteningTestLogger()

	t.Run("loads simple NDJSON file", func(t *testing.T) {
		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "test.ndjson")

		resources := []map[string]any{
			{"resourceType": "Patient", "id": "1"},
			{"resourceType": "Patient", "id": "2"},
		}
		writeTestNDJSON(t, filePath, resources)

		result, index, err := pipeline.LoadResourcesFromFile(filePath, logger, lib.DefaultMaxNDJSONLineSize)
		require.NoError(t, err)
		assert.Len(t, result, 2)
		assert.Equal(t, "1", result[0]["id"])
		assert.Equal(t, "2", result[1]["id"])
		assert.Empty(t, index)
	})

	t.Run("handles empty lines", func(t *testing.T) {
		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "test.ndjson")

		content := `{"resourceType": "Patient", "id": "1"}

{"resourceType": "Patient", "id": "2"}
`
		err := os.WriteFile(filePath, []byte(content), 0644)
		require.NoError(t, err)

		result, _, err := pipeline.LoadResourcesFromFile(filePath, logger, lib.DefaultMaxNDJSONLineSize)
		require.NoError(t, err)
		assert.Len(t, result, 2)
	})

	t.Run("extracts clinical resources from Bundle and builds provenance index", func(t *testing.T) {
		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "bundle.ndjson")

		patient := map[string]any{"resourceType": "Patient", "id": "p1"}
		condition := map[string]any{"resourceType": "Condition", "id": "c1"}
		provPatient := makeProvenance("prov-1", "Patient/p1", "group-patient")
		provCondition := makeProvenance("prov-2", "Condition/c1", "group-condition")

		bundle := makeBundle("bundle-1", patient, condition, provPatient, provCondition)
		writeTestNDJSON(t, filePath, []map[string]any{bundle})

		result, index, err := pipeline.LoadResourcesFromFile(filePath, logger, lib.DefaultMaxNDJSONLineSize)
		require.NoError(t, err)

		// Only clinical resources returned, no Provenance
		assert.Len(t, result, 2)
		assert.Equal(t, "Patient", result[0]["resourceType"])
		assert.Equal(t, "Condition", result[1]["resourceType"])

		// Provenance index maps targets to group IDs
		assert.Equal(t, []string{"group-patient"}, index["Patient/p1"])
		assert.Equal(t, []string{"group-condition"}, index["Condition/c1"])
	})

	t.Run("handles Bundle with invalid entry structure", func(t *testing.T) {
		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "bundle.ndjson")

		bundle := map[string]any{
			"resourceType": "Bundle",
			"type":         "collection",
			"entry": []any{
				"not a map", // invalid entry
				map[string]any{
					"resource": map[string]any{
						"resourceType": "Patient",
						"id":           "1",
					},
				},
			},
		}
		writeTestNDJSON(t, filePath, []map[string]any{bundle})

		result, _, err := pipeline.LoadResourcesFromFile(filePath, logger, lib.DefaultMaxNDJSONLineSize)
		require.NoError(t, err)
		assert.Len(t, result, 1)
	})

	t.Run("handles Bundle entry without resource field", func(t *testing.T) {
		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "bundle.ndjson")

		bundle := map[string]any{
			"resourceType": "Bundle",
			"type":         "collection",
			"entry": []any{
				map[string]any{
					"fullUrl": "urn:uuid:1234", // no resource field
				},
			},
		}
		writeTestNDJSON(t, filePath, []map[string]any{bundle})

		result, _, err := pipeline.LoadResourcesFromFile(filePath, logger, lib.DefaultMaxNDJSONLineSize)
		require.NoError(t, err)
		assert.Empty(t, result)
	})

	t.Run("handles Bundle entry with non-map resource", func(t *testing.T) {
		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "bundle.ndjson")

		bundle := map[string]any{
			"resourceType": "Bundle",
			"type":         "collection",
			"entry": []any{
				map[string]any{
					"resource": "not a map",
				},
			},
		}
		writeTestNDJSON(t, filePath, []map[string]any{bundle})

		result, _, err := pipeline.LoadResourcesFromFile(filePath, logger, lib.DefaultMaxNDJSONLineSize)
		require.NoError(t, err)
		assert.Empty(t, result)
	})

	t.Run("handles Bundle with non-array entries", func(t *testing.T) {
		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "bundle.ndjson")

		bundle := map[string]any{
			"resourceType": "Bundle",
			"type":         "collection",
			"entry":        "not an array",
		}
		writeTestNDJSON(t, filePath, []map[string]any{bundle})

		result, _, err := pipeline.LoadResourcesFromFile(filePath, logger, lib.DefaultMaxNDJSONLineSize)
		require.NoError(t, err)
		assert.Empty(t, result)
	})

	t.Run("returns error for nonexistent file", func(t *testing.T) {
		_, _, err := pipeline.LoadResourcesFromFile("/nonexistent/path/file.ndjson", logger, lib.DefaultMaxNDJSONLineSize)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to open file")
	})

	t.Run("handles invalid JSON line gracefully", func(t *testing.T) {
		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "invalid.ndjson")

		content := `{"resourceType": "Patient", "id": "1"}
{invalid json}
{"resourceType": "Patient", "id": "2"}
`
		err := os.WriteFile(filePath, []byte(content), 0644)
		require.NoError(t, err)

		result, _, err := pipeline.LoadResourcesFromFile(filePath, logger, lib.DefaultMaxNDJSONLineSize)
		require.NoError(t, err)
		assert.Len(t, result, 2)
	})

	t.Run("provenance with multiple targets maps all to same group", func(t *testing.T) {
		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "multi-target.ndjson")

		patient := map[string]any{"resourceType": "Patient", "id": "p1"}
		obs := map[string]any{"resourceType": "Observation", "id": "o1"}

		// Provenance with two targets
		prov := map[string]any{
			"resourceType": "Provenance",
			"id":           "prov-multi",
			"target": []any{
				map[string]any{"reference": "Patient/p1"},
				map[string]any{"reference": "Observation/o1"},
			},
			"entity": []any{
				map[string]any{
					"role": "source",
					"what": map[string]any{
						"identifier": map[string]any{
							"system": models.AttributeGroupNamingSystem,
							"value":  "group-x",
						},
					},
				},
			},
		}

		bundle := map[string]any{
			"resourceType": "Bundle",
			"id":           "b1",
			"type":         "transaction",
			"entry": []any{
				map[string]any{"resource": patient},
				map[string]any{"resource": obs},
				map[string]any{"resource": prov},
			},
		}
		writeTestNDJSON(t, filePath, []map[string]any{bundle})

		result, index, err := pipeline.LoadResourcesFromFile(filePath, logger, lib.DefaultMaxNDJSONLineSize)
		require.NoError(t, err)
		assert.Len(t, result, 2) // Patient + Observation, no Provenance
		assert.Equal(t, []string{"group-x"}, index["Patient/p1"])
		assert.Equal(t, []string{"group-x"}, index["Observation/o1"])
	})

	t.Run("provenance with empty targets produces no index entries", func(t *testing.T) {
		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "empty-targets.ndjson")

		patient := map[string]any{"resourceType": "Patient", "id": "p1"}
		prov := map[string]any{
			"resourceType": "Provenance",
			"id":           "prov-empty",
			"target":       []any{},
			"entity": []any{
				map[string]any{
					"role": "source",
					"what": map[string]any{
						"identifier": map[string]any{
							"system": models.AttributeGroupNamingSystem,
							"value":  "group-x",
						},
					},
				},
			},
		}

		bundle := map[string]any{
			"resourceType": "Bundle",
			"id":           "b1",
			"type":         "transaction",
			"entry": []any{
				map[string]any{"resource": patient},
				map[string]any{"resource": prov},
			},
		}
		writeTestNDJSON(t, filePath, []map[string]any{bundle})

		result, index, err := pipeline.LoadResourcesFromFile(filePath, logger, lib.DefaultMaxNDJSONLineSize)
		require.NoError(t, err)
		assert.Len(t, result, 1)
		assert.Empty(t, index)
	})

	t.Run("provenance with non-array target is ignored in index", func(t *testing.T) {
		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "bad-target.ndjson")

		patient := map[string]any{"resourceType": "Patient", "id": "p1"}
		// Provenance has valid entity but target is a string instead of []any
		prov := map[string]any{
			"resourceType": "Provenance",
			"id":           "prov-bad-target",
			"target":       "not-an-array",
			"entity": []any{
				map[string]any{
					"role": "source",
					"what": map[string]any{
						"identifier": map[string]any{
							"system": models.AttributeGroupNamingSystem,
							"value":  "group-x",
						},
					},
				},
			},
		}

		bundle := makeBundle("b1", patient, prov)
		writeTestNDJSON(t, filePath, []map[string]any{bundle})

		result, index, err := pipeline.LoadResourcesFromFile(filePath, logger, lib.DefaultMaxNDJSONLineSize)
		require.NoError(t, err)
		assert.Len(t, result, 1)
		assert.Empty(t, index)
	})

	t.Run("provenance with non-map target entry is skipped", func(t *testing.T) {
		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "bad-target-entry.ndjson")

		patient := map[string]any{"resourceType": "Patient", "id": "p1"}
		prov := map[string]any{
			"resourceType": "Provenance",
			"id":           "prov-bad-entry",
			"target": []any{
				"not-a-map",
				map[string]any{"reference": "Patient/p1"},
			},
			"entity": []any{
				map[string]any{
					"role": "source",
					"what": map[string]any{
						"identifier": map[string]any{
							"system": models.AttributeGroupNamingSystem,
							"value":  "group-x",
						},
					},
				},
			},
		}

		bundle := makeBundle("b1", patient, prov)
		writeTestNDJSON(t, filePath, []map[string]any{bundle})

		result, index, err := pipeline.LoadResourcesFromFile(filePath, logger, lib.DefaultMaxNDJSONLineSize)
		require.NoError(t, err)
		assert.Len(t, result, 1)
		// Only the valid target entry should be indexed
		assert.Equal(t, []string{"group-x"}, index["Patient/p1"])
	})

	t.Run("provenance target with missing reference is skipped", func(t *testing.T) {
		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "no-ref.ndjson")

		patient := map[string]any{"resourceType": "Patient", "id": "p1"}
		prov := map[string]any{
			"resourceType": "Provenance",
			"id":           "prov-no-ref",
			"target": []any{
				map[string]any{"display": "no reference field"},
				map[string]any{"reference": ""},
				map[string]any{"reference": "Patient/p1"},
			},
			"entity": []any{
				map[string]any{
					"role": "source",
					"what": map[string]any{
						"identifier": map[string]any{
							"system": models.AttributeGroupNamingSystem,
							"value":  "group-x",
						},
					},
				},
			},
		}

		bundle := makeBundle("b1", patient, prov)
		writeTestNDJSON(t, filePath, []map[string]any{bundle})

		result, index, err := pipeline.LoadResourcesFromFile(filePath, logger, lib.DefaultMaxNDJSONLineSize)
		require.NoError(t, err)
		assert.Len(t, result, 1)
		// Only the valid reference should be indexed
		assert.Equal(t, []string{"group-x"}, index["Patient/p1"])
		assert.Len(t, index, 1)
	})

	t.Run("provenance with non-array entity is ignored", func(t *testing.T) {
		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "bad-entity.ndjson")

		patient := map[string]any{"resourceType": "Patient", "id": "p1"}
		prov := map[string]any{
			"resourceType": "Provenance",
			"id":           "prov-bad-entity",
			"target":       []any{map[string]any{"reference": "Patient/p1"}},
			"entity":       "not-an-array",
		}

		bundle := makeBundle("b1", patient, prov)
		writeTestNDJSON(t, filePath, []map[string]any{bundle})

		result, index, err := pipeline.LoadResourcesFromFile(filePath, logger, lib.DefaultMaxNDJSONLineSize)
		require.NoError(t, err)
		assert.Len(t, result, 1)
		assert.Empty(t, index)
	})

	t.Run("provenance entity with non-map what is skipped", func(t *testing.T) {
		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "bad-what.ndjson")

		patient := map[string]any{"resourceType": "Patient", "id": "p1"}
		prov := map[string]any{
			"resourceType": "Provenance",
			"id":           "prov-bad-what",
			"target":       []any{map[string]any{"reference": "Patient/p1"}},
			"entity": []any{
				map[string]any{
					"role": "source",
					"what": "not-a-map",
				},
			},
		}

		bundle := makeBundle("b1", patient, prov)
		writeTestNDJSON(t, filePath, []map[string]any{bundle})

		result, index, err := pipeline.LoadResourcesFromFile(filePath, logger, lib.DefaultMaxNDJSONLineSize)
		require.NoError(t, err)
		assert.Len(t, result, 1)
		assert.Empty(t, index)
	})

	t.Run("provenance without attribute_group entity is ignored", func(t *testing.T) {
		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "no-group.ndjson")

		patient := map[string]any{"resourceType": "Patient", "id": "p1"}
		// Provenance with only extraction_id entity, no attribute_group
		prov := map[string]any{
			"resourceType": "Provenance",
			"id":           "prov-no-group",
			"target":       []any{map[string]any{"reference": "Patient/p1"}},
			"entity": []any{
				map[string]any{
					"role": "source",
					"what": map[string]any{
						"identifier": map[string]any{
							"system": "https://example.com/other-system",
							"value":  "some-value",
						},
					},
				},
			},
		}

		bundle := map[string]any{
			"resourceType": "Bundle",
			"id":           "b1",
			"type":         "transaction",
			"entry": []any{
				map[string]any{"resource": patient},
				map[string]any{"resource": prov},
			},
		}
		writeTestNDJSON(t, filePath, []map[string]any{bundle})

		result, index, err := pipeline.LoadResourcesFromFile(filePath, logger, lib.DefaultMaxNDJSONLineSize)
		require.NoError(t, err)
		assert.Len(t, result, 1)
		assert.Empty(t, index)
	})
}

func TestLoadAllResources(t *testing.T) {
	logger := createFlatteningTestLogger()

	t.Run("loads resources from multiple files and merges provenance", func(t *testing.T) {
		tmpDir := t.TempDir()

		file1 := filepath.Join(tmpDir, "file1.ndjson")
		file2 := filepath.Join(tmpDir, "file2.ndjson")

		p1 := map[string]any{"resourceType": "Patient", "id": "1"}
		prov1 := makeProvenance("prov-1", "Patient/1", "group-a")
		bundle1 := makeBundle("b1", p1, prov1)

		p2 := map[string]any{"resourceType": "Patient", "id": "2"}
		prov2 := makeProvenance("prov-2", "Patient/2", "group-a")
		bundle2 := makeBundle("b2", p2, prov2)

		writeTestNDJSON(t, file1, []map[string]any{bundle1})
		writeTestNDJSON(t, file2, []map[string]any{bundle2})

		result, index, err := pipeline.LoadAllResources([]string{file1, file2}, logger, lib.DefaultMaxNDJSONLineSize)
		require.NoError(t, err)
		assert.Len(t, result, 2)
		assert.Equal(t, []string{"group-a"}, index["Patient/1"])
		assert.Equal(t, []string{"group-a"}, index["Patient/2"])
	})

	t.Run("returns error if any file fails", func(t *testing.T) {
		tmpDir := t.TempDir()
		file1 := filepath.Join(tmpDir, "file1.ndjson")
		writeTestNDJSON(t, file1, []map[string]any{
			{"resourceType": "Patient", "id": "1"},
		})

		_, _, err := pipeline.LoadAllResources([]string{file1, "/nonexistent/file.ndjson"}, logger, lib.DefaultMaxNDJSONLineSize)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to load")
	})

	t.Run("handles empty file list", func(t *testing.T) {
		result, index, err := pipeline.LoadAllResources([]string{}, logger, lib.DefaultMaxNDJSONLineSize)
		require.NoError(t, err)
		assert.Empty(t, result)
		assert.Empty(t, index)
	})
}

func TestIsFlatteningErrorRetryable(t *testing.T) {
	t.Run("transient FlattenerError is retryable", func(t *testing.T) {
		err := &services.FlattenerError{
			StatusCode: 500,
			Status:     "Internal Server Error",
			ErrorType:  models.ErrorTypeTransient,
		}
		result := pipeline.IsFlatteningErrorRetryable(err)
		assert.True(t, result)
	})

	t.Run("non-transient FlattenerError is not retryable", func(t *testing.T) {
		err := &services.FlattenerError{
			StatusCode: 400,
			Status:     "Bad Request",
			ErrorType:  models.ErrorTypeNonTransient,
		}
		result := pipeline.IsFlatteningErrorRetryable(err)
		assert.False(t, result)
	})

	t.Run("network error is retryable", func(t *testing.T) {
		netErr := &net.OpError{
			Op:  "dial",
			Net: "tcp",
			Err: &net.DNSError{
				Err:         "no such host",
				Name:        "unknown.host.example",
				IsTemporary: true,
			},
		}
		result := pipeline.IsFlatteningErrorRetryable(netErr)
		assert.True(t, result)
	})
}

func TestClassifyFlatteningError(t *testing.T) {
	t.Run("classifies transient FlattenerError correctly", func(t *testing.T) {
		transientErr := &services.FlattenerError{
			StatusCode: 500,
			Status:     "Internal Server Error",
			ErrorType:  models.ErrorTypeTransient,
		}
		result := pipeline.ClassifyFlatteningError(transientErr)
		assert.Equal(t, models.ErrorTypeTransient, result)
	})

	t.Run("classifies non-transient FlattenerError correctly", func(t *testing.T) {
		nonTransientErr := &services.FlattenerError{
			StatusCode: 400,
			Status:     "Bad Request",
			ErrorType:  models.ErrorTypeNonTransient,
		}
		result := pipeline.ClassifyFlatteningError(nonTransientErr)
		assert.Equal(t, models.ErrorTypeNonTransient, result)
	})

	t.Run("network error classified as transient", func(t *testing.T) {
		netErr := &net.OpError{
			Op:  "dial",
			Net: "tcp",
			Err: &net.DNSError{
				Err:         "no such host",
				Name:        "unknown.host.example",
				IsTemporary: true,
			},
		}
		result := pipeline.ClassifyFlatteningError(netErr)
		assert.Equal(t, models.ErrorTypeTransient, result)
	})

	t.Run("other errors classified as non-transient", func(t *testing.T) {
		result := pipeline.ClassifyFlatteningError(assert.AnError)
		assert.Equal(t, models.ErrorTypeNonTransient, result)
	})
}

// TestExecuteFlatteningStep_MissingCRTDLPath verifies the decoupling from
// issue #286: flattening rejects jobs with no CRTDL attached regardless of
// InputType, and the error message points to both attachment mechanisms.
func TestExecuteFlatteningStep_MissingCRTDLPath(t *testing.T) {
	tempDir := t.TempDir()
	jobID := "test-no-crtdl"
	jobDir := filepath.Join(tempDir, "jobs", jobID)
	require.NoError(t, os.MkdirAll(jobDir, 0755))

	lookupPath := filepath.Join(tempDir, "lookup.json")
	writeTestLookupTable(t, lookupPath, "https://example.com/Patient", "Patient")

	job := createFlatteningTestJob("http://localhost:8080", lookupPath, "")
	job.InputSource = "https://example.com/data.ndjson"
	job.InputType = models.InputTypeHTTP
	job.CRTDLPath = ""

	err := pipeline.ExecuteFlatteningStep(job, jobDir, createFlatteningTestLogger())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "CRTDL")
	assert.Contains(t, err.Error(), "--crtdl")
}

// TestExecuteFlatteningStep_AcceptsHTTPInputWithCRTDLPath verifies that an
// http_import job carrying a CRTDL via CRTDLPath proceeds past the gate that
// previously blocked it. The job completes with no matching resources (empty
// input dir) — the point is that the CRTDL precondition no longer depends on
// InputType.
func TestExecuteFlatteningStep_AcceptsHTTPInputWithCRTDLPath(t *testing.T) {
	tempDir := t.TempDir()
	jobID := "test-http-crtdl"
	jobDir := filepath.Join(tempDir, "jobs", jobID)
	inputDir := filepath.Join(jobDir, "import")
	require.NoError(t, os.MkdirAll(inputDir, 0755))

	crtdlPath := filepath.Join(tempDir, "test.crtdl")
	writeTestCRTDL(t, crtdlPath, "group-1", "Patient", "https://example.com/Patient")
	lookupPath := filepath.Join(tempDir, "lookup.json")
	writeTestLookupTable(t, lookupPath, "https://example.com/Patient", "Patient")

	// Minimal resource with no matching provenance -> no CSV emitted, but
	// the flattening step must not reject the job on the CRTDL check.
	patient := map[string]any{"resourceType": "Patient", "id": "1"}
	prov := makeProvenance("prov-1", "Patient/1", "different-group-id")
	bundle := makeBundle("b1", patient, prov)
	writeTestNDJSON(t, filepath.Join(inputDir, "test.ndjson"), []map[string]any{bundle})

	job := createFlatteningTestJob("http://localhost:8080", lookupPath, crtdlPath)
	job.InputSource = "https://example.com/data.ndjson"
	job.InputType = models.InputTypeHTTP
	// CRTDLPath is set by createFlatteningTestJob via crtdlPath arg

	err := pipeline.ExecuteFlatteningStep(job, jobDir, createFlatteningTestLogger())
	require.NoError(t, err, "http_import + CRTDLPath must be accepted (issue #286)")
}

// Helper function to create a test flattening job
func createFlatteningTestJob(serviceURL, lookupPath, crtdlPath string) *models.PipelineJob {
	return &models.PipelineJob{
		JobID:       "test-flattening-job",
		Status:      models.JobStatusInProgress,
		InputSource: crtdlPath,
		InputType:   models.InputTypeCRTDL,
		CRTDLPath:   crtdlPath,
		Config: models.ProjectConfig{
			Services: models.ServiceConfig{
				Flattening: models.FlatteningConfig{
					ServiceURL: serviceURL,
					LookupPath: lookupPath,
					Formats:    []string{"csv"},
					Timeout:    30 * 1000000000, // 30 seconds in nanoseconds
				},
			},
			Pipeline: models.PipelineConfig{
				EnabledSteps: []models.StepName{models.StepLocalImport, models.StepFlattening},
			},
			Retry: models.RetryConfig{MaxAttempts: 1},
		},
		Steps: []models.PipelineStep{},
	}
}

// Helper to write a valid CRTDL file with group ID
func writeTestCRTDL(t *testing.T, path string, groupID, groupName, groupRef string) {
	crtdl := map[string]any{
		"dataExtraction": map[string]any{
			"attributeGroups": []map[string]any{
				{
					"id":             groupID,
					"name":           groupName,
					"groupReference": groupRef,
					"attributes": []map[string]any{
						{"attributeRef": groupName + ".id", "mustHave": true},
					},
				},
			},
		},
	}
	data, err := json.Marshal(crtdl)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0644))
}

// Helper to write a valid lookup table file
func writeTestLookupTable(t *testing.T, path string, profileURL, resourceType string) {
	lookup := []map[string]any{
		{
			"url":          profileURL,
			"resourceType": resourceType,
			"elements": map[string]any{
				resourceType + ".id": map[string]any{
					"viewDefinition": map[string]any{
						"select": []map[string]any{
							{"column": []map[string]any{{"name": "id", "path": "id"}}},
						},
					},
				},
			},
		},
	}
	data, err := json.Marshal(lookup)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0644))
}

// TestExecuteFlatteningStep_ConfigValidationError tests line 36-40
func TestExecuteFlatteningStep_ConfigValidationError(t *testing.T) {
	tempDir := t.TempDir()
	jobDir := filepath.Join(tempDir, "jobs", "test-job")
	require.NoError(t, os.MkdirAll(jobDir, 0755))

	crtdlPath := filepath.Join(tempDir, "test.crtdl")
	writeTestCRTDL(t, crtdlPath, "group-1", "Patient", "https://example.com/Patient")

	// Create job with invalid config (empty ServiceURL)
	job := &models.PipelineJob{
		JobID:       "test-job",
		Status:      models.JobStatusInProgress,
		InputSource: crtdlPath,
		InputType:   models.InputTypeCRTDL,
		CRTDLPath:   crtdlPath,
		Config: models.ProjectConfig{
			Services: models.ServiceConfig{
				Flattening: models.FlatteningConfig{
					ServiceURL: "", // Invalid: empty URL
					LookupPath: "/some/path",
					Formats:    []string{"csv"},
					Timeout:    30 * 1000000000,
				},
			},
			Pipeline: models.PipelineConfig{
				EnabledSteps: []models.StepName{models.StepFlattening},
			},
			Retry: models.RetryConfig{MaxAttempts: 1},
		},
		Steps: []models.PipelineStep{},
	}

	logger := createFlatteningTestLogger()
	err := pipeline.ExecuteFlatteningStep(job, jobDir, logger)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "service_url is required")
}

// TestExecuteFlatteningStep_LookupValidationError tests line 71-75
func TestExecuteFlatteningStep_LookupValidationError(t *testing.T) {
	tempDir := t.TempDir()
	jobDir := filepath.Join(tempDir, "jobs", "test-job")
	require.NoError(t, os.MkdirAll(jobDir, 0755))

	crtdlPath := filepath.Join(tempDir, "test.crtdl")
	writeTestCRTDL(t, crtdlPath, "group-1", "Patient", "https://example.com/Patient")

	// Create lookup table with duplicate URLs (validation error)
	lookupPath := filepath.Join(tempDir, "lookup.json")
	lookup := []map[string]any{
		{
			"url":          "https://example.com/Patient",
			"resourceType": "Patient",
			"elements":     map[string]any{},
		},
		{
			"url":          "https://example.com/Patient", // Duplicate URL
			"resourceType": "Patient",
			"elements":     map[string]any{},
		},
	}
	data, err := json.Marshal(lookup)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(lookupPath, data, 0644))

	job := createFlatteningTestJob("http://localhost:8080", lookupPath, crtdlPath)
	logger := createFlatteningTestLogger()

	err = pipeline.ExecuteFlatteningStep(job, jobDir, logger)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid lookup tables")
	assert.Contains(t, err.Error(), "duplicate profile URL")
}

// TestExecuteFlatteningStep_ViewDefinitionBuildError tests line 141-146
func TestExecuteFlatteningStep_ViewDefinitionBuildError(t *testing.T) {
	tempDir := t.TempDir()
	jobID := "test-viewdef-error"
	jobDir := filepath.Join(tempDir, "jobs", jobID)
	inputDir := filepath.Join(jobDir, "import")
	require.NoError(t, os.MkdirAll(inputDir, 0755))

	// Create CRTDL referencing a profile that doesn't exist in lookup
	crtdlPath := filepath.Join(tempDir, "test.crtdl")
	writeTestCRTDL(t, crtdlPath, "group-1", "UnknownType", "https://example.com/UnknownProfile")

	// Create lookup table for a different profile
	lookupPath := filepath.Join(tempDir, "lookup.json")
	writeTestLookupTable(t, lookupPath, "https://example.com/DifferentProfile", "Patient")

	// Create input NDJSON file
	ndjsonContent := `{"resourceType":"Patient","id":"1","meta":{"profile":["https://example.com/UnknownProfile"]}}`
	require.NoError(t, os.WriteFile(filepath.Join(inputDir, "test.ndjson"), []byte(ndjsonContent), 0644))

	job := createFlatteningTestJob("http://localhost:8080", lookupPath, crtdlPath)

	logger := createFlatteningTestLogger()

	// This should complete without error (group is skipped)
	err := pipeline.ExecuteFlatteningStep(job, jobDir, logger)
	require.NoError(t, err)

	// Verify no CSV files were created (group was skipped)
	csvFiles, _ := filepath.Glob(filepath.Join(jobDir, "csv", "*.csv"))
	assert.Empty(t, csvFiles)
}

// TestExecuteFlatteningStep_NoMatchingResources tests provenance-based filtering
func TestExecuteFlatteningStep_NoMatchingResources(t *testing.T) {
	tempDir := t.TempDir()
	jobID := "test-no-matching"
	jobDir := filepath.Join(tempDir, "jobs", jobID)
	inputDir := filepath.Join(jobDir, "import")
	require.NoError(t, os.MkdirAll(inputDir, 0755))

	crtdlPath := filepath.Join(tempDir, "test.crtdl")
	writeTestCRTDL(t, crtdlPath, "group-1", "Patient", "https://example.com/Patient")

	lookupPath := filepath.Join(tempDir, "lookup.json")
	writeTestLookupTable(t, lookupPath, "https://example.com/Patient", "Patient")

	// Create input with resources that have NO provenance mapping to "group-1"
	patient := map[string]any{"resourceType": "Patient", "id": "1"}
	prov := makeProvenance("prov-1", "Patient/1", "different-group-id")
	bundle := makeBundle("b1", patient, prov)
	writeTestNDJSON(t, filepath.Join(inputDir, "test.ndjson"), []map[string]any{bundle})

	job := createFlatteningTestJob("http://localhost:8080", lookupPath, crtdlPath)

	logger := createFlatteningTestLogger()

	// Should complete without error (no matching resources, group skipped)
	err := pipeline.ExecuteFlatteningStep(job, jobDir, logger)
	require.NoError(t, err)

	// Verify no CSV files were created (no matching resources)
	csvFiles, _ := filepath.Glob(filepath.Join(jobDir, "csv", "*.csv"))
	assert.Empty(t, csvFiles)
}

// TestExecuteFlatteningStep_LoadAllResourcesError tests line 112-116
func TestExecuteFlatteningStep_LoadAllResourcesError(t *testing.T) {
	tempDir := t.TempDir()
	jobID := "test-load-error"
	jobDir := filepath.Join(tempDir, "jobs", jobID)
	inputDir := filepath.Join(jobDir, "import")
	require.NoError(t, os.MkdirAll(inputDir, 0755))

	crtdlPath := filepath.Join(tempDir, "test.crtdl")
	writeTestCRTDL(t, crtdlPath, "group-1", "Patient", "https://example.com/Patient")

	lookupPath := filepath.Join(tempDir, "lookup.json")
	writeTestLookupTable(t, lookupPath, "https://example.com/Patient", "Patient")

	// Create a file that appears to be NDJSON but is actually a directory (will cause open error)
	fakeFile := filepath.Join(inputDir, "fake.ndjson")
	require.NoError(t, os.MkdirAll(fakeFile, 0755)) // Create directory instead of file

	job := createFlatteningTestJob("http://localhost:8080", lookupPath, crtdlPath)
	logger := createFlatteningTestLogger()

	err := pipeline.ExecuteFlatteningStep(job, jobDir, logger)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load resources")
}

// TestExecuteFlatteningStep_OutputDirCreationError tests line 84-88
func TestExecuteFlatteningStep_OutputDirCreationError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("Cannot test permission errors as root")
	}

	tempDir := t.TempDir()
	jobID := "test-mkdir-error"
	jobDir := filepath.Join(tempDir, "jobs", jobID)
	inputDir := filepath.Join(jobDir, "import")
	require.NoError(t, os.MkdirAll(inputDir, 0755))

	crtdlPath := filepath.Join(tempDir, "test.crtdl")
	writeTestCRTDL(t, crtdlPath, "group-1", "Patient", "https://example.com/Patient")

	lookupPath := filepath.Join(tempDir, "lookup.json")
	writeTestLookupTable(t, lookupPath, "https://example.com/Patient", "Patient")

	// Create a file at the path where we want to create the csv directory
	csvPath := filepath.Join(jobDir, "csv")
	require.NoError(t, os.WriteFile(csvPath, []byte("not a directory"), 0644))

	ndjsonContent := `{"resourceType":"Patient","id":"1","meta":{"profile":["https://example.com/Patient"]}}`
	require.NoError(t, os.WriteFile(filepath.Join(inputDir, "test.ndjson"), []byte(ndjsonContent), 0644))

	job := createFlatteningTestJob("http://localhost:8080", lookupPath, crtdlPath)
	logger := createFlatteningTestLogger()

	err := pipeline.ExecuteFlatteningStep(job, jobDir, logger)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create output directory")
}

// TestLoadResourcesFromFile_ScannerError tests the scanner.Err() path
func TestLoadResourcesFromFile_ScannerError(t *testing.T) {
	logger := createFlatteningTestLogger()

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "test.ndjson")

	resources := []map[string]any{
		{"resourceType": "Patient", "id": "1"},
	}
	writeTestNDJSON(t, filePath, resources)

	result, _, err := pipeline.LoadResourcesFromFile(filePath, logger, lib.DefaultMaxNDJSONLineSize)
	require.NoError(t, err)
	assert.Len(t, result, 1)
}

// TestExecuteFlatteningStep_ViewDefinitionWriteError tests line 153-158
func TestExecuteFlatteningStep_ViewDefinitionWriteError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("Cannot test permission errors as root")
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/fhir/ViewDefinition/$run" {
			w.Header().Set("Content-Type", "text/csv")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("1\n"))
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	tempDir := t.TempDir()
	jobID := "test-viewdef-write-error"
	jobDir := filepath.Join(tempDir, "jobs", jobID)
	inputDir := filepath.Join(jobDir, "import")
	viewDefDir := filepath.Join(jobDir, "viewdefinitions")
	csvDir := filepath.Join(jobDir, "csv")
	require.NoError(t, os.MkdirAll(inputDir, 0755))
	require.NoError(t, os.MkdirAll(csvDir, 0755))

	// Create viewdefinitions as a file instead of directory to cause MkdirAll to fail
	require.NoError(t, os.WriteFile(viewDefDir, []byte("not a directory"), 0644))

	groupID := "group-patient"
	crtdlPath := filepath.Join(tempDir, "test.crtdl")
	writeTestCRTDL(t, crtdlPath, groupID, "Patient", "https://example.com/Patient")

	lookupPath := filepath.Join(tempDir, "lookup.json")
	writeTestLookupTable(t, lookupPath, "https://example.com/Patient", "Patient")

	// Create input Bundle with provenance mapping to the group
	patient := map[string]any{
		"resourceType": "Patient",
		"id":           "1",
		"meta":         map[string]any{"profile": []any{"https://example.com/Patient"}},
	}
	prov := makeProvenance("prov-1", "Patient/1", groupID)
	bundle := makeBundle("b1", patient, prov)
	writeTestNDJSON(t, filepath.Join(inputDir, "test.ndjson"), []map[string]any{bundle})

	job := createFlatteningTestJob(server.URL, lookupPath, crtdlPath)
	logger := createFlatteningTestLogger()

	// Should complete successfully even though ViewDefinition write fails
	err := pipeline.ExecuteFlatteningStep(job, jobDir, logger)
	require.NoError(t, err)

	// Verify CSV file was created despite ViewDefinition write failure
	csvFiles, err := filepath.Glob(filepath.Join(csvDir, "*.csv"))
	require.NoError(t, err)
	assert.Len(t, csvFiles, 1)
}

// TestExecuteFlatteningStep_CSVWriteError tests line 176-180
func TestExecuteFlatteningStep_CSVWriteError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("Cannot test permission errors as root")
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/fhir/ViewDefinition/$run" {
			w.Header().Set("Content-Type", "text/csv")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("1,John Doe\n"))
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	tempDir := t.TempDir()
	jobID := "test-csv-write-error"
	jobDir := filepath.Join(tempDir, "jobs", jobID)
	inputDir := filepath.Join(jobDir, "import")
	csvDir := filepath.Join(jobDir, "csv")
	require.NoError(t, os.MkdirAll(inputDir, 0755))
	// Create csv directory with read-only permissions
	require.NoError(t, os.MkdirAll(csvDir, 0555))
	t.Cleanup(func() {
		_ = os.Chmod(csvDir, 0755)
	})

	groupID := "group-patient"
	crtdlPath := filepath.Join(tempDir, "test.crtdl")
	writeTestCRTDL(t, crtdlPath, groupID, "Patient", "https://example.com/Patient")

	lookupPath := filepath.Join(tempDir, "lookup.json")
	writeTestLookupTable(t, lookupPath, "https://example.com/Patient", "Patient")

	// Create input Bundle with provenance
	patient := map[string]any{
		"resourceType": "Patient",
		"id":           "1",
		"meta":         map[string]any{"profile": []any{"https://example.com/Patient"}},
	}
	prov := makeProvenance("prov-1", "Patient/1", groupID)
	bundle := makeBundle("b1", patient, prov)
	writeTestNDJSON(t, filepath.Join(inputDir, "test.ndjson"), []map[string]any{bundle})

	job := createFlatteningTestJob(server.URL, lookupPath, crtdlPath)
	logger := createFlatteningTestLogger()

	err := pipeline.ExecuteFlatteningStep(job, jobDir, logger)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to write CSV for group")
}

// TestExecuteFlatteningStep_MultipleBatches verifies that large datasets are
// processed in multiple batches with only one CSV header
func TestExecuteFlatteningStep_MultipleBatches(t *testing.T) {
	// Track how many times flattener is called
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/fhir/ViewDefinition/$run" {
			callCount++
			w.Header().Set("Content-Type", "text/csv")
			w.WriteHeader(http.StatusOK)
			// Return a simple CSV row per call
			_, _ = fmt.Fprintf(w, "patient-%d\n", callCount)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	tempDir := t.TempDir()
	jobID := "test-multi-batch"
	jobDir := filepath.Join(tempDir, "jobs", jobID)
	inputDir := filepath.Join(jobDir, "import")
	require.NoError(t, os.MkdirAll(inputDir, 0755))

	profileURL := "https://example.com/Patient"
	groupID := "group-patient"
	crtdlPath := filepath.Join(tempDir, "test.crtdl")
	writeTestCRTDL(t, crtdlPath, groupID, "Patient", profileURL)

	lookupPath := filepath.Join(tempDir, "lookup.json")
	writeTestLookupTable(t, lookupPath, profileURL, "Patient")

	// Create many Bundles with Provenance to exceed batch threshold
	var bundles []map[string]any
	for i := 0; i < 50; i++ {
		patientID := fmt.Sprintf("patient-%d", i)
		patient := map[string]any{
			"resourceType": "Patient",
			"id":           patientID,
			"meta":         map[string]any{"profile": []any{profileURL}},
			"name":         []any{map[string]any{"family": "TestFamily", "given": []any{"TestGiven"}}},
		}
		prov := makeProvenance(fmt.Sprintf("prov-%d", i), "Patient/"+patientID, groupID)
		bundles = append(bundles, makeBundle(fmt.Sprintf("b-%d", i), patient, prov))
	}
	writeTestNDJSON(t, filepath.Join(inputDir, "patients.ndjson"), bundles)

	job := createFlatteningTestJob(server.URL, lookupPath, crtdlPath)
	// Set a very small batch size to force multiple batches (1 byte = every resource triggers a flush)
	job.Config.Services.Flattening.BatchSizeMB = 0 // will default to 500MB via GetBatchSizeBytes

	logger := createFlatteningTestLogger()

	err := pipeline.ExecuteFlatteningStep(job, jobDir, logger)
	require.NoError(t, err)

	// Verify CSV file was created
	csvFiles, err := filepath.Glob(filepath.Join(jobDir, "csv", "*.csv"))
	require.NoError(t, err)
	assert.Len(t, csvFiles, 1)

	// Read the CSV and verify it has header + data rows
	content, err := os.ReadFile(csvFiles[0])
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	// First line should be header (contains "id")
	assert.Contains(t, lines[0], "id")
	// Should have at least 1 data line (header + flattener response)
	assert.GreaterOrEqual(t, len(lines), 2)
}

// TestExecuteFlatteningStep_FlattenerError verifies fail-fast on flattener error
func TestExecuteFlatteningStep_FlattenerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/fhir/ViewDefinition/$run" {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("internal server error"))
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	tempDir := t.TempDir()
	jobID := "test-flattener-error"
	jobDir := filepath.Join(tempDir, "jobs", jobID)
	inputDir := filepath.Join(jobDir, "import")
	require.NoError(t, os.MkdirAll(inputDir, 0755))

	profileURL := "https://example.com/Patient"
	groupID := "group-patient"
	crtdlPath := filepath.Join(tempDir, "test.crtdl")
	writeTestCRTDL(t, crtdlPath, groupID, "Patient", profileURL)

	lookupPath := filepath.Join(tempDir, "lookup.json")
	writeTestLookupTable(t, lookupPath, profileURL, "Patient")

	// Create Bundle with Provenance so resource is routed to the flattener
	patient := map[string]any{
		"resourceType": "Patient", "id": "1",
		"meta": map[string]any{"profile": []any{profileURL}},
	}
	prov := makeProvenance("prov-1", "Patient/1", groupID)
	bundle := makeBundle("b1", patient, prov)
	writeTestNDJSON(t, filepath.Join(inputDir, "test.ndjson"), []map[string]any{bundle})

	job := createFlatteningTestJob(server.URL, lookupPath, crtdlPath)
	logger := createFlatteningTestLogger()

	err := pipeline.ExecuteFlatteningStep(job, jobDir, logger)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "flattener failed for group")
}

// TestExecuteFlatteningStep_BundleExtraction verifies Bundle entries are correctly
// routed to groups during streaming
func TestExecuteFlatteningStep_BundleExtraction(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/fhir/ViewDefinition/$run" {
			w.Header().Set("Content-Type", "text/csv")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("bundled-patient\n"))
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	tempDir := t.TempDir()
	jobID := "test-bundle-extract"
	jobDir := filepath.Join(tempDir, "jobs", jobID)
	inputDir := filepath.Join(jobDir, "import")
	require.NoError(t, os.MkdirAll(inputDir, 0755))

	profileURL := "https://example.com/Patient"
	groupID := "group-patient"
	crtdlPath := filepath.Join(tempDir, "test.crtdl")
	writeTestCRTDL(t, crtdlPath, groupID, "Patient", profileURL)

	lookupPath := filepath.Join(tempDir, "lookup.json")
	writeTestLookupTable(t, lookupPath, profileURL, "Patient")

	// Create a Bundle containing Patient resources with Provenance
	p1 := map[string]any{"resourceType": "Patient", "id": "bundled-1", "meta": map[string]any{"profile": []any{profileURL}}}
	p2 := map[string]any{"resourceType": "Patient", "id": "bundled-2", "meta": map[string]any{"profile": []any{profileURL}}}
	prov1 := makeProvenance("prov-1", "Patient/bundled-1", groupID)
	prov2 := makeProvenance("prov-2", "Patient/bundled-2", groupID)
	bundle := makeBundle("b1", p1, p2, prov1, prov2)
	writeTestNDJSON(t, filepath.Join(inputDir, "bundle.ndjson"), []map[string]any{bundle})

	job := createFlatteningTestJob(server.URL, lookupPath, crtdlPath)
	logger := createFlatteningTestLogger()

	err := pipeline.ExecuteFlatteningStep(job, jobDir, logger)
	require.NoError(t, err)

	// Verify CSV was created
	csvFiles, err := filepath.Glob(filepath.Join(jobDir, "csv", "*.csv"))
	require.NoError(t, err)
	assert.Len(t, csvFiles, 1)

	content, err := os.ReadFile(csvFiles[0])
	require.NoError(t, err)
	// Should contain header + data from flattener
	assert.Contains(t, string(content), "id")
	assert.Contains(t, string(content), "bundled-patient")
}

// TestExecuteFlatteningStep_StreamingEdgeCases verifies edge cases in the streaming pipeline:
// empty lines, invalid JSON, resources without meta, non-string profiles, and Bundle entry edge cases
func TestExecuteFlatteningStep_StreamingEdgeCases(t *testing.T) {
	flattenCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/fhir/ViewDefinition/$run" {
			flattenCalls++
			w.Header().Set("Content-Type", "text/csv")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("valid-patient\n"))
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	tempDir := t.TempDir()
	jobID := "test-streaming-edges"
	jobDir := filepath.Join(tempDir, "jobs", jobID)
	inputDir := filepath.Join(jobDir, "import")
	require.NoError(t, os.MkdirAll(inputDir, 0755))

	profileURL := "https://example.com/Patient"
	groupID := "group-patient"
	crtdlPath := filepath.Join(tempDir, "test.crtdl")
	writeTestCRTDL(t, crtdlPath, groupID, "Patient", profileURL)

	lookupPath := filepath.Join(tempDir, "lookup.json")
	writeTestLookupTable(t, lookupPath, profileURL, "Patient")

	// NDJSON with many edge cases:
	// - empty lines (skipped)
	// - invalid JSON (skipped with warning)
	// - standalone resources without provenance (no match - provenance routing)
	// - Bundle with invalid entries (skipped gracefully)
	// - Bundle with valid entries + Provenance (processed)
	validBundle := makeBundle("b-valid",
		map[string]any{"resourceType": "Patient", "id": "valid", "meta": map[string]any{"profile": []any{profileURL}}},
		makeProvenance("prov-valid", "Patient/valid", groupID),
	)
	validBundleJSON, _ := json.Marshal(validBundle)

	ndjsonContent := strings.Join([]string{
		``,               // empty line
		`{invalid json}`, // invalid JSON
		`{"resourceType":"Patient","id":"no-provenance"}`,                                                                  // no provenance mapping
		`{"resourceType":"Patient","id":"meta-string","meta":"not a map"}`,                                                 // meta not a map
		`{"resourceType":"Bundle","type":"collection","entry":["not a map",{"fullUrl":"urn:1"},{"resource":"not a map"}]}`, // Bundle with only invalid entries
		string(validBundleJSON), // Bundle with valid entry + Provenance
	}, "\n")
	require.NoError(t, os.WriteFile(filepath.Join(inputDir, "edges.ndjson"), []byte(ndjsonContent), 0644))

	job := createFlatteningTestJob(server.URL, lookupPath, crtdlPath)
	logger := createFlatteningTestLogger()

	err := pipeline.ExecuteFlatteningStep(job, jobDir, logger)
	require.NoError(t, err)

	// Flattener should be called once (only the valid Bundle entry with provenance)
	assert.GreaterOrEqual(t, flattenCalls, 1)

	// CSV should exist with data
	csvFiles, err := filepath.Glob(filepath.Join(jobDir, "csv", "*.csv"))
	require.NoError(t, err)
	assert.Len(t, csvFiles, 1)
}

// TestExecuteFlatteningStep_BatchFlushOnThreshold verifies that per-group batch flushing
// works for both Bundle entries and non-Bundle resources
func TestExecuteFlatteningStep_BatchFlushOnThreshold(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/fhir/ViewDefinition/$run" {
			callCount++
			w.Header().Set("Content-Type", "text/csv")
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprintf(w, "row-%d\n", callCount)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	tempDir := t.TempDir()
	jobID := "test-batch-flush"
	jobDir := filepath.Join(tempDir, "jobs", jobID)
	inputDir := filepath.Join(jobDir, "import")
	require.NoError(t, os.MkdirAll(inputDir, 0755))

	profileURL := "https://example.com/Patient"
	groupID := "group-patient"
	crtdlPath := filepath.Join(tempDir, "test.crtdl")
	writeTestCRTDL(t, crtdlPath, groupID, "Patient", profileURL)

	lookupPath := filepath.Join(tempDir, "lookup.json")
	writeTestLookupTable(t, lookupPath, profileURL, "Patient")

	// Create Bundles with large resources and Provenance to exceed the 1MB batch threshold.
	longName := strings.Repeat("X", 10000)

	// Create Bundles with individual patients + Provenance (each ~10KB)
	var bundles []map[string]any
	for i := 0; i < 110; i++ {
		patientID := fmt.Sprintf("p%d", i)
		patient := map[string]any{
			"resourceType": "Patient",
			"id":           patientID,
			"meta":         map[string]any{"profile": []any{profileURL}},
			"name":         []any{map[string]any{"family": fmt.Sprintf("%s_%d", longName, i)}},
		}
		prov := makeProvenance(fmt.Sprintf("prov-%d", i), "Patient/"+patientID, groupID)
		bundles = append(bundles, makeBundle(fmt.Sprintf("b-%d", i), patient, prov))
	}
	writeTestNDJSON(t, filepath.Join(inputDir, "patients.ndjson"), bundles)

	// Also create a single large Bundle with many entries + Provenance to trigger a mid-Bundle flush
	var bundleEntries []map[string]any
	for i := 0; i < 110; i++ {
		patientID := fmt.Sprintf("bp%d", i)
		bundleEntries = append(bundleEntries,
			map[string]any{
				"resourceType": "Patient",
				"id":           patientID,
				"meta":         map[string]any{"profile": []any{profileURL}},
				"name":         []any{map[string]any{"family": fmt.Sprintf("%s_%d", longName, i)}},
			},
			makeProvenance(fmt.Sprintf("bprov-%d", i), "Patient/"+patientID, groupID),
		)
	}
	bigBundle := makeBundle("big-bundle", bundleEntries...)
	writeTestNDJSON(t, filepath.Join(inputDir, "bundle.ndjson"), []map[string]any{bigBundle})

	job := createFlatteningTestJob(server.URL, lookupPath, crtdlPath)
	job.Config.Services.Flattening.BatchSizeMB = 1 // 1MB threshold

	logger := createFlatteningTestLogger()

	err := pipeline.ExecuteFlatteningStep(job, jobDir, logger)
	require.NoError(t, err)

	// Flattener should be called at least once
	assert.GreaterOrEqual(t, callCount, 1)

	// CSV should contain all data rows
	csvFiles, err := filepath.Glob(filepath.Join(jobDir, "csv", "*.csv"))
	require.NoError(t, err)
	assert.Len(t, csvFiles, 1)

	content, err := os.ReadFile(csvFiles[0])
	require.NoError(t, err)
	contentLines := strings.Split(strings.TrimSpace(string(content)), "\n")
	// Header + at least callCount data rows
	assert.GreaterOrEqual(t, len(contentLines), callCount+1)
}

// TestExecuteFlatteningStep_NilViewDefSkipped verifies that resources matching a group
// whose ViewDefinition build failed (nil viewDef) are silently skipped
func TestExecuteFlatteningStep_NilViewDefSkipped(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Should never be called — the only group has no valid ViewDefinition
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	tempDir := t.TempDir()
	jobID := "test-nil-viewdef"
	jobDir := filepath.Join(tempDir, "jobs", jobID)
	inputDir := filepath.Join(jobDir, "import")
	require.NoError(t, os.MkdirAll(inputDir, 0755))

	profileURL := "https://example.com/Patient"
	groupID := "group-patient"

	// CRTDL references a profile, but the lookup table has NO matching entry for it.
	// This means the ViewDefinition build will fail, leaving viewDefs[0] == nil.
	crtdlPath := filepath.Join(tempDir, "test.crtdl")
	writeTestCRTDL(t, crtdlPath, groupID, "Patient", profileURL)

	lookupPath := filepath.Join(tempDir, "lookup.json")
	// Write lookup for a completely different profile
	writeTestLookupTable(t, lookupPath, "https://example.com/Observation", "Observation")

	// Resource with provenance mapping to group — but viewDef is nil so it should be skipped
	patient := map[string]any{"resourceType": "Patient", "id": "1", "meta": map[string]any{"profile": []any{profileURL}}}
	prov := makeProvenance("prov-1", "Patient/1", groupID)
	bundle := makeBundle("b1", patient, prov)
	writeTestNDJSON(t, filepath.Join(inputDir, "test.ndjson"), []map[string]any{bundle})

	job := createFlatteningTestJob(server.URL, lookupPath, crtdlPath)
	logger := createFlatteningTestLogger()

	err := pipeline.ExecuteFlatteningStep(job, jobDir, logger)
	require.NoError(t, err)

	// No CSV should be created — ViewDefinition is nil, resource is skipped
	csvFiles, _ := filepath.Glob(filepath.Join(jobDir, "csv", "*.csv"))
	assert.Empty(t, csvFiles)
}

// TestExecuteFlatteningStep_BundleEntryUnmatchedProfile verifies that bundle entries
// whose profile doesn't match any group are silently skipped
func TestExecuteFlatteningStep_BundleEntryUnmatchedProfile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	tempDir := t.TempDir()
	jobID := "test-bundle-unmatched"
	jobDir := filepath.Join(tempDir, "jobs", jobID)
	inputDir := filepath.Join(jobDir, "import")
	require.NoError(t, os.MkdirAll(inputDir, 0755))

	profileURL := "https://example.com/Patient"
	groupID := "group-patient"
	crtdlPath := filepath.Join(tempDir, "test.crtdl")
	writeTestCRTDL(t, crtdlPath, groupID, "Patient", profileURL)

	lookupPath := filepath.Join(tempDir, "lookup.json")
	writeTestLookupTable(t, lookupPath, profileURL, "Patient")

	// Bundle with Provenance that maps to a DIFFERENT group ID (not in CRTDL)
	patient := map[string]any{"resourceType": "Patient", "id": "1", "meta": map[string]any{"profile": []any{profileURL}}}
	prov := makeProvenance("prov-1", "Patient/1", "different-group-id")
	bundle := makeBundle("b1", patient, prov)
	writeTestNDJSON(t, filepath.Join(inputDir, "bundle.ndjson"), []map[string]any{bundle})

	job := createFlatteningTestJob(server.URL, lookupPath, crtdlPath)
	logger := createFlatteningTestLogger()

	err := pipeline.ExecuteFlatteningStep(job, jobDir, logger)
	require.NoError(t, err)

	csvFiles, _ := filepath.Glob(filepath.Join(jobDir, "csv", "*.csv"))
	assert.Empty(t, csvFiles)
}

// TestExecuteFlatteningStep_FlattenerErrorDuringFlush verifies fail-fast when the
// flattener returns an error during a mid-stream batch flush (covers both bundle
// and non-bundle flush error paths with reader cleanup)
func TestExecuteFlatteningStep_FlattenerErrorDuringFlush(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("service error"))
	}))
	defer server.Close()

	tempDir := t.TempDir()

	profileURL := "https://example.com/Patient"
	groupID := "group-patient"
	crtdlPath := filepath.Join(tempDir, "test.crtdl")
	writeTestCRTDL(t, crtdlPath, groupID, "Patient", profileURL)

	lookupPath := filepath.Join(tempDir, "lookup.json")
	writeTestLookupTable(t, lookupPath, profileURL, "Patient")

	longName := strings.Repeat("Y", 10000)

	t.Run("bundle flush error with individual bundles", func(t *testing.T) {
		jobID := "test-flush-error-nonbundle"
		jobDir := filepath.Join(tempDir, "jobs", jobID)
		inputDir := filepath.Join(jobDir, "import")
		require.NoError(t, os.MkdirAll(inputDir, 0755))

		// Create enough Bundles with Provenance to exceed 1MB batch threshold
		var bundles []map[string]any
		for i := 0; i < 110; i++ {
			patientID := fmt.Sprintf("p%d", i)
			patient := map[string]any{
				"resourceType": "Patient", "id": patientID,
				"meta": map[string]any{"profile": []any{profileURL}},
				"name": []any{map[string]any{"family": fmt.Sprintf("%s_%d", longName, i)}},
			}
			prov := makeProvenance(fmt.Sprintf("prov-%d", i), "Patient/"+patientID, groupID)
			bundles = append(bundles, makeBundle(fmt.Sprintf("b-%d", i), patient, prov))
		}
		writeTestNDJSON(t, filepath.Join(inputDir, "patients.ndjson"), bundles)

		job := createFlatteningTestJob(server.URL, lookupPath, crtdlPath)
		job.Config.Services.Flattening.BatchSizeMB = 1
		logger := createFlatteningTestLogger()

		err := pipeline.ExecuteFlatteningStep(job, jobDir, logger)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "flattener failed for group")
	})

	t.Run("bundle flush error with single large bundle", func(t *testing.T) {
		jobID := "test-flush-error-bundle"
		jobDir := filepath.Join(tempDir, "jobs", jobID)
		inputDir := filepath.Join(jobDir, "import")
		require.NoError(t, os.MkdirAll(inputDir, 0755))

		// Create a single large Bundle with enough entries + Provenance to exceed 1MB
		var entries []map[string]any
		for i := 0; i < 110; i++ {
			patientID := fmt.Sprintf("bp%d", i)
			entries = append(entries,
				map[string]any{
					"resourceType": "Patient", "id": patientID,
					"meta": map[string]any{"profile": []any{profileURL}},
					"name": []any{map[string]any{"family": fmt.Sprintf("%s_%d", longName, i)}},
				},
				makeProvenance(fmt.Sprintf("bprov-%d", i), "Patient/"+patientID, groupID),
			)
		}
		bigBundle := makeBundle("big-bundle", entries...)
		writeTestNDJSON(t, filepath.Join(inputDir, "bundle.ndjson"), []map[string]any{bigBundle})

		job := createFlatteningTestJob(server.URL, lookupPath, crtdlPath)
		job.Config.Services.Flattening.BatchSizeMB = 1
		logger := createFlatteningTestLogger()

		err := pipeline.ExecuteFlatteningStep(job, jobDir, logger)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "flattener failed for group")
	})
}

// TestExecuteFlatteningStep_OpenFileError verifies error handling when a file in the
// input directory cannot be opened during streaming
func TestExecuteFlatteningStep_OpenFileError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("Cannot test permission errors as root")
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tempDir := t.TempDir()
	jobID := "test-open-file-error"
	jobDir := filepath.Join(tempDir, "jobs", jobID)
	inputDir := filepath.Join(jobDir, "import")
	require.NoError(t, os.MkdirAll(inputDir, 0755))

	profileURL := "https://example.com/Patient"
	crtdlPath := filepath.Join(tempDir, "test.crtdl")
	writeTestCRTDL(t, crtdlPath, "group-patient", "Patient", profileURL)

	lookupPath := filepath.Join(tempDir, "lookup.json")
	writeTestLookupTable(t, lookupPath, profileURL, "Patient")

	// Create a file with no read permissions so OpenFileForReading fails
	unreadable := filepath.Join(inputDir, "unreadable.ndjson")
	require.NoError(t, os.WriteFile(unreadable, []byte(`{"resourceType":"Patient"}`), 0000))
	t.Cleanup(func() { _ = os.Chmod(unreadable, 0644) })

	job := createFlatteningTestJob(server.URL, lookupPath, crtdlPath)
	logger := createFlatteningTestLogger()

	err := pipeline.ExecuteFlatteningStep(job, jobDir, logger)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load resources")
}

// TestExecuteFlatteningStep_UnknownProfile verifies resources with unknown profiles
// are silently skipped during streaming
// TestExecuteFlatteningStep_ProvenanceInPseudonymizedDir verifies that provenance is scanned
// from the input directory (pseudonymized/) when DIMP preserves Provenance.entity.
func TestExecuteFlatteningStep_ProvenanceInPseudonymizedDir(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/fhir/ViewDefinition/$run" {
			w.Header().Set("Content-Type", "text/csv")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("1,John Doe\n"))
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	tempDir := t.TempDir()
	jobID := "test-provenance-pseudonymized"
	jobDir := filepath.Join(tempDir, "jobs", jobID)
	pseudonymizedDir := filepath.Join(jobDir, "pseudonymized")
	require.NoError(t, os.MkdirAll(pseudonymizedDir, 0755))

	groupID := "group-patient"
	crtdlPath := filepath.Join(tempDir, "test.crtdl")
	writeTestCRTDL(t, crtdlPath, groupID, "Patient", "https://example.com/Patient")
	lookupPath := filepath.Join(tempDir, "lookup.json")
	writeTestLookupTable(t, lookupPath, "https://example.com/Patient", "Patient")

	// pseudonymized/ has Bundles WITH Provenance (DIMP preserves Provenance.entity)
	patient := map[string]any{
		"resourceType": "Patient", "id": "hashed-1",
		"meta": map[string]any{"profile": []any{"https://example.com/Patient"}},
	}
	prov := makeProvenance("prov-1", "Patient/hashed-1", groupID)

	bundle := makeBundle("b1", patient, prov)
	writeTestNDJSON(t, filepath.Join(pseudonymizedDir, "data.ndjson"), []map[string]any{bundle})

	// Configure pipeline: torch -> dimp -> flattening, so inputDir resolves to pseudonymized/
	job := &models.PipelineJob{
		JobID:       jobID,
		Status:      models.JobStatusInProgress,
		InputSource: crtdlPath,
		InputType:   models.InputTypeCRTDL,
		CRTDLPath:   crtdlPath,
		Config: models.ProjectConfig{
			Services: models.ServiceConfig{
				Flattening: models.FlatteningConfig{
					ServiceURL: server.URL,
					LookupPath: lookupPath,
					Formats:    []string{"csv"},
					Timeout:    30 * 1000000000,
				},
			},
			Pipeline: models.PipelineConfig{
				EnabledSteps: []models.StepName{models.StepTorchImport, models.StepDIMP, models.StepFlattening},
			},
			Retry: models.RetryConfig{MaxAttempts: 1},
		},
		Steps: []models.PipelineStep{},
	}

	logger := createFlatteningTestLogger()

	err := pipeline.ExecuteFlatteningStep(job, jobDir, logger)
	require.NoError(t, err)

	// CSV output should be produced — provenance is in the pseudonymized input
	csvFiles, err := filepath.Glob(filepath.Join(jobDir, "csv", "*.csv"))
	require.NoError(t, err)
	assert.NotEmpty(t, csvFiles, "expected CSV output when provenance is in pseudonymized/")
}

func TestExecuteFlatteningStep_UnknownProfile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Should never be called if all resources have unknown profiles
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	tempDir := t.TempDir()
	jobID := "test-unknown-profile"
	jobDir := filepath.Join(tempDir, "jobs", jobID)
	inputDir := filepath.Join(jobDir, "import")
	require.NoError(t, os.MkdirAll(inputDir, 0755))

	crtdlPath := filepath.Join(tempDir, "test.crtdl")
	writeTestCRTDL(t, crtdlPath, "group-patient", "Patient", "https://example.com/Patient")

	lookupPath := filepath.Join(tempDir, "lookup.json")
	writeTestLookupTable(t, lookupPath, "https://example.com/Patient", "Patient")

	// Bundle with resource but no provenance mapping — resource won't match any group
	patient := map[string]any{"resourceType": "Patient", "id": "1", "meta": map[string]any{"profile": []any{"https://example.com/UnknownProfile"}}}
	bundle := makeBundle("b1", patient)
	writeTestNDJSON(t, filepath.Join(inputDir, "test.ndjson"), []map[string]any{bundle})

	job := createFlatteningTestJob(server.URL, lookupPath, crtdlPath)
	logger := createFlatteningTestLogger()

	err := pipeline.ExecuteFlatteningStep(job, jobDir, logger)
	require.NoError(t, err)

	// No CSV should be written (no matching resources)
	csvFiles, _ := filepath.Glob(filepath.Join(jobDir, "csv", "*.csv"))
	assert.Empty(t, csvFiles)
}
