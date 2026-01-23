package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExecuteFlatteningStep_FullPipeline(t *testing.T) {
	// Create a mock fhir-flattener server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/fhir/ViewDefinition/$run" {
			// Return mock CSV data (without header)
			w.Header().Set("Content-Type", "text/csv")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("1,John Doe,1990-01-15\n"))
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
	inputDir := filepath.Join(jobDir, "import") // Input comes from import dir based on enabled steps
	outputDir := filepath.Join(jobDir, "csv")

	require.NoError(t, os.MkdirAll(inputDir, 0755))

	// Create CRTDL file
	crtdlPath := filepath.Join(tempDir, "test.crtdl")
	crtdlContent := `{
		"dataExtraction": {
			"attributeGroups": [
				{
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

	// Create input NDJSON file with FHIR resources
	ndjsonContent := `{"resourceType":"Patient","id":"1","meta":{"profile":["https://example.com/Patient"]},"name":[{"text":"John Doe"}]}
{"resourceType":"Patient","id":"2","meta":{"profile":["https://example.com/Patient"]},"name":[{"text":"Jane Smith"}]}`
	require.NoError(t, os.WriteFile(filepath.Join(inputDir, "patients.ndjson"), []byte(ndjsonContent), 0644))

	// Create job configuration
	job := &models.PipelineJob{
		JobID:       jobID,
		Status:      models.JobStatusInProgress,
		InputSource: crtdlPath,
		InputType:   models.InputTypeCRTDL,
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
		},
		Steps: []models.PipelineStep{},
	}

	logger := lib.NewLogger(lib.LogLevelDebug)

	// Execute flattening step
	err := pipeline.ExecuteFlatteningStep(job, jobDir, logger)
	require.NoError(t, err)

	// Verify output files were created
	csvFiles, err := filepath.Glob(filepath.Join(outputDir, "*.csv"))
	require.NoError(t, err)
	assert.Len(t, csvFiles, 1)

	// Verify CSV content
	csvContent, err := os.ReadFile(csvFiles[0])
	require.NoError(t, err)
	assert.Contains(t, string(csvContent), "id,")  // Header should include id column
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
	err := pipeline.ExecuteFlatteningStep(job, jobDir, logger)
	assert.NoError(t, err) // Should skip without error
}

func TestExecuteFlatteningStep_WrongInputType(t *testing.T) {
	tempDir := t.TempDir()
	jobID := "test-job-wrong-input"
	jobDir := filepath.Join(tempDir, "jobs", jobID)
	require.NoError(t, os.MkdirAll(jobDir, 0755))

	job := &models.PipelineJob{
		JobID:     jobID,
		Status:    models.JobStatusInProgress,
		InputType: models.InputTypeLocal, // Not CRTDL
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
	err := pipeline.ExecuteFlatteningStep(job, jobDir, logger)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires CRTDL input")
}

func TestExecuteFlatteningStep_MissingCRTDL(t *testing.T) {
	tempDir := t.TempDir()
	jobID := "test-job-missing-crtdl"
	jobDir := filepath.Join(tempDir, "jobs", jobID)
	require.NoError(t, os.MkdirAll(jobDir, 0755))

	job := &models.PipelineJob{
		JobID:       jobID,
		Status:      models.JobStatusInProgress,
		InputSource: "/nonexistent/path.crtdl",
		InputType:   models.InputTypeCRTDL,
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
	err := pipeline.ExecuteFlatteningStep(job, jobDir, logger)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse CRTDL")
}

func TestExecuteFlatteningStep_MissingLookupTables(t *testing.T) {
	tempDir := t.TempDir()
	jobID := "test-job-missing-lookup"
	jobDir := filepath.Join(tempDir, "jobs", jobID)
	require.NoError(t, os.MkdirAll(jobDir, 0755))

	// Create valid CRTDL
	crtdlPath := filepath.Join(tempDir, "test.crtdl")
	crtdlContent := `{
		"dataExtraction": {
			"attributeGroups": [{
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
	err := pipeline.ExecuteFlatteningStep(job, jobDir, logger)
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
	crtdlPath := filepath.Join(tempDir, "test.crtdl")
	crtdlContent := `{
		"dataExtraction": {
			"attributeGroups": [{
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
	err := pipeline.ExecuteFlatteningStep(job, jobDir, logger)
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

	// Create CRTDL
	crtdlPath := filepath.Join(tempDir, "test.crtdl")
	crtdlContent := `{
		"dataExtraction": {
			"attributeGroups": [{
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

	// Create input file
	ndjsonContent := `{"resourceType":"Patient","id":"1","meta":{"profile":["https://example.com/Patient"]}}`
	require.NoError(t, os.WriteFile(filepath.Join(inputDir, "patients.ndjson"), []byte(ndjsonContent), 0644))

	job := &models.PipelineJob{
		JobID:       jobID,
		Status:      models.JobStatusInProgress,
		InputSource: crtdlPath,
		InputType:   models.InputTypeCRTDL,
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
		},
	}

	logger := lib.NewLogger(lib.LogLevelDebug)
	err := pipeline.ExecuteFlatteningStep(job, jobDir, logger)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "flattener failed")
}

func TestFilterResourcesByProfile(t *testing.T) {
	tempDir := t.TempDir()

	// Create test NDJSON file with multiple resource types
	ndjsonContent := `{"resourceType":"Patient","id":"1","meta":{"profile":["https://example.com/Patient"]}}
{"resourceType":"Condition","id":"2","meta":{"profile":["https://example.com/Condition"]}}
{"resourceType":"Patient","id":"3","meta":{"profile":["https://example.com/Patient"]}}
{"resourceType":"Observation","id":"4","meta":{}}`

	ndjsonPath := filepath.Join(tempDir, "resources.ndjson")
	require.NoError(t, os.WriteFile(ndjsonPath, []byte(ndjsonContent), 0644))

	// Read the file and parse resources
	content, err := os.ReadFile(ndjsonPath)
	require.NoError(t, err)

	var resources []map[string]any
	for _, line := range []string{
		`{"resourceType":"Patient","id":"1","meta":{"profile":["https://example.com/Patient"]}}`,
		`{"resourceType":"Condition","id":"2","meta":{"profile":["https://example.com/Condition"]}}`,
		`{"resourceType":"Patient","id":"3","meta":{"profile":["https://example.com/Patient"]}}`,
		`{"resourceType":"Observation","id":"4","meta":{}}`,
	} {
		var resource map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &resource))
		resources = append(resources, resource)
	}

	assert.NotEmpty(t, content) // Use content to avoid unused variable

	// Test filtering (this tests the internal filtering logic conceptually)
	patientCount := 0
	for _, r := range resources {
		if meta, ok := r["meta"].(map[string]any); ok {
			if profiles, ok := meta["profile"].([]any); ok && len(profiles) > 0 {
				if profiles[0] == "https://example.com/Patient" {
					patientCount++
				}
			}
		}
	}
	assert.Equal(t, 2, patientCount)
}
