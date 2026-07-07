package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/mattn/go-runewidth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/pipeline"
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

// TestExecuteStepManually_ForceRerunsCompletedDIMPStep proves that
// `aether job run --step dimp --force` on an already-completed step re-runs the
// step (resetting it to pending first) rather than hard-failing on an illegal
// completed->in_progress transition.
func TestExecuteStepManually_ForceRerunsCompletedDIMPStep(t *testing.T) {
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

	err = executeStepManually(job, models.StepDIMP, config, lib.NewLogger(lib.LogLevelError), true)

	require.NoError(t, err)
	s, ok := models.GetStepByName(*job, models.StepDIMP)
	require.True(t, ok)
	assert.Equal(t, models.StepStatusCompleted, s.Status)
}

// TestExecuteStepManually_ForceRerunOfCompletedJobReflectsInProgressThenCompleted
// covers the job-status semantics of re-running the *last* step: a --force re-run of
// an already-completed job (every step done) transitions the job to in_progress for
// the duration of the run — visible on disk while the step is running — and, because
// DIMP here is the final step with nothing downstream to invalidate, returns to
// completed once it finishes.
func TestExecuteStepManually_ForceRerunOfCompletedJobReflectsInProgressThenCompleted(t *testing.T) {
	jobsDir := t.TempDir()
	jobID := "550e8400-e29b-41d4-a716-446655440000"
	importDir := filepath.Join(jobsDir, jobID, "import")
	require.NoError(t, os.MkdirAll(importDir, 0o755))
	f, err := os.Create(filepath.Join(importDir, "patients.ndjson"))
	require.NoError(t, err)
	require.NoError(t, json.NewEncoder(f).Encode(map[string]any{"resourceType": "Patient", "id": "p1"}))
	require.NoError(t, f.Close())

	// Capture the persisted job status observed while the DIMP step is running.
	var mu sync.Mutex
	var statusDuringRun models.JobStatus
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if persisted, loadErr := pipeline.LoadJob(jobsDir, jobID); loadErr == nil {
			mu.Lock()
			statusDuringRun = persisted.Status
			mu.Unlock()
		}
		var resource map[string]any
		_ = json.NewDecoder(r.Body).Decode(&resource)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resource)
	}))
	defer server.Close()

	steps := []models.StepName{models.StepLocalImport, models.StepDIMP}
	config := &models.ProjectConfig{
		JobsDir:  jobsDir,
		Pipeline: models.PipelineConfig{EnabledSteps: steps},
		Services: models.ServiceConfig{DIMP: models.DIMPConfig{URL: server.URL, BundleSplitThresholdMB: 10}},
		Retry:    models.RetryConfig{MaxAttempts: 3, InitialBackoffMs: 10, MaxBackoffMs: 100},
	}
	job := &models.PipelineJob{
		JobID:       jobID,
		Status:      models.JobStatusCompleted,
		InputSource: importDir,
		InputType:   models.InputTypeLocal,
		Steps:       models.InitializeSteps(steps),
		Config:      *config,
	}
	// Every step already ran to completion: the job is terminally completed.
	for _, name := range steps {
		st, _ := models.GetStepByName(*job, name)
		done := models.ReplaceStep(*job, models.CompleteStep(st, 1, 0))
		job = &done
	}

	err = executeStepManually(job, models.StepDIMP, config, lib.NewLogger(lib.LogLevelError), true)
	require.NoError(t, err)

	mu.Lock()
	observed := statusDuringRun
	mu.Unlock()
	assert.Equal(t, models.JobStatusInProgress, observed, "job must read in_progress while the forced re-run is underway")

	assert.Equal(t, models.JobStatusCompleted, job.Status, "job must settle back to completed once all steps are done")
	persisted, loadErr := pipeline.LoadJob(jobsDir, jobID)
	require.NoError(t, loadErr)
	assert.Equal(t, models.JobStatusCompleted, persisted.Status)
}

// TestExecuteStepManually_ForceRerunOfMidStepInvalidatesDownstream proves that
// re-running a step that is NOT last invalidates every step after it: a --force
// re-run of DIMP (with a completed validation step downstream) re-runs DIMP but
// resets validation back to pending, so the job is in_progress — not completed —
// because the downstream result is now stale and must be re-run.
func TestExecuteStepManually_ForceRerunOfMidStepInvalidatesDownstream(t *testing.T) {
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

	steps := []models.StepName{models.StepLocalImport, models.StepDIMP, models.StepValidation}
	config := &models.ProjectConfig{
		JobsDir:  jobsDir,
		Pipeline: models.PipelineConfig{EnabledSteps: steps},
		Services: models.ServiceConfig{DIMP: models.DIMPConfig{URL: server.URL, BundleSplitThresholdMB: 10}},
		Retry:    models.RetryConfig{MaxAttempts: 3, InitialBackoffMs: 10, MaxBackoffMs: 100},
	}
	job := &models.PipelineJob{
		JobID:       jobID,
		Status:      models.JobStatusCompleted,
		InputSource: importDir,
		InputType:   models.InputTypeLocal,
		Steps:       models.InitializeSteps(steps),
		Config:      *config,
	}
	// Every step already ran to completion.
	for _, name := range steps {
		st, _ := models.GetStepByName(*job, name)
		done := models.ReplaceStep(*job, models.CompleteStep(st, 1, 0))
		job = &done
	}

	err = executeStepManually(job, models.StepDIMP, config, lib.NewLogger(lib.LogLevelError), true)
	require.NoError(t, err)

	dimp, _ := models.GetStepByName(*job, models.StepDIMP)
	assert.Equal(t, models.StepStatusCompleted, dimp.Status, "the re-run step must complete")

	val, _ := models.GetStepByName(*job, models.StepValidation)
	assert.Equal(t, models.StepStatusPending, val.Status, "a downstream step must be invalidated to pending")

	imp, _ := models.GetStepByName(*job, models.StepLocalImport)
	assert.Equal(t, models.StepStatusCompleted, imp.Status, "an upstream step must be untouched")

	assert.Equal(t, models.JobStatusInProgress, job.Status, "stale downstream keeps the job in_progress, not completed")
	persisted, loadErr := pipeline.LoadJob(jobsDir, jobID)
	require.NoError(t, loadErr)
	assert.Equal(t, models.JobStatusInProgress, persisted.Status)
}

