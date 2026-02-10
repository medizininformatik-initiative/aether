package unit

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/pipeline"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

// TestExecuteImportStep_UnsupportedStep tests error handling for unsupported import steps
// This covers import.go switch statement for currentStep
func TestExecuteImportStep_UnsupportedStep(t *testing.T) {
	tmpDir := t.TempDir()
	logger := lib.NewLogger(lib.LogLevelInfo)

	retryConfig := models.RetryConfig{
		MaxAttempts:      3,
		InitialBackoffMs: 500,
		MaxBackoffMs:     5000,
	}
	httpClient := services.NewHTTPClient(5*time.Second, retryConfig, models.TLSConfig{}, logger)

	// Create a job with an unsupported step (not an import step)
	job := &models.PipelineJob{
		JobID:       "test-job-123",
		InputSource: "/some/path",
		InputType:   models.InputTypeLocal,
		CurrentStep: string(models.StepDIMP), // Not an import step
		Status:      models.JobStatusPending,
		Steps:       models.InitializeSteps([]models.StepName{models.StepDIMP}),
		Config: models.ProjectConfig{
			JobsDir: tmpDir,
			Pipeline: models.PipelineConfig{
				EnabledSteps: []models.StepName{models.StepDIMP},
			},
			Retry: models.RetryConfig{
				MaxAttempts:      3,
				InitialBackoffMs: 500,
				MaxBackoffMs:     5000,
			},
		},
	}

	// Execute import step - should fail with unsupported step
	_, err := pipeline.ExecuteImportStep(job, logger, httpClient, false)

	// Verify error
	require.Error(t, err, "Should fail with unsupported import step")
	assert.Contains(t, err.Error(), "unsupported import step", "Error should mention unsupported import step")
}

// TestExecuteImportStep_LocalImportMissingDir tests error handling when local_import directory doesn't exist
func TestExecuteImportStep_LocalImportMissingDir(t *testing.T) {
	tmpDir := t.TempDir()
	logger := lib.NewLogger(lib.LogLevelInfo)

	retryConfig := models.RetryConfig{
		MaxAttempts:      3,
		InitialBackoffMs: 500,
		MaxBackoffMs:     5000,
	}
	httpClient := services.NewHTTPClient(5*time.Second, retryConfig, models.TLSConfig{}, logger)

	// Create a job with local_import step but non-existent directory
	job := &models.PipelineJob{
		JobID:       "test-job-123",
		InputSource: "/nonexistent/path",
		InputType:   models.InputTypeLocal,
		CurrentStep: string(models.StepLocalImport),
		Status:      models.JobStatusPending,
		Steps:       models.InitializeSteps([]models.StepName{models.StepLocalImport}),
		Config: models.ProjectConfig{
			JobsDir: tmpDir,
			Pipeline: models.PipelineConfig{
				EnabledSteps: []models.StepName{models.StepLocalImport},
			},
			Retry: models.RetryConfig{
				MaxAttempts:      3,
				InitialBackoffMs: 500,
				MaxBackoffMs:     5000,
			},
		},
	}

	// Execute import step - should fail because directory doesn't exist
	updatedJob, err := pipeline.ExecuteImportStep(job, logger, httpClient, false)

	// Verify error
	require.Error(t, err, "Should fail when directory doesn't exist")
	assert.Contains(t, err.Error(), "does not exist", "Error should mention directory doesn't exist")
	assert.NotNil(t, updatedJob, "Should return updated job even on error")
}

// TestClassifyImportError_NilError tests error classification with nil error
// This covers import.go line 170 (nil error handling)
func TestClassifyImportError_NilError(t *testing.T) {
	// Note: classifyImportError is not exported, so we can't test it directly
	// However, we can test the behavior through ExecuteImportStep
	// This test documents the expected behavior when err is nil

	// The function should return ErrorTypeNonTransient for nil errors
	// This is a defensive check that shouldn't normally occur
	t.Skip("classifyImportError is not exported - covered by integration tests")
}

