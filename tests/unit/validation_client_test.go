package unit

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

func newTestHTTPClient(logger *lib.Logger) *services.HTTPClient {
	return services.NewHTTPClient(
		5*time.Second,
		models.RetryConfig{MaxAttempts: 1, InitialBackoffMs: 100, MaxBackoffMs: 1000},
		models.TLSConfig{},
		logger,
	)
}

func TestValidationClient_ValidateResource_Success_NoErrors(t *testing.T) {
	// Validator returns OperationOutcome with only information-level issues
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/validateResource", r.URL.Path)
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "application/fhir+json", r.Header.Get("Content-Type"))

		outcome := map[string]any{
			"resourceType": "OperationOutcome",
			"issue": []map[string]any{
				{
					"severity":    "information",
					"code":        "informational",
					"diagnostics": "All validation checks passed",
				},
			},
		}
		w.Header().Set("Content-Type", "application/fhir+json")
		_ = json.NewEncoder(w).Encode(outcome)
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelError)
	client := services.NewValidationClient(server.URL, newTestHTTPClient(logger), logger)

	resource := map[string]any{
		"resourceType": "Patient",
		"id":           "test-1",
	}

	result, err := client.ValidateResource(resource)

	require.NoError(t, err)
	assert.False(t, result.HasErrors())
	assert.Equal(t, "OperationOutcome", result.OperationOutcome["resourceType"])
}

func TestValidationClient_ValidateResource_WithErrors(t *testing.T) {
	// Validator returns OperationOutcome with error-level issues
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		outcome := map[string]any{
			"resourceType": "OperationOutcome",
			"issue": []map[string]any{
				{
					"severity":    "error",
					"code":        "structure",
					"diagnostics": "Missing required field 'status'",
				},
				{
					"severity":    "warning",
					"code":        "business-rule",
					"diagnostics": "Code not in preferred value set",
				},
			},
		}
		w.Header().Set("Content-Type", "application/fhir+json")
		_ = json.NewEncoder(w).Encode(outcome)
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelError)
	client := services.NewValidationClient(server.URL, newTestHTTPClient(logger), logger)

	resource := map[string]any{
		"resourceType": "Condition",
		"id":           "test-2",
	}

	result, err := client.ValidateResource(resource)

	require.NoError(t, err)
	assert.True(t, result.HasErrors())
}

func TestValidationClient_ValidateResource_WarningOnly(t *testing.T) {
	// Warnings do not constitute errors
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		outcome := map[string]any{
			"resourceType": "OperationOutcome",
			"issue": []map[string]any{
				{
					"severity":    "warning",
					"code":        "business-rule",
					"diagnostics": "Code not in preferred value set",
				},
			},
		}
		w.Header().Set("Content-Type", "application/fhir+json")
		_ = json.NewEncoder(w).Encode(outcome)
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelError)
	client := services.NewValidationClient(server.URL, newTestHTTPClient(logger), logger)

	resource := map[string]any{
		"resourceType": "Observation",
		"id":           "test-3",
	}

	result, err := client.ValidateResource(resource)

	require.NoError(t, err)
	assert.False(t, result.HasErrors(), "warnings should not be treated as errors")
}

func TestValidationClient_ValidateResource_FatalSeverity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		outcome := map[string]any{
			"resourceType": "OperationOutcome",
			"issue": []map[string]any{
				{
					"severity":    "fatal",
					"code":        "exception",
					"diagnostics": "Internal validator error",
				},
			},
		}
		w.Header().Set("Content-Type", "application/fhir+json")
		_ = json.NewEncoder(w).Encode(outcome)
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelError)
	client := services.NewValidationClient(server.URL, newTestHTTPClient(logger), logger)

	resource := map[string]any{"resourceType": "Patient", "id": "test-4"}

	result, err := client.ValidateResource(resource)

	require.NoError(t, err)
	assert.True(t, result.HasErrors(), "fatal severity should be treated as error")
}

func TestValidationClient_ValidateResource_HTTPError500_Retries(t *testing.T) {
	// 500 errors are retried by the shared HTTPClient. After all retries are exhausted,
	// the error is wrapped by HTTPClient and not a *ValidationError.
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("Internal Server Error"))
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(
		5*time.Second,
		models.RetryConfig{MaxAttempts: 3, InitialBackoffMs: 10, MaxBackoffMs: 100},
		models.TLSConfig{},
		logger,
	)
	client := services.NewValidationClient(server.URL, httpClient, logger)

	resource := map[string]any{"resourceType": "Patient", "id": "test-5"}

	_, err := client.ValidateResource(resource)

	require.Error(t, err)
	assert.Greater(t, callCount, 1, "should retry 500 errors")
	assert.Contains(t, err.Error(), "request failed")
}

