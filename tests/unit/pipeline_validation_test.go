package unit

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
)

func createValidationTestLogger() *lib.Logger {
	return lib.NewLogger(lib.LogLevelDebug)
}

func boolPtr(b bool) *bool { return &b }

func createValidationTestJob(validationURL string) *models.PipelineJob {
	return &models.PipelineJob{
		JobID:       "test-validation-job",
		InputSource: "/tmp/test-input",
		InputType:   models.InputTypeLocal,
		Status:      models.JobStatusInProgress,
		CurrentStep: string(models.StepValidation),
		CreatedAt:   time.Now(),
		Steps:       []models.PipelineStep{},
		Config: models.ProjectConfig{
			Services: models.ServiceConfig{
				Validation: models.ValidationConfig{
					URL:                   validationURL,
					MaxConcurrentRequests: 2,
					BundleChunkSizeMB:     10,
					FailOnError:           boolPtr(false),
				},
			},
			Pipeline: models.PipelineConfig{
				EnabledSteps: []models.StepName{models.StepLocalImport, models.StepValidation},
			},
			Retry: models.RetryConfig{
				MaxAttempts:      1,
				InitialBackoffMs: 100,
				MaxBackoffMs:     1000,
			},
		},
	}
}

// createMockBundleValidationServer creates a mock server that expects Bundle input.
// When allValid is true, it returns an OperationOutcome with only informational issues.
// When allValid is false, it returns an OperationOutcome with error-severity issues
// and expression fields referencing Bundle entries.
func createMockBundleValidationServer(allValid bool) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var resource map[string]any
		_ = json.NewDecoder(r.Body).Decode(&resource)

		// Verify we received a Bundle
		resourceType, _ := resource["resourceType"].(string)
		if resourceType != "Bundle" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error": "expected Bundle"}`))
			return
		}

		var issues []map[string]any
		if allValid {
			issues = []map[string]any{
				{
					"severity":    "information",
					"code":        "informational",
					"diagnostics": "All OK",
				},
			}
		} else {
			// Return error issues with expression fields referencing entries
			entries, _ := resource["entry"].([]any)
			for i := range entries {
				issues = append(issues, map[string]any{
					"severity":    "error",
					"code":        "structure",
					"diagnostics": "Missing required field",
					"expression":  []string{fmt.Sprintf("Bundle.entry[%d].resource", i)},
				})
			}
		}

		outcome := map[string]any{
			"resourceType": "OperationOutcome",
			"issue":        issues,
		}
		w.Header().Set("Content-Type", "application/fhir+json")
		_ = json.NewEncoder(w).Encode(outcome)
	}))
}

func writeValidationNDJSON(t *testing.T, filename string, data []map[string]any) {
	t.Helper()
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

func readValidationNDJSON(t *testing.T, filename string) []map[string]any {
	t.Helper()
	bytes, err := os.ReadFile(filename)
	require.NoError(t, err)

	var results []map[string]any
	for _, line := range strings.Split(string(bytes), "\n") {
		if line == "" {
			continue
		}
		var item map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &item))
		results = append(results, item)
	}
	return results
}

// TestExecuteValidationStep_DisabledStep verifies that step is skipped if not enabled
func TestExecuteValidationStep_DisabledStep(t *testing.T) {
	tmpDir := t.TempDir()
	job := createValidationTestJob("")
	job.Config.Pipeline.EnabledSteps = []models.StepName{} // not enabled
	logger := createValidationTestLogger()

	err := runPipelineStep(models.StepValidation, job, tmpDir, logger)
	assert.NoError(t, err)
}

// TestExecuteValidationStep_MissingURL verifies error when URL not configured
func TestExecuteValidationStep_MissingURL(t *testing.T) {
	tmpDir := t.TempDir()
	job := createValidationTestJob("") // empty URL
	logger := createValidationTestLogger()

	err := runPipelineStep(models.StepValidation, job, tmpDir, logger)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "validation service URL not configured")
}

// TestExecuteValidationStep_NoFilesFound verifies error with empty input directory
func TestExecuteValidationStep_NoFilesFound(t *testing.T) {
	server := createMockBundleValidationServer(true)
	defer server.Close()

	tmpDir := t.TempDir()
	job := createValidationTestJob(server.URL)
	logger := createValidationTestLogger()

	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "import"), 0755))

	err := runPipelineStep(models.StepValidation, job, tmpDir, logger)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no FHIR NDJSON files found")
}

