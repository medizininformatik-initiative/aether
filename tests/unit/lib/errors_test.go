package lib

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
)

func TestAetherError_Error(t *testing.T) {
	err := &lib.AetherError{
		Category:    lib.CategoryNetwork,
		Message:     "Connection failed",
		Cause:       errors.New("dial tcp: connection refused"),
		HTTPStatus:  0,
		IsRetryable: true,
	}

	result := err.Error()
	assert.Contains(t, result, "[NETWORK]")
	assert.Contains(t, result, "Connection failed")
	assert.Contains(t, result, "connection refused")
}

func TestAetherError_ErrorWithHTTPStatus(t *testing.T) {
	err := &lib.AetherError{
		Category:   lib.CategoryService,
		Message:    "Service unavailable",
		HTTPStatus: 503,
	}

	result := err.Error()
	assert.Contains(t, result, "[SERVICE]")
	assert.Contains(t, result, "Service unavailable")
	assert.Contains(t, result, "(HTTP 503)")
}

func TestAetherError_UserMessage(t *testing.T) {
	err := &lib.AetherError{
		Category: lib.CategoryFileSystem,
		Message:  "Cannot access file",
		Cause:    errors.New("permission denied"),
		Guidance: []string{
			"Check file permissions",
			"Run with appropriate access rights",
		},
		IsRetryable: false,
	}

	msg := err.UserMessage()
	assert.Contains(t, msg, "❌ Error:")
	assert.Contains(t, msg, "Cannot access file")
	assert.Contains(t, msg, "💡 How to fix:")
	assert.Contains(t, msg, "1. Check file permissions")
	assert.Contains(t, msg, "2. Run with appropriate access rights")
	assert.Contains(t, msg, "Technical details: permission denied")
	assert.NotContains(t, msg, "🔄 This error is transient") // Not retryable
}

func TestAetherError_UserMessage_Retryable(t *testing.T) {
	err := &lib.AetherError{
		Category:    lib.CategoryNetwork,
		Message:     "Network timeout",
		IsRetryable: true,
	}

	msg := err.UserMessage()
	assert.Contains(t, msg, "🔄 This error is transient")
}

func TestErrNetworkUnreachable(t *testing.T) {
	url := "http://localhost:8083/fhir"
	cause := errors.New("dial tcp: no route to host")

	err := lib.ErrNetworkUnreachable(url, cause)

	assert.Equal(t, lib.CategoryNetwork, err.Category)
	assert.Contains(t, err.Message, url)
	assert.Equal(t, cause, err.Unwrap())
	assert.True(t, err.IsRetryable)
	assert.GreaterOrEqual(t, len(err.Guidance), 3)

	// Check guidance is actionable
	guidance := strings.Join(err.Guidance, " ")
	assert.Contains(t, guidance, "service is running")
	assert.Contains(t, guidance, url)
}

func TestErrFileNotFound(t *testing.T) {
	path := "/nonexistent/file.json"
	err := lib.ErrFileNotFound(path)

	assert.Equal(t, lib.CategoryFileSystem, err.Category)
	assert.Contains(t, err.Message, path)
	assert.False(t, err.IsRetryable)
	assert.Contains(t, err.Guidance[0], "path is correct")
}

func TestErrDiskFull(t *testing.T) {
	path := "/jobs"
	cause := errors.New("no space left on device")

	err := lib.ErrDiskFull(path, cause)

	assert.Equal(t, lib.CategoryFileSystem, err.Category)
	assert.False(t, err.IsRetryable)

	guidance := strings.Join(err.Guidance, " ")
	assert.Contains(t, guidance, "disk space")
	assert.Contains(t, guidance, path)
	assert.Contains(t, guidance, "--jobs-dir")
}

func TestErrInvalidFHIRFile(t *testing.T) {
	filename := "patient.ndjson"
	line := 42
	cause := errors.New("invalid JSON")

	err := lib.ErrInvalidFHIRFile(filename, line, cause)

	assert.Equal(t, lib.CategoryValidation, err.Category)
	assert.Contains(t, err.Message, filename)
	assert.Equal(t, cause, err.Unwrap())
	assert.False(t, err.IsRetryable)

	guidance := strings.Join(err.Guidance, " ")
	assert.Contains(t, guidance, "line 42")
	assert.Contains(t, guidance, "NDJSON")
}

func TestErrServiceUnavailable(t *testing.T) {
	serviceName := "DIMP"
	statusCode := 503
	cause := errors.New("service overloaded")

	err := lib.ErrServiceUnavailable(serviceName, statusCode, cause)

	assert.Equal(t, lib.CategoryService, err.Category)
	assert.Contains(t, err.Message, serviceName)
	assert.Equal(t, statusCode, err.HTTPStatus)
	assert.True(t, err.IsRetryable)

	guidance := strings.Join(err.Guidance, " ")
	assert.Contains(t, guidance, "automatic retry")
	assert.Contains(t, guidance, "service logs")
}

func TestErrServiceBadRequest(t *testing.T) {
	serviceName := "DIMP"
	statusCode := 400
	message := "Missing required field: resourceType"

	err := lib.ErrServiceBadRequest(serviceName, statusCode, message)

	assert.Equal(t, lib.CategoryService, err.Category)
	assert.Contains(t, err.Message, serviceName)
	assert.Contains(t, err.Message, message)
	assert.Equal(t, statusCode, err.HTTPStatus)
	assert.False(t, err.IsRetryable)

	guidance := strings.Join(err.Guidance, " ")
	assert.Contains(t, guidance, "automatic retry will not help")
}

