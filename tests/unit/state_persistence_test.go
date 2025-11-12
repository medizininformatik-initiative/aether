package unit

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

// TestStatePersistence_SaveAndLoad tests the complete save/load cycle
// Unit test for state persistence (save/load cycle)
func TestStatePersistence_SaveAndLoad(t *testing.T) {
	// Setup: Create temporary jobs directory
	tempDir := t.TempDir()
	jobID := uuid.New().String()

	// Create a test job
	now := time.Now()
	originalJob := &models.PipelineJob{
		JobID:       jobID,
		CreatedAt:   now,
		UpdatedAt:   now,
		InputSource: "/path/to/test/data",
		InputType:   models.InputTypeLocal,
		CurrentStep: string(models.StepLocalImport),
		Status:      models.JobStatusInProgress,
		Steps: []models.PipelineStep{
			{
				Name:           models.StepLocalImport,
				Status:         models.StepStatusInProgress,
				StartedAt:      &now,
				FilesProcessed: 0,
				BytesProcessed: 0,
				RetryCount:     0,
			},
		},
		Config: models.ProjectConfig{
			Pipeline: models.PipelineConfig{
				EnabledSteps: []models.StepName{models.StepLocalImport},
			},
			Retry: models.RetryConfig{
				MaxAttempts:      5,
				InitialBackoffMs: 1000,
				MaxBackoffMs:     30000,
			},
			JobsDir: tempDir,
		},
		TotalFiles: 0,
		TotalBytes: 0,
	}

	// Test: Save the job
	err := services.SaveJobState(tempDir, originalJob)
	require.NoError(t, err, "SaveJobState should succeed")

	// Verify: State file exists
	statePath := services.GetStateFilePath(tempDir, jobID)
	_, err = os.Stat(statePath)
	require.NoError(t, err, "State file should exist after save")

	// Test: Load the job
	loadedJob, err := services.LoadJobState(tempDir, jobID)
	require.NoError(t, err, "LoadJobState should succeed")
	require.NotNil(t, loadedJob, "Loaded job should not be nil")

	// Verify: All fields match
	assert.Equal(t, originalJob.JobID, loadedJob.JobID, "JobID should match")
	assert.Equal(t, originalJob.InputSource, loadedJob.InputSource, "InputSource should match")
	assert.Equal(t, originalJob.InputType, loadedJob.InputType, "InputType should match")
	assert.Equal(t, originalJob.CurrentStep, loadedJob.CurrentStep, "CurrentStep should match")
	assert.Equal(t, originalJob.Status, loadedJob.Status, "Status should match")
	assert.Equal(t, originalJob.TotalFiles, loadedJob.TotalFiles, "TotalFiles should match")
	assert.Equal(t, originalJob.TotalBytes, loadedJob.TotalBytes, "TotalBytes should match")
	assert.Len(t, loadedJob.Steps, len(originalJob.Steps), "Steps count should match")

	// Verify: Step details match
	assert.Equal(t, originalJob.Steps[0].Name, loadedJob.Steps[0].Name, "Step name should match")
	assert.Equal(t, originalJob.Steps[0].Status, loadedJob.Steps[0].Status, "Step status should match")
	assert.Equal(t, originalJob.Steps[0].RetryCount, loadedJob.Steps[0].RetryCount, "Retry count should match")
}

// TestStatePersistence_AtomicWrite verifies atomic write behavior
func TestStatePersistence_AtomicWrite(t *testing.T) {
	tempDir := t.TempDir()
	jobID := uuid.New().String()

	// Create initial job
	job := createTestJob(jobID, tempDir)

	// Save job (first write)
	err := services.SaveJobState(tempDir, job)
	require.NoError(t, err, "First save should succeed")

	// Modify job
	job.Status = models.JobStatusCompleted
	job.CurrentStep = string(models.StepLocalImport)
	job.TotalFiles = 100

	// Save again (atomic overwrite)
	err = services.SaveJobState(tempDir, job)
	require.NoError(t, err, "Second save should succeed")

	// Verify: No temporary files left behind
	jobDir := services.GetJobDir(tempDir, jobID)
	entries, err := os.ReadDir(jobDir)
	require.NoError(t, err)

	tempFileCount := 0
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) == ".tmp" || entry.Name() == ".state.tmp" {
			tempFileCount++
		}
	}
	assert.Equal(t, 0, tempFileCount, "No temporary files should remain after atomic write")

	// Verify: Latest state is persisted
	loadedJob, err := services.LoadJobState(tempDir, jobID)
	require.NoError(t, err)
	assert.Equal(t, models.JobStatusCompleted, loadedJob.Status, "Latest status should be persisted")
	assert.Equal(t, 100, loadedJob.TotalFiles, "Latest file count should be persisted")
}

// TestStatePersistence_LoadNonexistent tests loading a job that doesn't exist
func TestStatePersistence_LoadNonexistent(t *testing.T) {
	tempDir := t.TempDir()
	nonexistentJobID := uuid.New().String()

	// Attempt to load nonexistent job
	job, err := services.LoadJobState(tempDir, nonexistentJobID)

	// Verify: Error is returned
	assert.Error(t, err, "Loading nonexistent job should return error")
	assert.Nil(t, job, "Job should be nil for nonexistent ID")
	assert.Contains(t, err.Error(), "job not found", "Error message should indicate job not found")
}

// TestStatePersistence_SaveInvalidJob tests that invalid jobs cannot be saved
func TestStatePersistence_SaveInvalidJob(t *testing.T) {
	tempDir := t.TempDir()

	// Create invalid job (missing required JobID)
	invalidJob := &models.PipelineJob{
		JobID:       "", // Invalid: empty JobID
		InputSource: "/test",
		InputType:   models.InputTypeLocal,
		Status:      models.JobStatusPending,
	}

	// Attempt to save invalid job
	err := services.SaveJobState(tempDir, invalidJob)

	// Verify: Error is returned
	assert.Error(t, err, "Saving invalid job should return error")
	assert.Contains(t, err.Error(), "invalid", "Error message should indicate validation failure")
}

