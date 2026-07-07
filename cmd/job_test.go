package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/mattn/go-runewidth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
)

func TestFormatStatusField_DisplayWidthMatchesHeader(t *testing.T) {
	statuses := []string{"completed", "in_progress", "failed", "pending"}
	for _, status := range statuses {
		t.Run(status, func(t *testing.T) {
			symbol := getJobStatusSymbol(status)
			field := formatStatusField(symbol, status)
			assert.Equal(t, statusFieldWidth, runewidth.StringWidth(field),
				"status field must occupy exactly %d display columns to align with the %%-15s header",
				statusFieldWidth)
		})
	}
}

func TestFormatStatusField_UnknownStatusStillPads(t *testing.T) {
	field := formatStatusField(getJobStatusSymbol("mystery"), "mystery")
	assert.Equal(t, statusFieldWidth, runewidth.StringWidth(field))
}

// TestExecuteStepManually_RerunsCompletedDIMPStep is the regression guard for
// `aether job run --step dimp` on an already-completed step: it must re-run the
// step (matching pre-refactor behavior), not hard-fail on an illegal
// completed->in_progress transition.
func TestExecuteStepManually_RerunsCompletedDIMPStep(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var resource map[string]any
		_ = json.NewDecoder(r.Body).Decode(&resource)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resource)
	}))
	defer server.Close()

	jobsDir := t.TempDir()
	jobID := "550e8400-e29b-41d4-a716-446655440000"
	importDir := filepath.Join(jobsDir, jobID, "import")
	require.NoError(t, os.MkdirAll(importDir, 0o755))
	f, err := os.Create(filepath.Join(importDir, "patients.ndjson"))
	require.NoError(t, err)
	require.NoError(t, json.NewEncoder(f).Encode(map[string]any{"resourceType": "Patient", "id": "p1"}))
	require.NoError(t, f.Close())

	steps := []models.StepName{models.StepLocalImport, models.StepDIMP}
	config := &models.ProjectConfig{
		JobsDir:  jobsDir,
		Pipeline: models.PipelineConfig{EnabledSteps: steps},
		Services: models.ServiceConfig{DIMP: models.DIMPConfig{URL: server.URL, BundleSplitThresholdMB: 10}},
		Retry:    models.RetryConfig{MaxAttempts: 3, InitialBackoffMs: 10, MaxBackoffMs: 100},
	}
	job := &models.PipelineJob{
		JobID:       jobID,
		Status:      models.JobStatusInProgress,
		InputSource: importDir,
		InputType:   models.InputTypeLocal,
		CurrentStep: string(models.StepDIMP),
		Steps:       models.InitializeSteps(steps),
		Config:      *config,
	}
	// The DIMP step already ran to completion in a prior invocation.
	d, _ := models.GetStepByName(*job, models.StepDIMP)
	completed := models.ReplaceStep(*job, models.CompleteStep(d, 1, 0))
	job = &completed

	err = executeStepManually(job, models.StepDIMP, config, lib.NewLogger(lib.LogLevelError))

	require.NoError(t, err)
	s, ok := models.GetStepByName(*job, models.StepDIMP)
	require.True(t, ok)
	assert.Equal(t, models.StepStatusCompleted, s.Status)
}

// TestExecuteStepManually_RunsValidationStep proves the manual `job run --step
// validation` path runs the real validation step through the shared seam, rather
// than the old placeholder that returned "not yet implemented".
func TestExecuteStepManually_RunsValidationStep(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"resourceType": "OperationOutcome",
			"issue":        []map[string]any{{"severity": "information", "code": "informational"}},
		})
	}))
	defer server.Close()

	jobsDir := t.TempDir()
	jobID := "550e8400-e29b-41d4-a716-446655440000"
	importDir := filepath.Join(jobsDir, jobID, "import")
	require.NoError(t, os.MkdirAll(importDir, 0o755))
	f, err := os.Create(filepath.Join(importDir, "Patient.ndjson"))
	require.NoError(t, err)
	require.NoError(t, json.NewEncoder(f).Encode(map[string]any{"resourceType": "Patient", "id": "p1"}))
	require.NoError(t, f.Close())

	failOnError := false
	steps := []models.StepName{models.StepLocalImport, models.StepValidation}
	config := &models.ProjectConfig{
		JobsDir:  jobsDir,
		Pipeline: models.PipelineConfig{EnabledSteps: steps},
		Services: models.ServiceConfig{Validation: models.ValidationConfig{
			URL: server.URL, MaxConcurrentRequests: 2, BundleChunkSizeMB: 10, FailOnError: &failOnError,
		}},
		Retry: models.RetryConfig{MaxAttempts: 1, InitialBackoffMs: 10, MaxBackoffMs: 100},
	}
	job := &models.PipelineJob{
		JobID:       jobID,
		Status:      models.JobStatusInProgress,
		InputSource: importDir,
		InputType:   models.InputTypeLocal,
		CurrentStep: string(models.StepValidation),
		Steps:       models.InitializeSteps(steps),
		Config:      *config,
	}

	err = executeStepManually(job, models.StepValidation, config, lib.NewLogger(lib.LogLevelError))

	require.NoError(t, err)
	s, ok := models.GetStepByName(*job, models.StepValidation)
	require.True(t, ok)
	assert.Equal(t, models.StepStatusCompleted, s.Status)
}
