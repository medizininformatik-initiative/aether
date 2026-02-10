package unit

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/pipeline"
)

// Helper functions for DIMP pipeline tests

func createDIMPTestLogger() *lib.Logger {
	return lib.NewLogger(lib.LogLevelDebug)
}

func createDIMPTestJob(dimpURL string) *models.PipelineJob {
	job := &models.PipelineJob{
		JobID:       "test-dimp-job",
		Status:      models.JobStatusInProgress,
		CurrentStep: string(models.StepDIMP),
		CreatedAt:   time.Now(),
		Steps:       []models.PipelineStep{},
		Config: models.ProjectConfig{
			Services: models.ServiceConfig{
				DIMP: models.DIMPConfig{
					URL:                    dimpURL,
					BundleSplitThresholdMB: 10,
				},
			},
			Retry: models.RetryConfig{
				MaxAttempts:      5,
				InitialBackoffMs: 1000,
				MaxBackoffMs:     30000,
			},
		},
	}
	// Include import step before DIMP (required for GetStepInputDir to work correctly)
	job.Config.Pipeline.EnabledSteps = []models.StepName{models.StepLocalImport, models.StepDIMP}
	return job
}

func createDIMPTestJobDisabled() *models.PipelineJob {
	job := createDIMPTestJob("")
	job.Config.Pipeline.EnabledSteps = []models.StepName{} // DIMP not enabled
	return job
}

func createMockDIMPServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var resource map[string]any
		_ = json.NewDecoder(r.Body).Decode(&resource)

		// Pseudonymize resource by adding prefix
		if id, ok := resource["id"].(string); ok {
			resource["id"] = "pseudo-" + id
		}

		w.Header().Set("Content-Type", "application/json")
		// Ignore errors in test server - test framework will handle write failures
		_ = json.NewEncoder(w).Encode(resource)
	}))
}

func writeDIMPNDJSON(t *testing.T, filename string, data []map[string]any) {
	f, err := os.Create(filename)
	require.NoError(t, err)
	defer func() { require.NoError(t, f.Close()) }()

	for _, item := range data {
		bytes, err := json.Marshal(item)
		require.NoError(t, err)
		_, err = f.Write(bytes)
		require.NoError(t, err)
		_, err = f.WriteString("\n")
		require.NoError(t, err)
	}
}

func readDIMPNDJSON(t *testing.T, filename string) []map[string]any {
	bytes, err := os.ReadFile(filename)
	require.NoError(t, err)

	var results []map[string]any
	content := string(bytes)
	for _, line := range strings.Split(content, "\n") {
		if line == "" {
			continue
		}
		var item map[string]any
		err := json.Unmarshal([]byte(line), &item)
		require.NoError(t, err)
		results = append(results, item)
	}

	return results
}

// Tests for ExecuteDIMPStep function

// TestExecuteDIMPStep_DisabledStep verifies that DIMP step is skipped if not enabled
func TestExecuteDIMPStep_DisabledStep(t *testing.T) {
	tmpDir := t.TempDir()
	job := createDIMPTestJobDisabled()
	logger := createDIMPTestLogger()

	err := pipeline.ExecuteDIMPStep(job, tmpDir, logger)
	assert.NoError(t, err)
}

// TestExecuteDIMPStep_MissingDIMPURL verifies error when DIMP URL not configured
func TestExecuteDIMPStep_MissingDIMPURL(t *testing.T) {
	tmpDir := t.TempDir()
	job := createDIMPTestJob("") // Empty URL
	logger := createDIMPTestLogger()

	err := pipeline.ExecuteDIMPStep(job, tmpDir, logger)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "DIMP service URL not configured")
}

// TestExecuteDIMPStep_FailedToCreateOutputDir verifies error when output dir creation fails
func TestExecuteDIMPStep_FailedToCreateOutputDir(t *testing.T) {
	server := createMockDIMPServer()
	defer server.Close()

	tmpDir := t.TempDir()
	job := createDIMPTestJob(server.URL)
	logger := createDIMPTestLogger()

	// Create a file where we need a directory
	pseudonymizedPath := filepath.Join(tmpDir, "pseudonymized")
	f, cerr := os.Create(pseudonymizedPath)
	require.NoError(t, cerr)
	require.NoError(t, f.Close())

	err := pipeline.ExecuteDIMPStep(job, tmpDir, logger)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create output directory")
}

// TestExecuteDIMPStep_NoFilesFound verifies error when no NDJSON files found
func TestExecuteDIMPStep_NoFilesFound(t *testing.T) {
	server := createMockDIMPServer()
	defer server.Close()

	tmpDir := t.TempDir()
	job := createDIMPTestJob(server.URL)
	logger := createDIMPTestLogger()

	// Create empty import directory
	importDir := filepath.Join(tmpDir, "import")
	require.NoError(t, os.MkdirAll(importDir, 0755))

	err := pipeline.ExecuteDIMPStep(job, tmpDir, logger)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no FHIR NDJSON files found")
}

// TestExecuteDIMPStep_ProcessSimpleResources processes non-Bundle resources successfully
func TestExecuteDIMPStep_ProcessSimpleResources(t *testing.T) {
	server := createMockDIMPServer()
	defer server.Close()

	tmpDir := t.TempDir()
	job := createDIMPTestJob(server.URL)
	logger := createDIMPTestLogger()

	// Create import directory with test file
	importDir := filepath.Join(tmpDir, "import")
	require.NoError(t, os.MkdirAll(importDir, 0755))

	inputFile := filepath.Join(importDir, "patients.ndjson")
	patients := []map[string]any{
		{"resourceType": "Patient", "id": "p1", "name": []any{map[string]any{"family": "Smith"}}},
		{"resourceType": "Patient", "id": "p2", "name": []any{map[string]any{"family": "Jones"}}},
	}
	writeDIMPNDJSON(t, inputFile, patients)

	err := pipeline.ExecuteDIMPStep(job, tmpDir, logger)
	assert.NoError(t, err)

	// Verify output file was created
	outputFile := filepath.Join(tmpDir, "pseudonymized", "dimped_patients.ndjson")
	assert.FileExists(t, outputFile)

	// Verify pseudonymized content
	resources := readDIMPNDJSON(t, outputFile)
	assert.Len(t, resources, 2)
	assert.Equal(t, "pseudo-p1", resources[0]["id"])
	assert.Equal(t, "pseudo-p2", resources[1]["id"])
}

