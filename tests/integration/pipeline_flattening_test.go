package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
)

// makeProvenance builds a Provenance resource for integration testing
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

// makeBundle creates a Bundle with given resources as entries
func makeBundle(id string, resources ...map[string]any) map[string]any {
	entries := make([]any, 0, len(resources))
	for _, r := range resources {
		entries = append(entries, map[string]any{
			"resource": r,
			"request": map[string]any{
				"method": "PUT",
				"url":    r["resourceType"].(string) + "/" + r["id"].(string),
			},
		})
	}
	return map[string]any{
		"resourceType": "Bundle",
		"id":           id,
		"type":         "transaction",
		"entry":        entries,
	}
}

func TestExecuteFlatteningStep_FullPipeline(t *testing.T) {
	// Create a mock fhir-flattener server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/fhir/ViewDefinition/$run" {
			w.Header().Set("Content-Type", "application/x-ndjson")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"1","name":"John Doe"}` + "\n"))
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	// Setup test directories
	tempDir := t.TempDir()
	jobsDir := filepath.Join(tempDir, "jobs")
	jobID := "test-flattening-job"
	jobDir := filepath.Join(jobsDir, jobID)
	inputDir := filepath.Join(jobDir, "import")
	outputDir := filepath.Join(jobDir, "csv")

	require.NoError(t, os.MkdirAll(inputDir, 0755))

	groupID := "group-patient-1"

	// Create CRTDL file with group ID
	crtdlPath := filepath.Join(tempDir, "test.json")
	crtdlContent := `{
		"dataExtraction": {
			"attributeGroups": [
				{
					"id": "` + groupID + `",
					"name": "Patients",
					"groupReference": "https://example.com/Patient",
					"attributes": [
						{"attributeRef": "Patient.id", "mustHave": true},
						{"attributeRef": "Patient.name", "mustHave": false}
					]
				}
			]
		}
	}`
	require.NoError(t, os.WriteFile(crtdlPath, []byte(crtdlContent), 0644))

	// Create lookup table file
	lookupPath := filepath.Join(tempDir, "lookup.json")
	lookupContent := `[
		{
			"url": "https://example.com/Patient",
			"resourceType": "Patient",
			"elements": {
				"Patient.id": {
					"viewDefinition": {
						"select": [{"column": [{"name": "id", "path": "id"}]}]
					}
				},
				"Patient.name": {
					"viewDefinition": {
						"select": [{"column": [{"name": "name", "path": "name[0].text"}]}]
					}
				}
			}
		}
	]`
	require.NoError(t, os.WriteFile(lookupPath, []byte(lookupContent), 0644))

	// Create input NDJSON with Bundles containing Provenance resources
	patient1 := map[string]any{
		"resourceType": "Patient", "id": "1",
		"meta": map[string]any{"profile": []any{"https://example.com/Patient"}},
		"name": []any{map[string]any{"text": "John Doe"}},
	}
	patient2 := map[string]any{
		"resourceType": "Patient", "id": "2",
		"meta": map[string]any{"profile": []any{"https://example.com/Patient"}},
		"name": []any{map[string]any{"text": "Jane Smith"}},
	}
	prov1 := makeProvenance("prov-1", "Patient/1", groupID)
	prov2 := makeProvenance("prov-2", "Patient/2", groupID)

	bundle1 := makeBundle("b1", patient1, prov1)
	bundle2 := makeBundle("b2", patient2, prov2)

	// Write bundles as NDJSON
	f, err := os.Create(filepath.Join(inputDir, "patients.ndjson"))
	require.NoError(t, err)
	for _, b := range []map[string]any{bundle1, bundle2} {
		data, err := json.Marshal(b)
		require.NoError(t, err)
		_, err = f.Write(data)
		require.NoError(t, err)
		_, err = f.WriteString("\n")
		require.NoError(t, err)
	}
	require.NoError(t, f.Close())

	// Create job configuration
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
					Timeout:    30 * time.Second,
				},
			},
			Pipeline: models.PipelineConfig{
				EnabledSteps: []models.StepName{models.StepLocalImport, models.StepFlattening},
			},
			Retry: models.RetryConfig{MaxAttempts: 1},
		},
		Steps: []models.PipelineStep{},
	}

	logger := lib.NewLogger(lib.LogLevelDebug)

	// Execute flattening step
	err = runPipelineStep(models.StepFlattening, job, jobDir, logger)
	require.NoError(t, err)

	// Verify output files were created
	csvFiles, err := filepath.Glob(filepath.Join(outputDir, "*.csv"))
	require.NoError(t, err)
	assert.Len(t, csvFiles, 1)

	// Verify CSV content
	csvContent, err := os.ReadFile(csvFiles[0])
	require.NoError(t, err)
	assert.Contains(t, string(csvContent), "id,") // Header should include id column
}

func TestExecuteFlatteningStep_NotEnabled(t *testing.T) {
	tempDir := t.TempDir()
	jobID := "test-job-disabled"
	jobDir := filepath.Join(tempDir, "jobs", jobID)
	require.NoError(t, os.MkdirAll(jobDir, 0755))

	job := &models.PipelineJob{
		JobID:     jobID,
		Status:    models.JobStatusInProgress,
		InputType: models.InputTypeCRTDL,
		Config: models.ProjectConfig{
			Pipeline: models.PipelineConfig{
				EnabledSteps: []models.StepName{models.StepLocalImport}, // Flattening not enabled
			},
		},
	}

	logger := lib.NewLogger(lib.LogLevelDebug)
	err := runPipelineStep(models.StepFlattening, job, jobDir, logger)
	assert.NoError(t, err) // Should skip without error
}

// TestExecuteFlatteningStep_NoCRTDLAttached verifies that flattening rejects
// a job with no CRTDL (CRTDLPath empty) regardless of input type. Previously
// this test asserted coupling between InputType and CRTDL requirement; after
// issue #286 the two are decoupled and the check is purely on CRTDLPath.
func TestExecuteFlatteningStep_NoCRTDLAttached(t *testing.T) {
	tempDir := t.TempDir()
	jobID := "test-job-no-crtdl"
	jobDir := filepath.Join(tempDir, "jobs", jobID)
	require.NoError(t, os.MkdirAll(jobDir, 0755))

	job := &models.PipelineJob{
		JobID:     jobID,
		Status:    models.JobStatusInProgress,
		InputType: models.InputTypeLocal, // intentionally not CRTDL
		// CRTDLPath intentionally empty
		Config: models.ProjectConfig{
			Services: models.ServiceConfig{
				Flattening: models.FlatteningConfig{
					ServiceURL: "http://localhost:8080",
					LookupPath: "/path/to/lookup.json",
					Formats:    []string{"csv"},
					Timeout:    30 * time.Second,
				},
			},
			Pipeline: models.PipelineConfig{
				EnabledSteps: []models.StepName{models.StepFlattening},
			},
		},
	}

	logger := lib.NewLogger(lib.LogLevelDebug)
	err := runPipelineStep(models.StepFlattening, job, jobDir, logger)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires a CRTDL file")
	assert.Contains(t, err.Error(), "--crtdl")
}

func TestExecuteFlatteningStep_MissingCRTDL(t *testing.T) {
	tempDir := t.TempDir()
	jobID := "test-job-missing-crtdl"
	jobDir := filepath.Join(tempDir, "jobs", jobID)
	require.NoError(t, os.MkdirAll(jobDir, 0755))

	job := &models.PipelineJob{
		JobID:       jobID,
		Status:      models.JobStatusInProgress,
		InputSource: "/nonexistent/path.json",
		InputType:   models.InputTypeCRTDL,
		CRTDLPath:   "/nonexistent/path.json",
		Config: models.ProjectConfig{
			Services: models.ServiceConfig{
				Flattening: models.FlatteningConfig{
					ServiceURL: "http://localhost:8080",
					LookupPath: "/path/to/lookup.json",
					Formats:    []string{"csv"},
					Timeout:    30 * time.Second,
				},
			},
			Pipeline: models.PipelineConfig{
				EnabledSteps: []models.StepName{models.StepFlattening},
			},
		},
	}

	logger := lib.NewLogger(lib.LogLevelDebug)
	err := runPipelineStep(models.StepFlattening, job, jobDir, logger)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse CRTDL")
}

func TestExecuteFlatteningStep_MissingLookupTables(t *testing.T) {
	tempDir := t.TempDir()
	jobID := "test-job-missing-lookup"
	jobDir := filepath.Join(tempDir, "jobs", jobID)
	require.NoError(t, os.MkdirAll(jobDir, 0755))

	// Create valid CRTDL
	crtdlPath := filepath.Join(tempDir, "test.json")
	crtdlContent := `{
		"dataExtraction": {
			"attributeGroups": [{
				"id": "group-1",
				"name": "Test",
				"groupReference": "https://example.com/Test",
				"attributes": [{"attributeRef": "Test.id"}]
			}]
		}
	}`
	require.NoError(t, os.WriteFile(crtdlPath, []byte(crtdlContent), 0644))

	job := &models.PipelineJob{
		JobID:       jobID,
		Status:      models.JobStatusInProgress,
		InputSource: crtdlPath,
		InputType:   models.InputTypeCRTDL,
		CRTDLPath:   crtdlPath,
		Config: models.ProjectConfig{
			Services: models.ServiceConfig{
				Flattening: models.FlatteningConfig{
					ServiceURL: "http://localhost:8080",
					LookupPath: "/nonexistent/lookup.json",
					Formats:    []string{"csv"},
					Timeout:    30 * time.Second,
				},
			},
			Pipeline: models.PipelineConfig{
				EnabledSteps: []models.StepName{models.StepFlattening},
			},
		},
	}

	logger := lib.NewLogger(lib.LogLevelDebug)
	err := runPipelineStep(models.StepFlattening, job, jobDir, logger)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load lookup tables")
}

func TestExecuteFlatteningStep_NoInputFiles(t *testing.T) {
	tempDir := t.TempDir()
	jobID := "test-job-no-input"
	jobDir := filepath.Join(tempDir, "jobs", jobID)
	inputDir := filepath.Join(jobDir, "import")
	require.NoError(t, os.MkdirAll(inputDir, 0755)) // Empty input dir

	// Create valid CRTDL
	crtdlPath := filepath.Join(tempDir, "test.json")
	crtdlContent := `{
		"dataExtraction": {
			"attributeGroups": [{
				"id": "group-1",
				"name": "Test",
				"groupReference": "https://example.com/Test",
				"attributes": [{"attributeRef": "Test.id"}]
			}]
		}
	}`
	require.NoError(t, os.WriteFile(crtdlPath, []byte(crtdlContent), 0644))

	// Create valid lookup table
	lookupPath := filepath.Join(tempDir, "lookup.json")
	lookupContent := `[{"url": "https://example.com/Test", "resourceType": "Test", "elements": {}}]`
	require.NoError(t, os.WriteFile(lookupPath, []byte(lookupContent), 0644))

	job := &models.PipelineJob{
		JobID:       jobID,
		Status:      models.JobStatusInProgress,
		InputSource: crtdlPath,
		InputType:   models.InputTypeCRTDL,
		CRTDLPath:   crtdlPath,
		Config: models.ProjectConfig{
			Services: models.ServiceConfig{
				Flattening: models.FlatteningConfig{
					ServiceURL: "http://localhost:8080",
					LookupPath: lookupPath,
					Formats:    []string{"csv"},
					Timeout:    30 * time.Second,
				},
			},
			Pipeline: models.PipelineConfig{
				EnabledSteps: []models.StepName{models.StepLocalImport, models.StepFlattening},
			},
		},
	}

	logger := lib.NewLogger(lib.LogLevelDebug)
	err := runPipelineStep(models.StepFlattening, job, jobDir, logger)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no FHIR NDJSON files found")
}

func TestExecuteFlatteningStep_FlattenerServiceError(t *testing.T) {
	// Create a mock server that returns 500 error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error": "Internal server error"}`))
	}))
	defer server.Close()

	tempDir := t.TempDir()
	jobID := "test-job-service-error"
	jobDir := filepath.Join(tempDir, "jobs", jobID)
	inputDir := filepath.Join(jobDir, "import")
	require.NoError(t, os.MkdirAll(inputDir, 0755))

	groupID := "group-patient"

	// Create CRTDL
	crtdlPath := filepath.Join(tempDir, "test.json")
	crtdlContent := `{
		"dataExtraction": {
			"attributeGroups": [{
				"id": "` + groupID + `",
				"name": "Test",
				"groupReference": "https://example.com/Patient",
				"attributes": [{"attributeRef": "Patient.id"}]
			}]
		}
	}`
	require.NoError(t, os.WriteFile(crtdlPath, []byte(crtdlContent), 0644))

	// Create lookup table
	lookupPath := filepath.Join(tempDir, "lookup.json")
	lookupContent := `[{
		"url": "https://example.com/Patient",
		"resourceType": "Patient",
		"elements": {
			"Patient.id": {"viewDefinition": {"select": [{"column": [{"name": "id", "path": "id"}]}]}}
		}
	}]`
	require.NoError(t, os.WriteFile(lookupPath, []byte(lookupContent), 0644))

	// Create input Bundle with provenance
	patient := map[string]any{
		"resourceType": "Patient", "id": "1",
		"meta": map[string]any{"profile": []any{"https://example.com/Patient"}},
	}
	prov := makeProvenance("prov-1", "Patient/1", groupID)
	bundle := makeBundle("b1", patient, prov)

	data, err := json.Marshal(bundle)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(inputDir, "patients.ndjson"), append(data, '\n'), 0644))

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
					Timeout:    30 * time.Second,
				},
			},
			Pipeline: models.PipelineConfig{
				EnabledSteps: []models.StepName{models.StepLocalImport, models.StepFlattening},
			},
			Retry: models.RetryConfig{MaxAttempts: 1},
		},
	}

	logger := lib.NewLogger(lib.LogLevelDebug)
	err = runPipelineStep(models.StepFlattening, job, jobDir, logger)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "flattener failed")
}

