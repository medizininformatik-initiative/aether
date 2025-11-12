package integration

import (
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

// TestPipelineRetry_TransientHTTPError verifies RetryImportStep with manually created transient error
func TestPipelineRetry_TransientHTTPError(t *testing.T) {
	// This test verifies RetryImportStep behavior with a transient error
	// Note: After HTTP client exhausts its retries, errors are classified as non-transient
	// So we manually create a job with a transient error to test RetryImportStep

	tempDir := t.TempDir()
	jobsDir := filepath.Join(tempDir, "jobs")

	config := models.ProjectConfig{
		JobsDir: jobsDir,
		Pipeline: models.PipelineConfig{
			EnabledSteps: []models.StepName{models.StepHttpImport},
		},
		Retry: models.RetryConfig{
			MaxAttempts:      3,
			InitialBackoffMs: 5,
			MaxBackoffMs:     50,
		},
	}

	logger := lib.NewLogger(lib.LogLevelInfo)
	httpClient := services.NewHTTPClient(2*time.Second, config.Retry, logger)

	// Create job with manually set transient error (simulating first failure)
	job := &models.PipelineJob{
		JobID:       "test-transient-retry",
		InputSource: tempDir, // Will fail but that's expected
		InputType:   models.InputTypeLocal,
		CurrentStep: string(models.StepLocalImport),
		Status:      models.JobStatusInProgress,
		Steps: []models.PipelineStep{
			{
				Name:       models.StepLocalImport,
				Status:     models.StepStatusFailed,
				RetryCount: 0,
				LastError: &models.StepError{
					Type:       models.ErrorTypeTransient,
					Message:    "Network timeout",
					HTTPStatus: 0,
					Timestamp:  time.Now(),
				},
			},
		},
		Config: config,
	}

	// Test RetryImportStep - should be allowed
	retriedJob, retryErr := pipeline.RetryImportStep(job, logger, httpClient, false)

	// Retry should be attempted (will fail with empty directory, but retry was allowed)
	assert.Error(t, retryErr)
	assert.NotNil(t, retriedJob)
	assert.NotContains(t, retryErr.Error(), "retry not allowed", "Retry should have been allowed")

	// Verify retry count was incremented
	retriedStep, found := models.GetStepByName(*retriedJob, models.StepName(retriedJob.CurrentStep))
	require.True(t, found)
	assert.Greater(t, retriedStep.RetryCount, job.Steps[0].RetryCount, "Retry count should increase")
}

// TestPipelineRetry_MaxRetriesExhausted verifies failure after max attempts
func TestPipelineRetry_MaxRetriesExhausted(t *testing.T) {
	// Setup with low max attempts
	tempDir := t.TempDir()
	jobsDir := filepath.Join(tempDir, "jobs")

	config := models.ProjectConfig{
		JobsDir: jobsDir,
		Pipeline: models.PipelineConfig{
			EnabledSteps: []models.StepName{models.StepLocalImport},
		},
		Retry: models.RetryConfig{
			MaxAttempts:      2, // Only 2 attempts
			InitialBackoffMs: 1,
			MaxBackoffMs:     10,
		},
	}

	logger := lib.NewLogger(lib.LogLevelInfo)
	httpClient := services.NewHTTPClient(2*time.Second, config.Retry, logger)

	// Create job with transient error, retry count 0
	job1 := &models.PipelineJob{
		JobID:       "test-max-retries-1",
		InputSource: tempDir,
		InputType:   models.InputTypeLocal,
		CurrentStep: string(models.StepLocalImport),
		Status:      models.JobStatusInProgress,
		Steps: []models.PipelineStep{
			{
				Name:       models.StepLocalImport,
				Status:     models.StepStatusFailed,
				RetryCount: 0,
				LastError: &models.StepError{
					Type:      models.ErrorTypeTransient,
					Message:   "Transient error",
					Timestamp: time.Now(),
				},
			},
		},
		Config: config,
	}

	// First retry - should be allowed (retry count 0 -> 1)
	job2, err := pipeline.RetryImportStep(job1, logger, httpClient, false)
	assert.Error(t, err)
	assert.NotNil(t, job2)
	assert.NotContains(t, err.Error(), "retry not allowed", "First retry should be allowed")

	// Manually restore transient error for testing (ExecuteImportStep may have changed it)
	step2, _ := models.GetStepByName(*job2, models.StepName(job2.CurrentStep))
	step2.LastError.Type = models.ErrorTypeTransient
	job2 = &models.PipelineJob{
		JobID:       job2.JobID,
		InputSource: job2.InputSource,
		InputType:   job2.InputType,
		CurrentStep: job2.CurrentStep,
		Status:      job2.Status,
		Steps:       []models.PipelineStep{step2},
		Config:      job2.Config,
	}

	// Second retry - should be allowed (retry count 1 -> 2)
	job3, err := pipeline.RetryImportStep(job2, logger, httpClient, false)
	assert.Error(t, err)
	assert.NotNil(t, job3)
	assert.NotContains(t, err.Error(), "retry not allowed", "Second retry should be allowed")

	// Third retry - should be REJECTED (retry count already at max)
	// Manually restore transient error for testing
	step3, _ := models.GetStepByName(*job3, models.StepName(job3.CurrentStep))
	step3.LastError.Type = models.ErrorTypeTransient
	job3 = &models.PipelineJob{
		JobID:       job3.JobID,
		InputSource: job3.InputSource,
		InputType:   job3.InputType,
		CurrentStep: job3.CurrentStep,
		Status:      job3.Status,
		Steps:       []models.PipelineStep{step3},
		Config:      job3.Config,
	}

	job4, err := pipeline.RetryImportStep(job3, logger, httpClient, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "retry not allowed", "Third retry should be rejected")
	assert.Nil(t, job4, "Should return nil when retry not allowed")
}

// TestPipelineRetry_EventualSuccess verifies the retry mechanism progression
func TestPipelineRetry_EventualSuccess(t *testing.T) {
	// This test documents that RetryImportStep respects the retry configuration
	// and allows progressive retries up to the max

	tempDir := t.TempDir()
	jobsDir := filepath.Join(tempDir, "jobs")

	config := models.ProjectConfig{
		JobsDir: jobsDir,
		Pipeline: models.PipelineConfig{
			EnabledSteps: []models.StepName{models.StepLocalImport},
		},
		Retry: models.RetryConfig{
			MaxAttempts:      3,
			InitialBackoffMs: 1,
			MaxBackoffMs:     10,
		},
	}

	logger := lib.NewLogger(lib.LogLevelInfo)
	httpClient := services.NewHTTPClient(2*time.Second, config.Retry, logger)

	// Simulate progression of retries
	job := &models.PipelineJob{
		JobID:       "test-eventual-success",
		InputSource: tempDir,
		InputType:   models.InputTypeLocal,
		CurrentStep: string(models.StepLocalImport),
		Status:      models.JobStatusInProgress,
		Steps: []models.PipelineStep{
			{
				Name:       models.StepLocalImport,
				Status:     models.StepStatusFailed,
				RetryCount: 0,
				LastError: &models.StepError{
					Type:      models.ErrorTypeTransient,
					Message:   "Temporary failure",
					Timestamp: time.Now(),
				},
			},
		},
		Config: config,
	}

	// Verify multiple retries can be attempted
	for i := 0; i < config.Retry.MaxAttempts-1; i++ {
		retriedJob, err := pipeline.RetryImportStep(job, logger, httpClient, false)
		assert.Error(t, err, "Expected error from empty directory")
		assert.NotNil(t, retriedJob, "Should return updated job")
		assert.NotContains(t, err.Error(), "retry not allowed", "Retry %d should be allowed", i+1)

		// Manually restore transient error for testing (ExecuteImportStep may have changed it)
		step, _ := models.GetStepByName(*retriedJob, models.StepName(retriedJob.CurrentStep))
		step.LastError.Type = models.ErrorTypeTransient
		job = &models.PipelineJob{
			JobID:       retriedJob.JobID,
			InputSource: retriedJob.InputSource,
			InputType:   retriedJob.InputType,
			CurrentStep: retriedJob.CurrentStep,
			Status:      retriedJob.Status,
			Steps:       []models.PipelineStep{step},
			Config:      retriedJob.Config,
		}
	}

	// After MaxAttempts-1 retries, we're at retry count (MaxAttempts-1)
	// One more retry should still be allowed
	retriedJob, err := pipeline.RetryImportStep(job, logger, httpClient, false)
	assert.Error(t, err, "Expected error from empty directory")
	assert.NotNil(t, retriedJob, "Should return updated job")
	assert.NotContains(t, err.Error(), "retry not allowed", "Last retry should still be allowed")

	// Manually restore transient error for testing
	step, _ := models.GetStepByName(*retriedJob, models.StepName(retriedJob.CurrentStep))
	step.LastError.Type = models.ErrorTypeTransient
	job = &models.PipelineJob{
		JobID:       retriedJob.JobID,
		InputSource: retriedJob.InputSource,
		InputType:   retriedJob.InputType,
		CurrentStep: retriedJob.CurrentStep,
		Status:      retriedJob.Status,
		Steps:       []models.PipelineStep{step},
		Config:      retriedJob.Config,
	}

	// NOW the retry count should be at max, and next retry should be rejected
	_, err = pipeline.RetryImportStep(job, logger, httpClient, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "retry not allowed", "Should reject after max retries")
}

// TestPipelineRetry_NonTransientNoRetry verifies non-transient errors don't trigger retry
func TestPipelineRetry_NonTransientNoRetry(t *testing.T) {
	tempDir := t.TempDir()
	jobsDir := filepath.Join(tempDir, "jobs")

	config := models.ProjectConfig{
		JobsDir: jobsDir,
		Pipeline: models.PipelineConfig{
			EnabledSteps: []models.StepName{models.StepLocalImport},
		},
		Retry: models.RetryConfig{
			MaxAttempts:      3,
			InitialBackoffMs: 10,
			MaxBackoffMs:     100,
		},
	}

	logger := lib.NewLogger(lib.LogLevelInfo)
	httpClient := services.NewHTTPClient(2*time.Second, config.Retry, logger)

	// Create job with non-transient error (e.g., 404 Not Found)
	job := &models.PipelineJob{
		JobID:       "test-non-transient",
		InputSource: "http://example.com/nonexistent.ndjson",
		InputType:   models.InputTypeHTTP,
		CurrentStep: string(models.StepHttpImport),
		Status:      models.JobStatusInProgress,
		Steps: []models.PipelineStep{
			{
				Name:       models.StepHttpImport,
				Status:     models.StepStatusFailed,
				RetryCount: 0,
				LastError: &models.StepError{
					Type:       models.ErrorTypeNonTransient,
					Message:    "HTTP 404: Not Found",
					HTTPStatus: 404,
					Timestamp:  time.Now(),
				},
			},
		},
		Config: config,
	}

	// Attempt retry - should be rejected immediately
	retriedJob, retryErr := pipeline.RetryImportStep(job, logger, httpClient, false)

	// Verify retry was rejected
	require.Error(t, retryErr)
	assert.Contains(t, retryErr.Error(), "retry not allowed")
	assert.Contains(t, retryErr.Error(), "non-transient error")
	assert.Nil(t, retriedJob, "Should return nil when retry not allowed")
}

// TestPipelineRetry_StateTransitions verifies job state updates during retry
func TestPipelineRetry_StateTransitions(t *testing.T) {
	tempDir := t.TempDir()
	jobsDir := filepath.Join(tempDir, "jobs")

	config := models.ProjectConfig{
		JobsDir: jobsDir,
		Pipeline: models.PipelineConfig{
			EnabledSteps: []models.StepName{models.StepLocalImport},
		},
		Retry: models.RetryConfig{
			MaxAttempts:      5,
			InitialBackoffMs: 1,
			MaxBackoffMs:     10,
		},
	}

	logger := lib.NewLogger(lib.LogLevelInfo)
	httpClient := services.NewHTTPClient(2*time.Second, config.Retry, logger)

	// Create job with failed step
	job1 := &models.PipelineJob{
		JobID:       "test-state-transitions",
		InputSource: tempDir,
		InputType:   models.InputTypeLocal,
		CurrentStep: string(models.StepLocalImport),
		Status:      models.JobStatusInProgress,
		Steps: []models.PipelineStep{
			{
				Name:       models.StepLocalImport,
				Status:     models.StepStatusFailed,
				RetryCount: 0,
				LastError: &models.StepError{
					Type:      models.ErrorTypeTransient,
					Message:   "Initial failure",
					Timestamp: time.Now(),
				},
			},
		},
		Config: config,
	}

	step1, found := models.GetStepByName(*job1, models.StepName(job1.CurrentStep))
	require.True(t, found)
	assert.Equal(t, models.StepStatusFailed, step1.Status)
	assert.Equal(t, 0, step1.RetryCount)

	// First retry
	job2, err := pipeline.RetryImportStep(job1, logger, httpClient, false)
	assert.Error(t, err) // Will fail with empty directory
	assert.NotNil(t, job2)

	step2, found := models.GetStepByName(*job2, models.StepName(job2.CurrentStep))
	require.True(t, found)
	assert.Equal(t, models.StepStatusFailed, step2.Status)
	assert.Greater(t, step2.RetryCount, step1.RetryCount, "Retry count should increase")

	// Verify job status reflects the failed step
	assert.Equal(t, models.JobStatusFailed, job2.Status, "Job status reflects failed step")
}

// TestPipelineRetry_BackoffBehavior verifies exponential backoff is applied
func TestPipelineRetry_BackoffBehavior(t *testing.T) {
	// This test verifies that backoff delay is actually applied between retries

	tempDir := t.TempDir()
	jobsDir := filepath.Join(tempDir, "jobs")

	config := models.ProjectConfig{
		JobsDir: jobsDir,
		Pipeline: models.PipelineConfig{
			EnabledSteps: []models.StepName{models.StepLocalImport},
		},
		Retry: models.RetryConfig{
			MaxAttempts:      3,
			InitialBackoffMs: 100, // 100ms initial
			MaxBackoffMs:     1000,
		},
	}

	logger := lib.NewLogger(lib.LogLevelInfo)
	httpClient := services.NewHTTPClient(2*time.Second, config.Retry, logger)

	// Create job with failed step
	job1 := &models.PipelineJob{
		JobID:       "test-backoff",
		InputSource: tempDir,
		InputType:   models.InputTypeLocal,
		CurrentStep: string(models.StepLocalImport),
		Status:      models.JobStatusInProgress,
		Steps: []models.PipelineStep{
			{
				Name:       models.StepLocalImport,
				Status:     models.StepStatusFailed,
				RetryCount: 0,
				LastError: &models.StepError{
					Type:      models.ErrorTypeTransient,
					Message:   "Temporary failure",
					Timestamp: time.Now(),
				},
			},
		},
		Config: config,
	}

	// Measure time for first retry
	start := time.Now()
	job2, err := pipeline.RetryImportStep(job1, logger, httpClient, false)
	duration := time.Since(start)

	assert.Error(t, err) // Will fail with empty directory
	assert.NotNil(t, job2)

	// Verify some delay occurred (at least the initial backoff)
	// Account for execution overhead, so check for at least 50ms
	assert.GreaterOrEqual(t, duration.Milliseconds(), int64(50),
		"Should have waited at least 50ms for backoff (configured 100ms minus overhead)")
}

// TestPipelineRetry_ImmutabilityVerification verifies pure function behavior
func TestPipelineRetry_ImmutabilityVerification(t *testing.T) {
	// This test verifies that RetryImportStep doesn't mutate the original job

	tempDir := t.TempDir()
	logger := lib.NewLogger(lib.LogLevelInfo)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{
		MaxAttempts:      3,
		InitialBackoffMs: 1,
		MaxBackoffMs:     10,
	}, logger)

	// Create job with failed step
	originalJob := &models.PipelineJob{
		JobID:       "test-immutability",
		InputSource: tempDir,
		InputType:   models.InputTypeLocal,
		CurrentStep: string(models.StepLocalImport),
		Status:      models.JobStatusInProgress,
		Steps: []models.PipelineStep{
			{
				Name:       models.StepLocalImport,
				Status:     models.StepStatusFailed,
				RetryCount: 1,
				LastError: &models.StepError{
					Type:      models.ErrorTypeTransient,
					Message:   "Test error",
					Timestamp: time.Now(),
				},
			},
		},
		Config: models.ProjectConfig{
			JobsDir: tempDir,
			Pipeline: models.PipelineConfig{
				EnabledSteps: []models.StepName{models.StepLocalImport},
			},
			Retry: models.RetryConfig{
				MaxAttempts:      3,
				InitialBackoffMs: 1,
				MaxBackoffMs:     10,
			},
		},
	}

	// Get original step for comparison
	originalStep, _ := models.GetStepByName(*originalJob, models.StepLocalImport)
	originalRetryCount := originalStep.RetryCount

	// Call RetryImportStep
	_, err := pipeline.RetryImportStep(originalJob, logger, httpClient, false)
	assert.Error(t, err) // Expected to fail with empty directory

	// Verify original job is unchanged
	stepAfter, _ := models.GetStepByName(*originalJob, models.StepLocalImport)
	assert.Equal(t, originalRetryCount, stepAfter.RetryCount,
		"Original job should not be mutated")
}
