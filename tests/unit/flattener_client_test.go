package unit

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test helpers for flattener client tests

// newTestFlattenerHTTPClient creates an HTTPClient with no retries for fast unit tests
func newTestFlattenerHTTPClient() *services.HTTPClient {
	return services.NewHTTPClient(
		30*time.Second,
		models.RetryConfig{MaxAttempts: 1},
		lib.DefaultLogger,
	)
}

func newTestFlattenerHTTPClientWithTimeout(timeout time.Duration) *services.HTTPClient {
	return services.NewHTTPClient(
		timeout,
		models.RetryConfig{MaxAttempts: 1},
		lib.DefaultLogger,
	)
}

func newTestViewDefinition(name, resource string) models.ViewDefinition {
	return models.ViewDefinition{
		ResourceType: "https://sql-on-fhir.org/ig/StructureDefinition/ViewDefinition",
		Name:         name,
		Status:       "draft",
		Resource:     resource,
		Select:       []models.SelectClause{},
	}
}

func newTestResources(ids ...string) []map[string]any {
	resources := make([]map[string]any, len(ids))
	for i, id := range ids {
		resources[i] = map[string]any{"resourceType": "Patient", "id": id}
	}
	return resources
}

func TestFlattenerClient_Flatten(t *testing.T) {
	t.Run("successful flatten", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/fhir/ViewDefinition/$run", r.URL.Path)
			assert.Equal(t, "POST", r.Method)
			assert.Equal(t, "application/fhir+json", r.Header.Get("Content-Type"))
			assert.Equal(t, "text/csv", r.Header.Get("Accept"))

			w.Header().Set("Content-Type", "text/csv")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("1,John,Doe\n2,Jane,Smith\n"))
		}))
		defer server.Close()

		client := services.NewFlattenerClient(server.URL, newTestFlattenerHTTPClient(), lib.DefaultLogger)
		viewDef := newTestViewDefinition("TestView", "Patient")
		resources := newTestResources("1", "2")

		result, err := client.Flatten(viewDef, resources)
		require.NoError(t, err)
		assert.Equal(t, "1,John,Doe\n2,Jane,Smith\n", result)
	})

	t.Run("empty resources", func(t *testing.T) {
		client := services.NewFlattenerClient("http://localhost:8080", newTestFlattenerHTTPClient(), lib.DefaultLogger)

		result, err := client.Flatten(models.ViewDefinition{Name: "TestView"}, []map[string]any{})
		require.NoError(t, err)
		assert.Empty(t, result)
	})

	t.Run("connection refused", func(t *testing.T) {
		httpClient := newTestFlattenerHTTPClientWithTimeout(1 * time.Second)
		client := services.NewFlattenerClient("http://localhost:59999", httpClient, lib.DefaultLogger)

		_, err := client.Flatten(newTestViewDefinition("TestView", "Patient"), newTestResources("1"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "request failed")
	})
}

func TestFlattenerClient_HealthCheck(t *testing.T) {
	t.Run("healthy service", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/health", r.URL.Path)
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		client := services.NewFlattenerClient(server.URL, newTestFlattenerHTTPClient(), lib.DefaultLogger)
		assert.NoError(t, client.HealthCheck())
	})

	t.Run("unhealthy service", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		defer server.Close()

		client := services.NewFlattenerClient(server.URL, newTestFlattenerHTTPClient(), lib.DefaultLogger)
		err := client.HealthCheck()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "HTTP 503")
	})

	t.Run("connection refused", func(t *testing.T) {
		httpClient := newTestFlattenerHTTPClientWithTimeout(1 * time.Second)
		client := services.NewFlattenerClient("http://localhost:59998", httpClient, lib.DefaultLogger)
		err := client.HealthCheck()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "health check failed")
	})
}

func TestFlattenerError_Error(t *testing.T) {
	t.Run("basic error message", func(t *testing.T) {
		err := &services.FlattenerError{
			StatusCode: 400,
			Status:     "Bad Request",
		}
		msg := err.Error()
		assert.Contains(t, msg, "HTTP 400")
		assert.Contains(t, msg, "Bad Request")
	})

	t.Run("error with body", func(t *testing.T) {
		err := &services.FlattenerError{
			StatusCode: 400,
			Status:     "Bad Request",
			Body:       `{"error": "Invalid ViewDefinition"}`,
		}
		msg := err.Error()
		assert.Contains(t, msg, "Invalid ViewDefinition")
	})

	t.Run("400 error includes helpful context", func(t *testing.T) {
		err := &services.FlattenerError{
			StatusCode: 400,
			Status:     "Bad Request",
		}
		msg := err.Error()
		assert.Contains(t, msg, "Possible causes")
		assert.Contains(t, msg, "Invalid ViewDefinition")
	})

	t.Run("500 error includes helpful context", func(t *testing.T) {
		err := &services.FlattenerError{
			StatusCode: 500,
			Status:     "Internal Server Error",
		}
		msg := err.Error()
		assert.Contains(t, msg, "Possible causes")
	})
}

func TestFlattenerError_IsRetryable(t *testing.T) {
	tests := []struct {
		errorType models.ErrorType
		expected  bool
	}{
		{models.ErrorTypeTransient, true},
		{models.ErrorTypeNonTransient, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.errorType), func(t *testing.T) {
			err := &services.FlattenerError{
				ErrorType: tt.errorType,
			}
			assert.Equal(t, tt.expected, err.IsRetryable())
		})
	}
}

func TestFlattenerClient_InvalidURL(t *testing.T) {
	client := services.NewFlattenerClient("://invalid-url", newTestFlattenerHTTPClient(), lib.DefaultLogger)

	t.Run("flatten request", func(t *testing.T) {
		_, err := client.Flatten(newTestViewDefinition("TestView", "Patient"), newTestResources("1"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create request")
	})

	t.Run("health check", func(t *testing.T) {
		err := client.HealthCheck()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "health check failed")
	})
}

func TestFlattenerClient_HTTPErrors(t *testing.T) {
	tests := []struct {
		name        string
		statusCode  int
		isRetryable bool
	}{
		{"400 Bad Request", http.StatusBadRequest, false},
		{"404 Not Found", http.StatusNotFound, false},
		{"500 Internal Server Error", http.StatusInternalServerError, true},
		{"503 Service Unavailable", http.StatusServiceUnavailable, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(`{"error": "test error"}`))
			}))
			defer server.Close()

			client := services.NewFlattenerClient(server.URL, newTestFlattenerHTTPClient(), lib.DefaultLogger)
			_, err := client.Flatten(newTestViewDefinition("TestView", "Patient"), newTestResources("1"))

			if !tt.isRetryable {
				// Non-transient errors (4xx): HTTPClient returns response, FlattenerClient returns FlattenerError
				require.Error(t, err)
				flattenerErr, ok := err.(*services.FlattenerError)
				require.True(t, ok, "expected FlattenerError")
				assert.Equal(t, tt.statusCode, flattenerErr.StatusCode)
				assert.Equal(t, tt.isRetryable, flattenerErr.IsRetryable())
			} else {
				// Transient errors (5xx): HTTPClient retries and exhausts attempts,
				// returns a generic error (not FlattenerError) since body is consumed during retry
				require.Error(t, err)
				assert.Contains(t, err.Error(), "request failed")
			}
		})
	}
}