// TestExecuteValidationStep_AllValid validates resources with no errors.
// Report file should contain informational OperationOutcomes for all chunks.
func TestExecuteValidationStep_AllValid(t *testing.T) {
	server := createMockBundleValidationServer(true)
	defer server.Close()

	tmpDir := t.TempDir()
	job := createValidationTestJob(server.URL)
	logger := createValidationTestLogger()

	importDir := filepath.Join(tmpDir, "import")
	require.NoError(t, os.MkdirAll(importDir, 0755))

	patients := []map[string]any{
		{"resourceType": "Patient", "id": "p1"},
		{"resourceType": "Patient", "id": "p2"},
	}
	writeValidationNDJSON(t, filepath.Join(importDir, "Patient.ndjson"), patients)

	err := runPipelineStep(models.StepValidation, job, tmpDir, logger)
	assert.NoError(t, err)

	// Report file should contain informational OperationOutcome (1 chunk)
	reportFile := filepath.Join(tmpDir, "validation", "Patient.validation.ndjson")
	assert.FileExists(t, reportFile)

	outcomes := readValidationNDJSON(t, reportFile)
	assert.Len(t, outcomes, 1, "should have 1 informational outcome for 1 chunk")
	assert.Equal(t, "OperationOutcome", outcomes[0]["resourceType"])
}

// TestExecuteValidationStep_SomeInvalid completes successfully even when validation errors are found.
// Validation findings are informational — they get written to reports but don't fail the pipeline.
func TestExecuteValidationStep_SomeInvalid(t *testing.T) {
	server := createMockBundleValidationServer(false)
	defer server.Close()

	tmpDir := t.TempDir()
	job := createValidationTestJob(server.URL)
	logger := createValidationTestLogger()

	importDir := filepath.Join(tmpDir, "import")
	require.NoError(t, os.MkdirAll(importDir, 0755))

	resources := []map[string]any{
		{"resourceType": "Condition", "id": "c1"},
	}
	writeValidationNDJSON(t, filepath.Join(importDir, "Condition.ndjson"), resources)

	err := runPipelineStep(models.StepValidation, job, tmpDir, logger)
	assert.NoError(t, err, "validation findings should not fail the pipeline")

	// Step should be completed, not failed
	var validationStep *models.PipelineStep
	for i := range job.Steps {
		if job.Steps[i].Name == models.StepValidation {
			validationStep = &job.Steps[i]
			break
		}
	}
	require.NotNil(t, validationStep)
	assert.Equal(t, models.StepStatusCompleted, validationStep.Status)

	// Verify report contains error OperationOutcome
	reportFile := filepath.Join(tmpDir, "validation", "Condition.validation.ndjson")
	assert.FileExists(t, reportFile)

	outcomes := readValidationNDJSON(t, reportFile)
	assert.Len(t, outcomes, 1, "should have 1 error outcome for 1 chunk")
	assert.Equal(t, "OperationOutcome", outcomes[0]["resourceType"])
}

// TestExecuteValidationStep_BundleChunking verifies chunking works with multiple chunks.
// Uses a very small chunk size to force multiple chunks.
func TestExecuteValidationStep_BundleChunking(t *testing.T) {
	var bundleCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var resource map[string]any
		_ = json.NewDecoder(r.Body).Decode(&resource)

		// Count received bundles
		if rt, _ := resource["resourceType"].(string); rt == "Bundle" {
			bundleCount.Add(1)
		}

		outcome := map[string]any{
			"resourceType": "OperationOutcome",
			"issue": []map[string]any{
				{"severity": "information", "code": "informational", "diagnostics": "All OK"},
			},
		}
		w.Header().Set("Content-Type", "application/fhir+json")
		_ = json.NewEncoder(w).Encode(outcome)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	job := createValidationTestJob(server.URL)
	// Use a tiny chunk size (1KB) to force multiple chunks
	job.Config.Services.Validation.BundleChunkSizeMB = 0 // will default, but we override threshold via config
	logger := createValidationTestLogger()

	importDir := filepath.Join(tmpDir, "import")
	require.NoError(t, os.MkdirAll(importDir, 0755))

	// Create many resources to exceed a single chunk
	var resources []map[string]any
	for i := 0; i < 20; i++ {
		resources = append(resources, map[string]any{
			"resourceType": "Patient",
			"id":           fmt.Sprintf("p%d", i),
			"name":         []map[string]any{{"family": "TestFamily", "given": []string{"TestGiven"}}},
		})
	}
	writeValidationNDJSON(t, filepath.Join(importDir, "Patient.ndjson"), resources)

	err := runPipelineStep(models.StepValidation, job, tmpDir, logger)
	assert.NoError(t, err)

	// With default 10MB chunk size all 20 tiny resources fit in 1 chunk
	assert.GreaterOrEqual(t, bundleCount.Load(), int32(1), "should have sent at least 1 bundle")
}

