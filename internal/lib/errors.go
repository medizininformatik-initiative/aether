package lib

import (
	"fmt"
	"strings"

	"github.com/medizininformatik-initiative/aether/internal/models"
)

// AetherError represents a user-friendly error with context and guidance
type AetherError struct {
	Category    ErrorCategory
	Message     string   // Short description of what went wrong
	Cause       error    // Underlying error
	Guidance    []string // What the user can do to fix it
	HTTPStatus  int      // HTTP status code if applicable
	IsRetryable bool     // Can this error be automatically retried?
}

// ErrorCategory classifies errors for better UX
type ErrorCategory string

const (
	CategoryNetwork       ErrorCategory = "network"
	CategoryFileSystem    ErrorCategory = "filesystem"
	CategoryValidation    ErrorCategory = "validation"
	CategoryService       ErrorCategory = "service"
	CategoryConfiguration ErrorCategory = "configuration"
	CategoryState         ErrorCategory = "state"
)

// Error implements the error interface
func (e *AetherError) Error() string {
	var sb strings.Builder

	// Category prefix for clarity
	sb.WriteString(fmt.Sprintf("[%s] ", strings.ToUpper(string(e.Category))))
	sb.WriteString(e.Message)

	if e.Cause != nil {
		sb.WriteString(fmt.Sprintf(": %v", e.Cause))
	}

	if e.HTTPStatus > 0 {
		sb.WriteString(fmt.Sprintf(" (HTTP %d)", e.HTTPStatus))
	}

	return sb.String()
}

// UserMessage returns a formatted message suitable for displaying to end users
func (e *AetherError) UserMessage() string {
	var sb strings.Builder

	sb.WriteString("❌ Error: ")
	sb.WriteString(e.Message)
	sb.WriteString("\n\n")

	if len(e.Guidance) > 0 {
		sb.WriteString("💡 How to fix:\n")
		for i, guide := range e.Guidance {
			sb.WriteString(fmt.Sprintf("  %d. %s\n", i+1, guide))
		}
	}

	if e.Cause != nil {
		sb.WriteString(fmt.Sprintf("\nTechnical details: %v\n", e.Cause))
	}

	if e.IsRetryable {
		sb.WriteString("\n🔄 This error is transient and will be automatically retried.\n")
	}

	return sb.String()
}

// Unwrap returns the underlying cause for errors.Is/As compatibility
func (e *AetherError) Unwrap() error {
	return e.Cause
}

// Network Errors

// ErrNetworkUnreachable creates an error for network connectivity issues
func ErrNetworkUnreachable(url string, cause error) *AetherError {
	return &AetherError{
		Category: CategoryNetwork,
		Message:  fmt.Sprintf("Cannot reach service at %s", url),
		Cause:    cause,
		Guidance: []string{
			"Check that the service is running",
			fmt.Sprintf("Verify the URL is correct: %s", url),
			"Check your network connection",
			"Ensure no firewall is blocking the connection",
		},
		IsRetryable: true,
	}
}

// ErrNetworkTimeout creates an error for request timeouts
func ErrNetworkTimeout(url string, cause error) *AetherError {
	return &AetherError{
		Category: CategoryNetwork,
		Message:  fmt.Sprintf("Request to %s timed out", url),
		Cause:    cause,
		Guidance: []string{
			"The service may be overloaded or slow to respond",
			"Wait a moment and try again",
			"Check service health and performance",
			"Consider increasing timeout in configuration if large datasets",
		},
		IsRetryable: true,
	}
}

// Filesystem Errors

// ErrFileNotFound creates an error for missing files or directories
func ErrFileNotFound(path string) *AetherError {
	return &AetherError{
		Category: CategoryFileSystem,
		Message:  fmt.Sprintf("File or directory not found: %s", path),
		Guidance: []string{
			"Check that the path is correct",
			"Ensure the file/directory exists",
			"Verify you have permission to access it",
		},
		IsRetryable: false,
	}
}

