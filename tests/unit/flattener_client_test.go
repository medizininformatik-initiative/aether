package unit

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

// Test helpers for flattener client tests

func newTestFlatteningConfig(serverURL string) models.FlatteningConfig {
	return models.FlatteningConfig{
		ServiceURL: serverURL,
		Timeout:    30 * time.Second,
	}
}

// noRetryConfig returns a retry config that does not retry (1 attempt).
func noRetryConfig() models.RetryConfig {
	return models.RetryConfig{
		MaxAttempts:      1,
		InitialBackoffMs: 1,
		MaxBackoffMs:     1,
	}
}

// fastRetryConfig returns a retry config with fast backoff for testing.
func fastRetryConfig(maxAttempts int) models.RetryConfig {
	return models.RetryConfig{
		MaxAttempts:      maxAttempts,
		InitialBackoffMs: 1,
		MaxBackoffMs:     10,
	}
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

// newTestViewDefinitionWithColumns builds a ViewDefinition whose select clause
// contains one column per name, so the client can map NDJSON objects to rows.
func newTestViewDefinitionWithColumns(name, resource string, columns ...string) models.ViewDefinition {
	cols := make([]models.ColumnDefinition, len(columns))
	for i, col := range columns {
		cols[i] = models.ColumnDefinition{Name: col, Path: col}
	}
	viewDef := newTestViewDefinition(name, resource)
	viewDef.Select = []models.SelectClause{{Column: cols}}
	return viewDef
}

func newTestResources(ids ...string) []map[string]any {
	resources := make([]map[string]any, len(ids))
	for i, id := range ids {
		resources[i] = map[string]any{"resourceType": "Patient", "id": id}
	}
	return resources
}

func TestFlattenerClient_Flatten(t *testing.T) {
	t.Run("successful flatten returns rows in ViewDefinition column order", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/fhir/ViewDefinition/$run", r.URL.Path)
			assert.Equal(t, "ndjson", r.URL.Query().Get("_format"))
			assert.Equal(t, "POST", r.Method)
			assert.Equal(t, "application/fhir+json", r.Header.Get("Content-Type"))
			assert.Equal(t, "application/x-ndjson", r.Header.Get("Accept"))

			w.Header().Set("Content-Type", "application/x-ndjson")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"1","given":"John","family":"Doe"}` + "\n" +
				`{"family":"Smith","given":"Jane","id":"2"}` + "\n"))
		}))
		defer server.Close()

		client := services.NewFlattenerClient(newTestFlatteningConfig(server.URL), noRetryConfig(), nil, lib.DefaultLogger)
		viewDef := newTestViewDefinitionWithColumns("TestView", "Patient", "id", "given", "family")
		resources := newTestResources("1", "2")

		rows, err := client.Flatten(viewDef, resources)
		require.NoError(t, err)
		assert.Equal(t, [][]string{
			{"1", "John", "Doe"},
			{"2", "Jane", "Smith"},
		}, rows)
	})

	t.Run("hostile free text survives verbatim", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/x-ndjson")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"1","note":"He said \"hi\", then C:\\path\nnew line, lone \\ backslash, =formula"}` + "\n"))
		}))
		defer server.Close()

		client := services.NewFlattenerClient(newTestFlatteningConfig(server.URL), noRetryConfig(), nil, lib.DefaultLogger)
		viewDef := newTestViewDefinitionWithColumns("TestView", "Observation", "id", "note")

		rows, err := client.Flatten(viewDef, newTestResources("1"))
		require.NoError(t, err)
		assert.Equal(t, [][]string{
			{"1", "He said \"hi\", then C:\\path\nnew line, lone \\ backslash, =formula"},
		}, rows)
	})

	t.Run("missing column becomes empty cell, not a shift", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/x-ndjson")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"1","family":"Doe"}` + "\n"))
		}))
		defer server.Close()

		client := services.NewFlattenerClient(newTestFlatteningConfig(server.URL), noRetryConfig(), nil, lib.DefaultLogger)
		viewDef := newTestViewDefinitionWithColumns("TestView", "Patient", "id", "given", "family")

		rows, err := client.Flatten(viewDef, newTestResources("1"))
		require.NoError(t, err)
		// "given" is absent from the object: its cell stays empty and
		// "family" stays in its own column.
		assert.Equal(t, [][]string{{"1", "", "Doe"}}, rows)
	})

	t.Run("rows with different missing columns stay aligned", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/x-ndjson")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"1","family":"Doe"}` + "\n" + `{"id":"2","given":"Jane"}` + "\n"))
		}))
		defer server.Close()

		client := services.NewFlattenerClient(newTestFlatteningConfig(server.URL), noRetryConfig(), nil, lib.DefaultLogger)
		viewDef := newTestViewDefinitionWithColumns("TestView", "Patient", "id", "given", "family")

		rows, err := client.Flatten(viewDef, newTestResources("1", "2"))
		require.NoError(t, err)
		assert.Equal(t, [][]string{
			{"1", "", "Doe"},
			{"2", "Jane", ""},
		}, rows)
	})

	t.Run("non-object NDJSON line returns parse error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/x-ndjson")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("42\n"))
		}))
		defer server.Close()

		client := services.NewFlattenerClient(newTestFlatteningConfig(server.URL), noRetryConfig(), nil, lib.DefaultLogger)
		viewDef := newTestViewDefinitionWithColumns("TestView", "Patient", "id")

		_, err := client.Flatten(viewDef, newTestResources("1"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse NDJSON from flattener")
	})

	t.Run("empty body yields no rows", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/x-ndjson")
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		client := services.NewFlattenerClient(newTestFlatteningConfig(server.URL), noRetryConfig(), nil, lib.DefaultLogger)
		viewDef := newTestViewDefinitionWithColumns("TestView", "Patient", "id")

		rows, err := client.Flatten(viewDef, newTestResources("1"))
		require.NoError(t, err)
		assert.Empty(t, rows)
	})

	t.Run("renders JSON types as documented", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/x-ndjson")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"decimal":1.50,"int":2,"bool":true,"null":null,"object":{"a":1.50},"array":["x",false]}` + "\n"))
		}))
		defer server.Close()

		client := services.NewFlattenerClient(newTestFlatteningConfig(server.URL), noRetryConfig(), nil, lib.DefaultLogger)
		viewDef := newTestViewDefinitionWithColumns("TestView", "Observation",
			"decimal", "int", "bool", "null", "object", "array")

		rows, err := client.Flatten(viewDef, newTestResources("1"))
		require.NoError(t, err)
		// A number keeps its source text (1.50 stays 1.50), a boolean becomes
		// true/false, null becomes an empty cell, and nested structures
		// become compact JSON.
		assert.Equal(t, [][]string{
			{"1.50", "2", "true", "", `{"a":1.50}`, `["x",false]`},
		}, rows)
	})

	t.Run("empty resources", func(t *testing.T) {
		client := services.NewFlattenerClient(newTestFlatteningConfig("http://localhost:8080"), noRetryConfig(), nil, lib.DefaultLogger)

		result, err := client.Flatten(models.ViewDefinition{Name: "TestView"}, []map[string]any{})
		require.NoError(t, err)
		assert.Empty(t, result)
	})

	t.Run("connection refused", func(t *testing.T) {
		config := models.FlatteningConfig{
			ServiceURL: "http://localhost:59999",
			Timeout:    1 * time.Second,
		}
		client := services.NewFlattenerClient(config, noRetryConfig(), nil, lib.DefaultLogger)

		_, err := client.Flatten(newTestViewDefinition("TestView", "Patient"), newTestResources("1"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "request failed")
	})
}

