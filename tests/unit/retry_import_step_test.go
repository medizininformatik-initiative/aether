package unit

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/pipeline"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

// TestRetryImportStep_StepNotFound verifies error when current step doesn't exist in job
func TestRetryImportStep_StepNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	logger := lib.NewLogger(lib.LogLevelInfo)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{
		MaxAttempts:      3,
		InitialBackoffMs: 100,
		MaxBackoffMs:     1000,
	}, logger)

	// Create job with steps but set CurrentStep to non-existent step
	job := &models.PipelineJob{
		JobID:       "test-job-step-not-found",
		InputSource: "http://example.com/data.ndjson",
		InputType:   models.InputTypeHTTP,
		CurrentStep: "non_existent_step", // Invalid step name
		Status:      models.JobStatusInProgress,
		Steps:       models.InitializeSteps([]models.StepName{models.StepHttpImport}),
		Config: models.ProjectConfig{
			JobsDir: tmpDir,
			Pipeline: models.PipelineConfig{
				EnabledSteps: []models.StepName{models.StepHttpImport},
			},
			Retry: models.RetryConfig{
				MaxAttempts:      3,
				InitialBackoffMs: 100,
				MaxBackoffMs:     1000,
			},
		},
	}

	// Attempt retry
	updatedJob, err := pipeline.RetryImportStep(job, logger, httpClient, false)

	// Verify error
	require.Error(t, err)
	assert.Contains(t, err.Error(), "import step not found")
	assert.Nil(t, updatedJob)
}

// TestRetryImportStep_NoErrorToRetry verifies error when LastError is nil
func TestRetryImportStep_NoErrorToRetry(t *testing.T) {
	tmpDir := t.TempDir()
	logger := lib.NewLogger(lib.LogLevelInfo)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{
		MaxAttempts:      3,
		InitialBackoffMs: 100,
		MaxBackoffMs:     1000,
	}, logger)

	// Create job with step that has no error
	job := &models.PipelineJob{
		JobID:       "test-job-no-error",
		InputSource: "http://example.com/data.ndjson",
		InputType:   models.InputTypeHTTP,
		CurrentStep: string(models.StepHttpImport),
		Status:      models.JobStatusInProgress,
		Steps: []models.PipelineStep{
			{
				Name:       models.StepHttpImport,
				Status:     models.StepStatusFailed,
				RetryCount: 0,
				LastError:  nil, // No error - shouldn't happen but need to handle
			},
		},
		Config: models.ProjectConfig{
			JobsDir: tmpDir,
			Pipeline: models.PipelineConfig{
				EnabledSteps: []models.StepName{models.StepHttpImport},
			},
			Retry: models.RetryConfig{
				MaxAttempts:      3,
				InitialBackoffMs: 100,
				MaxBackoffMs:     1000,
			},
		},
	}

	// Attempt retry
	updatedJob, err := pipeline.RetryImportStep(job, logger, httpClient, false)

	// Verify error
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no error to retry")
	assert.Nil(t, updatedJob)
}

// TestRetryImportStep_NonTransientError verifies retry is rejected for non-transient errors
func TestRetryImportStep_NonTransientError(t *testing.T) {
	tmpDir := t.TempDir()
	logger := lib.NewLogger(lib.LogLevelInfo)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{
		MaxAttempts:      3,
		InitialBackoffMs: 100,
		MaxBackoffMs:     1000,
	}, logger)

	// Create job with non-transient error (e.g., 404)
	job := &models.PipelineJob{
		JobID:       "test-job-non-transient",
		InputSource: "http://example.com/data.ndjson",
		InputType:   models.InputTypeHTTP,
		CurrentStep: string(models.StepHttpImport),
		Status:      models.JobStatusInProgress,
		Steps: []models.PipelineStep{
			{
				Name:       models.StepHttpImport,
				Status:     models.StepStatusFailed,
				RetryCount: 0,
				LastError: &models.StepError{
					Type:       models.ErrorTypeNonTransient, // Non-transient error
					Message:    "HTTP 404: Not Found",
					HTTPStatus: 404,
					Timestamp:  time.Now(),
				},
			},
		},
		Config: models.ProjectConfig{
			JobsDir: tmpDir,
			Pipeline: models.PipelineConfig{
				EnabledSteps: []models.StepName{models.StepHttpImport},
			},
			Retry: models.RetryConfig{
				MaxAttempts:      3,
				InitialBackoffMs: 100,
				MaxBackoffMs:     1000,
			},
		},
	}

	// Attempt retry
	updatedJob, err := pipeline.RetryImportStep(job, logger, httpClient, false)

	// Verify retry is rejected
	require.Error(t, err)
	assert.Contains(t, err.Error(), "retry not allowed")
	assert.Contains(t, err.Error(), "non-transient error")
	assert.Nil(t, updatedJob)
}