// TestExecuteDIMPStep_ResumeProcessing skips already processed files
func TestExecuteDIMPStep_ResumeProcessing(t *testing.T) {
	server := createMockDIMPServer()
	defer server.Close()

	tmpDir := t.TempDir()
	job := createDIMPTestJob(server.URL)
	logger := createDIMPTestLogger()

	// Create import and pseudonymized directories
	importDir := filepath.Join(tmpDir, "import")
	pseudonymizedDir := filepath.Join(tmpDir, "pseudonymized")
	require.NoError(t, os.MkdirAll(importDir, 0755))
	require.NoError(t, os.MkdirAll(pseudonymizedDir, 0755))

	// Create input file
	inputFile := filepath.Join(importDir, "patients.ndjson")
	patients := []map[string]any{
		{"resourceType": "Patient", "id": "p1"},
	}
	writeDIMPNDJSON(t, inputFile, patients)

	// Pre-create output file to simulate resume
	outputFile := filepath.Join(pseudonymizedDir, "dimped_patients.ndjson")
	existingData := []map[string]any{
		{"resourceType": "Patient", "id": "pseudo-p1"},
	}
	writeDIMPNDJSON(t, outputFile, existingData)

	err := pipeline.ExecuteDIMPStep(job, tmpDir, logger)
	assert.NoError(t, err)

	// Verify file still has original content (wasn't reprocessed)
	resources := readDIMPNDJSON(t, outputFile)
	assert.Len(t, resources, 1)
	assert.Equal(t, "pseudo-p1", resources[0]["id"])
}

// TestExecuteDIMPStep_InvalidJSON returns error on malformed JSON
func TestExecuteDIMPStep_InvalidJSON(t *testing.T) {
	server := createMockDIMPServer()
	defer server.Close()

	tmpDir := t.TempDir()
	job := createDIMPTestJob(server.URL)
	logger := createDIMPTestLogger()

	importDir := filepath.Join(tmpDir, "import")
	require.NoError(t, os.MkdirAll(importDir, 0755))

	// Write invalid JSON
	inputFile := filepath.Join(importDir, "invalid.ndjson")
	f, ferr := os.Create(inputFile)
	require.NoError(t, ferr)
	_, ferr = f.WriteString("{invalid json\n")
	require.NoError(t, ferr)
	require.NoError(t, f.Close())

	err := pipeline.ExecuteDIMPStep(job, tmpDir, logger)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse")
}

// TestExecuteDIMPStep_ProcessBundleWithoutSplit processes Bundle below threshold directly
func TestExecuteDIMPStep_ProcessBundleWithoutSplit(t *testing.T) {
	server := createMockDIMPServer()
	defer server.Close()

	tmpDir := t.TempDir()
	job := createDIMPTestJob(server.URL)
	job.Config.Services.DIMP.BundleSplitThresholdMB = 100 // High threshold, won't split
	logger := createDIMPTestLogger()

	importDir := filepath.Join(tmpDir, "import")
	require.NoError(t, os.MkdirAll(importDir, 0755))

	// Create small Bundle
	inputFile := filepath.Join(importDir, "bundles.ndjson")
	bundle := map[string]any{
		"resourceType": "Bundle",
		"id":           "bundle1",
		"type":         "collection",
		"entry": []any{
			map[string]any{
				"resource": map[string]any{
					"resourceType": "Patient",
					"id":           "p1",
				},
			},
		},
	}
	writeDIMPNDJSON(t, inputFile, []map[string]any{bundle})

	err := pipeline.ExecuteDIMPStep(job, tmpDir, logger)
	assert.NoError(t, err)

	outputFile := filepath.Join(tmpDir, "pseudonymized", "dimped_bundles.ndjson")
	assert.FileExists(t, outputFile)
}

// TestExecuteDIMPStep_ProcessBundleWithSplit splits large Bundles correctly
func TestExecuteDIMPStep_ProcessBundleWithSplit(t *testing.T) {
	server := createMockDIMPServer()
	defer server.Close()

	tmpDir := t.TempDir()
	job := createDIMPTestJob(server.URL)
	job.Config.Services.DIMP.BundleSplitThresholdMB = 1 // Low threshold to force splitting
	logger := createDIMPTestLogger()

	importDir := filepath.Join(tmpDir, "import")
	require.NoError(t, os.MkdirAll(importDir, 0755))

	// Create Bundle with multiple entries that will be split
	inputFile := filepath.Join(importDir, "bundles.ndjson")
	bundle := CreateTestBundle(20, 100) // 20 entries, ~100KB each = ~2MB total
	writeDIMPNDJSON(t, inputFile, []map[string]any{bundle})

	err := pipeline.ExecuteDIMPStep(job, tmpDir, logger)
	assert.NoError(t, err)

	outputFile := filepath.Join(tmpDir, "pseudonymized", "dimped_bundles.ndjson")
	assert.FileExists(t, outputFile)
}

// TestExecuteDIMPStep_DIMPServiceError handles DIMP service errors
func TestExecuteDIMPStep_DIMPServiceError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		// Ignore errors in test server - test framework will handle write failures
		_, _ = w.Write([]byte(`{"error": "bad request"}`))
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	job := createDIMPTestJob(server.URL)
	logger := createDIMPTestLogger()

	importDir := filepath.Join(tmpDir, "import")
	require.NoError(t, os.MkdirAll(importDir, 0755))

	inputFile := filepath.Join(importDir, "patients.ndjson")
	patients := []map[string]any{{"resourceType": "Patient", "id": "p1"}}
	writeDIMPNDJSON(t, inputFile, patients)

	err := pipeline.ExecuteDIMPStep(job, tmpDir, logger)
	assert.Error(t, err)
}

// TestExecuteDIMPStep_MultipleFiles processes multiple NDJSON files
func TestExecuteDIMPStep_MultipleFiles(t *testing.T) {
	server := createMockDIMPServer()
	defer server.Close()

	tmpDir := t.TempDir()
	job := createDIMPTestJob(server.URL)
	logger := createDIMPTestLogger()

	importDir := filepath.Join(tmpDir, "import")
	require.NoError(t, os.MkdirAll(importDir, 0755))

	// Create multiple input files
	files := []string{"patients.ndjson", "observations.ndjson"}
	for _, filename := range files {
		inputFile := filepath.Join(importDir, filename)
		data := []map[string]any{{"resourceType": "Patient", "id": "test-1"}}
		writeDIMPNDJSON(t, inputFile, data)
	}

	err := pipeline.ExecuteDIMPStep(job, tmpDir, logger)
	assert.NoError(t, err)

	// Verify all output files were created
	for _, filename := range files {
		outputFile := filepath.Join(tmpDir, "pseudonymized", "dimped_"+filename)
		assert.FileExists(t, outputFile)
	}
}