func TestFlattenerClient_WithCustomTransport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"1","name":"Test"}` + "\n"))
	}))
	defer server.Close()

	// Provide a non-nil transport to exercise the transport injection branch
	transport := &http.Transport{}
	client := services.NewFlattenerClient(newTestFlatteningConfig(server.URL), noRetryConfig(), transport, lib.DefaultLogger)

	rows, err := client.Flatten(newTestViewDefinitionWithColumns("TestView", "Patient", "id", "name"), newTestResources("1"))
	require.NoError(t, err)
	assert.Equal(t, [][]string{{"1", "Test"}}, rows)
}

func TestFlattenerClient_DefaultTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{}\n"))
	}))
	defer server.Close()

	config := models.FlatteningConfig{
		ServiceURL: server.URL,
		Timeout:    0, // No timeout set - should use default
	}
	client := services.NewFlattenerClient(config, noRetryConfig(), nil, lib.DefaultLogger)

	_, err := client.Flatten(newTestViewDefinition("TestView", "Patient"), newTestResources("1"))
	require.NoError(t, err)
}

func TestFlattenerClient_HealthCheck(t *testing.T) {
	t.Run("healthy service", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/health", r.URL.Path)
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		client := services.NewFlattenerClient(newTestFlatteningConfig(server.URL), noRetryConfig(), nil, lib.DefaultLogger)
		assert.NoError(t, client.HealthCheck())
	})

	t.Run("unhealthy service", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		defer server.Close()

		client := services.NewFlattenerClient(newTestFlatteningConfig(server.URL), noRetryConfig(), nil, lib.DefaultLogger)
		err := client.HealthCheck()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "HTTP 503")
	})

	t.Run("connection refused", func(t *testing.T) {
		config := models.FlatteningConfig{
			ServiceURL: "http://localhost:59998",
			Timeout:    1 * time.Second,
		}
		client := services.NewFlattenerClient(config, noRetryConfig(), nil, lib.DefaultLogger)
		err := client.HealthCheck()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "health check failed")
	})
}

func TestFlattenerServiceError_Error(t *testing.T) {
	t.Run("basic error message", func(t *testing.T) {
		err := &services.ServiceError{
			Service:    "Flattener",
			StatusCode: 400,
			Status:     "Bad Request",
		}
		msg := err.Error()
		assert.Contains(t, msg, "HTTP 400")
		assert.Contains(t, msg, "Bad Request")
	})

	t.Run("error with body", func(t *testing.T) {
		err := &services.ServiceError{
			Service:    "Flattener",
			StatusCode: 400,
			Status:     "Bad Request",
			Body:       `{"error": "Invalid ViewDefinition"}`,
		}
		msg := err.Error()
		assert.Contains(t, msg, "Invalid ViewDefinition")
	})

	t.Run("400 error includes helpful context", func(t *testing.T) {
		err := &services.ServiceError{
			Service:    "Flattener",
			StatusCode: 400,
			Status:     "Bad Request",
		}
		msg := err.Error()
		assert.Contains(t, msg, "Possible causes")
		assert.Contains(t, msg, "Invalid ViewDefinition")
	})

	t.Run("500 error includes helpful context", func(t *testing.T) {
		err := &services.ServiceError{
			Service:    "Flattener",
			StatusCode: 500,
			Status:     "Internal Server Error",
		}
		msg := err.Error()
		assert.Contains(t, msg, "Possible causes")
	})
}

func TestFlattenerClient_InvalidURL(t *testing.T) {
	invalidConfig := models.FlatteningConfig{
		ServiceURL: "://invalid-url",
		Timeout:    1 * time.Second,
	}
	client := services.NewFlattenerClient(invalidConfig, noRetryConfig(), nil, lib.DefaultLogger)

	t.Run("flatten request", func(t *testing.T) {
		_, err := client.Flatten(newTestViewDefinition("TestView", "Patient"), newTestResources("1"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create request")
	})

	t.Run("health check", func(t *testing.T) {
		err := client.HealthCheck()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create health check request")
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

			client := services.NewFlattenerClient(newTestFlatteningConfig(server.URL), noRetryConfig(), nil, lib.DefaultLogger)
			_, err := client.Flatten(newTestViewDefinition("TestView", "Patient"), newTestResources("1"))

			require.Error(t, err)
			var flattenerErr *services.ServiceError
			require.True(t, errors.As(err, &flattenerErr), "expected *ServiceError in chain")
			assert.Equal(t, tt.statusCode, flattenerErr.StatusCode)
			assert.Equal(t, tt.isRetryable, flattenerErr.IsRetryable())
		})
	}
}

func TestFlattenerClient_Flatten_RetriesOnEOF(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt := attempts.Add(1)
		if attempt <= 2 {
			// Simulate EOF by closing connection without writing a response.
			// httptest.Server exposes the hijacker interface on the ResponseWriter.
			hj, ok := w.(http.Hijacker)
			require.True(t, ok, "httptest server must support Hijack")
			conn, _, err := hj.Hijack()
			require.NoError(t, err)
			_ = conn.Close()
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"1","name":"Alice"}` + "\n"))
	}))
	defer server.Close()

	client := services.NewFlattenerClient(
		newTestFlatteningConfig(server.URL),
		fastRetryConfig(3),
		nil, lib.DefaultLogger,
	)

	rows, err := client.Flatten(newTestViewDefinitionWithColumns("TestView", "Patient", "id", "name"), newTestResources("1"))
	require.NoError(t, err)
	assert.Equal(t, [][]string{{"1", "Alice"}}, rows)
	assert.Equal(t, int32(3), attempts.Load(), "expected 3 attempts (2 EOF + 1 success)")
}