func TestValidationClient_ValidateResource_HTTPError400(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("Bad Request"))
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelError)
	client := services.NewValidationClient(server.URL, newTestHTTPClient(logger), logger)

	resource := map[string]any{"resourceType": "Patient", "id": "test-6"}

	_, err := client.ValidateResource(resource)

	require.Error(t, err)
	valErr, ok := err.(*services.ValidationError)
	require.True(t, ok, "expected ValidationError")
	assert.Equal(t, 400, valErr.StatusCode)
	assert.False(t, valErr.IsRetryable())
	assert.Equal(t, models.ErrorTypeNonTransient, valErr.ErrorType)
}

func TestValidationClient_ValidateResource_ConnectionRefused(t *testing.T) {
	logger := lib.NewLogger(lib.LogLevelError)
	client := services.NewValidationClient("http://localhost:59999", newTestHTTPClient(logger), logger)

	resource := map[string]any{"resourceType": "Patient", "id": "test-7"}

	_, err := client.ValidateResource(resource)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "request failed")
}

func TestValidationClient_ValidateResource_EmptyIssues(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		outcome := map[string]any{
			"resourceType": "OperationOutcome",
			"issue":        []map[string]any{},
		}
		w.Header().Set("Content-Type", "application/fhir+json")
		_ = json.NewEncoder(w).Encode(outcome)
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelError)
	client := services.NewValidationClient(server.URL, newTestHTTPClient(logger), logger)

	resource := map[string]any{"resourceType": "Patient", "id": "test-8"}

	result, err := client.ValidateResource(resource)

	require.NoError(t, err)
	assert.False(t, result.HasErrors())
}

func TestValidationResult_HasErrors_NoIssueField(t *testing.T) {
	result := &services.ValidationResult{
		OperationOutcome: map[string]any{
			"resourceType": "OperationOutcome",
		},
	}
	assert.False(t, result.HasErrors())
}

// TestValidationResult_HasErrors_NonMapIssue verifies HasErrors skips issues that aren't maps
func TestValidationResult_HasErrors_NonMapIssue(t *testing.T) {
	result := &services.ValidationResult{
		OperationOutcome: map[string]any{
			"resourceType": "OperationOutcome",
			"issue": []any{
				"not a map", // Should be skipped
				map[string]any{"severity": "warning", "code": "informational"},
			},
		},
	}
	assert.False(t, result.HasErrors())
}

// TestValidationError_Error verifies the Error() string formatting
func TestValidationError_Error(t *testing.T) {
	t.Run("With body", func(t *testing.T) {
		err := &services.ValidationError{
			StatusCode: 422,
			Status:     "422 Unprocessable Entity",
			ErrorType:  models.ErrorTypeNonTransient,
			Body:       `{"error": "invalid resource"}`,
		}
		msg := err.Error()
		assert.Contains(t, msg, "HTTP 422")
		assert.Contains(t, msg, "422 Unprocessable Entity")
		assert.Contains(t, msg, "invalid resource")
	})

	t.Run("Without body", func(t *testing.T) {
		err := &services.ValidationError{
			StatusCode: 503,
			Status:     "503 Service Unavailable",
			ErrorType:  models.ErrorTypeTransient,
			Body:       "",
		}
		msg := err.Error()
		assert.Contains(t, msg, "HTTP 503")
		assert.NotContains(t, msg, "Response:")
	})
}

// TestValidationError_IsRetryable verifies retryability classification
func TestValidationError_IsRetryable(t *testing.T) {
	transient := &services.ValidationError{ErrorType: models.ErrorTypeTransient}
	assert.True(t, transient.IsRetryable())

	nonTransient := &services.ValidationError{ErrorType: models.ErrorTypeNonTransient}
	assert.False(t, nonTransient.IsRetryable())
}

// TestValidationClient_ValidateResource_InvalidResponseJSON verifies handling of invalid JSON response
func TestValidationClient_ValidateResource_InvalidResponseJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/fhir+json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not valid json{{{"))
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelError)
	client := services.NewValidationClient(server.URL, newTestHTTPClient(logger), logger)

	resource := map[string]any{"resourceType": "Patient", "id": "test-decode"}

	_, err := client.ValidateResource(resource)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to decode response")
}