// TestExecuteFlatteningStep_NonBundleStreamRouting exercises the streaming path
// where clinical resources arrive in NDJSON as top-level (non-Bundle) objects
// and are routed via a provenance index sourced from a separate Bundle file.
// Covers the non-Bundle branch in streamAndFlattenResources.
func TestExecuteFlatteningStep_NonBundleStreamRouting(t *testing.T) {
	flushed := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/fhir/ViewDefinition/$run" {
			flushed = true
			w.Header().Set("Content-Type", "application/x-ndjson")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"1","name":"Alice"}` + "\n" + `{"id":"2","name":"Bob"}` + "\n"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	tempDir := t.TempDir()
	jobID := "test-non-bundle-routing"
	jobDir := filepath.Join(tempDir, "jobs", jobID)
	inputDir := filepath.Join(jobDir, "import")
	outputDir := filepath.Join(jobDir, "csv")
	require.NoError(t, os.MkdirAll(inputDir, 0755))

	groupID := "group-patient-nb"

	crtdlPath := filepath.Join(tempDir, "test.json")
	crtdlContent := `{
		"dataExtraction": {
			"attributeGroups": [{
				"id": "` + groupID + `",
				"name": "Patients",
				"groupReference": "https://example.com/Patient",
				"attributes": [
					{"attributeRef": "Patient.id", "mustHave": true},
					{"attributeRef": "Patient.name", "mustHave": false}
				]
			}]
		}
	}`
	require.NoError(t, os.WriteFile(crtdlPath, []byte(crtdlContent), 0644))

	lookupPath := filepath.Join(tempDir, "lookup.json")
	lookupContent := `[{
		"url": "https://example.com/Patient",
		"resourceType": "Patient",
		"elements": {
			"Patient.id": {"viewDefinition": {"select": [{"column": [{"name": "id", "path": "id"}]}]}},
			"Patient.name": {"viewDefinition": {"select": [{"column": [{"name": "name", "path": "name[0].text"}]}]}}
		}
	}]`
	require.NoError(t, os.WriteFile(lookupPath, []byte(lookupContent), 0644))

	// File A: Bundle containing only Provenance entries (builds provenance index in pass 1).
	provBundle := makeBundle("prov-bundle",
		makeProvenance("prov-a", "Patient/1", groupID),
		makeProvenance("prov-b", "Patient/2", groupID),
	)
	provData, err := json.Marshal(provBundle)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(inputDir, "01-prov.ndjson"), append(provData, '\n'), 0644))

	// File B: NDJSON of raw, top-level Patient resources (exercises non-Bundle pass 2 routing).
	patientFile, err := os.Create(filepath.Join(inputDir, "02-patients.ndjson"))
	require.NoError(t, err)
	for _, p := range []map[string]any{
		{
			"resourceType": "Patient", "id": "1",
			"meta": map[string]any{"profile": []any{"https://example.com/Patient"}},
			"name": []any{map[string]any{"text": "Alice"}},
		},
		{
			"resourceType": "Patient", "id": "2",
			"meta": map[string]any{"profile": []any{"https://example.com/Patient"}},
			"name": []any{map[string]any{"text": "Bob"}},
		},
	} {
		b, mErr := json.Marshal(p)
		require.NoError(t, mErr)
		_, wErr := patientFile.Write(append(b, '\n'))
		require.NoError(t, wErr)
	}
	require.NoError(t, patientFile.Close())

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
					Timeout:    30 * time.Second,
					// Tiny memory budget forces a flush inside the non-Bundle branch
					// after each resource is appended.
					BatchSizeMB: 1,
				},
			},
			Pipeline: models.PipelineConfig{
				EnabledSteps: []models.StepName{models.StepLocalImport, models.StepFlattening},
			},
			Retry: models.RetryConfig{MaxAttempts: 1},
		},
	}

	logger := lib.NewLogger(lib.LogLevelDebug)
	err = runPipelineStep(models.StepFlattening, job, jobDir, logger)
	require.NoError(t, err)
	assert.True(t, flushed, "flattener service should have been called")

	csvFiles, err := filepath.Glob(filepath.Join(outputDir, "*.csv"))
	require.NoError(t, err)
	require.Len(t, csvFiles, 1)
}
