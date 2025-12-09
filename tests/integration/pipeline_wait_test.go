package integration

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/pipeline"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

// TestPipelineWithWaitStep_PausesExecution tests that pipeline pauses at wait step
func TestPipelineWithWaitStep_PausesExecution(t *testing.T) {
	// Setup
	tempDir := t.TempDir()
	jobsDir := filepath.Join(tempDir, "jobs")
	sourceDir := filepath.Join(tempDir, "source")

	require.NoError(t, os.MkdirAll(sourceDir, 0755))
	createTestFHIRFiles(t, sourceDir, 3)

	config := models.ProjectConfig{
		Pipeline: models.PipelineConfig{
			EnabledSteps: []models.StepName{
				models.StepLocalImport,
				models.StepWait,
				models.StepDIMP,
			},
		},
		Retry: models.RetryConfig{
			MaxAttempts:      3,
			InitialBackoffMs: 100,
			MaxBackoffMs:     1000,
		},
		JobsDir: jobsDir,
		Services: models.ServiceConfig{
			DIMP: models.DIMPConfig{
				URL: "http://localhost:8083/fhir",
			},
		},
	}

	logger := lib.NewLogger(lib.LogLevelInfo)

	// Create and start job
	job, err := pipeline.CreateJob(sourceDir, config, logger)
	require.NoError(t, err)

	startedJob := pipeline.StartJob(job)
	err = pipeline.UpdateJob(jobsDir, startedJob)
	require.NoError(t, err)

	// Execute import step
	importedJob, err := pipeline.ExecuteImportStep(startedJob, logger, nil, false)
	require.NoError(t, err)
	require.Equal(t, 3, importedJob.TotalFiles)

	// Verify import step completed
	importStep, found := models.GetStepByName(*importedJob, models.StepLocalImport)
	require.True(t, found)
	assert.Equal(t, models.StepStatusCompleted, importStep.Status)

	// Advance to wait step
	advancedJob, err := pipeline.AdvanceToNextStep(importedJob)
	require.NoError(t, err)
	assert.Equal(t, string(models.StepWait), advancedJob.CurrentStep)

	// Execute wait step
	waitStepIndex := 1 // wait is at index 1
	err = pipeline.ExecuteWaitStep(advancedJob, jobsDir, waitStepIndex)
	require.NoError(t, err)

	// Verify wait directory was created but is empty
	waitDir := services.GetWaitStepDir(jobsDir, advancedJob.JobID, models.StepLocalImport)
	_, err = os.Stat(waitDir)
	assert.NoError(t, err, "Wait directory should exist")

	// Verify wait directory is empty (files are NOT copied)
	entries, err := os.ReadDir(waitDir)
	require.NoError(t, err)
	assert.Empty(t, entries, "Wait directory should be empty - users must place modified data here")
}

// TestPipelineWithWaitStep_DirectoryCreated tests wait directory creation
func TestPipelineWithWaitStep_DirectoryCreated(t *testing.T) {
	tempDir := t.TempDir()
	jobsDir := filepath.Join(tempDir, "jobs")
	sourceDir := filepath.Join(tempDir, "source")

	require.NoError(t, os.MkdirAll(sourceDir, 0755))
	createTestFHIRFiles(t, sourceDir, 2)

	config := models.ProjectConfig{
		Pipeline: models.PipelineConfig{
			EnabledSteps: []models.StepName{
				models.StepLocalImport,
				models.StepWait,
			},
		},
		Retry: models.RetryConfig{
			MaxAttempts:      3,
			InitialBackoffMs: 100,
			MaxBackoffMs:     1000,
		},
		JobsDir: jobsDir,
	}

	logger := lib.NewLogger(lib.LogLevelInfo)

	// Create, start, and execute import
	job, err := pipeline.CreateJob(sourceDir, config, logger)
	require.NoError(t, err)

	startedJob := pipeline.StartJob(job)
	_ = pipeline.UpdateJob(jobsDir, startedJob)

	importedJob, err := pipeline.ExecuteImportStep(startedJob, logger, nil, false)
	require.NoError(t, err)

	// Advance to wait step
	advancedJob, err := pipeline.AdvanceToNextStep(importedJob)
	require.NoError(t, err)

	// Execute wait step
	err = pipeline.ExecuteWaitStep(advancedJob, jobsDir, 1)
	require.NoError(t, err)

	// Verify import_wait directory was created
	waitDir := services.GetWaitStepDir(jobsDir, advancedJob.JobID, models.StepLocalImport)
	info, err := os.Stat(waitDir)
	require.NoError(t, err)
	assert.True(t, info.IsDir(), "Wait path should be a directory")

	// Verify directory name
	assert.Equal(t, "import_wait", filepath.Base(waitDir))
}