// TestExecuteImportStep_HttpImportValidationFailure tests error handling when HTTP import has invalid URL
// Covers import.go lines 55-59 (validation path) and download error handling
func TestExecuteImportStep_HttpImportValidationFailure(t *testing.T) {
	tmpDir := t.TempDir()
	logger := lib.NewLogger(lib.LogLevelInfo)

	retryConfig := models.RetryConfig{
		MaxAttempts:      3,
		InitialBackoffMs: 500,
		MaxBackoffMs:     5000,
	}
	httpClient := services.NewHTTPClient(5*time.Second, retryConfig, models.TLSConfig{}, logger)

	// Create a job with http_import step but invalid (non-HTTP) input source
	job := &models.PipelineJob{
		JobID:       "test-job-123",
		InputSource: "/local/path", // Not an HTTP URL
		InputType:   models.InputTypeLocal,
		CurrentStep: string(models.StepHttpImport),
		Status:      models.JobStatusPending,
		Steps:       models.InitializeSteps([]models.StepName{models.StepHttpImport}),
		Config: models.ProjectConfig{
			JobsDir: tmpDir,
			Pipeline: models.PipelineConfig{
				EnabledSteps: []models.StepName{models.StepHttpImport},
			},
			Retry: retryConfig,
		},
	}

	// Execute import step - should fail with unsupported protocol
	updatedJob, err := pipeline.ExecuteImportStep(job, logger, httpClient, false)

	require.Error(t, err, "Should fail when HTTP import has invalid URL")
	assert.NotNil(t, updatedJob, "Should return updated job even on error")
	assert.Contains(t, err.Error(), "unsupported protocol", "Error should mention unsupported protocol")
}

// TestExecuteImportStep_HttpImportEmptyURL tests error handling when HTTP import has empty URL
// Covers import.go lines 55-59 (validation failure path)
func TestExecuteImportStep_HttpImportEmptyURL(t *testing.T) {
	tmpDir := t.TempDir()
	logger := lib.NewLogger(lib.LogLevelInfo)

	retryConfig := models.RetryConfig{
		MaxAttempts:      3,
		InitialBackoffMs: 500,
		MaxBackoffMs:     5000,
	}
	httpClient := services.NewHTTPClient(5*time.Second, retryConfig, models.TLSConfig{}, logger)

	// Create a job with http_import step but empty URL
	job := &models.PipelineJob{
		JobID:       "test-job-123",
		InputSource: "", // Empty URL
		InputType:   models.InputTypeHTTP,
		CurrentStep: string(models.StepHttpImport),
		Status:      models.JobStatusPending,
		Steps:       models.InitializeSteps([]models.StepName{models.StepHttpImport}),
		Config: models.ProjectConfig{
			JobsDir: tmpDir,
			Pipeline: models.PipelineConfig{
				EnabledSteps: []models.StepName{models.StepHttpImport},
			},
			Retry: retryConfig,
		},
	}

	// Execute import step - should fail validation
	updatedJob, err := pipeline.ExecuteImportStep(job, logger, httpClient, false)

	require.Error(t, err, "Should fail when HTTP import URL is empty")
	assert.NotNil(t, updatedJob, "Should return updated job even on error")
	assert.Contains(t, err.Error(), "cannot be empty", "Error should mention empty URL")

	// Verify step was marked as failed
	importStep, found := models.GetStepByName(*updatedJob, models.StepHttpImport)
	require.True(t, found, "Import step should exist")
	assert.Equal(t, models.StepStatusFailed, importStep.Status, "Import step should be failed")
}

// TestExecuteImportStep_TorchImportCRTDLValidationFailure tests error handling when CRTDL validation fails in torch import
// Covers import.go lines 73-77
func TestExecuteImportStep_TorchImportCRTDLValidationFailure(t *testing.T) {
	tmpDir := t.TempDir()
	logger := lib.NewLogger(lib.LogLevelInfo)

	retryConfig := models.RetryConfig{
		MaxAttempts:      3,
		InitialBackoffMs: 500,
		MaxBackoffMs:     5000,
	}
	httpClient := services.NewHTTPClient(5*time.Second, retryConfig, models.TLSConfig{}, logger)

	// Create a job with torch import step but non-existent CRTDL file
	job := &models.PipelineJob{
		JobID:       "test-job-123",
		InputSource: "/nonexistent/file.crtdl", // Non-existent CRTDL file
		InputType:   models.InputTypeCRTDL,
		CurrentStep: string(models.StepTorchImport),
		Status:      models.JobStatusPending,
		Steps:       models.InitializeSteps([]models.StepName{models.StepTorchImport}),
		Config: models.ProjectConfig{
			JobsDir: tmpDir,
			Pipeline: models.PipelineConfig{
				EnabledSteps: []models.StepName{models.StepTorchImport},
			},
			Retry: retryConfig,
		},
	}

	// Execute import step - should fail validation
	updatedJob, err := pipeline.ExecuteImportStep(job, logger, httpClient, false)

	require.Error(t, err, "Should fail when CRTDL validation fails")
	assert.NotNil(t, updatedJob, "Should return updated job even on error")
	assert.Contains(t, err.Error(), "does not exist", "Error should mention file doesn't exist")
}

