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

// TestRetryTracking_IncrementRetryCount tests that retry counts are properly incremented
// Integration test for retry count tracking
func TestRetryTracking_IncrementRetryCount(t *testing.T) {
	tempDir := t.TempDir()
	jobsDir := filepath.Join(tempDir, "jobs")
	sourceDir := filepath.Join(tempDir, "source")

	// Create source with test files
	require.NoError(t, os.MkdirAll(sourceDir, 0755))
	createTestFile(t, sourceDir)

	config := models.ProjectConfig{
		Pipeline: models.PipelineConfig{
			EnabledSteps: []models.StepName{models.StepLocalImport, models.StepDIMP},
		},
		Retry: models.RetryConfig{
			MaxAttempts:      5,
			InitialBackoffMs: 100,
			MaxBackoffMs:     1000,
		},
		JobsDir: jobsDir,
	}

	logger := lib.NewLogger(lib.LogLevelInfo)

	// Create and start job
	job, err := pipeline.CreateJob(sourceDir, config, logger)
	require.NoError(t, err)

	startedJob := pipeline.StartJob(job)
	err = pipeline.UpdateJob(jobsDir, startedJob)
	require.NoError(t, err)

	// Complete import step
	importedJob, err := pipeline.ExecuteImportStep(startedJob, logger, nil, false)
	require.NoError(t, err)

	// Advance to DIMP step
	advancedJob, err := pipeline.AdvanceToNextStep(importedJob)
	require.NoError(t, err)

	// Get DIMP step
	dimpStep, found := models.GetStepByName(*advancedJob, models.StepDIMP)
	require.True(t, found)
	assert.Equal(t, 0, dimpStep.RetryCount, "Initial retry count should be 0")

	// Simulate failure and retry (manually, since we don't have DIMP service)
	failedStep := models.FailStep(dimpStep, models.ErrorTypeTransient, "Connection timeout", 503)
	updatedJob := models.ReplaceStep(*advancedJob, failedStep)

	// Increment retry count
	retriedStep := models.IncrementRetry(failedStep)
	updatedJob = models.ReplaceStep(updatedJob, retriedStep)

	// Verify: Retry count is 1
	dimpStepAfterRetry, _ := models.GetStepByName(updatedJob, models.StepDIMP)
	assert.Equal(t, 1, dimpStepAfterRetry.RetryCount, "Retry count should be 1 after first retry")

	// Save and reload
	err = pipeline.UpdateJob(jobsDir, &updatedJob)
	require.NoError(t, err)

	reloadedJob, err := pipeline.LoadJob(jobsDir, job.JobID)
	require.NoError(t, err)

	// Verify: Retry count persisted
	reloadedDimpStep, _ := models.GetStepByName(*reloadedJob, models.StepDIMP)
	assert.Equal(t, 1, reloadedDimpStep.RetryCount, "Retry count should persist")
}

// TestRetryTracking_MultipleRetries tests multiple retry attempts
func TestRetryTracking_MultipleRetries(t *testing.T) {
	tempDir := t.TempDir()
	jobsDir := filepath.Join(tempDir, "jobs")
	sourceDir := filepath.Join(tempDir, "source")

	require.NoError(t, os.MkdirAll(sourceDir, 0755))
	createTestFile(t, sourceDir)

	config := models.ProjectConfig{
		Pipeline: models.PipelineConfig{
			EnabledSteps: []models.StepName{models.StepLocalImport},
		},
		Retry: models.RetryConfig{
			MaxAttempts:      5,
			InitialBackoffMs: 100,
			MaxBackoffMs:     1000,
		},
		JobsDir: jobsDir,
	}

	logger := lib.NewLogger(lib.LogLevelInfo)
	job, err := pipeline.CreateJob(sourceDir, config, logger)
	require.NoError(t, err)

	startedJob := pipeline.StartJob(job)

	// Get import step
	importStep, found := models.GetStepByName(*startedJob, models.StepLocalImport)
	require.True(t, found)

	// Simulate 3 retries
	currentStep := importStep
	for i := 0; i < 3; i++ {
		// Fail the step
		failedStep := models.FailStep(currentStep, models.ErrorTypeTransient, "Temporary error", 503)

		// Increment retry
		currentStep = models.IncrementRetry(failedStep)

		// Verify retry count
		assert.Equal(t, i+1, currentStep.RetryCount, "Retry count should be %d", i+1)
	}

	// Update job with retried step
	updatedJob := models.ReplaceStep(*startedJob, currentStep)
	err = pipeline.UpdateJob(jobsDir, &updatedJob)
	require.NoError(t, err)

	// Reload and verify
	reloadedJob, err := pipeline.LoadJob(jobsDir, job.JobID)
	require.NoError(t, err)

	reloadedImportStep, _ := models.GetStepByName(*reloadedJob, models.StepLocalImport)
	assert.Equal(t, 3, reloadedImportStep.RetryCount, "Should have 3 retries")
	assert.NotNil(t, reloadedImportStep.LastError, "Should have last error")
	assert.Equal(t, models.ErrorTypeTransient, reloadedImportStep.LastError.Type)
}