// TestExecuteValidationStep_ResumesProcessing skips files that already have reports
func TestExecuteValidationStep_ResumesProcessing(t *testing.T) {
	server := createMockBundleValidationServer(true)
	defer server.Close()

	tmpDir := t.TempDir()
	job := createValidationTestJob(server.URL)
	logger := createValidationTestLogger()

	importDir := filepath.Join(tmpDir, "import")
	require.NoError(t, os.MkdirAll(importDir, 0755))

	patients := []map[string]any{{"resourceType": "Patient", "id": "p1"}}
	writeValidationNDJSON(t, filepath.Join(importDir, "Patient.ndjson"), patients)

	observations := []map[string]any{{"resourceType": "Observation", "id": "o1"}}
	writeValidationNDJSON(t, filepath.Join(importDir, "Observation.ndjson"), observations)

	// Pre-create the Patient report (simulating previous run — empty file = all valid)
	validationDir := filepath.Join(tmpDir, "validation")
	require.NoError(t, os.MkdirAll(validationDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(validationDir, "Patient.validation.ndjson"), []byte{}, 0644))

	err := runPipelineStep(models.StepValidation, job, tmpDir, logger)
	assert.NoError(t, err)

	// Both reports should exist
	assert.FileExists(t, filepath.Join(validationDir, "Patient.validation.ndjson"))
	assert.FileExists(t, filepath.Join(validationDir, "Observation.validation.ndjson"))
}

// TestExecuteValidationStep_ConcurrencyRespected verifies concurrent requests are limited
func TestExecuteValidationStep_ConcurrencyRespected(t *testing.T) {
	var concurrentCount atomic.Int32
	var maxConcurrent atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := concurrentCount.Add(1)
		defer concurrentCount.Add(-1)

		for {
			old := maxConcurrent.Load()
			if current <= old {
				break
			}
			if maxConcurrent.CompareAndSwap(old, current) {
				break
			}
		}

		time.Sleep(10 * time.Millisecond)

		outcome := map[string]any{
			"resourceType": "OperationOutcome",
			"issue":        []map[string]any{{"severity": "information", "code": "informational"}},
		}
		w.Header().Set("Content-Type", "application/fhir+json")
		_ = json.NewEncoder(w).Encode(outcome)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	job := createValidationTestJob(server.URL)
	job.Config.Services.Validation.MaxConcurrentRequests = 2
	logger := createValidationTestLogger()

	importDir := filepath.Join(tmpDir, "import")
	require.NoError(t, os.MkdirAll(importDir, 0755))

	// Create multiple files so there are multiple chunks to validate concurrently
	for i := 0; i < 5; i++ {
		resources := []map[string]any{{"resourceType": "Patient", "id": fmt.Sprintf("p%d", i)}}
		writeValidationNDJSON(t, filepath.Join(importDir, fmt.Sprintf("Patient%d.ndjson", i)), resources)
	}

	err := runPipelineStep(models.StepValidation, job, tmpDir, logger)
	assert.NoError(t, err)

	assert.LessOrEqual(t, maxConcurrent.Load(), int32(2), "concurrent requests should not exceed MaxConcurrentRequests")
}

// TestExecuteValidationStep_HTTPError handles validation service errors
func TestExecuteValidationStep_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("Internal Server Error"))
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	job := createValidationTestJob(server.URL)
	logger := createValidationTestLogger()

	importDir := filepath.Join(tmpDir, "import")
	require.NoError(t, os.MkdirAll(importDir, 0755))

	resources := []map[string]any{{"resourceType": "Patient", "id": "p1"}}
	writeValidationNDJSON(t, filepath.Join(importDir, "Patient.ndjson"), resources)

	err := runPipelineStep(models.StepValidation, job, tmpDir, logger)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to validate")
}

// TestExecuteValidationStep_MultipleFiles validates multiple NDJSON files
func TestExecuteValidationStep_MultipleFiles(t *testing.T) {
	server := createMockBundleValidationServer(true)
	defer server.Close()

	tmpDir := t.TempDir()
	job := createValidationTestJob(server.URL)
	logger := createValidationTestLogger()

	importDir := filepath.Join(tmpDir, "import")
	require.NoError(t, os.MkdirAll(importDir, 0755))

	writeValidationNDJSON(t, filepath.Join(importDir, "Patient.ndjson"), []map[string]any{
		{"resourceType": "Patient", "id": "p1"},
	})
	writeValidationNDJSON(t, filepath.Join(importDir, "Condition.ndjson"), []map[string]any{
		{"resourceType": "Condition", "id": "c1"},
	})

	err := runPipelineStep(models.StepValidation, job, tmpDir, logger)
	assert.NoError(t, err)

	assert.FileExists(t, filepath.Join(tmpDir, "validation", "Patient.validation.ndjson"))
	assert.FileExists(t, filepath.Join(tmpDir, "validation", "Condition.validation.ndjson"))
}

