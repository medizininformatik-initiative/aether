package integration

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/pipeline"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

// TestPipelineImportLocal_EndToEnd verifies the complete pipeline import workflow from local directory
func TestPipelineImportLocal_EndToEnd(t *testing.T) {
	// Setup temporary directories
	tempDir := t.TempDir()
	sourceDir := filepath.Join(tempDir, "source")
	jobsDir := filepath.Join(tempDir, "jobs")

	// Create source directory with test FHIR files
	require.NoError(t, os.MkdirAll(sourceDir, 0755))

	testFiles := map[string]string{
		"Patient_001.ndjson": `{"resourceType":"Patient","id":"patient-1","name":[{"family":"Smith"}]}
{"resourceType":"Patient","id":"patient-2","name":[{"family":"Jones"}]}
{"resourceType":"Patient","id":"patient-3","name":[{"family":"Brown"}]}`,
		"Observation_001.ndjson": `{"resourceType":"Observation","id":"obs-1","status":"final"}
{"resourceType":"Observation","id":"obs-2","status":"final"}`,
		"Encounter_001.ndjson": `{"resourceType":"Encounter","id":"enc-1","status":"finished"}`,
	}

	for filename, content := range testFiles {
		path := filepath.Join(sourceDir, filename)
		require.NoError(t, os.WriteFile(path, []byte(content), 0644))
	}

	// Create project configuration
	config := models.ProjectConfig{
		JobsDir: jobsDir,
		Pipeline: models.PipelineConfig{
			EnabledSteps: []models.StepName{models.StepLocalImport},
		},
		Retry: models.RetryConfig{
			MaxAttempts:      5,
			InitialBackoffMs: 1000,
			MaxBackoffMs:     30000,
		},
	}

	// Create logger and HTTP client
	logger := lib.NewLogger(lib.LogLevelInfo)
	httpClient := services.DefaultHTTPClient()

	// Step 1: Create new job
	job, err := pipeline.CreateJob(sourceDir, config, logger)
	require.NoError(t, err, "Job creation should succeed")
	require.NotNil(t, job, "Job should be created")
	assert.NotEmpty(t, job.JobID, "Job should have an ID")
	assert.Equal(t, models.InputTypeLocal, job.InputType, "Input type should be local")
	assert.Equal(t, models.JobStatusPending, job.Status, "Initial status should be pending")

	// Verify job directory was created
	jobDir := services.GetJobDir(jobsDir, job.JobID)
	assert.DirExists(t, jobDir, "Job directory should exist")

	// Verify state file was created
	statePath := services.GetStateFilePath(jobsDir, job.JobID)
	assert.FileExists(t, statePath, "State file should exist")

	// Step 2: Start the job
	startedJob := pipeline.StartJob(job)
	require.NotNil(t, startedJob, "Started job should be returned")
	assert.Equal(t, models.JobStatusInProgress, startedJob.Status, "Status should be in_progress")

	// Save updated job state
	require.NoError(t, pipeline.UpdateJob(jobsDir, startedJob))

	// Step 3: Execute import step
	importedJob, err := pipeline.ExecuteImportStep(startedJob, logger, httpClient, false)
	require.NoError(t, err, "Import should succeed")
	require.NotNil(t, importedJob, "Imported job should be returned")

	// Verify import step completed
	importStep, found := models.GetStepByName(*importedJob, models.StepLocalImport)
	require.True(t, found, "Import step should exist")
	assert.Equal(t, models.StepStatusCompleted, importStep.Status, "Import step should be completed")
	assert.Equal(t, 3, importStep.FilesProcessed, "Should process 3 files")
	assert.Greater(t, importStep.BytesProcessed, int64(0), "Should process bytes")
	assert.Equal(t, 0, importStep.RetryCount, "Should not require retries")

	// Verify job metrics
	assert.Equal(t, 3, importedJob.TotalFiles, "Job should have 3 total files")
	assert.Greater(t, importedJob.TotalBytes, int64(0), "Job should have bytes processed")

	// Save final job state
	require.NoError(t, pipeline.UpdateJob(jobsDir, importedJob))

	// Step 4: Verify files were imported
	importDir := services.GetJobOutputDir(jobsDir, job.JobID, models.StepLocalImport)
	assert.DirExists(t, importDir, "Import directory should exist")

	// Check that all files were copied
	for filename := range testFiles {
		importedPath := filepath.Join(importDir, filename)
		assert.FileExists(t, importedPath, "File %s should be imported", filename)
	}

	// Step 5: Verify state persistence - reload job from disk
	reloadedJob, err := pipeline.LoadJob(jobsDir, job.JobID)
	require.NoError(t, err, "Should reload job from disk")
	assert.Equal(t, importedJob.JobID, reloadedJob.JobID, "Job ID should match")
	assert.Equal(t, importedJob.Status, reloadedJob.Status, "Status should be persisted")
	assert.Equal(t, importedJob.TotalFiles, reloadedJob.TotalFiles, "File count should be persisted")
	assert.Equal(t, importedJob.TotalBytes, reloadedJob.TotalBytes, "Byte count should be persisted")

	// Verify import step state
	reloadedImportStep, found := models.GetStepByName(*reloadedJob, models.StepLocalImport)
	require.True(t, found, "Import step should exist after reload")
	assert.Equal(t, models.StepStatusCompleted, reloadedImportStep.Status, "Import step status should be persisted")
	assert.Equal(t, 3, reloadedImportStep.FilesProcessed, "Files processed should be persisted")
}