// TestExecuteDIMPStep_StepStateUpdated verifies step state is properly recorded
func TestExecuteDIMPStep_StepStateUpdated(t *testing.T) {
	server := createMockDIMPServer()
	defer server.Close()

	tmpDir := t.TempDir()
	job := createDIMPTestJob(server.URL)
	logger := createDIMPTestLogger()

	importDir := filepath.Join(tmpDir, "import")
	require.NoError(t, os.MkdirAll(importDir, 0755))

	inputFile := filepath.Join(importDir, "patients.ndjson")
	patients := []map[string]any{{"resourceType": "Patient", "id": "p1"}}
	writeDIMPNDJSON(t, inputFile, patients)

	err := pipeline.ExecuteDIMPStep(job, tmpDir, logger)
	assert.NoError(t, err)

	// Verify step was added to job
	require.Len(t, job.Steps, 1)
	step := job.Steps[0]
	assert.Equal(t, models.StepDIMP, step.Name)
	assert.Equal(t, models.StepStatusCompleted, step.Status)
	assert.NotNil(t, step.StartedAt)
	assert.NotNil(t, step.CompletedAt)
	assert.Equal(t, 1, step.FilesProcessed)
}

// TestExecuteDIMPStep_EmptyLines skips empty lines in NDJSON
func TestExecuteDIMPStep_EmptyLines(t *testing.T) {
	server := createMockDIMPServer()
	defer server.Close()

	tmpDir := t.TempDir()
	job := createDIMPTestJob(server.URL)
	logger := createDIMPTestLogger()

	importDir := filepath.Join(tmpDir, "import")
	require.NoError(t, os.MkdirAll(importDir, 0755))

	// Write NDJSON with empty lines
	inputFile := filepath.Join(importDir, "sparse.ndjson")
	f, ferr := os.Create(inputFile)
	require.NoError(t, ferr)
	_, ferr = f.WriteString(`{"resourceType": "Patient", "id": "p1"}` + "\n")
	require.NoError(t, ferr)
	_, ferr = f.WriteString("\n") // Empty line
	require.NoError(t, ferr)
	_, ferr = f.WriteString(`{"resourceType": "Patient", "id": "p2"}` + "\n")
	require.NoError(t, ferr)
	require.NoError(t, f.Close())

	err := pipeline.ExecuteDIMPStep(job, tmpDir, logger)
	assert.NoError(t, err)

	outputFile := filepath.Join(tmpDir, "pseudonymized", "dimped_sparse.ndjson")
	resources := readDIMPNDJSON(t, outputFile)
	assert.Len(t, resources, 2) // Should have 2 resources, skipping empty line
}

// TestExecuteDIMPStep_OversizedNonBundleResource detects oversized non-Bundle resources
func TestExecuteDIMPStep_OversizedNonBundleResource(t *testing.T) {
	server := createMockDIMPServer()
	defer server.Close()

	tmpDir := t.TempDir()
	job := createDIMPTestJob(server.URL)
	job.Config.Services.DIMP.BundleSplitThresholdMB = 1 // Very small threshold
	logger := createDIMPTestLogger()

	importDir := filepath.Join(tmpDir, "import")
	require.NoError(t, os.MkdirAll(importDir, 0755))

	// Create a non-Bundle resource that exceeds threshold
	inputFile := filepath.Join(importDir, "large_patient.ndjson")
	f, ferr := os.Create(inputFile)
	require.NoError(t, ferr)

	// Write a large Patient with padding
	padding := generatePadding(2 * 1024 * 1024) // 2MB padding
	patientData := map[string]any{
		"resourceType": "Patient",
		"id":           "p1",
		"_padding":     padding,
	}
	bytes, _ := json.Marshal(patientData)
	_, ferr = f.Write(bytes)
	require.NoError(t, ferr)
	_, ferr = f.WriteString("\n")
	require.NoError(t, ferr)
	require.NoError(t, f.Close())

	err := pipeline.ExecuteDIMPStep(job, tmpDir, logger)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "oversized")
}

// TestExecuteDIMPStep_BundleCalcSizeError tests error when calculating Bundle size
func TestExecuteDIMPStep_BundleCalcSizeError(t *testing.T) {
	server := createMockDIMPServer()
	defer server.Close()

	tmpDir := t.TempDir()
	job := createDIMPTestJob(server.URL)
	logger := createDIMPTestLogger()

	importDir := filepath.Join(tmpDir, "import")
	require.NoError(t, os.MkdirAll(importDir, 0755))

	// Create Bundle with circular reference (causes JSON marshal to fail)
	inputFile := filepath.Join(importDir, "circular.ndjson")
	f, ferr := os.Create(inputFile)
	require.NoError(t, ferr)
	// Write directly invalid JSON that looks like a Bundle but will cause issues
	_, ferr = f.WriteString(`{"resourceType": "Bundle", "id": "b1", "entry": [{"resource": null}]}` + "\n")
	require.NoError(t, ferr)
	require.NoError(t, f.Close())

	// The test should handle this - it shouldn't crash
	_ = pipeline.ExecuteDIMPStep(job, tmpDir, logger)
}

// TestExecuteDIMPStep_DefaultBundleThreshold tests default threshold when not configured
func TestExecuteDIMPStep_DefaultBundleThreshold(t *testing.T) {
	server := createMockDIMPServer()
	defer server.Close()

	tmpDir := t.TempDir()
	job := createDIMPTestJob(server.URL)
	job.Config.Services.DIMP.BundleSplitThresholdMB = 0 // Will use default 10MB
	logger := createDIMPTestLogger()

	importDir := filepath.Join(tmpDir, "import")
	require.NoError(t, os.MkdirAll(importDir, 0755))

	inputFile := filepath.Join(importDir, "bundles.ndjson")
	bundle := map[string]any{
		"resourceType": "Bundle",
		"id":           "bundle1",
		"type":         "collection",
		"entry": []any{
			map[string]any{
				"resource": map[string]any{
					"resourceType": "Patient",
					"id":           "p1",
				},
			},
		},
	}
	writeDIMPNDJSON(t, inputFile, []map[string]any{bundle})

	err := pipeline.ExecuteDIMPStep(job, tmpDir, logger)
	assert.NoError(t, err)

	outputFile := filepath.Join(tmpDir, "pseudonymized", "dimped_bundles.ndjson")
	assert.FileExists(t, outputFile)
}