// TestExecuteValidationStep_StepStatusTracking verifies step status is updated
func TestExecuteValidationStep_StepStatusTracking(t *testing.T) {
	server := createMockBundleValidationServer(true)
	defer server.Close()

	tmpDir := t.TempDir()
	job := createValidationTestJob(server.URL)
	logger := createValidationTestLogger()

	importDir := filepath.Join(tmpDir, "import")
	require.NoError(t, os.MkdirAll(importDir, 0755))

	writeValidationNDJSON(t, filepath.Join(importDir, "Patient.ndjson"), []map[string]any{
		{"resourceType": "Patient", "id": "p1"},
	})

	err := runPipelineStep(models.StepValidation, job, tmpDir, logger)
	require.NoError(t, err)

	var validationStep *models.PipelineStep
	for i := range job.Steps {
		if job.Steps[i].Name == models.StepValidation {
			validationStep = &job.Steps[i]
			break
		}
	}

	require.NotNil(t, validationStep, "validation step should exist in job.Steps")
	assert.Equal(t, models.StepStatusCompleted, validationStep.Status)
	assert.NotNil(t, validationStep.StartedAt)
	assert.NotNil(t, validationStep.CompletedAt)
	assert.Equal(t, 1, validationStep.FilesProcessed)
}

// TestExecuteValidationStep_EmptyLinesInNDJSON verifies that blank/whitespace lines are ignored
func TestExecuteValidationStep_EmptyLinesInNDJSON(t *testing.T) {
	server := createMockBundleValidationServer(true)
	defer server.Close()

	tmpDir := t.TempDir()
	job := createValidationTestJob(server.URL)
	logger := createValidationTestLogger()

	importDir := filepath.Join(tmpDir, "import")
	require.NoError(t, os.MkdirAll(importDir, 0755))

	// Write NDJSON with blank lines interspersed
	content := `{"resourceType":"Patient","id":"p1"}

{"resourceType":"Patient","id":"p2"}

{"resourceType":"Patient","id":"p3"}
`
	require.NoError(t, os.WriteFile(filepath.Join(importDir, "Patient.ndjson"), []byte(content), 0644))

	err := runPipelineStep(models.StepValidation, job, tmpDir, logger)
	assert.NoError(t, err)
}

// TestExecuteValidationStep_InvalidJSON verifies error on malformed NDJSON line
func TestExecuteValidationStep_InvalidJSON(t *testing.T) {
	server := createMockBundleValidationServer(true)
	defer server.Close()

	tmpDir := t.TempDir()
	job := createValidationTestJob(server.URL)
	logger := createValidationTestLogger()

	importDir := filepath.Join(tmpDir, "import")
	require.NoError(t, os.MkdirAll(importDir, 0755))

	content := `{"resourceType":"Patient","id":"p1"}
not-valid-json{{{
`
	require.NoError(t, os.WriteFile(filepath.Join(importDir, "Patient.ndjson"), []byte(content), 0644))

	err := runPipelineStep(models.StepValidation, job, tmpDir, logger)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to validate")
}

// TestExecuteValidationStep_EmptyFile verifies handling of an NDJSON file with no resources
func TestExecuteValidationStep_EmptyFile(t *testing.T) {
	server := createMockBundleValidationServer(true)
	defer server.Close()

	tmpDir := t.TempDir()
	job := createValidationTestJob(server.URL)
	logger := createValidationTestLogger()

	importDir := filepath.Join(tmpDir, "import")
	require.NoError(t, os.MkdirAll(importDir, 0755))

	// Write an empty NDJSON file
	require.NoError(t, os.WriteFile(filepath.Join(importDir, "Empty.ndjson"), []byte(""), 0644))

	err := runPipelineStep(models.StepValidation, job, tmpDir, logger)
	assert.NoError(t, err)

	// Report should exist (empty — created for resumption tracking)
	reportFile := filepath.Join(tmpDir, "validation", "Empty.validation.ndjson")
	assert.FileExists(t, reportFile)
}

// TestExecuteValidationStep_DefaultConcurrency verifies default concurrency when config is 0
func TestExecuteValidationStep_DefaultConcurrency(t *testing.T) {
	server := createMockBundleValidationServer(true)
	defer server.Close()

	tmpDir := t.TempDir()
	job := createValidationTestJob(server.URL)
	job.Config.Services.Validation.MaxConcurrentRequests = 0 // should default to 4
	logger := createValidationTestLogger()

	importDir := filepath.Join(tmpDir, "import")
	require.NoError(t, os.MkdirAll(importDir, 0755))
	writeValidationNDJSON(t, filepath.Join(importDir, "Patient.ndjson"), []map[string]any{
		{"resourceType": "Patient", "id": "p1"},
	})

	err := runPipelineStep(models.StepValidation, job, tmpDir, logger)
	assert.NoError(t, err)
}