// TestPipelineImportLocal_InvalidSource verifies error handling for invalid source directory
func TestPipelineImportLocal_InvalidSource(t *testing.T) {
	tempDir := t.TempDir()
	jobsDir := filepath.Join(tempDir, "jobs")
	nonexistentDir := filepath.Join(tempDir, "nonexistent")

	config := models.ProjectConfig{
		JobsDir: jobsDir,
		Pipeline: models.PipelineConfig{
			EnabledSteps: []models.StepName{models.StepLocalImport},
		},
		Retry: models.RetryConfig{
			MaxAttempts:      3,
			InitialBackoffMs: 100,
			MaxBackoffMs:     1000,
		},
	}

	logger := lib.NewLogger(lib.LogLevelInfo)
	httpClient := services.DefaultHTTPClient()

	// Create job with invalid source
	job, err := pipeline.CreateJob(nonexistentDir, config, logger)
	require.NoError(t, err, "Job creation should succeed even with invalid source")

	// Start job
	startedJob := pipeline.StartJob(job)

	// Execute import - should fail
	importedJob, err := pipeline.ExecuteImportStep(startedJob, logger, httpClient, false)
	assert.Error(t, err, "Import should fail for nonexistent directory")
	assert.NotNil(t, importedJob, "Job should be returned even on failure")

	// Verify import step failed
	importStep, found := models.GetStepByName(*importedJob, models.StepLocalImport)
	require.True(t, found, "Import step should exist")
	assert.Equal(t, models.StepStatusFailed, importStep.Status, "Import step should be failed")
	assert.NotNil(t, importStep.LastError, "Should have error details")
	assert.Equal(t, models.ErrorTypeNonTransient, importStep.LastError.Type, "Should be non-transient error")
	assert.Contains(t, importStep.LastError.Message, "does not exist", "Error should mention nonexistent directory")
}

// TestPipelineImportLocal_EmptyDirectory verifies error handling for directory with no FHIR files
func TestPipelineImportLocal_EmptyDirectory(t *testing.T) {
	tempDir := t.TempDir()
	sourceDir := filepath.Join(tempDir, "source")
	jobsDir := filepath.Join(tempDir, "jobs")

	// Create empty source directory
	require.NoError(t, os.MkdirAll(sourceDir, 0755))

	// Create non-FHIR files
	require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "readme.txt"), []byte("test"), 0644))

	config := models.ProjectConfig{
		JobsDir: jobsDir,
		Pipeline: models.PipelineConfig{
			EnabledSteps: []models.StepName{models.StepLocalImport},
		},
		Retry: models.RetryConfig{
			MaxAttempts:      3,
			InitialBackoffMs: 100,
			MaxBackoffMs:     1000,
		},
	}

	logger := lib.NewLogger(lib.LogLevelInfo)
	httpClient := services.DefaultHTTPClient()

	// Create and start job
	job, err := pipeline.CreateJob(sourceDir, config, logger)
	require.NoError(t, err)
	startedJob := pipeline.StartJob(job)

	// Execute import - should fail
	importedJob, err := pipeline.ExecuteImportStep(startedJob, logger, httpClient, false)
	assert.Error(t, err, "Import should fail for directory with no FHIR files")

	// Verify error details
	importStep, _ := models.GetStepByName(*importedJob, models.StepLocalImport)
	assert.Equal(t, models.StepStatusFailed, importStep.Status)
	assert.Contains(t, importStep.LastError.Message, "no FHIR NDJSON files", "Error should mention no FHIR files")
}