// TestStatePersistence_RetryCountPersistence tests that retry counts are preserved
func TestStatePersistence_RetryCountPersistence(t *testing.T) {
	tempDir := t.TempDir()
	jobID := uuid.New().String()

	// Create job with retry count
	job := createTestJob(jobID, tempDir)
	job.Steps[0].RetryCount = 3
	job.Steps[0].LastError = &models.StepError{
		Type:      models.ErrorTypeTransient,
		Message:   "Connection timeout",
		Timestamp: time.Now(),
	}

	// Save and reload
	err := services.SaveJobState(tempDir, job)
	require.NoError(t, err)

	loadedJob, err := services.LoadJobState(tempDir, jobID)
	require.NoError(t, err)

	// Verify: Retry count is preserved
	assert.Equal(t, 3, loadedJob.Steps[0].RetryCount, "Retry count should be preserved")
	assert.NotNil(t, loadedJob.Steps[0].LastError, "Last error should be preserved")
	assert.Equal(t, models.ErrorTypeTransient, loadedJob.Steps[0].LastError.Type, "Error type should be preserved")
	assert.Equal(t, "Connection timeout", loadedJob.Steps[0].LastError.Message, "Error message should be preserved")
}

// TestStatePersistence_MultipleSteps tests persistence of jobs with multiple steps
func TestStatePersistence_MultipleSteps(t *testing.T) {
	tempDir := t.TempDir()
	jobID := uuid.New().String()

	now := time.Now()
	completedTime := now.Add(5 * time.Minute)

	// Create job with multiple steps
	job := createTestJob(jobID, tempDir)
	job.Config.Pipeline.EnabledSteps = []models.StepName{
		models.StepLocalImport,
		models.StepDIMP,
		models.StepCSVConversion,
	}

	job.Steps = []models.PipelineStep{
		{
			Name:           models.StepLocalImport,
			Status:         models.StepStatusCompleted,
			StartedAt:      &now,
			CompletedAt:    &completedTime,
			FilesProcessed: 100,
			BytesProcessed: 1024000,
			RetryCount:     0,
		},
		{
			Name:           models.StepDIMP,
			Status:         models.StepStatusInProgress,
			StartedAt:      &completedTime,
			FilesProcessed: 50,
			BytesProcessed: 512000,
			RetryCount:     1,
		},
		{
			Name:           models.StepCSVConversion,
			Status:         models.StepStatusPending,
			FilesProcessed: 0,
			BytesProcessed: 0,
			RetryCount:     0,
		},
	}

	// Save and reload
	err := services.SaveJobState(tempDir, job)
	require.NoError(t, err)

	loadedJob, err := services.LoadJobState(tempDir, jobID)
	require.NoError(t, err)

	// Verify: All steps are preserved
	require.Len(t, loadedJob.Steps, 3, "All steps should be preserved")

	assert.Equal(t, models.StepStatusCompleted, loadedJob.Steps[0].Status)
	assert.Equal(t, 100, loadedJob.Steps[0].FilesProcessed)
	assert.NotNil(t, loadedJob.Steps[0].CompletedAt)

	assert.Equal(t, models.StepStatusInProgress, loadedJob.Steps[1].Status)
	assert.Equal(t, 50, loadedJob.Steps[1].FilesProcessed)
	assert.Equal(t, 1, loadedJob.Steps[1].RetryCount)

	assert.Equal(t, models.StepStatusPending, loadedJob.Steps[2].Status)
	assert.Equal(t, 0, loadedJob.Steps[2].FilesProcessed)
}

// TestStatePersistence_ConcurrentAccess tests that state file remains consistent
// even with rapid successive writes (simulating concurrent scenarios)
func TestStatePersistence_ConcurrentAccess(t *testing.T) {
	tempDir := t.TempDir()
	jobID := uuid.New().String()

	job := createTestJob(jobID, tempDir)

	// Perform rapid successive writes
	for i := 0; i < 10; i++ {
		job.TotalFiles = i * 10
		job.UpdatedAt = time.Now()

		err := services.SaveJobState(tempDir, job)
		require.NoError(t, err, "Rapid save %d should succeed", i)
	}

	// Verify: Final state is consistent
	loadedJob, err := services.LoadJobState(tempDir, jobID)
	require.NoError(t, err)
	assert.Equal(t, 90, loadedJob.TotalFiles, "Final state should have last written value")
}

// Helper function to create a test job
func createTestJob(jobID, jobsDir string) *models.PipelineJob {
	now := time.Now()
	return &models.PipelineJob{
		JobID:       jobID,
		CreatedAt:   now,
		UpdatedAt:   now,
		InputSource: "/test/data",
		InputType:   models.InputTypeLocal,
		CurrentStep: string(models.StepLocalImport),
		Status:      models.JobStatusPending,
		Steps: []models.PipelineStep{
			{
				Name:           models.StepLocalImport,
				Status:         models.StepStatusPending,
				FilesProcessed: 0,
				BytesProcessed: 0,
				RetryCount:     0,
			},
		},
		Config: models.ProjectConfig{
			Pipeline: models.PipelineConfig{
				EnabledSteps: []models.StepName{models.StepLocalImport},
			},
			Retry: models.RetryConfig{
				MaxAttempts:      5,
				InitialBackoffMs: 1000,
				MaxBackoffMs:     30000,
			},
			JobsDir: jobsDir,
		},
		TotalFiles: 0,
		TotalBytes: 0,
	}
}