// TestExecuteImportStep_TorchImportTORCHURLValidationFailure tests error handling when TORCH URL validation fails
// Covers import.go lines 84-88
func TestExecuteImportStep_TorchImportTORCHURLValidationFailure(t *testing.T) {
	tmpDir := t.TempDir()
	logger := lib.NewLogger(lib.LogLevelInfo)

	retryConfig := models.RetryConfig{
		MaxAttempts:      3,
		InitialBackoffMs: 500,
		MaxBackoffMs:     5000,
	}
	httpClient := services.NewHTTPClient(5*time.Second, retryConfig, models.TLSConfig{}, logger)

	// Create a job with torch import step but invalid TORCH URL
	job := &models.PipelineJob{
		JobID:       "test-job-123",
		InputSource: "not-a-valid-url", // Invalid TORCH URL
		InputType:   models.InputTypeTORCHURL,
		CurrentStep: string(models.StepTorchImport),
		Status:      models.JobStatusPending,
		Steps:       models.InitializeSteps([]models.StepName{models.StepTorchImport}),
		Config: models.ProjectConfig{
			JobsDir: tmpDir,
			Pipeline: models.PipelineConfig{
				EnabledSteps: []models.StepName{models.StepTorchImport},
			},
			Retry: retryConfig,
		},
	}

	// Execute import step - should fail validation
	updatedJob, err := pipeline.ExecuteImportStep(job, logger, httpClient, false)

	require.Error(t, err, "Should fail when TORCH URL validation fails")
	assert.NotNil(t, updatedJob, "Should return updated job even on error")
	assert.Contains(t, err.Error(), "must start with http", "Error should mention HTTP requirement")
}

// TestExecuteImportStep_TorchImportInvalidInputType tests error handling when torch import has invalid input type
// Covers import.go lines 93-94
func TestExecuteImportStep_TorchImportInvalidInputType(t *testing.T) {
	tmpDir := t.TempDir()
	logger := lib.NewLogger(lib.LogLevelInfo)

	retryConfig := models.RetryConfig{
		MaxAttempts:      3,
		InitialBackoffMs: 500,
		MaxBackoffMs:     5000,
	}
	httpClient := services.NewHTTPClient(5*time.Second, retryConfig, models.TLSConfig{}, logger)

	// Create a job with torch import step but local input type (not CRTDL or TORCH URL)
	job := &models.PipelineJob{
		JobID:       "test-job-123",
		InputSource: "/some/local/path",
		InputType:   models.InputTypeLocal, // Invalid for torch import
		CurrentStep: string(models.StepTorchImport),
		Status:      models.JobStatusPending,
		Steps:       models.InitializeSteps([]models.StepName{models.StepTorchImport}),
		Config: models.ProjectConfig{
			JobsDir: tmpDir,
			Pipeline: models.PipelineConfig{
				EnabledSteps: []models.StepName{models.StepTorchImport},
			},
			Retry: retryConfig,
		},
	}

	// Execute import step - should fail with invalid input type
	updatedJob, err := pipeline.ExecuteImportStep(job, logger, httpClient, false)

	require.Error(t, err, "Should fail when torch import has invalid input type")
	assert.NotNil(t, updatedJob, "Should return updated job even on error")
	assert.Contains(t, err.Error(), "torch import requires CRTDL file or TORCH URL", "Error should mention requirement")
}