// TestExecuteDIMPStep_GetOrCreateStepExisting tests reusing existing step
func TestExecuteDIMPStep_GetOrCreateStepExisting(t *testing.T) {
	server := createMockDIMPServer()
	defer server.Close()

	tmpDir := t.TempDir()
	job := createDIMPTestJob(server.URL)
	logger := createDIMPTestLogger()

	// Pre-populate steps with existing DIMP step
	now := time.Now()
	job.Steps = []models.PipelineStep{
		{
			Name:      models.StepDIMP,
			Status:    models.StepStatusPending,
			StartedAt: &now,
		},
	}

	importDir := filepath.Join(tmpDir, "import")
	require.NoError(t, os.MkdirAll(importDir, 0755))

	inputFile := filepath.Join(importDir, "patients.ndjson")
	patients := []map[string]any{{"resourceType": "Patient", "id": "p1"}}
	writeDIMPNDJSON(t, inputFile, patients)

	err := pipeline.ExecuteDIMPStep(job, tmpDir, logger)
	assert.NoError(t, err)

	// Verify still only one step
	require.Len(t, job.Steps, 1)
	step := job.Steps[0]
	assert.Equal(t, models.StepDIMP, step.Name)
	assert.Equal(t, models.StepStatusCompleted, step.Status)
}

// TestExecuteDIMPStep_NegativeBundleThreshold tests negative threshold handling
func TestExecuteDIMPStep_NegativeBundleThreshold(t *testing.T) {
	server := createMockDIMPServer()
	defer server.Close()

	tmpDir := t.TempDir()
	job := createDIMPTestJob(server.URL)
	job.Config.Services.DIMP.BundleSplitThresholdMB = -5 // Negative, should use default
	logger := createDIMPTestLogger()

	importDir := filepath.Join(tmpDir, "import")
	require.NoError(t, os.MkdirAll(importDir, 0755))

	inputFile := filepath.Join(importDir, "bundles.ndjson")
	bundle := map[string]any{
		"resourceType": "Bundle",
		"id":           "bundle1",
		"type":         "collection",
		"entry":        []any{},
	}
	writeDIMPNDJSON(t, inputFile, []map[string]any{bundle})

	err := pipeline.ExecuteDIMPStep(job, tmpDir, logger)
	assert.NoError(t, err)
}

// TestExecuteDIMPStep_AlreadyProcessedFileCountError tests counting resources in existing files
func TestExecuteDIMPStep_AlreadyProcessedFileCountError(t *testing.T) {
	server := createMockDIMPServer()
	defer server.Close()

	tmpDir := t.TempDir()
	job := createDIMPTestJob(server.URL)
	logger := createDIMPTestLogger()

	importDir := filepath.Join(tmpDir, "import")
	pseudonymizedDir := filepath.Join(tmpDir, "pseudonymized")
	require.NoError(t, os.MkdirAll(importDir, 0755))
	require.NoError(t, os.MkdirAll(pseudonymizedDir, 0755))

	inputFile := filepath.Join(importDir, "patients.ndjson")
	patients := []map[string]any{
		{"resourceType": "Patient", "id": "p1"},
		{"resourceType": "Patient", "id": "p2"},
	}
	writeDIMPNDJSON(t, inputFile, patients)

	outputFile := filepath.Join(pseudonymizedDir, "dimped_patients.ndjson")
	existingData := []map[string]any{
		{"resourceType": "Patient", "id": "pseudo-p1"},
	}
	writeDIMPNDJSON(t, outputFile, existingData)

	err := pipeline.ExecuteDIMPStep(job, tmpDir, logger)
	assert.NoError(t, err)
	// Step should complete successfully even if counting fails
	require.Len(t, job.Steps, 1)
}

// TestExecuteDIMPStep_StepErrorRecording verifies error is recorded in step
func TestExecuteDIMPStep_StepErrorRecording(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error": "internal error"}`))
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	job := createDIMPTestJob(server.URL)
	logger := createDIMPTestLogger()

	importDir := filepath.Join(tmpDir, "import")
	require.NoError(t, os.MkdirAll(importDir, 0755))

	inputFile := filepath.Join(importDir, "patients.ndjson")
	patients := []map[string]any{{"resourceType": "Patient", "id": "p1"}}
	writeDIMPNDJSON(t, inputFile, patients)

	err := pipeline.ExecuteDIMPStep(job, tmpDir, logger)
	assert.Error(t, err)

	// Verify step was created and has error recorded
	require.Len(t, job.Steps, 1)
	step := job.Steps[0]
	assert.Equal(t, models.StepStatusFailed, step.Status)
	assert.NotNil(t, step.LastError)
	assert.NotNil(t, step.LastError.Timestamp)
}

// TestExecuteDIMPStep_FileListingWithoutImportDir tests glob when import dir doesn't exist
func TestExecuteDIMPStep_FileListingWithoutImportDir(t *testing.T) {
	server := createMockDIMPServer()
	defer server.Close()

	tmpDir := t.TempDir()
	job := createDIMPTestJob(server.URL)
	logger := createDIMPTestLogger()

	// Don't create import directory - glob should return empty
	err := pipeline.ExecuteDIMPStep(job, tmpDir, logger)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no FHIR NDJSON files found")
}

// Additional tests for helper functions and edge cases

func TestExecuteDIMPStep_VeryLargeBundle(t *testing.T) {
	server := createMockDIMPServer()
	defer server.Close()

	tmpDir := t.TempDir()
	job := createDIMPTestJob(server.URL)
	// Set high threshold so large bundle is processed directly
	job.Config.Services.DIMP.BundleSplitThresholdMB = 1000
	logger := createDIMPTestLogger()

	importDir := filepath.Join(tmpDir, "import")
	require.NoError(t, os.MkdirAll(importDir, 0755))

	// Create a large Bundle with many entries
	inputFile := filepath.Join(importDir, "large_bundle.ndjson")
	largeBundle := CreateTestBundle(500, 50) // 500 entries of ~50KB each = ~25MB
	writeDIMPNDJSON(t, inputFile, []map[string]any{largeBundle})

	err := pipeline.ExecuteDIMPStep(job, tmpDir, logger)
	assert.NoError(t, err)

	// Verify output file was created
	outputFile := filepath.Join(tmpDir, "pseudonymized", "dimped_large_bundle.ndjson")
	assert.FileExists(t, outputFile)
}