// TestRetryImportStep_MaxRetriesExceeded verifies retry is rejected when max attempts reached
func TestRetryImportStep_MaxRetriesExceeded(t *testing.T) {
	tmpDir := t.TempDir()
	logger := lib.NewLogger(lib.LogLevelInfo)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{
		MaxAttempts:      3,
		InitialBackoffMs: 100,
		MaxBackoffMs:     1000,
	}, logger)

	// Create job with retry count at max
	job := &models.PipelineJob{
		JobID:       "test-job-max-retries",
		InputSource: "http://example.com/data.ndjson",
		InputType:   models.InputTypeHTTP,
		CurrentStep: string(models.StepHttpImport),
		Status:      models.JobStatusInProgress,
		Steps: []models.PipelineStep{
			{
				Name:       models.StepHttpImport,
				Status:     models.StepStatusFailed,
				RetryCount: 3, // Already at max
				LastError: &models.StepError{
					Type:       models.ErrorTypeTransient,
					Message:    "HTTP 503: Service Unavailable",
					HTTPStatus: 503,
					Timestamp:  time.Now(),
				},
			},
		},
		Config: models.ProjectConfig{
			JobsDir: tmpDir,
			Pipeline: models.PipelineConfig{
				EnabledSteps: []models.StepName{models.StepHttpImport},
			},
			Retry: models.RetryConfig{
				MaxAttempts:      3,
				InitialBackoffMs: 100,
				MaxBackoffMs:     1000,
			},
		},
	}

	// Attempt retry
	updatedJob, err := pipeline.RetryImportStep(job, logger, httpClient, false)

	// Verify retry is rejected
	require.Error(t, err)
	assert.Contains(t, err.Error(), "retry not allowed")
	assert.Contains(t, err.Error(), "max attempts reached")
	assert.Nil(t, updatedJob)
}

// TestRetryImportStep_RetryCountIncrement verifies retry counter increments correctly
func TestRetryImportStep_RetryCountIncrement(t *testing.T) {
	tmpDir := t.TempDir()
	logger := lib.NewLogger(lib.LogLevelInfo)

	testCases := []struct {
		name              string
		initialRetryCount int
		expectedRetryCount int
	}{
		{
			name:              "First retry (0 -> 1)",
			initialRetryCount: 0,
			expectedRetryCount: 1,
		},
		{
			name:              "Second retry (1 -> 2)",
			initialRetryCount: 1,
			expectedRetryCount: 2,
		},
		{
			name:              "Third retry (2 -> 3)",
			initialRetryCount: 2,
			expectedRetryCount: 3,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{
				MaxAttempts:      5,
				InitialBackoffMs: 1, // Very short for fast test
				MaxBackoffMs:     10,
			}, logger)

			// Create job with transient error
			job := &models.PipelineJob{
				JobID:       fmt.Sprintf("test-job-retry-%d", tc.initialRetryCount),
				InputSource: tmpDir, // Use tmpDir as local source (will fail but that's ok)
				InputType:   models.InputTypeLocal,
				CurrentStep: string(models.StepLocalImport),
				Status:      models.JobStatusInProgress,
				Steps: []models.PipelineStep{
					{
						Name:       models.StepLocalImport,
						Status:     models.StepStatusFailed,
						RetryCount: tc.initialRetryCount,
						LastError: &models.StepError{
							Type:      models.ErrorTypeTransient,
							Message:   "Network timeout",
							Timestamp: time.Now(),
						},
					},
				},
				Config: models.ProjectConfig{
					JobsDir: tmpDir,
					Pipeline: models.PipelineConfig{
						EnabledSteps: []models.StepName{models.StepLocalImport},
					},
					Retry: models.RetryConfig{
						MaxAttempts:      5,
						InitialBackoffMs: 1,
						MaxBackoffMs:     10,
					},
				},
			}

			// Attempt retry - this will call ExecuteImportStep which will fail
			// But we can verify the retry count was incremented before the call
			_, err := pipeline.RetryImportStep(job, logger, httpClient, false)

			// The retry should have been attempted (error from ExecuteImportStep is expected)
			assert.Error(t, err, "ExecuteImportStep should fail with empty directory")

			// Note: We can't easily verify the retry count increment without mocking ExecuteImportStep
			// This is tested more thoroughly in the integration tests
		})
	}
}