// TestRetryTracking_ShouldRetryLogic tests the retry decision logic
func TestRetryTracking_ShouldRetryLogic(t *testing.T) {
	tests := []struct {
		name          string
		errorType     models.ErrorType
		retryCount    int
		maxAttempts   int
		expectedRetry bool
	}{
		{
			name:          "Transient error, retry 0, max 5",
			errorType:     models.ErrorTypeTransient,
			retryCount:    0,
			maxAttempts:   5,
			expectedRetry: true,
		},
		{
			name:          "Transient error, retry 4, max 5",
			errorType:     models.ErrorTypeTransient,
			retryCount:    4,
			maxAttempts:   5,
			expectedRetry: true, // Can still make one more attempt (5th)
		},
		{
			name:          "Transient error, retry 5, max 5",
			errorType:     models.ErrorTypeTransient,
			retryCount:    5,
			maxAttempts:   5,
			expectedRetry: false, // Already at max attempts
		},
		{
			name:          "Non-transient error, retry 0, max 5",
			errorType:     models.ErrorTypeNonTransient,
			retryCount:    0,
			maxAttempts:   5,
			expectedRetry: false,
		},
		{
			name:          "Transient error, retry 2, max 5",
			errorType:     models.ErrorTypeTransient,
			retryCount:    2,
			maxAttempts:   5,
			expectedRetry: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shouldRetry := lib.ShouldRetry(tt.errorType, tt.retryCount, tt.maxAttempts)
			assert.Equal(t, tt.expectedRetry, shouldRetry,
				"ShouldRetry(%v, %d, %d) = %v, want %v",
				tt.errorType, tt.retryCount, tt.maxAttempts, shouldRetry, tt.expectedRetry)
		})
	}
}

// TestRetryTracking_ErrorTypePersistence tests that error type is correctly persisted
func TestRetryTracking_ErrorTypePersistence(t *testing.T) {
	tempDir := t.TempDir()
	jobsDir := filepath.Join(tempDir, "jobs")
	sourceDir := filepath.Join(tempDir, "source")

	require.NoError(t, os.MkdirAll(sourceDir, 0755))
	createTestFile(t, sourceDir)

	config := models.ProjectConfig{
		Pipeline: models.PipelineConfig{
			EnabledSteps: []models.StepName{models.StepLocalImport, models.StepDIMP},
		},
		Retry: models.RetryConfig{
			MaxAttempts:      5,
			InitialBackoffMs: 100,
			MaxBackoffMs:     1000,
		},
		JobsDir: jobsDir,
	}

	logger := lib.NewLogger(lib.LogLevelInfo)
	job, err := pipeline.CreateJob(sourceDir, config, logger)
	require.NoError(t, err)

	startedJob := pipeline.StartJob(job)

	// Get DIMP step
	dimpStep, found := models.GetStepByName(*startedJob, models.StepDIMP)
	require.True(t, found)

	// Test transient error
	transientFailedStep := models.FailStep(dimpStep, models.ErrorTypeTransient, "Network error", 503)
	updatedJob := models.ReplaceStep(*startedJob, transientFailedStep)

	err = pipeline.UpdateJob(jobsDir, &updatedJob)
	require.NoError(t, err)

	reloadedJob, err := pipeline.LoadJob(jobsDir, job.JobID)
	require.NoError(t, err)

	reloadedDimpStep, _ := models.GetStepByName(*reloadedJob, models.StepDIMP)
	require.NotNil(t, reloadedDimpStep.LastError)
	assert.Equal(t, models.ErrorTypeTransient, reloadedDimpStep.LastError.Type, "Transient error type should be preserved")
	assert.Equal(t, "Network error", reloadedDimpStep.LastError.Message)
	assert.Equal(t, 503, reloadedDimpStep.LastError.HTTPStatus)

	// Test non-transient error
	nonTransientFailedStep := models.FailStep(dimpStep, models.ErrorTypeNonTransient, "Invalid data", 400)
	updatedJob2 := models.ReplaceStep(*startedJob, nonTransientFailedStep)

	err = pipeline.UpdateJob(jobsDir, &updatedJob2)
	require.NoError(t, err)

	reloadedJob2, err := pipeline.LoadJob(jobsDir, job.JobID)
	require.NoError(t, err)

	reloadedDimpStep2, _ := models.GetStepByName(*reloadedJob2, models.StepDIMP)
	require.NotNil(t, reloadedDimpStep2.LastError)
	assert.Equal(t, models.ErrorTypeNonTransient, reloadedDimpStep2.LastError.Type, "Non-transient error type should be preserved")
	assert.Equal(t, "Invalid data", reloadedDimpStep2.LastError.Message)
	assert.Equal(t, 400, reloadedDimpStep2.LastError.HTTPStatus)
}