func TestFlattenerClient_Flatten_RetriesOnTransientHTTP(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt := attempts.Add(1)
		if attempt == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("temporarily unavailable"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"1","name":"Bob"}` + "\n"))
	}))
	defer server.Close()

	client := services.NewFlattenerClient(
		newTestFlatteningConfig(server.URL),
		fastRetryConfig(3),
		nil, lib.DefaultLogger,
	)

	rows, err := client.Flatten(newTestViewDefinitionWithColumns("TestView", "Patient", "id", "name"), newTestResources("1"))
	require.NoError(t, err)
	assert.Equal(t, [][]string{{"1", "Bob"}}, rows)
	assert.Equal(t, int32(2), attempts.Load())
}

func TestFlattenerClient_Flatten_NoRetryOnNonTransient(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("bad request"))
	}))
	defer server.Close()

	client := services.NewFlattenerClient(
		newTestFlatteningConfig(server.URL),
		fastRetryConfig(3),
		nil, lib.DefaultLogger,
	)

	_, err := client.Flatten(newTestViewDefinition("TestView", "Patient"), newTestResources("1"))
	require.Error(t, err)
	assert.Equal(t, int32(1), attempts.Load(), "should not retry non-transient errors")

	var flattenerErr *services.ServiceError
	require.True(t, errors.As(err, &flattenerErr))
	assert.Equal(t, http.StatusBadRequest, flattenerErr.StatusCode)
}

func TestFlattenerClient_Flatten_ExhaustsRetries(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		// Always close connection to simulate persistent EOF
		hj, ok := w.(http.Hijacker)
		require.True(t, ok)
		conn, _, err := hj.Hijack()
		require.NoError(t, err)
		_ = conn.Close()
	}))
	defer server.Close()

	client := services.NewFlattenerClient(
		newTestFlatteningConfig(server.URL),
		fastRetryConfig(3),
		nil, lib.DefaultLogger,
	)

	_, err := client.Flatten(newTestViewDefinition("TestView", "Patient"), newTestResources("1"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "3 attempts")
	assert.Equal(t, int32(3), attempts.Load())
}

func TestFlattenerClient_HealthCheck_RetriesOnEOF(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt := attempts.Add(1)
		if attempt == 1 {
			hj, ok := w.(http.Hijacker)
			require.True(t, ok)
			conn, _, err := hj.Hijack()
			require.NoError(t, err)
			_ = conn.Close()
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := services.NewFlattenerClient(
		newTestFlatteningConfig(server.URL),
		fastRetryConfig(3),
		nil, lib.DefaultLogger,
	)

	err := client.HealthCheck()
	require.NoError(t, err)
	assert.Equal(t, int32(2), attempts.Load())
}

func TestFlattenerClient_HealthCheck_RetriesOnTransientHTTP(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt := attempts.Add(1)
		if attempt == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := services.NewFlattenerClient(
		newTestFlatteningConfig(server.URL),
		fastRetryConfig(3),
		nil, lib.DefaultLogger,
	)

	err := client.HealthCheck()
	require.NoError(t, err)
	assert.Equal(t, int32(2), attempts.Load())
}

// TestFlattenerClient_ReadResponseBodyError exercises the response-body read
// failure branch: the server promises more bytes than it sends, then closes the
// connection mid-object, so the streaming NDJSON decode fails.
func TestFlattenerClient_ReadResponseBodyError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Error("expected an http.Hijacker")
			return
		}
		conn, buf, err := hj.Hijack()
		if err != nil {
			return
		}
		_, _ = buf.WriteString("HTTP/1.1 200 OK\r\nContent-Length: 100\r\nContent-Type: application/x-ndjson\r\n\r\n{\"id\":\"1")
		_ = buf.Flush()
		_ = conn.Close()
	}))
	defer server.Close()

	client := services.NewFlattenerClient(
		models.FlatteningConfig{ServiceURL: server.URL, Timeout: 5 * time.Second},
		models.RetryConfig{MaxAttempts: 1, InitialBackoffMs: 1, MaxBackoffMs: 1},
		nil,
		lib.NewLogger(lib.LogLevelError),
	)

	_, err := client.Flatten(
		models.ViewDefinition{Name: "V", Resource: "Patient"},
		[]map[string]any{{"resourceType": "Patient"}},
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse NDJSON from flattener")
}

// A resource that encoding/json cannot marshal must fail the request before
// the client opens a connection, so the caller sees a marshal error rather
// than a transport error.
func TestFlattenerClient_Flatten_UnmarshalableResource(t *testing.T) {
	client := services.NewFlattenerClient(
		newTestFlatteningConfig("http://flattener.invalid"),
		noRetryConfig(),
		nil,
		lib.NewLogger(lib.LogLevelError),
	)

	// A channel has no JSON representation.
	resources := []map[string]any{{"resourceType": "Patient", "bad": make(chan int)}}

	rows, err := client.Flatten(newTestViewDefinition("patients", "Patient"), resources)

	require.Error(t, err)
	assert.Nil(t, rows)
	assert.Contains(t, err.Error(), "failed to marshal request")
}