// TestPipelineImportLocal_ResourceCounting verifies correct line/resource counting
func TestPipelineImportLocal_ResourceCounting(t *testing.T) {
	tempDir := t.TempDir()
	sourceDir := filepath.Join(tempDir, "source")
	jobsDir := filepath.Join(tempDir, "jobs")

	require.NoError(t, os.MkdirAll(sourceDir, 0755))

	// Create file with known number of resources (lines)
	resourceCount := 100
	var content string
	for i := 0; i < resourceCount; i++ {
		content += fmt.Sprintf(`{"resourceType":"Patient","id":"patient-%d"}`+"\n", i)
	}

	require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "Patient.ndjson"), []byte(content), 0644))

	config := models.ProjectConfig{
		JobsDir: jobsDir,
		Pipeline: models.PipelineConfig{
			EnabledSteps: []models.StepName{models.StepLocalImport},
		},
		Retry: models.RetryConfig{
			MaxAttempts:      3,
			InitialBackoffMs: 100,
			MaxBackoffMs:     1000,
		},
	}

	logger := lib.NewLogger(lib.LogLevelInfo)
	httpClient := services.DefaultHTTPClient()

	// Execute import
	job, _ := pipeline.CreateJob(sourceDir, config, logger)
	startedJob := pipeline.StartJob(job)
	importedJob, err := pipeline.ExecuteImportStep(startedJob, logger, httpClient, false)

	require.NoError(t, err)

	// Verify resource count
	importStep, _ := models.GetStepByName(*importedJob, models.StepLocalImport)
	assert.Equal(t, 1, importStep.FilesProcessed, "Should process 1 file")

	// Check imported file metadata includes correct line count
	importDir := services.GetJobOutputDir(jobsDir, job.JobID, models.StepLocalImport)
	importedPath := filepath.Join(importDir, "Patient.ndjson")
	assert.FileExists(t, importedPath)

	// Count lines in imported file
	lineCount, err := lib.CountResourcesInFile(importedPath)
	require.NoError(t, err)
	assert.Equal(t, resourceCount, lineCount, "Should count correct number of resources")
}

// TestPipelineImportLocal_JobListAndStatus verifies job listing and status retrieval
func TestPipelineImportLocal_JobListAndStatus(t *testing.T) {
	tempDir := t.TempDir()
	sourceDir := filepath.Join(tempDir, "source")
	jobsDir := filepath.Join(tempDir, "jobs")

	require.NoError(t, os.MkdirAll(sourceDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "test.ndjson"), []byte(`{"resourceType":"Patient"}`), 0644))

	config := models.ProjectConfig{
		JobsDir: jobsDir,
		Pipeline: models.PipelineConfig{
			EnabledSteps: []models.StepName{models.StepLocalImport},
		},
		Retry: models.RetryConfig{
			MaxAttempts:      3,
			InitialBackoffMs: 100,
			MaxBackoffMs:     1000,
		},
	}

	logger := lib.NewLogger(lib.LogLevelInfo)
	httpClient := services.DefaultHTTPClient()

	// Create multiple jobs
	job1, _ := pipeline.CreateJob(sourceDir, config, logger)
	job2, _ := pipeline.CreateJob(sourceDir, config, logger)

	// Execute import for first job
	startedJob1 := pipeline.StartJob(job1)
	importedJob1, _ := pipeline.ExecuteImportStep(startedJob1, logger, httpClient, false)
	_ = pipeline.UpdateJob(jobsDir, importedJob1)

	// List all jobs
	jobIDs, err := services.ListAllJobs(jobsDir)
	require.NoError(t, err, "Should list jobs")
	assert.Len(t, jobIDs, 2, "Should list 2 jobs")
	assert.Contains(t, jobIDs, job1.JobID, "Should include job1")
	assert.Contains(t, jobIDs, job2.JobID, "Should include job2")

	// Load and verify job status
	loadedJob1, err := pipeline.LoadJob(jobsDir, job1.JobID)
	require.NoError(t, err, "Should load job1")
	assert.Equal(t, models.JobStatusInProgress, loadedJob1.Status)

	importStep, _ := models.GetStepByName(*loadedJob1, models.StepLocalImport)
	assert.Equal(t, models.StepStatusCompleted, importStep.Status)

	loadedJob2, err := pipeline.LoadJob(jobsDir, job2.JobID)
	require.NoError(t, err, "Should load job2")
	assert.Equal(t, models.JobStatusPending, loadedJob2.Status)
}