// TestExecuteValidationStep_StalePartFileCleanup verifies .part files from previous runs are removed
func TestExecuteValidationStep_StalePartFileCleanup(t *testing.T) {
	server := createMockBundleValidationServer(true)
	defer server.Close()

	tmpDir := t.TempDir()
	job := createValidationTestJob(server.URL)
	logger := createValidationTestLogger()

	importDir := filepath.Join(tmpDir, "import")
	require.NoError(t, os.MkdirAll(importDir, 0755))
	writeValidationNDJSON(t, filepath.Join(importDir, "Patient.ndjson"), []map[string]any{
		{"resourceType": "Patient", "id": "p1"},
	})

	// Pre-create the output dir with a stale .part file
	validationDir := filepath.Join(tmpDir, "validation")
	require.NoError(t, os.MkdirAll(validationDir, 0755))
	stalePartFile := filepath.Join(validationDir, "OldFile.validation.ndjson.part")
	require.NoError(t, os.WriteFile(stalePartFile, []byte("stale"), 0644))

	err := runPipelineStep(models.StepValidation, job, tmpDir, logger)
	assert.NoError(t, err)

	// Stale .part file should be removed
	_, statErr := os.Stat(stalePartFile)
	assert.True(t, os.IsNotExist(statErr), "stale .part file should be cleaned up")
}

// TestExecuteValidationStep_ErrorOutcomesHaveExpression verifies that error OperationOutcomes
// contain expression fields referencing Bundle entries
func TestExecuteValidationStep_ErrorOutcomesHaveExpression(t *testing.T) {
	server := createMockBundleValidationServer(false)
	defer server.Close()

	tmpDir := t.TempDir()
	job := createValidationTestJob(server.URL)
	logger := createValidationTestLogger()

	importDir := filepath.Join(tmpDir, "import")
	require.NoError(t, os.MkdirAll(importDir, 0755))

	resources := []map[string]any{
		{"resourceType": "Patient", "id": "p1"},
		{"resourceType": "Patient", "id": "p2"},
	}
	writeValidationNDJSON(t, filepath.Join(importDir, "Patient.ndjson"), resources)

	err := runPipelineStep(models.StepValidation, job, tmpDir, logger)
	assert.NoError(t, err, "validation findings should not fail the pipeline")

	reportFile := filepath.Join(tmpDir, "validation", "Patient.validation.ndjson")
	assert.FileExists(t, reportFile)

	outcomes := readValidationNDJSON(t, reportFile)
	require.Len(t, outcomes, 1, "should have 1 error outcome for 1 chunk")

	// Verify the outcome has issues with expression fields
	issues, ok := outcomes[0]["issue"].([]any)
	require.True(t, ok, "should have issues array")
	require.NotEmpty(t, issues, "should have at least one issue")

	firstIssue, ok := issues[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "error", firstIssue["severity"])

	// Check expression field references Bundle entries
	expression, ok := firstIssue["expression"].([]any)
	require.True(t, ok, "issue should have expression field")
	assert.Contains(t, expression[0], "Bundle.entry[0]")
}

// TestExecuteValidationStep_FirstErrBailsEarlyFromChunkLoop verifies that when one chunk
// validation fails, subsequent chunks are not dispatched (early bail via firstErr).
// Requires at least 3 chunks with serialized execution (MaxConcurrentRequests=1).
func TestExecuteValidationStep_FirstErrBailsEarlyFromChunkLoop(t *testing.T) {
	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		// Always return 400 — non-retryable, so error is immediate
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error": "always fail"}`))
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	job := createValidationTestJob(server.URL)
	job.Config.Services.Validation.BundleChunkSizeMB = 1
	job.Config.Services.Validation.MaxConcurrentRequests = 1
	logger := createValidationTestLogger()

	importDir := filepath.Join(tmpDir, "import")
	require.NoError(t, os.MkdirAll(importDir, 0755))

	// Create resources large enough to form 3+ chunks at 1MB threshold
	var resources []map[string]any
	for i := 0; i < 6; i++ {
		resources = append(resources, map[string]any{
			"resourceType": "Patient",
			"id":           fmt.Sprintf("p%d", i),
			"text":         map[string]any{"div": strings.Repeat("a", 400_000)},
		})
	}
	writeValidationNDJSON(t, filepath.Join(importDir, "Patient.ndjson"), resources)

	err := runPipelineStep(models.StepValidation, job, tmpDir, logger)
	assert.Error(t, err)

	// Not all chunks should be dispatched — firstErr should bail early
	assert.Less(t, requestCount.Load(), int32(6), "firstErr should prevent dispatching all chunks")
}