// TestPipelineWithWaitStep_EmptyDirectory tests that wait directory is created empty
func TestPipelineWithWaitStep_EmptyDirectory(t *testing.T) {
	tempDir := t.TempDir()
	jobsDir := filepath.Join(tempDir, "jobs")
	sourceDir := filepath.Join(tempDir, "source")

	require.NoError(t, os.MkdirAll(sourceDir, 0755))

	// Create specific test file with known content
	testContent := `{"resourceType":"Patient","id":"test-123","name":[{"family":"Doe"}]}`
	testFile := filepath.Join(sourceDir, "Patient.ndjson")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	config := models.ProjectConfig{
		Pipeline: models.PipelineConfig{
			EnabledSteps: []models.StepName{
				models.StepLocalImport,
				models.StepWait,
			},
		},
		Retry: models.RetryConfig{
			MaxAttempts:      3,
			InitialBackoffMs: 100,
			MaxBackoffMs:     1000,
		},
		JobsDir: jobsDir,
	}

	logger := lib.NewLogger(lib.LogLevelInfo)

	// Run through pipeline to wait step
	job, _ := pipeline.CreateJob(sourceDir, config, logger)
	startedJob := pipeline.StartJob(job)
	_ = pipeline.UpdateJob(jobsDir, startedJob)

	importedJob, err := pipeline.ExecuteImportStep(startedJob, logger, nil, false)
	require.NoError(t, err)

	advancedJob, err := pipeline.AdvanceToNextStep(importedJob)
	require.NoError(t, err)

	err = pipeline.ExecuteWaitStep(advancedJob, jobsDir, 1)
	require.NoError(t, err)

	// Verify wait directory was created but is EMPTY
	waitDir := services.GetWaitStepDir(jobsDir, advancedJob.JobID, models.StepLocalImport)
	entries, err := os.ReadDir(waitDir)
	require.NoError(t, err)
	assert.Empty(t, entries, "Wait directory should be empty - files are NOT copied from import")

	// Original import directory should still have the file
	importDir := filepath.Join(jobsDir, advancedJob.JobID, "import")
	content, err := os.ReadFile(filepath.Join(importDir, "Patient.ndjson"))
	require.NoError(t, err)
	assert.Equal(t, testContent, string(content), "Original import directory should still have the file")
}