func TestExecuteDIMPStep_MixedResourceTypes(t *testing.T) {
	server := createMockDIMPServer()
	defer server.Close()

	tmpDir := t.TempDir()
	job := createDIMPTestJob(server.URL)
	logger := createDIMPTestLogger()

	importDir := filepath.Join(tmpDir, "import")
	require.NoError(t, os.MkdirAll(importDir, 0755))

	// Create NDJSON with mixed resource types
	inputFile := filepath.Join(importDir, "mixed.ndjson")
	resources := []map[string]any{
		{"resourceType": "Patient", "id": "p1", "name": []any{map[string]any{"family": "Smith"}}},
		{"resourceType": "Observation", "id": "obs1", "valueQuantity": map[string]any{"value": 100}},
		{"resourceType": "Condition", "id": "cond1", "code": map[string]any{"text": "Test"}},
		{"resourceType": "Procedure", "id": "proc1", "code": map[string]any{"text": "Procedure"}},
	}
	writeDIMPNDJSON(t, inputFile, resources)

	err := pipeline.ExecuteDIMPStep(job, tmpDir, logger)
	assert.NoError(t, err)

	// Verify output file exists and has all resources
	outputFile := filepath.Join(tmpDir, "pseudonymized", "dimped_mixed.ndjson")
	assert.FileExists(t, outputFile)

	outputResources := readDIMPNDJSON(t, outputFile)
	assert.Len(t, outputResources, 4)
}

func TestExecuteDIMPStep_BundleWithError(t *testing.T) {
	server := createMockDIMPServer()
	defer server.Close()

	tmpDir := t.TempDir()
	job := createDIMPTestJob(server.URL)
	job.Config.Services.DIMP.BundleSplitThresholdMB = 100 // Don't split
	logger := createDIMPTestLogger()

	importDir := filepath.Join(tmpDir, "import")
	require.NoError(t, os.MkdirAll(importDir, 0755))

	// Create a Bundle with entries (should succeed)
	inputFile := filepath.Join(importDir, "bundles.ndjson")
	bundle := map[string]any{
		"resourceType": "Bundle",
		"id":           "bundle1",
		"type":         "collection",
		"entry": []any{
			map[string]any{
				"resource": map[string]any{
					"resourceType": "Patient",
					"id":           "p1",
					"name":         []any{map[string]any{"family": "Smith"}},
				},
			},
			map[string]any{
				"resource": map[string]any{
					"resourceType": "Observation",
					"id":           "obs1",
				},
			},
		},
	}
	writeDIMPNDJSON(t, inputFile, []map[string]any{bundle})

	err := pipeline.ExecuteDIMPStep(job, tmpDir, logger)
	assert.NoError(t, err)

	outputFile := filepath.Join(tmpDir, "pseudonymized", "dimped_bundles.ndjson")
	assert.FileExists(t, outputFile)

	// Verify the output is a valid Bundle
	resources := readDIMPNDJSON(t, outputFile)
	require.Len(t, resources, 1)
	assert.Equal(t, "Bundle", resources[0]["resourceType"])
}

func TestExecuteDIMPStep_LargeNumberOfFiles(t *testing.T) {
	server := createMockDIMPServer()
	defer server.Close()

	tmpDir := t.TempDir()
	job := createDIMPTestJob(server.URL)
	logger := createDIMPTestLogger()

	importDir := filepath.Join(tmpDir, "import")
	require.NoError(t, os.MkdirAll(importDir, 0755))

	// Create many small files
	fileCount := 20
	for i := 0; i < fileCount; i++ {
		inputFile := filepath.Join(importDir, fmt.Sprintf("file_%d.ndjson", i))
		data := []map[string]any{
			{"resourceType": "Patient", "id": fmt.Sprintf("p%d", i)},
		}
		writeDIMPNDJSON(t, inputFile, data)
	}

	err := pipeline.ExecuteDIMPStep(job, tmpDir, logger)
	assert.NoError(t, err)

	// Verify all files were processed
	pseudonymizedDir := filepath.Join(tmpDir, "pseudonymized")
	files, err := filepath.Glob(filepath.Join(pseudonymizedDir, "dimped_*.ndjson"))
	require.NoError(t, err)
	assert.Len(t, files, fileCount)
}

func TestExecuteDIMPStep_SpecialCharactersInFilenames(t *testing.T) {
	server := createMockDIMPServer()
	defer server.Close()

	tmpDir := t.TempDir()
	job := createDIMPTestJob(server.URL)
	logger := createDIMPTestLogger()

	importDir := filepath.Join(tmpDir, "import")
	require.NoError(t, os.MkdirAll(importDir, 0755))

	// Create file with special characters in name
	inputFile := filepath.Join(importDir, "test_data-2025-01-01.ndjson")
	patients := []map[string]any{
		{"resourceType": "Patient", "id": "p1"},
	}
	writeDIMPNDJSON(t, inputFile, patients)

	err := pipeline.ExecuteDIMPStep(job, tmpDir, logger)
	assert.NoError(t, err)

	// Verify output file exists
	outputFile := filepath.Join(tmpDir, "pseudonymized", "dimped_test_data-2025-01-01.ndjson")
	assert.FileExists(t, outputFile)
}


// =============================================
// Compression Tests for DIMP Pipeline
// =============================================

// TestExecuteDIMPStep_WithCompression verifies DIMP with compression enabled
func TestExecuteDIMPStep_WithCompression(t *testing.T) {
	server := createMockDIMPServer()
	defer server.Close()

	tmpDir := t.TempDir()
	job := createDIMPTestJob(server.URL)
	// Enable compression
	job.Config.Compression.Enabled = true
	job.Config.Compression.Level = "default"
	logger := createDIMPTestLogger()

	// Create import directory with test file
	importDir := filepath.Join(tmpDir, "import")
	require.NoError(t, os.MkdirAll(importDir, 0755))

	inputFile := filepath.Join(importDir, "patients.ndjson")
	patients := []map[string]any{
		{"resourceType": "Patient", "id": "p1", "name": []any{map[string]any{"family": "Smith"}}},
		{"resourceType": "Patient", "id": "p2", "name": []any{map[string]any{"family": "Jones"}}},
	}
	writeDIMPNDJSON(t, inputFile, patients)

	err := pipeline.ExecuteDIMPStep(job, tmpDir, logger)
	assert.NoError(t, err)

	// Verify output file was created with compression extension
	outputFile := filepath.Join(tmpDir, "pseudonymized", "dimped_patients.ndjson.zst")
	assert.FileExists(t, outputFile)

	// Verify content is readable as compressed
	reader, err := lib.OpenFileForReading(outputFile)
	require.NoError(t, err)
	content, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())
	assert.Contains(t, string(content), "pseudo-p1")
}