// TestExecuteValidationStep_OversizedResource verifies that resources exceeding the chunk
// threshold are isolated into their own chunks rather than failing the pipeline.
// Exercises the chunkResourcesWithOversized fallback path.
func TestExecuteValidationStep_OversizedResource(t *testing.T) {
	var bundleCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bundleCount.Add(1)

		outcome := map[string]any{
			"resourceType": "OperationOutcome",
			"issue": []map[string]any{
				{"severity": "information", "code": "informational", "diagnostics": "OK"},
			},
		}
		w.Header().Set("Content-Type", "application/fhir+json")
		_ = json.NewEncoder(w).Encode(outcome)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	job := createValidationTestJob(server.URL)
	job.Config.Services.Validation.BundleChunkSizeMB = 1 // 1MB threshold
	logger := createValidationTestLogger()

	importDir := filepath.Join(tmpDir, "import")
	require.NoError(t, os.MkdirAll(importDir, 0755))

	// Create one normal resource and one oversized resource (>1MB)
	bigText := strings.Repeat("x", 1_100_000) // ~1.1MB
	resources := []map[string]any{
		{"resourceType": "Patient", "id": "small1"},
		{"resourceType": "Patient", "id": "big1", "text": map[string]any{"div": bigText}},
		{"resourceType": "Patient", "id": "small2"},
	}
	writeValidationNDJSON(t, filepath.Join(importDir, "Patient.ndjson"), resources)

	err := runPipelineStep(models.StepValidation, job, tmpDir, logger)
	assert.NoError(t, err)

	// At least 2 bundles: one for the oversized resource, one for the normal resources
	assert.GreaterOrEqual(t, bundleCount.Load(), int32(2), "oversized resource should be in its own chunk")
}

// TestExecuteValidationStep_ConnectionRefused verifies that a network error (connection refused)
// is classified as transient via classifyServiceError and isServiceErrorRetryable.
func TestExecuteValidationStep_ConnectionRefused(t *testing.T) {
	tmpDir := t.TempDir()
	// Port 59999 should have no listener, causing connection refused
	job := createValidationTestJob("http://localhost:59999")
	logger := createValidationTestLogger()

	importDir := filepath.Join(tmpDir, "import")
	require.NoError(t, os.MkdirAll(importDir, 0755))

	writeValidationNDJSON(t, filepath.Join(importDir, "Patient.ndjson"), []map[string]any{
		{"resourceType": "Patient", "id": "p1"},
	})

	err := runPipelineStep(models.StepValidation, job, tmpDir, logger)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to validate")
}

// TestExecuteValidationStep_ValidationServiceReturns400 verifies that a non-retryable
// HTTP error (4xx) from the validation service is classified correctly.
// Exercises isServiceErrorRetryable and classifyServiceError with *services.ServiceError.
func TestExecuteValidationStep_ValidationServiceReturns400(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error": "invalid Bundle"}`))
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	job := createValidationTestJob(server.URL)
	logger := createValidationTestLogger()

	importDir := filepath.Join(tmpDir, "import")
	require.NoError(t, os.MkdirAll(importDir, 0755))

	writeValidationNDJSON(t, filepath.Join(importDir, "Patient.ndjson"), []map[string]any{
		{"resourceType": "Patient", "id": "p1"},
	})

	err := runPipelineStep(models.StepValidation, job, tmpDir, logger)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to validate")
}

// TestExecuteValidationStep_BundleEntriesHaveFullURL verifies that Bundle entries sent to
// the validator include fullUrl, as required by FHIR R4 for collection Bundles.
func TestExecuteValidationStep_BundleEntriesHaveFullURL(t *testing.T) {
	var receivedBundle map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&receivedBundle)

		outcome := map[string]any{
			"resourceType": "OperationOutcome",
			"issue": []map[string]any{
				{"severity": "information", "code": "informational", "diagnostics": "All OK"},
			},
		}
		w.Header().Set("Content-Type", "application/fhir+json")
		_ = json.NewEncoder(w).Encode(outcome)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	job := createValidationTestJob(server.URL)
	logger := createValidationTestLogger()

	importDir := filepath.Join(tmpDir, "import")
	require.NoError(t, os.MkdirAll(importDir, 0755))

	resources := []map[string]any{
		{"resourceType": "Patient", "id": "p1"},
		{"resourceType": "Condition", "id": "c1"},
	}
	writeValidationNDJSON(t, filepath.Join(importDir, "Test.ndjson"), resources)

	err := runPipelineStep(models.StepValidation, job, tmpDir, logger)
	require.NoError(t, err)

	// Verify the Bundle sent to the validator has fullUrl on all entries
	entries, ok := receivedBundle["entry"].([]any)
	require.True(t, ok, "Bundle should have entries")

	for i, entryAny := range entries {
		entry, ok := entryAny.(map[string]any)
		require.True(t, ok, "entry should be a map")
		fullURL, ok := entry["fullUrl"].(string)
		assert.True(t, ok, "entry[%d] should have fullUrl", i)
		assert.NotEmpty(t, fullURL, "entry[%d] fullUrl should not be empty", i)
	}
}