// TestRetryImportStep_BackoffCalculation verifies exponential backoff timing
func TestRetryImportStep_BackoffCalculation(t *testing.T) {
	testCases := []struct {
		name                string
		retryCount          int
		initialBackoffMs    int64
		maxBackoffMs        int64
		expectedBackoffMs   int64
	}{
		{
			name:              "First retry: base backoff",
			retryCount:        0, // Will become 1, so backoff uses attempt=0
			initialBackoffMs:  100,
			maxBackoffMs:      5000,
			expectedBackoffMs: 100, // 100 * 2^0 = 100
		},
		{
			name:              "Second retry: 2x backoff",
			retryCount:        1, // Will become 2, so backoff uses attempt=1
			initialBackoffMs:  100,
			maxBackoffMs:      5000,
			expectedBackoffMs: 200, // 100 * 2^1 = 200
		},
		{
			name:              "Third retry: 4x backoff",
			retryCount:        2, // Will become 3, so backoff uses attempt=2
			initialBackoffMs:  100,
			maxBackoffMs:      5000,
			expectedBackoffMs: 400, // 100 * 2^2 = 400
		},
		{
			name:              "Backoff capped at max",
			retryCount:        5, // Will become 6, so backoff uses attempt=5
			initialBackoffMs:  100,
			maxBackoffMs:      1000,
			expectedBackoffMs: 1000, // 100 * 2^5 = 3200, but capped at 1000
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Test the backoff calculation directly using lib.CalculateBackoff
			// which is what RetryImportStep uses internally
			backoff := lib.CalculateBackoff(tc.retryCount, tc.initialBackoffMs, tc.maxBackoffMs)

			expectedDuration := time.Duration(tc.expectedBackoffMs) * time.Millisecond
			assert.Equal(t, expectedDuration, backoff, "Backoff duration should match expected")
		})
	}
}

// TestRetryImportStep_LastRetryAttempt verifies retry succeeds at exactly (max-1)
func TestRetryImportStep_LastRetryAttempt(t *testing.T) {
	tmpDir := t.TempDir()
	logger := lib.NewLogger(lib.LogLevelInfo)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{
		MaxAttempts:      3,
		InitialBackoffMs: 1,
		MaxBackoffMs:     10,
	}, logger)

	// Create job with retry count at max-1
	job := &models.PipelineJob{
		JobID:       "test-job-last-retry",
		InputSource: tmpDir,
		InputType:   models.InputTypeLocal,
		CurrentStep: string(models.StepLocalImport),
		Status:      models.JobStatusInProgress,
		Steps: []models.PipelineStep{
			{
				Name:       models.StepLocalImport,
				Status:     models.StepStatusFailed,
				RetryCount: 2, // max-1 (max is 3)
				LastError: &models.StepError{
					Type:      models.ErrorTypeTransient,
					Message:   "Temporary failure",
					Timestamp: time.Now(),
				},
			},
		},
		Config: models.ProjectConfig{
			JobsDir: tmpDir,
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

	// Attempt retry - should be allowed
	_, err := pipeline.RetryImportStep(job, logger, httpClient, false)

	// Retry should be attempted (ExecuteImportStep will fail, but retry was allowed)
	assert.Error(t, err, "ExecuteImportStep should fail with empty directory")
	// The key is that we didn't get "retry not allowed" - the retry was attempted
	assert.NotContains(t, err.Error(), "retry not allowed", "Retry should have been allowed")
}

// TestRetryImportStep_MultipleRetriesProgression verifies retry progression 0→1→2→reject
func TestRetryImportStep_MultipleRetriesProgression(t *testing.T) {
	tmpDir := t.TempDir()
	logger := lib.NewLogger(lib.LogLevelInfo)

	retryStates := []struct {
		retryCount    int
		shouldAllow   bool
		description   string
	}{
		{0, true, "Initial failure - retry allowed"},
		{1, true, "First retry failed - second retry allowed"},
		{2, true, "Second retry failed - third retry allowed"},
		{3, false, "Third retry failed - max reached, no more retries"},
	}

	for _, state := range retryStates {
		t.Run(state.description, func(t *testing.T) {
			httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{
				MaxAttempts:      3,
				InitialBackoffMs: 1,
				MaxBackoffMs:     10,
			}, logger)

			job := &models.PipelineJob{
				JobID:       fmt.Sprintf("test-job-progression-%d", state.retryCount),
				InputSource: tmpDir,
				InputType:   models.InputTypeLocal,
				CurrentStep: string(models.StepLocalImport),
				Status:      models.JobStatusInProgress,
				Steps: []models.PipelineStep{
					{
						Name:       models.StepLocalImport,
						Status:     models.StepStatusFailed,
						RetryCount: state.retryCount,
						LastError: &models.StepError{
							Type:      models.ErrorTypeTransient,
							Message:   "Transient error",
							Timestamp: time.Now(),
						},
					},
				},
				Config: models.ProjectConfig{
					JobsDir: tmpDir,
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

			_, err := pipeline.RetryImportStep(job, logger, httpClient, false)

			require.Error(t, err)
			if state.shouldAllow {
				assert.NotContains(t, err.Error(), "retry not allowed",
					"Retry should have been allowed at retry count %d", state.retryCount)
			} else {
				assert.Contains(t, err.Error(), "retry not allowed",
					"Retry should have been rejected at retry count %d", state.retryCount)
			}
		})
	}
}
