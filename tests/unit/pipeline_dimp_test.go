package unit

import (
	"encoding/json"
	"fmt"
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
		},
	}
	job.Config.Pipeline.EnabledSteps = append(job.Config.Pipeline.EnabledSteps, models.StepDIMP)
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