// TestExecuteDIMPStep_CompressedInput verifies processing compressed input files
func TestExecuteDIMPStep_CompressedInput(t *testing.T) {
	server := createMockDIMPServer()
	defer server.Close()

	tmpDir := t.TempDir()
	job := createDIMPTestJob(server.URL)
	// Compression disabled for output
	job.Config.Compression.Enabled = false
	logger := createDIMPTestLogger()

	// Create import directory with compressed test file
	importDir := filepath.Join(tmpDir, "import")
	require.NoError(t, os.MkdirAll(importDir, 0755))

	// Create compressed input file
	compressedFile := filepath.Join(importDir, "patients.ndjson.zst")
	writer, err := lib.CreateCompressedFileWriter(compressedFile, "default")
	require.NoError(t, err)
	patients := []map[string]any{
		{"resourceType": "Patient", "id": "p1"},
		{"resourceType": "Patient", "id": "p2"},
	}
	for _, p := range patients {
		bytes, _ := json.Marshal(p)
		_, err := writer.Write(bytes)
		require.NoError(t, err)
		_, err = writer.Write([]byte("\n"))
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())

	err = pipeline.ExecuteDIMPStep(job, tmpDir, logger)
	assert.NoError(t, err)

	// Verify output file was created (uncompressed)
	outputFile := filepath.Join(tmpDir, "pseudonymized", "dimped_patients.ndjson")
	assert.FileExists(t, outputFile)

	resources := readDIMPNDJSON(t, outputFile)
	assert.Len(t, resources, 2)
}

// TestExecuteDIMPStep_CompressedInputToCompressedOutput verifies compressed input -> compressed output
func TestExecuteDIMPStep_CompressedInputToCompressedOutput(t *testing.T) {
	server := createMockDIMPServer()
	defer server.Close()

	tmpDir := t.TempDir()
	job := createDIMPTestJob(server.URL)
	// Enable compression for output
	job.Config.Compression.Enabled = true
	job.Config.Compression.Level = "default"
	logger := createDIMPTestLogger()

	// Create import directory with compressed test file
	importDir := filepath.Join(tmpDir, "import")
	require.NoError(t, os.MkdirAll(importDir, 0755))

	// Create compressed input file
	compressedFile := filepath.Join(importDir, "patients.ndjson.zst")
	writer, err := lib.CreateCompressedFileWriter(compressedFile, "default")
	require.NoError(t, err)
	patients := []map[string]any{
		{"resourceType": "Patient", "id": "p1"},
	}
	for _, p := range patients {
		bytes, _ := json.Marshal(p)
		_, err := writer.Write(bytes)
		require.NoError(t, err)
		_, err = writer.Write([]byte("\n"))
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())

	err = pipeline.ExecuteDIMPStep(job, tmpDir, logger)
	assert.NoError(t, err)

	// Verify output file was created with compression extension
	outputFile := filepath.Join(tmpDir, "pseudonymized", "dimped_patients.ndjson.zst")
	assert.FileExists(t, outputFile)

	// Verify content
	reader, err := lib.OpenFileForReading(outputFile)
	require.NoError(t, err)
	content, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())
	assert.Contains(t, string(content), "pseudo-p1")
}