func TestErrMissingServiceURL(t *testing.T) {
	stepName := models.StepDIMP

	err := lib.ErrMissingServiceURL(stepName)

	assert.Equal(t, lib.CategoryConfiguration, err.Category)
	assert.Contains(t, err.Message, string(stepName))
	assert.False(t, err.IsRetryable)

	guidance := strings.Join(err.Guidance, " ")
	assert.Contains(t, guidance, "aether.yaml")
	assert.Contains(t, guidance, "enabled_steps")
}

func TestErrJobNotFound(t *testing.T) {
	jobID := "nonexistent-job-123"

	err := lib.ErrJobNotFound(jobID)

	assert.Equal(t, lib.CategoryState, err.Category)
	assert.Contains(t, err.Message, jobID)
	assert.False(t, err.IsRetryable)

	guidance := strings.Join(err.Guidance, " ")
	assert.Contains(t, guidance, "aether job list")
}

func TestErrStepPrerequisiteNotMet(t *testing.T) {
	stepName := models.StepDIMP
	prerequisite := models.StepLocalImport

	err := lib.ErrStepPrerequisiteNotMet(stepName, prerequisite)

	assert.Equal(t, lib.CategoryValidation, err.Category)
	assert.Contains(t, err.Message, string(stepName))
	assert.Contains(t, err.Message, string(prerequisite))
	assert.False(t, err.IsRetryable)

	guidance := strings.Join(err.Guidance, " ")
	assert.Contains(t, guidance, "pipeline status")
	assert.Contains(t, guidance, string(prerequisite))
}

func TestErrJobLocked(t *testing.T) {
	jobID := "locked-job-456"

	err := lib.ErrJobLocked(jobID)

	assert.Equal(t, lib.CategoryState, err.Category)
	assert.Contains(t, err.Message, jobID)
	assert.True(t, err.IsRetryable) // Can retry after lock is released

	guidance := strings.Join(err.Guidance, " ")
	assert.Contains(t, guidance, "aether process")
	assert.Contains(t, guidance, ".lock")
}

func TestClassifyError_AlreadyAetherError(t *testing.T) {
	original := lib.ErrJobNotFound("test-job")
	result := lib.ClassifyError(original)

	assert.Equal(t, original, result) // Should return same instance
}

func TestClassifyError_NetworkError(t *testing.T) {
	networkErr := errors.New("dial tcp: connection refused")

	result := lib.ClassifyError(networkErr)

	assert.Equal(t, lib.CategoryNetwork, result.Category)
	assert.True(t, result.IsRetryable)
	assert.Contains(t, result.Guidance[0], "network")
}

func TestClassifyError_DiskFull(t *testing.T) {
	diskErr := errors.New("write failed: no space left on device")

	result := lib.ClassifyError(diskErr)

	assert.Equal(t, lib.CategoryFileSystem, result.Category)
	assert.False(t, result.IsRetryable)

	guidance := strings.Join(result.Guidance, " ")
	assert.Contains(t, guidance, "disk space")
}

func TestClassifyError_PermissionDenied(t *testing.T) {
	permErr := errors.New("open /root/file: permission denied")

	result := lib.ClassifyError(permErr)

	assert.Equal(t, lib.CategoryFileSystem, result.Category)
	assert.False(t, result.IsRetryable)

	guidance := strings.Join(result.Guidance, " ")
	assert.Contains(t, guidance, "permission")
}

func TestClassifyError_Generic(t *testing.T) {
	genericErr := errors.New("something unexpected happened")

	result := lib.ClassifyError(genericErr)

	assert.NotNil(t, result)
	assert.Equal(t, genericErr, result.Unwrap())
	assert.False(t, result.IsRetryable)
}

func TestClassifyError_Nil(t *testing.T) {
	result := lib.ClassifyError(nil)
	assert.Nil(t, result)
}

func TestWrapError(t *testing.T) {
	category := lib.CategoryValidation
	message := "Invalid input"
	cause := errors.New("unexpected character at position 42")
	guidance := []string{"Check input format", "See documentation"}

	err := lib.WrapError(category, message, cause, guidance...)

	assert.Equal(t, category, err.Category)
	assert.Equal(t, message, err.Message)
	assert.Equal(t, cause, err.Unwrap())
	assert.Equal(t, guidance, err.Guidance)
}

func TestWrapError_NetworkCause(t *testing.T) {
	// Network errors should be marked retryable
	networkCause := errors.New("connection timeout")

	err := lib.WrapError(lib.CategoryService, "Service failed", networkCause)

	assert.True(t, err.IsRetryable) // Should detect network error in cause
}

// Integration test: Full error flow
func TestErrorFlow_UserExperience(t *testing.T) {
	// Simulate DIMP service 503 error
	err := lib.ErrServiceUnavailable("DIMP", 503, fmt.Errorf("upstream service down"))

	// Verify error message is user-friendly
	userMsg := err.UserMessage()
	require.Contains(t, userMsg, "❌ Error:")
	require.Contains(t, userMsg, "DIMP")
	require.Contains(t, userMsg, "💡 How to fix:")
	require.Contains(t, userMsg, "🔄 This error is transient")

	// Verify error can be logged/printed
	logMsg := err.Error()
	require.Contains(t, logMsg, "[SERVICE]")
	require.Contains(t, logMsg, "(HTTP 503)")

	// Verify retry logic can inspect it
	assert.True(t, err.IsRetryable)
	assert.Equal(t, 503, err.HTTPStatus)

	// Verify can unwrap for errors.Is/As
	cause := err.Unwrap()
	assert.NotNil(t, cause)
	assert.Contains(t, cause.Error(), "upstream")
}
