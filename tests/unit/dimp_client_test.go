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

// Unit test for DIMP client with success response

func TestDIMPClient_Pseudonymize_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var resource map[string]any
		_ = json.NewDecoder(r.Body).Decode(&resource)

		// Return pseudonymized version
		resource["id"] = "pseudonym-123"
		if names, ok := resource["name"].([]any); ok && len(names) > 0 {
			if nameMap, ok := names[0].(map[string]any); ok {
				nameMap["family"] = "REDACTED"
			}
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resource)
	}))
	defer server.Close()

	// Test implementation
	logger := lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1, InitialBackoffMs: 100, MaxBackoffMs: 1000}, logger)
	client := services.NewDIMPClient(server.URL, httpClient, logger)

	originalPatient := map[string]any{
		"resourceType": "Patient",
		"id":           "original-123",
		"name": []map[string]any{
			{"family": "Smith", "given": []string{"John"}},
		},
	}

	result, err := client.Pseudonymize(originalPatient)

	assert.NoError(t, err)
	assert.Equal(t, "Patient", result["resourceType"])
	assert.Equal(t, "pseudonym-123", result["id"])
	assert.Equal(t, "REDACTED", result["name"].([]any)[0].(map[string]any)["family"])
}

func TestDIMPClient_Pseudonymize_PreservesResourceType(t *testing.T) {
	testCases := []struct {
		name         string
		resourceType string
	}{
		{"Patient resource", "Patient"},
		{"Observation resource", "Observation"},
		{"Condition resource", "Condition"},
		{"MedicationRequest resource", "MedicationRequest"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var resource map[string]any
				_ = json.NewDecoder(r.Body).Decode(&resource)

				// Return pseudonymized version with same resourceType
				resource["id"] = "pseudo-" + resource["id"].(string)

				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(resource)
			}))
			defer server.Close()

			// Test that resourceType is preserved
			logger := lib.NewLogger(lib.LogLevelError)
			httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1, InitialBackoffMs: 100, MaxBackoffMs: 1000}, logger)
			client := services.NewDIMPClient(server.URL, httpClient, logger)

			original := map[string]any{
				"resourceType": tc.resourceType,
				"id":           "test-123",
			}

			result, err := client.Pseudonymize(original)

			assert.NoError(t, err)
			assert.Equal(t, tc.resourceType, result["resourceType"])
			assert.Equal(t, "pseudo-test-123", result["id"])
		})
	}
}

func TestDIMPClient_Pseudonymize_HandlesEmptyResource(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error": {"code": "empty_resource", "message": "Empty resource"}}`))
	}))
	defer server.Close()

	// Test error handling for empty resource
	logger := lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1, InitialBackoffMs: 100, MaxBackoffMs: 1000}, logger)
	client := services.NewDIMPClient(server.URL, httpClient, logger)

	emptyResource := map[string]any{}

	_, err := client.Pseudonymize(emptyResource)
	assert.Error(t, err)
}