// ErrFilePermissionDenied creates an error for permission issues
func ErrFilePermissionDenied(path string, cause error) *AetherError {
	return &AetherError{
		Category: CategoryFileSystem,
		Message:  fmt.Sprintf("Permission denied accessing: %s", path),
		Cause:    cause,
		Guidance: []string{
			"Check file/directory permissions",
			"Ensure your user has read/write access",
			"Try running with appropriate permissions",
		},
		IsRetryable: false,
	}
}

// ErrDiskFull creates an error for out of disk space
func ErrDiskFull(path string, cause error) *AetherError {
	return &AetherError{
		Category: CategoryFileSystem,
		Message:  "No space left on device",
		Cause:    cause,
		Guidance: []string{
			"Free up disk space",
			fmt.Sprintf("Clean old jobs from %s", path),
			"Use --jobs-dir flag to specify a different location with more space",
			"Consider deleting unnecessary files",
		},
		IsRetryable: false,
	}
}

// ErrInvalidFHIRFile creates an error for malformed FHIR data
func ErrInvalidFHIRFile(filename string, line int, cause error) *AetherError {
	guidance := []string{
		fmt.Sprintf("Check FHIR file format in %s", filename),
		"Ensure the file contains valid NDJSON (newline-delimited JSON)",
		"Verify each line is valid FHIR resource JSON",
	}

	if line > 0 {
		guidance = append(guidance, fmt.Sprintf("Error occurred at line %d", line))
	}

	return &AetherError{
		Category:    CategoryValidation,
		Message:     fmt.Sprintf("Invalid FHIR data in %s", filename),
		Cause:       cause,
		Guidance:    guidance,
		IsRetryable: false,
	}
}

// Service Errors

// ErrServiceUnavailable creates an error for 5xx service errors
func ErrServiceUnavailable(serviceName string, statusCode int, cause error) *AetherError {
	return &AetherError{
		Category:   CategoryService,
		Message:    fmt.Sprintf("%s service is temporarily unavailable", serviceName),
		Cause:      cause,
		HTTPStatus: statusCode,
		Guidance: []string{
			"The service may be experiencing issues",
			"Wait a moment - automatic retry is in progress",
			fmt.Sprintf("Check %s service logs for errors", serviceName),
			"Verify the service is running and healthy",
		},
		IsRetryable: true,
	}
}

// ErrServiceBadRequest creates an error for 4xx client errors
func ErrServiceBadRequest(serviceName string, statusCode int, message string) *AetherError {
	return &AetherError{
		Category:   CategoryService,
		Message:    fmt.Sprintf("%s rejected the request: %s", serviceName, message),
		HTTPStatus: statusCode,
		Guidance: []string{
			"The data sent to the service was invalid or malformed",
			"Check FHIR resource structure and content",
			"Review service documentation for required formats",
			"This error requires manual investigation - automatic retry will not help",
		},
		IsRetryable: false,
	}
}

// Configuration Errors

// ErrMissingServiceURL creates an error for missing service configuration
func ErrMissingServiceURL(stepName models.StepName) *AetherError {
	return &AetherError{
		Category: CategoryConfiguration,
		Message:  fmt.Sprintf("%s step is enabled but service URL is not configured", stepName),
		Guidance: []string{
			"Add the service URL to your aether.yaml config file",
			fmt.Sprintf("Or disable the %s step in pipeline.enabled_steps", stepName),
			"See config/aether.example.yaml for reference",
		},
		IsRetryable: false,
	}
}

// ErrInvalidConfig creates an error for configuration validation failures
func ErrInvalidConfig(field string, reason string) *AetherError {
	return &AetherError{
		Category: CategoryConfiguration,
		Message:  fmt.Sprintf("Invalid configuration: %s", reason),
		Guidance: []string{
			fmt.Sprintf("Check the '%s' field in your config file", field),
			"Compare with config/aether.example.yaml for correct format",
			"Ensure all required fields are populated",
		},
		IsRetryable: false,
	}
}

// State Errors