// TestRetryTracking_BackoffCalculation tests exponential backoff calculation
func TestRetryTracking_BackoffCalculation(t *testing.T) {
	tests := []struct {
		name             string
		attempt          int
		initialBackoffMs int64
		maxBackoffMs     int64
		expectedMs       int64
	}{
		{
			name:             "First retry (attempt 0)",
			attempt:          0,
			initialBackoffMs: 1000,
			maxBackoffMs:     30000,
			expectedMs:       1000,
		},
		{
			name:             "Second retry (attempt 1)",
			attempt:          1,
			initialBackoffMs: 1000,
			maxBackoffMs:     30000,
			expectedMs:       2000,
		},
		{
			name:             "Third retry (attempt 2)",
			attempt:          2,
			initialBackoffMs: 1000,
			maxBackoffMs:     30000,
			expectedMs:       4000,
		},
		{
			name:             "Fourth retry (attempt 3)",
			attempt:          3,
			initialBackoffMs: 1000,
			maxBackoffMs:     30000,
			expectedMs:       8000,
		},
		{
			name:             "Exceeds max (attempt 10)",
			attempt:          10,
			initialBackoffMs: 1000,
			maxBackoffMs:     30000,
			expectedMs:       30000, // Capped at max
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			backoff := lib.CalculateBackoff(tt.attempt, tt.initialBackoffMs, tt.maxBackoffMs)
			backoffMs := backoff.Milliseconds()
			assert.Equal(t, tt.expectedMs, backoffMs,
				"CalculateBackoff(%d, %d, %d) = %dms, want %dms",
				tt.attempt, tt.initialBackoffMs, tt.maxBackoffMs, backoffMs, tt.expectedMs)
		})
	}
}

// TestRetryTracking_ResetOnSuccess tests that successful steps don't have retry counts
func TestRetryTracking_ResetOnSuccess(t *testing.T) {
	tempDir := t.TempDir()
	jobsDir := filepath.Join(tempDir, "jobs")
	sourceDir := filepath.Join(tempDir, "source")

	require.NoError(t, os.MkdirAll(sourceDir, 0755))
	createTestFile(t, sourceDir)

	config := models.ProjectConfig{
		Pipeline: models.PipelineConfig{
			EnabledSteps: []models.StepName{models.StepLocalImport},
		},
		Retry: models.RetryConfig{
			MaxAttempts:      5,
			InitialBackoffMs: 100,
			MaxBackoffMs:     1000,
		},
		JobsDir: jobsDir,
	}

	logger := lib.NewLogger(lib.LogLevelInfo)

	job, err := pipeline.CreateJob(sourceDir, config, logger)
	require.NoError(t, err)

	startedJob := pipeline.StartJob(job)

	// Execute import successfully
	importedJob, err := pipeline.ExecuteImportStep(startedJob, logger, nil, false)
	require.NoError(t, err)

	// Verify: Successful step has no retries
	importStep, _ := models.GetStepByName(*importedJob, models.StepLocalImport)
	assert.Equal(t, 0, importStep.RetryCount, "Successful step should have 0 retries")
	assert.Nil(t, importStep.LastError, "Successful step should have no error")
	assert.Equal(t, models.StepStatusCompleted, importStep.Status)
}

// Helper: Create a test FHIR file
func createTestFile(t *testing.T, dir string) {
	filename := filepath.Join(dir, uuid.New().String()+".ndjson")
	content := `{"resourceType":"Patient","id":"test-123"}`
	err := os.WriteFile(filename, []byte(content), 0644)
	require.NoError(t, err)
}
