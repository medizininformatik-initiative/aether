package integration

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/pipeline"
)

// TestPipelineResume_AfterImportComplete tests resuming pipeline after import step completes
// Integration test for pipeline resumption
func TestPipelineResume_AfterImportComplete(t *testing.T) {
	// Setup: Create temporary directories
	tempDir := t.TempDir()
	jobsDir := filepath.Join(tempDir, "jobs")
	sourceDir := filepath.Join(tempDir, "source")

	// Create source directory with test FHIR files
	require.NoError(t, os.MkdirAll(sourceDir, 0755))
	createTestFHIRFiles(t, sourceDir, 5)

	// Create config with multiple enabled steps
	config := models.ProjectConfig{
		Pipeline: models.PipelineConfig{
			EnabledSteps: []models.StepName{
				models.StepLocalImport,
				models.StepDIMP,
				models.StepCSVConversion,
			},
		},
		Retry: models.RetryConfig{
			MaxAttempts:      5,
			InitialBackoffMs: 100,
			MaxBackoffMs:     1000,
		},
		JobsDir: jobsDir,
		Services: models.ServiceConfig{
			DIMP: models.DIMPConfig{
				URL: "http://localhost:8083/fhir",
			},
			CSVConversion: models.CSVConversionConfig{
				URL: "http://localhost:9000/convert/csv",
			},
		},
	}

	logger := lib.NewLogger(lib.LogLevelInfo)

	// Phase 1: Create and start job (SESSION 1 simulation)
	job, err := pipeline.CreateJob(sourceDir, config, logger)
	require.NoError(t, err, "CreateJob should succeed")
	require.NotNil(t, job, "Job should be created")

	originalJobID := job.JobID

	// Start job
	startedJob := pipeline.StartJob(job)
	err = pipeline.UpdateJob(jobsDir, startedJob)
	require.NoError(t, err, "UpdateJob should succeed")

	// Execute import step only
	importedJob, err := pipeline.ExecuteImportStep(startedJob, logger, nil, false)
	require.NoError(t, err, "ExecuteImportStep should succeed")
	require.Equal(t, 5, importedJob.TotalFiles, "Should import 5 files")

	// Verify import step is completed
	importStep, found := models.GetStepByName(*importedJob, models.StepLocalImport)
	require.True(t, found, "Import step should exist")
	assert.Equal(t, models.StepStatusCompleted, importStep.Status, "Import step should be completed")

	// Advance to next step (DIMP) but don't execute it yet
	advancedJob, err := pipeline.AdvanceToNextStep(importedJob)
	require.NoError(t, err, "AdvanceToNextStep should succeed")
	assert.Equal(t, string(models.StepDIMP), advancedJob.CurrentStep, "Current step should advance to DIMP")

	// Save state (simulating state persistence before terminal close)
	err = pipeline.UpdateJob(jobsDir, advancedJob)
	require.NoError(t, err, "UpdateJob should persist state")

	// Phase 2: Simulate terminal close and reopen (SESSION 2 simulation)
	// Load job from disk (simulating fresh CLI session)
	reloadedJob, err := pipeline.LoadJob(jobsDir, originalJobID)
	require.NoError(t, err, "LoadJob should succeed after terminal close/reopen")
	require.NotNil(t, reloadedJob, "Reloaded job should not be nil")

	// Verify: Job state is preserved
	assert.Equal(t, originalJobID, reloadedJob.JobID, "Job ID should match")
	assert.Equal(t, string(models.StepDIMP), reloadedJob.CurrentStep, "Current step should be DIMP")
	assert.Equal(t, 5, reloadedJob.TotalFiles, "Total files should be preserved")
	assert.Equal(t, models.JobStatusInProgress, reloadedJob.Status, "Job status should be in_progress")

	// Verify: Import step is still completed
	reloadedImportStep, found := models.GetStepByName(*reloadedJob, models.StepLocalImport)
	require.True(t, found, "Import step should exist after reload")
	assert.Equal(t, models.StepStatusCompleted, reloadedImportStep.Status, "Import step should still be completed")
	assert.NotNil(t, reloadedImportStep.CompletedAt, "Import step should have completion time")

	// Verify: DIMP step is in pending/in_progress state (ready to execute)
	dimpStep, found := models.GetStepByName(*reloadedJob, models.StepDIMP)
	require.True(t, found, "DIMP step should exist after reload")
	assert.Contains(t, []models.StepStatus{models.StepStatusPending, models.StepStatusInProgress},
		dimpStep.Status, "DIMP step should be ready to execute")

	// Verify: Config is preserved
	assert.Equal(t, 3, len(reloadedJob.Config.Pipeline.EnabledSteps), "Config should preserve all enabled steps")
	assert.Equal(t, models.StepLocalImport, reloadedJob.Config.Pipeline.EnabledSteps[0])
	assert.Equal(t, models.StepDIMP, reloadedJob.Config.Pipeline.EnabledSteps[1])
	assert.Equal(t, models.StepCSVConversion, reloadedJob.Config.Pipeline.EnabledSteps[2])
}