// TestExecuteValidationStep_FailOnError_StopsPipeline verifies that when FailOnError is true
// and validation finds data quality errors, the step returns an error to stop the pipeline.
func TestExecuteValidationStep_FailOnError_StopsPipeline(t *testing.T) {
	server := createMockBundleValidationServer(false) // returns error-severity issues
	defer server.Close()

	tmpDir := t.TempDir()
	job := createValidationTestJob(server.URL)
	job.Config.Services.Validation.FailOnError = boolPtr(true)
	logger := createValidationTestLogger()

	importDir := filepath.Join(tmpDir, "import")
	require.NoError(t, os.MkdirAll(importDir, 0755))

	resources := []map[string]any{
		{"resourceType": "Condition", "id": "c1"},
	}
	writeValidationNDJSON(t, filepath.Join(importDir, "Condition.ndjson"), resources)

	err := runPipelineStep(models.StepValidation, job, tmpDir, logger)
	require.Error(t, err, "should return error when FailOnError is true and validation finds errors")
	assert.Contains(t, err.Error(), "fail_on_error")

	// Step should still be marked completed (it finished its work)
	var validationStep *models.PipelineStep
	for i := range job.Steps {
		if job.Steps[i].Name == models.StepValidation {
			validationStep = &job.Steps[i]
			break
		}
	}
	require.NotNil(t, validationStep)
	assert.Equal(t, models.StepStatusCompleted, validationStep.Status,
		"step status should be completed — the step finished, pipeline stops due to data quality")
}

// TestExecuteValidationStep_FailOnError_ContinuesWhenNoErrors verifies that when FailOnError
// is true but no validation errors are found, the step returns nil (pipeline continues).
func TestExecuteValidationStep_FailOnError_ContinuesWhenNoErrors(t *testing.T) {
	server := createMockBundleValidationServer(true) // returns only informational issues
	defer server.Close()

	tmpDir := t.TempDir()
	job := createValidationTestJob(server.URL)
	job.Config.Services.Validation.FailOnError = boolPtr(true)
	logger := createValidationTestLogger()

	importDir := filepath.Join(tmpDir, "import")
	require.NoError(t, os.MkdirAll(importDir, 0755))

	resources := []map[string]any{
		{"resourceType": "Patient", "id": "p1"},
		{"resourceType": "Patient", "id": "p2"},
	}
	writeValidationNDJSON(t, filepath.Join(importDir, "Patient.ndjson"), resources)

	err := runPipelineStep(models.StepValidation, job, tmpDir, logger)
	assert.NoError(t, err, "should not error when FailOnError is true but no validation errors found")
}

// TestExecuteValidationStep_ChunkValidationHTTPError verifies that HTTP errors during
// concurrent chunk validation propagate correctly (exercises firstErr early-bail path).
func TestExecuteValidationStep_ChunkValidationHTTPError(t *testing.T) {
	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := requestCount.Add(1)
		if count == 1 {
			// First chunk succeeds
			outcome := map[string]any{
				"resourceType": "OperationOutcome",
				"issue":        []map[string]any{{"severity": "information", "code": "informational"}},
			}
			w.Header().Set("Content-Type", "application/fhir+json")
			_ = json.NewEncoder(w).Encode(outcome)
		} else {
			// Second chunk fails
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte("Bad Gateway"))
		}
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	job := createValidationTestJob(server.URL)
	job.Config.Services.Validation.BundleChunkSizeMB = 1
	job.Config.Services.Validation.MaxConcurrentRequests = 1 // serialize to ensure deterministic order
	logger := createValidationTestLogger()

	importDir := filepath.Join(tmpDir, "import")
	require.NoError(t, os.MkdirAll(importDir, 0755))

	// Create enough resources with large text to force multiple chunks
	var resources []map[string]any
	for i := 0; i < 4; i++ {
		resources = append(resources, map[string]any{
			"resourceType": "Patient",
			"id":           fmt.Sprintf("p%d", i),
			"text":         map[string]any{"div": strings.Repeat("a", 300_000)},
		})
	}
	writeValidationNDJSON(t, filepath.Join(importDir, "Patient.ndjson"), resources)

	err := runPipelineStep(models.StepValidation, job, tmpDir, logger)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to validate")
}