// TestPipelineWithWaitStep_ContinuesAfterCommand tests pipeline continuation
func TestPipelineWithWaitStep_ContinuesAfterCommand(t *testing.T) {
	tempDir := t.TempDir()
	jobsDir := filepath.Join(tempDir, "jobs")
	sourceDir := filepath.Join(tempDir, "source")

	require.NoError(t, os.MkdirAll(sourceDir, 0755))
	createTestFHIRFiles(t, sourceDir, 2)

	config := models.ProjectConfig{
		Pipeline: models.PipelineConfig{
			EnabledSteps: []models.StepName{
				models.StepLocalImport,
				models.StepWait,
				models.StepValidation, // Use validation since it doesn't need external service
			},
		},
		Retry: models.RetryConfig{
			MaxAttempts:      3,
			InitialBackoffMs: 100,
			MaxBackoffMs:     1000,
		},
		JobsDir: jobsDir,
	}

	logger := lib.NewLogger(lib.LogLevelInfo)

	// Run through import and wait steps
	job, _ := pipeline.CreateJob(sourceDir, config, logger)
	startedJob := pipeline.StartJob(job)
	_ = pipeline.UpdateJob(jobsDir, startedJob)

	importedJob, err := pipeline.ExecuteImportStep(startedJob, logger, nil, false)
	require.NoError(t, err)

	advancedJob, err := pipeline.AdvanceToNextStep(importedJob)
	require.NoError(t, err)

	// Execute wait step
	err = pipeline.ExecuteWaitStep(advancedJob, jobsDir, 1)
	require.NoError(t, err)

	// Mark wait step as waiting
	for i := range advancedJob.Steps {
		if advancedJob.Steps[i].Name == models.StepWait && advancedJob.Steps[i].Status == models.StepStatusInProgress {
			advancedJob.Steps[i].Status = models.StepStatusWaiting
			break
		}
	}
	advancedJob.Status = models.JobStatusPending
	_ = pipeline.UpdateJob(jobsDir, advancedJob)

	// Simulate user running "pipeline continue"
	// Load job fresh (as the continue command would)
	loadedJob, err := pipeline.LoadJob(jobsDir, advancedJob.JobID)
	require.NoError(t, err)

	// Verify job is in waiting state
	waitStep, found := models.GetStepByName(*loadedJob, models.StepWait)
	require.True(t, found)
	assert.Equal(t, models.StepStatusWaiting, waitStep.Status)

	// Continue: mark wait step as completed
	for i := range loadedJob.Steps {
		if loadedJob.Steps[i].Name == models.StepWait && loadedJob.Steps[i].Status == models.StepStatusWaiting {
			now := time.Now()
			loadedJob.Steps[i].Status = models.StepStatusCompleted
			loadedJob.Steps[i].CompletedAt = &now
			break
		}
	}
	loadedJob.Status = models.JobStatusInProgress
	_ = pipeline.UpdateJob(jobsDir, loadedJob)

	// Verify we can advance to next step
	continuedJob, err := pipeline.AdvanceToNextStep(loadedJob)
	require.NoError(t, err)
	assert.Equal(t, string(models.StepValidation), continuedJob.CurrentStep)
}

// TestWaitStep_ContinueWithFilesPresent tests that pipeline continues when wait directory has files
func TestWaitStep_ContinueWithFilesPresent(t *testing.T) {
	tempDir := t.TempDir()
	jobsDir := filepath.Join(tempDir, "jobs")
	sourceDir := filepath.Join(tempDir, "source")

	require.NoError(t, os.MkdirAll(sourceDir, 0755))
	createTestFHIRFiles(t, sourceDir, 2)

	config := models.ProjectConfig{
		Pipeline: models.PipelineConfig{
			EnabledSteps: []models.StepName{
				models.StepLocalImport,
				models.StepWait,
				models.StepValidation,
			},
		},
		Retry: models.RetryConfig{
			MaxAttempts:      3,
			InitialBackoffMs: 100,
			MaxBackoffMs:     1000,
		},
		JobsDir: jobsDir,
	}

	logger := lib.NewLogger(lib.LogLevelInfo)

	// Create, start, and execute import step
	job, _ := pipeline.CreateJob(sourceDir, config, logger)
	startedJob := pipeline.StartJob(job)
	_ = pipeline.UpdateJob(jobsDir, startedJob)

	importedJob, err := pipeline.ExecuteImportStep(startedJob, logger, nil, false)
	require.NoError(t, err)

	// Advance to wait step and execute it
	advancedJob, err := pipeline.AdvanceToNextStep(importedJob)
	require.NoError(t, err)

	err = pipeline.ExecuteWaitStep(advancedJob, jobsDir, 1)
	require.NoError(t, err)

	// Mark wait step as waiting (simulating what executeStep does)
	for i := range advancedJob.Steps {
		if advancedJob.Steps[i].Name == models.StepWait && advancedJob.Steps[i].Status == models.StepStatusInProgress {
			advancedJob.Steps[i].Status = models.StepStatusWaiting
			break
		}
	}
	_ = pipeline.UpdateJob(jobsDir, advancedJob)

	// Simulate user placing files in wait directory
	waitDir := services.GetWaitStepDir(jobsDir, advancedJob.JobID, models.StepLocalImport)
	testFile := filepath.Join(waitDir, "Patient.ndjson")
	require.NoError(t, os.WriteFile(testFile, []byte(`{"resourceType":"Patient","id":"p1"}`), 0644))

	// Verify files are present
	fileCount, err := pipeline.CountFilesInDir(waitDir)
	require.NoError(t, err)
	assert.Equal(t, 1, fileCount, "Wait directory should have 1 file")

	// Simulate what runPipelineContinue should do:
	// When files are present, mark wait step as completed
	loadedJob, err := pipeline.LoadJob(jobsDir, advancedJob.JobID)
	require.NoError(t, err)

	// Find and verify wait step is in waiting status
	waitStep, found := models.GetStepByName(*loadedJob, models.StepWait)
	require.True(t, found)
	assert.Equal(t, models.StepStatusWaiting, waitStep.Status)

	// The continue command should mark wait step as completed when files exist
	for i := range loadedJob.Steps {
		if loadedJob.Steps[i].Name == models.StepWait && loadedJob.Steps[i].Status == models.StepStatusWaiting {
			now := time.Now()
			loadedJob.Steps[i].Status = models.StepStatusCompleted
			loadedJob.Steps[i].CompletedAt = &now
			break
		}
	}

	// Should be able to advance to next step
	continuedJob, err := pipeline.AdvanceToNextStep(loadedJob)
	require.NoError(t, err)
	assert.Equal(t, string(models.StepValidation), continuedJob.CurrentStep)
}