// ErrJobNotFound creates an error for missing job state
func ErrJobNotFound(jobID string) *AetherError {
	return &AetherError{
		Category: CategoryState,
		Message:  fmt.Sprintf("Job '%s' not found", jobID),
		Guidance: []string{
			"Check the job ID is correct",
			"Use 'aether job list' to see all available jobs",
			"The job may have been deleted",
		},
		IsRetryable: false,
	}
}

// ErrCorruptedJobState creates an error for invalid job state files
func ErrCorruptedJobState(jobID string, cause error) *AetherError {
	return &AetherError{
		Category: CategoryState,
		Message:  fmt.Sprintf("Job state file for '%s' is corrupted", jobID),
		Cause:    cause,
		Guidance: []string{
			"The job state file may have been manually edited or corrupted",
			"Check jobs/<job-id>/state.json for syntax errors",
			"You may need to delete this job and restart",
			"Consider restoring from backup if available",
		},
		IsRetryable: false,
	}
}

// ErrStepPrerequisiteNotMet creates an error for missing step prerequisites
func ErrStepPrerequisiteNotMet(stepName models.StepName, prerequisite models.StepName) *AetherError {
	return &AetherError{
		Category: CategoryValidation,
		Message:  fmt.Sprintf("Cannot run %s: prerequisite step %s has not completed", stepName, prerequisite),
		Guidance: []string{
			fmt.Sprintf("Ensure %s step completes successfully first", prerequisite),
			"Use 'aether pipeline status <job-id>' to check step progress",
			fmt.Sprintf("Run 'aether pipeline continue <job-id>' to resume from %s", prerequisite),
		},
		IsRetryable: false,
	}
}

// ErrJobLocked creates an error when job is locked by another process
func ErrJobLocked(jobID string) *AetherError {
	return &AetherError{
		Category: CategoryState,
		Message:  fmt.Sprintf("Job '%s' is currently being modified by another process", jobID),
		Guidance: []string{
			"Wait for the other operation to complete",
			"Check if another aether process is running for this job",
			"If stuck, remove the lock file: jobs/<job-id>/.lock",
		},
		IsRetryable: true, // May succeed if we retry after lock is released
	}
}

// Helper Functions

// WrapError wraps a standard error with AetherError context
func WrapError(category ErrorCategory, message string, cause error, guidance ...string) *AetherError {
	isRetryable := IsNetworkError(cause)

	return &AetherError{
		Category:    category,
		Message:     message,
		Cause:       cause,
		Guidance:    guidance,
		IsRetryable: isRetryable,
	}
}

// ClassifyError examines an error and returns appropriate user guidance
func ClassifyError(err error) *AetherError {
	if err == nil {
		return nil
	}

	// Already an AetherError
	if aetherErr, ok := err.(*AetherError); ok {
		return aetherErr
	}

	errMsg := err.Error()

	// Network errors
	if IsNetworkError(err) {
		return &AetherError{
			Category:    CategoryNetwork,
			Message:     "Network connectivity issue",
			Cause:       err,
			Guidance:    []string{"Check network connection", "Verify service is running", "Will retry automatically"},
			IsRetryable: true,
		}
	}

	// Disk space errors
	if containsIgnoreCase(errMsg, "no space left") || containsIgnoreCase(errMsg, "disk full") {
		return &AetherError{
			Category:    CategoryFileSystem,
			Message:     "Insufficient disk space",
			Cause:       err,
			Guidance:    []string{"Free up disk space", "Clean old jobs", "Use --jobs-dir to specify different location"},
			IsRetryable: false,
		}
	}

	// Permission errors
	if containsIgnoreCase(errMsg, "permission denied") || containsIgnoreCase(errMsg, "access denied") {
		return &AetherError{
			Category:    CategoryFileSystem,
			Message:     "Permission denied",
			Cause:       err,
			Guidance:    []string{"Check file/directory permissions", "Ensure proper access rights"},
			IsRetryable: false,
		}
	}

	// Generic fallback
	return &AetherError{
		Category:    CategoryValidation,
		Message:     "An error occurred",
		Cause:       err,
		Guidance:    []string{"Check the technical details below", "See logs for more information"},
		IsRetryable: false,
	}
}