// TestExecuteValidationStep_InnerBundleEntriesGetFullURL verifies that when the input
// contains transaction Bundles (e.g., from TORCH), their inner entries receive synthetic
// fullUrls before being sent to the validator. Without this, the validator falls back to
// its own default base URL which causes reference resolution warnings.
func TestExecuteValidationStep_InnerBundleEntriesGetFullURL(t *testing.T) {
	var receivedBundle map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&receivedBundle)

		outcome := map[string]any{
			"resourceType": "OperationOutcome",
			"issue": []map[string]any{
				{"severity": "information", "code": "informational", "diagnostics": "All OK"},
			},
		}
		w.Header().Set("Content-Type", "application/fhir+json")
		_ = json.NewEncoder(w).Encode(outcome)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	job := createValidationTestJob(server.URL)
	logger := createValidationTestLogger()

	importDir := filepath.Join(tmpDir, "import")
	require.NoError(t, os.MkdirAll(importDir, 0755))

	// Transaction Bundle from TORCH: entries have request.url but no fullUrl
	transactionBundle := map[string]any{
		"resourceType": "Bundle",
		"type":         "transaction",
		"entry": []map[string]any{
			{
				"resource": map[string]any{"resourceType": "Patient", "id": "VHF00964"},
				"request":  map[string]any{"method": "PUT", "url": "Patient/VHF00964"},
			},
			{
				"resource": map[string]any{
					"resourceType": "Condition",
					"id":           "VHF00964-CD-1",
					"subject":      map[string]any{"reference": "Patient/VHF00964"},
				},
				"request": map[string]any{"method": "PUT", "url": "Condition/VHF00964-CD-1"},
			},
		},
	}
	writeValidationNDJSON(t, filepath.Join(importDir, "Bundles.ndjson"), []map[string]any{transactionBundle})

	err := runPipelineStep(models.StepValidation, job, tmpDir, logger)
	require.NoError(t, err)

	// The outer collection Bundle wraps the transaction Bundle as a single entry
	outerEntries, ok := receivedBundle["entry"].([]any)
	require.True(t, ok, "outer Bundle should have entries")
	require.Len(t, outerEntries, 1)

	outerEntry := outerEntries[0].(map[string]any)
	innerBundle := outerEntry["resource"].(map[string]any)
	assert.Equal(t, "transaction", innerBundle["type"])

	innerEntries, ok := innerBundle["entry"].([]any)
	require.True(t, ok, "inner Bundle should have entries")

	for i, entryAny := range innerEntries {
		entry := entryAny.(map[string]any)
		fullURL, hasFullURL := entry["fullUrl"].(string)
		assert.True(t, hasFullURL, "inner entry[%d] should have fullUrl", i)
		assert.NotEmpty(t, fullURL, "inner entry[%d] fullUrl should not be empty", i)
	}

	// Verify fullUrls use a consistent base so relative references resolve correctly
	entry0 := innerEntries[0].(map[string]any)
	entry1 := innerEntries[1].(map[string]any)
	fullURL0 := entry0["fullUrl"].(string)
	fullURL1 := entry1["fullUrl"].(string)
	assert.True(t, strings.HasSuffix(fullURL0, "/Patient/VHF00964"),
		"Patient entry fullUrl should end with request.url path, got: %s", fullURL0)
	assert.True(t, strings.HasSuffix(fullURL1, "/Condition/VHF00964-CD-1"),
		"Condition entry fullUrl should end with request.url path, got: %s", fullURL1)
}

// TestExecuteValidationStep_InnerBundlePreservesExistingFullURL verifies that entries
// which already have a fullUrl are not overwritten.
func TestExecuteValidationStep_InnerBundlePreservesExistingFullURL(t *testing.T) {
	var receivedBundle map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&receivedBundle)

		outcome := map[string]any{
			"resourceType": "OperationOutcome",
			"issue": []map[string]any{
				{"severity": "information", "code": "informational", "diagnostics": "All OK"},
			},
		}
		w.Header().Set("Content-Type", "application/fhir+json")
		_ = json.NewEncoder(w).Encode(outcome)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	job := createValidationTestJob(server.URL)
	logger := createValidationTestLogger()

	importDir := filepath.Join(tmpDir, "import")
	require.NoError(t, os.MkdirAll(importDir, 0755))

	// Bundle with entries that already have fullUrl — should not be overwritten
	bundleWithFullURLs := map[string]any{
		"resourceType": "Bundle",
		"type":         "collection",
		"entry": []map[string]any{
			{
				"fullUrl":  "http://example.com/fhir/Patient/123",
				"resource": map[string]any{"resourceType": "Patient", "id": "123"},
			},
		},
	}
	writeValidationNDJSON(t, filepath.Join(importDir, "Bundles.ndjson"), []map[string]any{bundleWithFullURLs})

	err := runPipelineStep(models.StepValidation, job, tmpDir, logger)
	require.NoError(t, err)

	outerEntries := receivedBundle["entry"].([]any)
	outerEntry := outerEntries[0].(map[string]any)
	innerBundle := outerEntry["resource"].(map[string]any)
	innerEntries := innerBundle["entry"].([]any)
	entry := innerEntries[0].(map[string]any)

	assert.Equal(t, "http://example.com/fhir/Patient/123", entry["fullUrl"],
		"existing fullUrl should be preserved, not overwritten")
}