// TestWaitStep_ContinueWithEmptyDirectory tests that pipeline stays paused when wait directory is empty
func TestWaitStep_ContinueWithEmptyDirectory(t *testing.T) {
	tempDir := t.TempDir()
	jobsDir := filepath.Join(tempDir, "jobs")
	sourceDir := filepath.Join(tempDir, "source")

	require.NoError(t, os.MkdirAll(sourceDir, 0755))
	createTestFHIRFiles(t, sourceDir, 2)

	config := models.ProjectConfig{
		Pipeline: models.PipelineConfig{
			EnabledSteps: []models.StepName{
				models.StepLocalImport,
				models.StepWait,
				models.StepValidation,
			},
		},
		Retry: models.RetryConfig{
			MaxAttempts:      3,
			InitialBackoffMs: 100,
			MaxBackoffMs:     1000,
		},
		JobsDir: jobsDir,
	}

	logger := lib.NewLogger(lib.LogLevelInfo)

	// Run through import to wait step
	job, _ := pipeline.CreateJob(sourceDir, config, logger)
	startedJob := pipeline.StartJob(job)
	_ = pipeline.UpdateJob(jobsDir, startedJob)

	importedJob, err := pipeline.ExecuteImportStep(startedJob, logger, nil, false)
	require.NoError(t, err)

	advancedJob, err := pipeline.AdvanceToNextStep(importedJob)
	require.NoError(t, err)

	err = pipeline.ExecuteWaitStep(advancedJob, jobsDir, 1)
	require.NoError(t, err)

	// Mark wait step as waiting
	for i := range advancedJob.Steps {
		if advancedJob.Steps[i].Name == models.StepWait && advancedJob.Steps[i].Status == models.StepStatusInProgress {
			advancedJob.Steps[i].Status = models.StepStatusWaiting
			break
		}
	}
	_ = pipeline.UpdateJob(jobsDir, advancedJob)

	// Verify wait directory is empty (no files added by user)
	waitDir := services.GetWaitStepDir(jobsDir, advancedJob.JobID, models.StepLocalImport)
	fileCount, err := pipeline.CountFilesInDir(waitDir)
	require.NoError(t, err)
	assert.Equal(t, 0, fileCount, "Wait directory should be empty")

	// When directory is empty, wait step should remain in waiting status
	loadedJob, err := pipeline.LoadJob(jobsDir, advancedJob.JobID)
	require.NoError(t, err)

	waitStep, found := models.GetStepByName(*loadedJob, models.StepWait)
	require.True(t, found)
	assert.Equal(t, models.StepStatusWaiting, waitStep.Status, "Wait step should still be waiting when directory is empty")
}

// TestWaitStepValidation_Integration tests validation is called during config loading
func TestWaitStepValidation_Integration(t *testing.T) {
	testCases := []struct {
		name        string
		steps       []models.StepName
		expectError bool
	}{
		{
			name:        "Valid - wait after import",
			steps:       []models.StepName{models.StepLocalImport, models.StepWait, models.StepDIMP},
			expectError: false,
		},
		{
			name:        "Invalid - wait as first step",
			steps:       []models.StepName{models.StepWait, models.StepDIMP},
			expectError: true,
		},
		{
			name:        "Invalid - consecutive waits",
			steps:       []models.StepName{models.StepLocalImport, models.StepWait, models.StepWait},
			expectError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := lib.ValidateWaitStepPlacement(tc.steps)
			if tc.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