// TestPipelineResume_AfterFailedStep tests resuming after a step failure
func TestPipelineResume_AfterFailedStep(t *testing.T) {
	tempDir := t.TempDir()
	jobsDir := filepath.Join(tempDir, "jobs")

	// Create job with import already completed
	job := createCompletedImportJob(t, jobsDir, 10)
	originalJobID := job.JobID

	// Simulate a failed DIMP step
	dimpStep, found := models.GetStepByName(*job, models.StepDIMP)
	require.True(t, found, "DIMP step should exist")

	failedStep := models.FailStep(dimpStep, models.ErrorTypeTransient, "Connection timeout", 503)
	updatedJob := models.ReplaceStep(*job, failedStep)
	updatedJob.CurrentStep = string(models.StepDIMP)

	// Save failed state
	err := pipeline.UpdateJob(jobsDir, &updatedJob)
	require.NoError(t, err)

	// Reload job (simulating terminal restart)
	reloadedJob, err := pipeline.LoadJob(jobsDir, originalJobID)
	require.NoError(t, err)

	// Verify: Failure state is preserved
	reloadedDimpStep, found := models.GetStepByName(*reloadedJob, models.StepDIMP)
	require.True(t, found)
	assert.Equal(t, models.StepStatusFailed, reloadedDimpStep.Status, "DIMP step should still be failed")
	assert.NotNil(t, reloadedDimpStep.LastError, "Error should be preserved")
	assert.Equal(t, models.ErrorTypeTransient, reloadedDimpStep.LastError.Type, "Error type should be preserved")
	assert.Equal(t, "Connection timeout", reloadedDimpStep.LastError.Message, "Error message should be preserved")
	assert.Equal(t, 503, reloadedDimpStep.LastError.HTTPStatus, "HTTP status should be preserved")
}

// TestPipelineResume_MultipleStepsCompleted tests resumption after multiple completed steps
func TestPipelineResume_MultipleStepsCompleted(t *testing.T) {
	tempDir := t.TempDir()
	jobsDir := filepath.Join(tempDir, "jobs")

	// Create job with import and DIMP completed
	job := createCompletedImportJob(t, jobsDir, 10)

	// Advance to DIMP step first
	advancedToDIMP, err := pipeline.AdvanceToNextStep(job)
	require.NoError(t, err)

	// Complete DIMP step
	dimpStep, found := models.GetStepByName(*advancedToDIMP, models.StepDIMP)
	require.True(t, found)
	completedDimpStep := models.CompleteStep(dimpStep, 10, 512000)
	updatedJob := models.ReplaceStep(*advancedToDIMP, completedDimpStep)
	job = &updatedJob

	// Advance to CSV conversion step
	advancedJob, err := pipeline.AdvanceToNextStep(job)
	require.NoError(t, err)

	// Save state
	err = pipeline.UpdateJob(jobsDir, advancedJob)
	require.NoError(t, err)

	originalJobID := advancedJob.JobID

	// Reload job
	reloadedJob, err := pipeline.LoadJob(jobsDir, originalJobID)
	require.NoError(t, err)

	// Verify: Current step is CSV conversion
	assert.Equal(t, string(models.StepCSVConversion), reloadedJob.CurrentStep, "Should advance to CSV conversion")

	// Verify: Previous steps are completed
	importStep, _ := models.GetStepByName(*reloadedJob, models.StepLocalImport)
	assert.Equal(t, models.StepStatusCompleted, importStep.Status, "Import should be completed")

	dimpStep, _ = models.GetStepByName(*reloadedJob, models.StepDIMP)
	assert.Equal(t, models.StepStatusCompleted, dimpStep.Status, "DIMP should be completed")
}