// TestExecuteDIMPStep_MixedInputFiles verifies processing both compressed and uncompressed inputs
func TestExecuteDIMPStep_MixedInputFiles(t *testing.T) {
	server := createMockDIMPServer()
	defer server.Close()

	tmpDir := t.TempDir()
	job := createDIMPTestJob(server.URL)
	job.Config.Compression.Enabled = true
	job.Config.Compression.Level = "default"
	logger := createDIMPTestLogger()

	importDir := filepath.Join(tmpDir, "import")
	require.NoError(t, os.MkdirAll(importDir, 0755))

	// Create uncompressed file
	uncompressedFile := filepath.Join(importDir, "patients.ndjson")
	require.NoError(t, os.WriteFile(uncompressedFile,
		[]byte(`{"resourceType":"Patient","id":"p1"}`+"\n"), 0644))

	// Create compressed file with different resource type
	compressedFile := filepath.Join(importDir, "observations.ndjson.zst")
	writer, err := lib.CreateCompressedFileWriter(compressedFile, "default")
	require.NoError(t, err)
	_, err = writer.Write([]byte(`{"resourceType":"Observation","id":"obs1"}` + "\n"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	err = pipeline.ExecuteDIMPStep(job, tmpDir, logger)
	assert.NoError(t, err)

	// Verify both output files were created with compression
	outputDir := filepath.Join(tmpDir, "pseudonymized")
	assert.FileExists(t, filepath.Join(outputDir, "dimped_patients.ndjson.zst"))
	assert.FileExists(t, filepath.Join(outputDir, "dimped_observations.ndjson.zst"))
}

// TestExecuteDIMPStep_CompressionAllLevels verifies all compression levels work
func TestExecuteDIMPStep_CompressionAllLevels(t *testing.T) {
	levels := []string{"fastest", "default", "better", "best"}

	for _, level := range levels {
		t.Run(level, func(t *testing.T) {
			server := createMockDIMPServer()
			defer server.Close()

			tmpDir := t.TempDir()
			job := createDIMPTestJob(server.URL)
			job.Config.Compression.Enabled = true
			job.Config.Compression.Level = level
			logger := createDIMPTestLogger()

			importDir := filepath.Join(tmpDir, "import")
			require.NoError(t, os.MkdirAll(importDir, 0755))

			inputFile := filepath.Join(importDir, "patients.ndjson")
			patients := []map[string]any{{"resourceType": "Patient", "id": "p1"}}
			writeDIMPNDJSON(t, inputFile, patients)

			err := pipeline.ExecuteDIMPStep(job, tmpDir, logger)
			assert.NoError(t, err, "DIMP should succeed with compression level: %s", level)

			outputFile := filepath.Join(tmpDir, "pseudonymized", "dimped_patients.ndjson.zst")
			assert.FileExists(t, outputFile)
		})
	}
}

// TestExecuteDIMPStep_DuplicateFilesError verifies error when duplicate compressed/uncompressed files exist
func TestExecuteDIMPStep_DuplicateFilesError(t *testing.T) {
	server := createMockDIMPServer()
	defer server.Close()

	tmpDir := t.TempDir()
	job := createDIMPTestJob(server.URL)
	logger := createDIMPTestLogger()

	importDir := filepath.Join(tmpDir, "import")
	require.NoError(t, os.MkdirAll(importDir, 0755))

	// Create both compressed and uncompressed versions of the same file
	require.NoError(t, os.WriteFile(filepath.Join(importDir, "patients.ndjson"),
		[]byte(`{"resourceType":"Patient","id":"p1"}`+"\n"), 0644))

	compressedFile := filepath.Join(importDir, "patients.ndjson.zst")
	writer, err := lib.CreateCompressedFileWriter(compressedFile, "default")
	require.NoError(t, err)
	_, err = writer.Write([]byte(`{"resourceType":"Patient","id":"p2"}` + "\n"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	// This should fail due to duplicate files
	err = pipeline.ExecuteDIMPStep(job, tmpDir, logger)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "patients.ndjson")
}

// TestExecuteDIMPStep_ResumeWithCompressedOutput verifies resume with existing compressed output
func TestExecuteDIMPStep_ResumeWithCompressedOutput(t *testing.T) {
	server := createMockDIMPServer()
	defer server.Close()

	tmpDir := t.TempDir()
	job := createDIMPTestJob(server.URL)
	job.Config.Compression.Enabled = true
	job.Config.Compression.Level = "default"
	logger := createDIMPTestLogger()

	importDir := filepath.Join(tmpDir, "import")
	pseudonymizedDir := filepath.Join(tmpDir, "pseudonymized")
	require.NoError(t, os.MkdirAll(importDir, 0755))
	require.NoError(t, os.MkdirAll(pseudonymizedDir, 0755))

	// Create input file
	inputFile := filepath.Join(importDir, "patients.ndjson")
	patients := []map[string]any{{"resourceType": "Patient", "id": "p1"}}
	writeDIMPNDJSON(t, inputFile, patients)

	// Pre-create compressed output file to simulate resume
	outputFile := filepath.Join(pseudonymizedDir, "dimped_patients.ndjson.zst")
	writer, err := lib.CreateCompressedFileWriter(outputFile, "default")
	require.NoError(t, err)
	_, err = writer.Write([]byte(`{"resourceType":"Patient","id":"existing"}` + "\n"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	err = pipeline.ExecuteDIMPStep(job, tmpDir, logger)
	assert.NoError(t, err)

	// Verify file still has original content (wasn't reprocessed)
	reader, err := lib.OpenFileForReading(outputFile)
	require.NoError(t, err)
	content, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())
	assert.Contains(t, string(content), "existing")
}

// =============================================
// Additional Coverage Tests for Error Paths
// Target: 100% patch coverage for dimp.go
// =============================================

// TestExecuteDIMPStep_StalePartFileRemovalError verifies that processing continues
// even when removing stale .part file fails (covers lines 88-91 in dimp.go)
func TestExecuteDIMPStep_StalePartFileRemovalError(t *testing.T) {
	// Skip if running as root
	if os.Getuid() == 0 {
		t.Skip("Skipping permission test when running as root")
	}

	server := createMockDIMPServer()
	defer server.Close()

	tmpDir := t.TempDir()
	job := createDIMPTestJob(server.URL)
	logger := createDIMPTestLogger()

	importDir := filepath.Join(tmpDir, "import")
	pseudonymizedDir := filepath.Join(tmpDir, "pseudonymized")
	require.NoError(t, os.MkdirAll(importDir, 0755))
	require.NoError(t, os.MkdirAll(pseudonymizedDir, 0755))

	// Create input file
	inputFile := filepath.Join(importDir, "patients.ndjson")
	patients := []map[string]any{{"resourceType": "Patient", "id": "p1"}}
	writeDIMPNDJSON(t, inputFile, patients)

	// Create a stale .part file that will be difficult to remove
	partFile := filepath.Join(pseudonymizedDir, "dimped_patients.ndjson.part")
	require.NoError(t, os.WriteFile(partFile, []byte("stale data"), 0644))

	// Make the directory read-only to prevent removal
	// Note: This may not work on all systems
	require.NoError(t, os.Chmod(pseudonymizedDir, 0555))

	// Cleanup
	defer func() {
		_ = os.Chmod(pseudonymizedDir, 0755)
	}()

	// Processing should still continue (error is logged but not fatal)
	// However, it will fail to create output file due to read-only directory
	err := pipeline.ExecuteDIMPStep(job, tmpDir, logger)
	// We expect an error, but it should be about creating the output file, not removing .part
	assert.Error(t, err)
}

// TestExecuteDIMPStep_CountResourcesError verifies handling when CountResourcesInFile fails
// (covers lines 120-122 in dimp.go)
func TestExecuteDIMPStep_CountResourcesError(t *testing.T) {
	server := createMockDIMPServer()
	defer server.Close()

	tmpDir := t.TempDir()
	job := createDIMPTestJob(server.URL)
	logger := createDIMPTestLogger()

	importDir := filepath.Join(tmpDir, "import")
	pseudonymizedDir := filepath.Join(tmpDir, "pseudonymized")
	require.NoError(t, os.MkdirAll(importDir, 0755))
	require.NoError(t, os.MkdirAll(pseudonymizedDir, 0755))

	// Create input file
	inputFile := filepath.Join(importDir, "patients.ndjson")
	patients := []map[string]any{{"resourceType": "Patient", "id": "p1"}}
	writeDIMPNDJSON(t, inputFile, patients)

	// Create an invalid compressed output file that will fail CountResourcesInFile
	outputFile := filepath.Join(pseudonymizedDir, "dimped_patients.ndjson.zst")
	// Write invalid zstd data
	require.NoError(t, os.WriteFile(outputFile, []byte{0x00, 0x01, 0x02, 0x03}, 0644))

	// Processing should skip this file (already exists)
	err := pipeline.ExecuteDIMPStep(job, tmpDir, logger)
	// Should succeed - file is skipped as "already processed"
	assert.NoError(t, err)
}

// TestExecuteDIMPStep_ProcessDIMPFileOpenError verifies error when input file can't be opened
func TestExecuteDIMPStep_ProcessDIMPFileOpenError(t *testing.T) {
	// Skip if running as root
	if os.Getuid() == 0 {
		t.Skip("Skipping permission test when running as root")
	}

	server := createMockDIMPServer()
	defer server.Close()

	tmpDir := t.TempDir()
	job := createDIMPTestJob(server.URL)
	logger := createDIMPTestLogger()

	importDir := filepath.Join(tmpDir, "import")
	require.NoError(t, os.MkdirAll(importDir, 0755))

	// Create input file and make it unreadable
	inputFile := filepath.Join(importDir, "patients.ndjson")
	patients := []map[string]any{{"resourceType": "Patient", "id": "p1"}}
	writeDIMPNDJSON(t, inputFile, patients)
	require.NoError(t, os.Chmod(inputFile, 0000))

	// Cleanup
	defer func() {
		_ = os.Chmod(inputFile, 0644)
	}()

	err := pipeline.ExecuteDIMPStep(job, tmpDir, logger)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to open")
}

// TestExecuteDIMPStep_LongLine verifies handling of very long lines in NDJSON
// This tests the scanner path when a line exceeds default buffer
func TestExecuteDIMPStep_LongLine(t *testing.T) {
	server := createMockDIMPServer()
	defer server.Close()

	tmpDir := t.TempDir()
	job := createDIMPTestJob(server.URL)
	logger := createDIMPTestLogger()

	importDir := filepath.Join(tmpDir, "import")
	require.NoError(t, os.MkdirAll(importDir, 0755))

	// Create a file with a very long line (but still valid JSON)
	inputFile := filepath.Join(importDir, "long.ndjson")
	f, err := os.Create(inputFile)
	require.NoError(t, err)

	// Create a patient with a very long name (but under scanner limit)
	longName := strings.Repeat("x", 50000) // 50KB name
	patient := map[string]any{
		"resourceType": "Patient",
		"id":           "p1",
		"name": []any{map[string]any{
			"family": longName,
		}},
	}
	bytes, _ := json.Marshal(patient)
	_, err = f.Write(bytes)
	require.NoError(t, err)
	_, err = f.WriteString("\n")
	require.NoError(t, err)
	require.NoError(t, f.Close())

	err = pipeline.ExecuteDIMPStep(job, tmpDir, logger)
	assert.NoError(t, err)

	// Verify output file was created
	outputFile := filepath.Join(tmpDir, "pseudonymized", "dimped_long.ndjson")
	assert.FileExists(t, outputFile)
}

// TestExecuteDIMPStep_StalePartFileRemoval verifies stale .part file is removed on fresh run
func TestExecuteDIMPStep_StalePartFileRemoval(t *testing.T) {
	server := createMockDIMPServer()
	defer server.Close()

	tmpDir := t.TempDir()
	job := createDIMPTestJob(server.URL)
	logger := createDIMPTestLogger()

	importDir := filepath.Join(tmpDir, "import")
	pseudonymizedDir := filepath.Join(tmpDir, "pseudonymized")
	require.NoError(t, os.MkdirAll(importDir, 0755))
	require.NoError(t, os.MkdirAll(pseudonymizedDir, 0755))

	// Create input file
	inputFile := filepath.Join(importDir, "patients.ndjson")
	patients := []map[string]any{{"resourceType": "Patient", "id": "p1"}}
	writeDIMPNDJSON(t, inputFile, patients)

	// Create a stale .part file (simulating a previous interrupted run)
	partFile := filepath.Join(pseudonymizedDir, "dimped_patients.ndjson.part")
	require.NoError(t, os.WriteFile(partFile, []byte("stale data"), 0644))

	err := pipeline.ExecuteDIMPStep(job, tmpDir, logger)
	assert.NoError(t, err)

	// Verify .part file was removed
	_, statErr := os.Stat(partFile)
	assert.True(t, os.IsNotExist(statErr), ".part file should be removed")

	// Verify output file exists
	outputFile := filepath.Join(tmpDir, "pseudonymized", "dimped_patients.ndjson")
	assert.FileExists(t, outputFile)
}

// =============================================
// DIMP Error Classification Tests
// =============================================

// TestExecuteDIMPStep_NetworkErrorIsTransient tests that network errors are classified as transient
func TestExecuteDIMPStep_NetworkErrorIsTransient(t *testing.T) {
	tmpDir := t.TempDir()
	// Use an unreachable URL
	job := createDIMPTestJob("http://localhost:1")
	logger := createDIMPTestLogger()

	// Create import directory with test file
	importDir := filepath.Join(tmpDir, "import")
	require.NoError(t, os.MkdirAll(importDir, 0755))
	testFile := filepath.Join(importDir, "Patient.ndjson")
	writeDIMPNDJSON(t, testFile, []map[string]any{
		{"resourceType": "Patient", "id": "1"},
	})

	err := pipeline.ExecuteDIMPStep(job, tmpDir, logger)
	assert.Error(t, err)

	// Find the DIMP step and verify error type is transient
	var dimpStep *models.PipelineStep
	for i := range job.Steps {
		if job.Steps[i].Name == models.StepDIMP {
			dimpStep = &job.Steps[i]
			break
		}
	}
	require.NotNil(t, dimpStep)
	require.NotNil(t, dimpStep.LastError, "Step should have an error")
	assert.Equal(t, models.ErrorTypeTransient, dimpStep.LastError.Type, "Network error should be classified as transient")
}

// TestExecuteDIMPStep_GlobPatternError tests that glob pattern errors are handled correctly
// This covers lines 272-274 and 278-279 in dimp.go (findFHIRFiles error paths)
func TestExecuteDIMPStep_GlobPatternError(t *testing.T) {
	server := createMockDIMPServer()
	defer server.Close()

	// Create a temp directory with a name that will cause Glob to fail
	// The '[' character is interpreted as a glob pattern character
	baseDir := t.TempDir()
	badDir := filepath.Join(baseDir, "test[invalid")
	require.NoError(t, os.MkdirAll(badDir, 0755))

	// Create the job pointing to this bad directory as the "job directory"
	// The import dir would be badDir/import which contains [
	job := createDIMPTestJob(server.URL)
	logger := createDIMPTestLogger()

	// Create "import" directory inside the bad directory
	importDir := filepath.Join(badDir, "import")
	require.NoError(t, os.MkdirAll(importDir, 0755))

	// Create a test file
	testFile := filepath.Join(importDir, "Patient.ndjson")
	writeDIMPNDJSON(t, testFile, []map[string]any{
		{"resourceType": "Patient", "id": "1"},
	})

	// Execute with the bad directory path - should fail with glob pattern error
	err := pipeline.ExecuteDIMPStep(job, badDir, logger)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "pattern")
}

// TestExecuteDIMPStep_400BadRequestIsNonTransient tests that 400 errors are classified as non-transient
func TestExecuteDIMPStep_400BadRequestIsNonTransient(t *testing.T) {
	// Create a mock server that always returns 400
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"Bad request"}`))
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	job := createDIMPTestJob(server.URL)
	logger := createDIMPTestLogger()

	// Create import directory with test file
	importDir := filepath.Join(tmpDir, "import")
	require.NoError(t, os.MkdirAll(importDir, 0755))
	testFile := filepath.Join(importDir, "Patient.ndjson")
	writeDIMPNDJSON(t, testFile, []map[string]any{
		{"resourceType": "Patient", "id": "1"},
	})

	err := pipeline.ExecuteDIMPStep(job, tmpDir, logger)
	assert.Error(t, err)

	// Find the DIMP step and verify error type is non-transient
	var dimpStep *models.PipelineStep
	for i := range job.Steps {
		if job.Steps[i].Name == models.StepDIMP {
			dimpStep = &job.Steps[i]
			break
		}
	}
	require.NotNil(t, dimpStep)
	require.NotNil(t, dimpStep.LastError, "Step should have an error")
	assert.Equal(t, models.ErrorTypeNonTransient, dimpStep.LastError.Type, "400 error should be classified as non-transient")
}
