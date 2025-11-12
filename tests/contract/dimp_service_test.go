package contract

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

// Contract test for DIMP service interaction per contracts/dimp-service.md

func TestDIMPService_Pseudonymize_Success(t *testing.T) {
	// Mock HTTP server that behaves like DIMP service
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/$de-identify", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		// Read original resource
		var resource map[string]any
		err := json.NewDecoder(r.Body).Decode(&resource)
		assert.NoError(t, err)

		// Return pseudonymized version
		pseudonymized := map[string]any{
			"resourceType": resource["resourceType"],
			"id":           "pseudonym-abc123xyz",
		}

		if resource["resourceType"] == "Patient" {
			pseudonymized["identifier"] = []map[string]string{
				{"system": "http://hospital.org/patients", "value": "PSEUDO_98765"},
			}
			pseudonymized["name"] = []map[string]any{
				{"family": "REDACTED", "given": []string{"REDACTED"}},
			}
			pseudonymized["birthDate"] = "1980"
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(pseudonymized)
	}))
	defer server.Close()

	// Test the contract
	logger := lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1, InitialBackoffMs: 100, MaxBackoffMs: 1000}, logger)
	client := services.NewDIMPClient(server.URL, httpClient, logger)

	originalPatient := map[string]any{
		"resourceType": "Patient",
		"id":           "example-patient-123",
		"identifier": []map[string]string{
			{"system": "http://hospital.org/patients", "value": "12345"},
		},
		"name": []map[string]any{
			{"family": "Doe", "given": []string{"John"}},
		},
		"birthDate": "1980-01-01",
	}

	result, err := client.Pseudonymize(originalPatient)
	assert.NoError(t, err)
	assert.Equal(t, "pseudonym-abc123xyz", result["id"])
	assert.Equal(t, "REDACTED", result["name"].([]any)[0].(map[string]any)["family"])
}

func TestDIMPService_400BadRequest(t *testing.T) {
	// Mock DIMP service returning 400 for malformed resource
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

	// Test will verify non-retryable error
	logger := lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1, InitialBackoffMs: 100, MaxBackoffMs: 1000}, logger)
	client := services.NewDIMPClient(server.URL, httpClient, logger)

	malformedResource := map[string]any{
		"id": "no-resource-type",
	}

	_, err := client.Pseudonymize(malformedResource)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "400")
	assert.Contains(t, err.Error(), "invalid_resource")
}

func TestDIMPService_500InternalServerError(t *testing.T) {
	// Mock DIMP service returning 500 (transient error)
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]string{
				"code":    "internal_error",
				"message": "Database connection failed",
			},
		})
	}))
	defer server.Close()

	// Test will verify retryable error behavior
	logger := lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 3, InitialBackoffMs: 10, MaxBackoffMs: 100}, logger)
	client := services.NewDIMPClient(server.URL, httpClient, logger)

	resource := map[string]any{
		"resourceType": "Patient",
		"id":           "test",
	}

	_, err := client.Pseudonymize(resource)
	assert.Error(t, err)
	assert.Greater(t, callCount, 1, "Should retry on 500 error")
}

func TestDIMPService_502BadGateway(t *testing.T) {
	// Mock DIMP service returning 502 (VFPS unavailable)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("VFPS service unavailable"))
	}))
	defer server.Close()

	// Test will verify this is treated as transient/retryable
	logger := lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 3, InitialBackoffMs: 10, MaxBackoffMs: 100}, logger)
	client := services.NewDIMPClient(server.URL, httpClient, logger)

	resource := map[string]any{
		"resourceType": "Patient",
		"id":           "test",
	}

	_, err := client.Pseudonymize(resource)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "502")
}

func TestDIMPService_422UnprocessableEntity(t *testing.T) {
	// Mock DIMP service returning 422 for invalid FHIR schema
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]string{
				"code":    "invalid_schema",
				"message": "Resource does not conform to FHIR schema",
			},
		})
	}))
	defer server.Close()

	// Test will verify non-retryable error
	logger := lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1, InitialBackoffMs: 100, MaxBackoffMs: 1000}, logger)
	client := services.NewDIMPClient(server.URL, httpClient, logger)

	resource := map[string]any{
		"resourceType": "Patient",
		"invalidField": "bad",
	}

	_, err := client.Pseudonymize(resource)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "422")
	assert.Contains(t, err.Error(), "invalid_schema")
}