// TestPipelineResume_PreservesRetryCount tests that retry counts are preserved across sessions
func TestPipelineResume_PreservesRetryCount(t *testing.T) {
	tempDir := t.TempDir()
	jobsDir := filepath.Join(tempDir, "jobs")

	job := createCompletedImportJob(t, jobsDir, 5)
	originalJobID := job.JobID

	// Simulate failed DIMP with retries
	dimpStep, _ := models.GetStepByName(*job, models.StepDIMP)

	// First retry
	retriedStep := models.IncrementRetry(dimpStep)
	retriedStep = models.IncrementRetry(retriedStep)
	retriedStep = models.IncrementRetry(retriedStep)

	// Fail after 3 retries
	failedStep := models.FailStep(retriedStep, models.ErrorTypeTransient, "Service unavailable", 503)

	updatedJob := models.ReplaceStep(*job, failedStep)
	err := pipeline.UpdateJob(jobsDir, &updatedJob)
	require.NoError(t, err)

	// Reload job
	reloadedJob, err := pipeline.LoadJob(jobsDir, originalJobID)
	require.NoError(t, err)

	// Verify: Retry count is preserved
	reloadedDimpStep, _ := models.GetStepByName(*reloadedJob, models.StepDIMP)
	assert.Equal(t, 3, reloadedDimpStep.RetryCount, "Retry count should be preserved")
	assert.Equal(t, models.StepStatusFailed, reloadedDimpStep.Status)
}

// TestPipelineResume_CompletedJob tests loading a fully completed job
func TestPipelineResume_CompletedJob(t *testing.T) {
	tempDir := t.TempDir()
	jobsDir := filepath.Join(tempDir, "jobs")

	// Create fully completed job
	job := createCompletedImportJob(t, jobsDir, 10)

	// Complete DIMP
	dimpStep, _ := models.GetStepByName(*job, models.StepDIMP)
	updatedJob := models.ReplaceStep(*job, models.CompleteStep(dimpStep, 10, 512000))
	job = &updatedJob

	// Complete CSV conversion
	csvStep, _ := models.GetStepByName(*job, models.StepCSVConversion)
	updatedJob2 := models.ReplaceStep(*job, models.CompleteStep(csvStep, 10, 256000))
	job = &updatedJob2

	// Mark job as completed
	completedJob := pipeline.CompleteJob(job)
	err := pipeline.UpdateJob(jobsDir, completedJob)
	require.NoError(t, err)

	originalJobID := completedJob.JobID

	// Reload completed job
	reloadedJob, err := pipeline.LoadJob(jobsDir, originalJobID)
	require.NoError(t, err)

	// Verify: Job is completed
	assert.Equal(t, models.JobStatusCompleted, reloadedJob.Status, "Job should be completed")
	assert.Equal(t, "", reloadedJob.CurrentStep, "Completed job should have no current step")

	// Verify: All steps are completed
	for _, step := range reloadedJob.Steps {
		assert.Equal(t, models.StepStatusCompleted, step.Status, "All steps should be completed")
		assert.NotNil(t, step.CompletedAt, "All steps should have completion time")
	}
}

// Helper: Create test FHIR files in a directory
func createTestFHIRFiles(t *testing.T, dir string, count int) {
	for i := 0; i < count; i++ {
		filename := filepath.Join(dir, uuid.New().String()+".ndjson")
		content := `{"resourceType":"Patient","id":"123","name":[{"family":"Test"}]}`
		err := os.WriteFile(filename, []byte(content), 0644)
		require.NoError(t, err)
	}
}

// Helper: Create a job with completed import step
func createCompletedImportJob(t *testing.T, jobsDir string, fileCount int) *models.PipelineJob {
	// Create source directory with test files
	tempSourceDir := t.TempDir()
	createTestFHIRFiles(t, tempSourceDir, fileCount)

	config := models.ProjectConfig{
		Pipeline: models.PipelineConfig{
			EnabledSteps: []models.StepName{
				models.StepLocalImport,
				models.StepDIMP,
				models.StepCSVConversion,
			},
		},
		Retry: models.RetryConfig{
			MaxAttempts:      5,
			InitialBackoffMs: 100,
			MaxBackoffMs:     1000,
		},
		JobsDir: jobsDir,
	}

	logger := lib.NewLogger(lib.LogLevelInfo)
	// Create job
	job, err := pipeline.CreateJob(tempSourceDir, config, logger)
	require.NoError(t, err)

	// Start job and complete import
	startedJob := pipeline.StartJob(job)
	logger = lib.NewLogger(lib.LogLevelInfo)
	importedJob, err := pipeline.ExecuteImportStep(startedJob, logger, nil, false)
	require.NoError(t, err)

	// Save completed import state
	err = pipeline.UpdateJob(jobsDir, importedJob)
	require.NoError(t, err)

	return importedJob
}
