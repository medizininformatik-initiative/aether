package unit

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

// Unit test for DIMP client with error responses (4xx, 5xx)

func TestDIMPClient_Error_400BadRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]string{
				"code":    "invalid_resource",
				"message": "Missing required field: resourceType",
			},
		})
	}))
	defer server.Close()

	// Test 400 error handling
	logger := lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1, InitialBackoffMs: 100, MaxBackoffMs: 1000}, logger)
	client := services.NewDIMPClient(server.URL, httpClient, logger)

	malformed := map[string]any{
		"id": "no-type",
	}

	_, err := client.Pseudonymize(malformed)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "400")
}

func TestDIMPClient_Error_422UnprocessableEntity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]string{
				"code":    "invalid_schema",
				"message": "Does not conform to FHIR schema",
			},
		})
	}))
	defer server.Close()

	// Test 422 error handling
	logger := lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1, InitialBackoffMs: 100, MaxBackoffMs: 1000}, logger)
	client := services.NewDIMPClient(server.URL, httpClient, logger)

	invalid := map[string]any{
		"resourceType": "Patient",
		"invalidField": "bad",
	}

	_, err := client.Pseudonymize(invalid)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "422")
}

func TestDIMPClient_Error_500InternalServerError(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error": {"code": "internal_error", "message": "Database error"}}`))
	}))
	defer server.Close()

	// Test 500 error is retryable
	logger := lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 3, InitialBackoffMs: 10, MaxBackoffMs: 100}, logger)
	client := services.NewDIMPClient(server.URL, httpClient, logger)

	resource := map[string]any{
		"resourceType": "Patient",
		"id":           "test",
	}

	_, err := client.Pseudonymize(resource)

	assert.Error(t, err)
	assert.Greater(t, callCount, 1, "Should retry 500 errors")
}

func TestDIMPClient_Error_502BadGateway(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("VFPS unavailable"))
	}))
	defer server.Close()

	// Test 502 is retryable (upstream dependency down)
	logger := lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 3, InitialBackoffMs: 10, MaxBackoffMs: 100}, logger)
	client := services.NewDIMPClient(server.URL, httpClient, logger)

	resource := map[string]any{
		"resourceType": "Patient",
		"id":           "test",
	}

	_, err := client.Pseudonymize(resource)

	assert.Error(t, err)
	assert.Greater(t, callCount, 1, "Should retry 502 errors")
}

func TestDIMPClient_Error_503ServiceUnavailable(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("Service overloaded"))
	}))
	defer server.Close()

	// Test 503 is retryable
	logger := lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 3, InitialBackoffMs: 10, MaxBackoffMs: 100}, logger)
	client := services.NewDIMPClient(server.URL, httpClient, logger)

	resource := map[string]any{
		"resourceType": "Patient",
		"id":           "test",
	}

	_, err := client.Pseudonymize(resource)

	assert.Error(t, err)
	assert.Greater(t, callCount, 1, "Should retry 503 errors")
}

func TestDIMPClient_Error_504GatewayTimeout(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusGatewayTimeout)
		_, _ = w.Write([]byte("Request timeout"))
	}))
	defer server.Close()

	// Test 504 is retryable
	logger := lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 3, InitialBackoffMs: 10, MaxBackoffMs: 100}, logger)
	client := services.NewDIMPClient(server.URL, httpClient, logger)

	resource := map[string]any{
		"resourceType": "Patient",
		"id":           "test",
	}

	_, err := client.Pseudonymize(resource)

	assert.Error(t, err)
	assert.Greater(t, callCount, 1, "Should retry 504 errors")
}

func TestDIMPClient_Error_NetworkFailure(t *testing.T) {
	// Test network connectivity error
	logger := lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(1*time.Second, models.RetryConfig{MaxAttempts: 3, InitialBackoffMs: 10, MaxBackoffMs: 100}, logger)
	client := services.NewDIMPClient("http://192.0.2.1:9999", httpClient, logger) // Non-routable IP

	resource := map[string]any{
		"resourceType": "Patient",
		"id":           "test",
	}

	_, err := client.Pseudonymize(resource)

	assert.Error(t, err)
}

func TestDIMPClient_Error_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("invalid json{"))
	}))
	defer server.Close()

	// Test handling of invalid JSON response
	logger := lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1, InitialBackoffMs: 100, MaxBackoffMs: 1000}, logger)
	client := services.NewDIMPClient(server.URL, httpClient, logger)

	resource := map[string]any{
		"resourceType": "Patient",
		"id":           "test",
	}

	_, err := client.Pseudonymize(resource)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "decode")
}