// TestExecuteStepManually_BlocksCompletedStepWithoutForce proves that
// `aether job run --step dimp` without --force on an already-completed step fails
// fast with a clear error, but does NOT mutate or persist job state: the block is
// a pre-execution guard (nothing ran), so the job must not be marked failed and
// the completed step must stay clean.
func TestExecuteStepManually_BlocksCompletedStepWithoutForce(t *testing.T) {
	jobsDir := t.TempDir()
	jobID := "550e8400-e29b-41d4-a716-446655440000"
	require.NoError(t, os.MkdirAll(filepath.Join(jobsDir, jobID), 0o755))

	steps := []models.StepName{models.StepLocalImport, models.StepDIMP}
	config := &models.ProjectConfig{
		JobsDir:  jobsDir,
		Pipeline: models.PipelineConfig{EnabledSteps: steps},
		Services: models.ServiceConfig{DIMP: models.DIMPConfig{URL: "http://dimp.invalid", BundleSplitThresholdMB: 10}},
		Retry:    models.RetryConfig{MaxAttempts: 3, InitialBackoffMs: 10, MaxBackoffMs: 100},
	}
	job := &models.PipelineJob{
		JobID:       jobID,
		Status:      models.JobStatusInProgress,
		InputSource: filepath.Join(jobsDir, jobID, "import"),
		InputType:   models.InputTypeLocal,
		CurrentStep: string(models.StepDIMP),
		Steps:       models.InitializeSteps(steps),
		Config:      *config,
	}
	// The DIMP step already ran to completion in a prior invocation.
	d, _ := models.GetStepByName(*job, models.StepDIMP)
	completed := models.ReplaceStep(*job, models.CompleteStep(d, 1, 0))
	job = &completed

	err := executeStepManually(job, models.StepDIMP, config, lib.NewLogger(lib.LogLevelError), false)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "already completed")
	assert.Contains(t, err.Error(), "--force")

	// The pipeline never ran, so the job must not be marked failed and the
	// in-memory state must be untouched.
	assert.Equal(t, models.JobStatusInProgress, job.Status, "a blocked re-run must not fail the job")
	assert.Empty(t, job.ErrorMessage)

	s, ok := models.GetStepByName(*job, models.StepDIMP)
	require.True(t, ok)
	assert.Equal(t, models.StepStatusCompleted, s.Status, "a blocked re-run must not un-complete the step")
	assert.Nil(t, s.LastError, "a completed step that never errored must not carry a LastError")

	// The guard must persist nothing: no job file should have been written.
	_, loadErr := pipeline.LoadJob(jobsDir, jobID)
	assert.Error(t, loadErr, "a blocked re-run must not persist job state")
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

	err = executeStepManually(job, models.StepValidation, config, lib.NewLogger(lib.LogLevelError), false)

	require.NoError(t, err)
	s, ok := models.GetStepByName(*job, models.StepValidation)
	require.True(t, ok)
	assert.Equal(t, models.StepStatusCompleted, s.Status)
}

// TestExecuteStepManually_CompletingLastStepFlipsJobToCompleted covers the status
// recompute: a manual single-step run (no --force) that finishes the last outstanding
// step must flip the job from in_progress to completed, rather than leaving the job
// status stale.
func TestExecuteStepManually_CompletingLastStepFlipsJobToCompleted(t *testing.T) {
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
	// The import already completed; DIMP is the last outstanding step.
	imp, _ := models.GetStepByName(*job, models.StepLocalImport)
	withImport := models.ReplaceStep(*job, models.CompleteStep(imp, 1, 0))
	job = &withImport

	err = executeStepManually(job, models.StepDIMP, config, lib.NewLogger(lib.LogLevelError), false)
	require.NoError(t, err)

	assert.Equal(t, models.JobStatusCompleted, job.Status, "finishing the last step must complete the job")
	assert.Empty(t, job.CurrentStep, "a completed job has no current step")
	persisted, loadErr := pipeline.LoadJob(jobsDir, jobID)
	require.NoError(t, loadErr)
	assert.Equal(t, models.JobStatusCompleted, persisted.Status)
}
