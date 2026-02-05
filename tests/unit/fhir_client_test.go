package unit

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFHIRClient_UploadNDJSON_Success(t *testing.T) {
	var receivedBundles []map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "application/fhir+json", r.Header.Get("Content-Type"))

		var bundle map[string]any
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &bundle)
		receivedBundles = append(receivedBundles, bundle)

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1}, logger)
	config := models.SendConfig{
		SendAs:    models.SendModeDirectResourceLoad,
		URL:       server.URL,
		BatchSize: 100,
	}
	client := services.NewFHIRClient(config, httpClient, logger)

	ndjsonContent := `{"resourceType":"Patient","id":"1"}
{"resourceType":"Patient","id":"2"}
`
	reader := strings.NewReader(ndjsonContent)

	stats, err := client.UploadNDJSON("test.ndjson", reader)
	require.NoError(t, err)
	assert.Equal(t, 2, stats.ResourcesUploaded)
	assert.Equal(t, 1, stats.BatchesSent)

	// Verify bundle was sent with correct structure
	require.Len(t, receivedBundles, 1)
	bundle := receivedBundles[0]
	assert.Equal(t, "Bundle", bundle["resourceType"])
	assert.Equal(t, "transaction", bundle["type"])

	entries := bundle["entry"].([]any)
	assert.Len(t, entries, 2)
}

func TestFHIRClient_UploadNDJSON_DefaultBatchSize(t *testing.T) {
	// Test that batch size defaults to 100 when not set
	bundleCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bundleCount++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1}, logger)
	config := models.SendConfig{
		SendAs:    models.SendModeDirectResourceLoad,
		URL:       server.URL,
		BatchSize: 0, // Not set - should default to 100
	}
	client := services.NewFHIRClient(config, httpClient, logger)

	// Create 150 resources - should be split into 2 batches (100 + 50)
	var lines []string
	for i := 0; i < 150; i++ {
		lines = append(lines, `{"resourceType":"Patient","id":"`+string(rune('A'+i%26))+`"}`)
	}
	reader := strings.NewReader(strings.Join(lines, "\n"))

	stats, err := client.UploadNDJSON("test.ndjson", reader)
	require.NoError(t, err)
	assert.Equal(t, 150, stats.ResourcesUploaded)
	assert.Equal(t, 2, stats.BatchesSent)
	assert.Equal(t, 2, bundleCount)
}

func TestFHIRClient_UploadNDJSON_EmptyLines(t *testing.T) {
	var receivedEntries int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var bundle map[string]any
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &bundle)

		if entries, ok := bundle["entry"].([]any); ok {
			receivedEntries += len(entries)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1}, logger)
	config := models.SendConfig{
		SendAs:    models.SendModeDirectResourceLoad,
		URL:       server.URL,
		BatchSize: 100,
	}
	client := services.NewFHIRClient(config, httpClient, logger)

	// NDJSON with empty lines and whitespace-only lines
	ndjsonContent := `{"resourceType":"Patient","id":"1"}

{"resourceType":"Patient","id":"2"}

{"resourceType":"Patient","id":"3"}
`
	reader := strings.NewReader(ndjsonContent)

	stats, err := client.UploadNDJSON("test.ndjson", reader)
	require.NoError(t, err)
	// Only 3 resources should be processed (empty lines ignored)
	assert.Equal(t, 3, stats.ResourcesUploaded)
	assert.Equal(t, 3, receivedEntries)
}

func TestFHIRClient_UploadNDJSON_BatchSize(t *testing.T) {
	batchSizes := []int{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var bundle map[string]any
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &bundle)

		if entries, ok := bundle["entry"].([]any); ok {
			batchSizes = append(batchSizes, len(entries))
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1}, logger)
	config := models.SendConfig{
		SendAs:    models.SendModeDirectResourceLoad,
		URL:       server.URL,
		BatchSize: 2, // Small batch size for testing
	}
	client := services.NewFHIRClient(config, httpClient, logger)

	// 5 resources with batch size 2 = 3 batches (2, 2, 1)
	ndjsonContent := `{"resourceType":"Patient","id":"1"}
{"resourceType":"Patient","id":"2"}
{"resourceType":"Patient","id":"3"}
{"resourceType":"Patient","id":"4"}
{"resourceType":"Patient","id":"5"}
`
	reader := strings.NewReader(ndjsonContent)

	stats, err := client.UploadNDJSON("test.ndjson", reader)
	require.NoError(t, err)
	assert.Equal(t, 5, stats.ResourcesUploaded)
	assert.Equal(t, 3, stats.BatchesSent)
	assert.Equal(t, []int{2, 2, 1}, batchSizes)
}

func TestFHIRClient_UploadNDJSON_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"resourceType":"OperationOutcome"}`))
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1}, logger)
	config := models.SendConfig{
		SendAs:    models.SendModeDirectResourceLoad,
		URL:       server.URL,
		BatchSize: 100,
	}
	client := services.NewFHIRClient(config, httpClient, logger)

	reader := strings.NewReader(`{"resourceType":"Patient","id":"1"}`)

	_, err := client.UploadNDJSON("test.ndjson", reader)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to send")
}

func TestFHIRClient_UploadNDJSON_ScannerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1}, logger)
	config := models.SendConfig{
		SendAs:    models.SendModeDirectResourceLoad,
		URL:       server.URL,
		BatchSize: 100,
	}
	client := services.NewFHIRClient(config, httpClient, logger)

	// Create a reader that will cause a scanner error (reading from closed pipe)
	reader := &errorReader{err: io.ErrUnexpectedEOF}

	_, err := client.UploadNDJSON("test.ndjson", reader)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "error reading")
}

func TestFHIRClient_UploadNDJSON_PartialBatchError(t *testing.T) {
	requestCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if requestCount == 2 {
			// Fail on second batch
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"bad request"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1}, logger)
	config := models.SendConfig{
		SendAs:    models.SendModeDirectResourceLoad,
		URL:       server.URL,
		BatchSize: 2,
	}
	client := services.NewFHIRClient(config, httpClient, logger)

	// 4 resources with batch size 2 = 2 batches
	ndjsonContent := `{"resourceType":"Patient","id":"1"}
{"resourceType":"Patient","id":"2"}
{"resourceType":"Patient","id":"3"}
{"resourceType":"Patient","id":"4"}
`
	reader := strings.NewReader(ndjsonContent)

	stats, err := client.UploadNDJSON("test.ndjson", reader)
	require.Error(t, err)
	// First batch succeeded (2 resources), second batch failed
	assert.Equal(t, 2, stats.ResourcesUploaded)
	assert.Equal(t, 1, stats.BatchesSent)
}

func TestFHIRClient_UploadNDJSON_BasicAuth(t *testing.T) {
	var receivedAuthHeader string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuthHeader = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1}, logger)
	config := models.SendConfig{
		SendAs:    models.SendModeDirectResourceLoad,
		URL:       server.URL,
		BatchSize: 100,
		Auth: models.AuthConfig{
			Username: "testuser",
			Password: "testpass",
		},
	}
	client := services.NewFHIRClient(config, httpClient, logger)

	reader := strings.NewReader(`{"resourceType":"Patient","id":"1"}`)

	_, err := client.UploadNDJSON("test.ndjson", reader)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(receivedAuthHeader, "Basic "))
}

func TestFHIRClient_UploadNDJSON_NoAuth(t *testing.T) {
	var receivedAuthHeader string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuthHeader = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1}, logger)
	config := models.SendConfig{
		SendAs:    models.SendModeDirectResourceLoad,
		URL:       server.URL,
		BatchSize: 100,
		// No username/password
	}
	client := services.NewFHIRClient(config, httpClient, logger)

	reader := strings.NewReader(`{"resourceType":"Patient","id":"1"}`)

	_, err := client.UploadNDJSON("test.ndjson", reader)
	require.NoError(t, err)
	assert.Empty(t, receivedAuthHeader)
}

func TestFHIRClient_CreateTransactionBundle_ResourceWithID(t *testing.T) {
	var receivedBundle map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedBundle)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1}, logger)
	config := models.SendConfig{
		SendAs:    models.SendModeDirectResourceLoad,
		URL:       server.URL,
		BatchSize: 100,
	}
	client := services.NewFHIRClient(config, httpClient, logger)

	// Resource with ID should use PUT
	reader := strings.NewReader(`{"resourceType":"Patient","id":"patient-123"}`)

	_, err := client.UploadNDJSON("test.ndjson", reader)
	require.NoError(t, err)

	entries := receivedBundle["entry"].([]any)
	require.Len(t, entries, 1)

	entry := entries[0].(map[string]any)
	request := entry["request"].(map[string]any)
	assert.Equal(t, "PUT", request["method"])
	assert.Equal(t, "Patient/patient-123", request["url"])
}

func TestFHIRClient_CreateTransactionBundle_ResourceWithoutID(t *testing.T) {
	var receivedBundle map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedBundle)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1}, logger)
	config := models.SendConfig{
		SendAs:    models.SendModeDirectResourceLoad,
		URL:       server.URL,
		BatchSize: 100,
	}
	client := services.NewFHIRClient(config, httpClient, logger)

	// Resource without ID should use POST
	reader := strings.NewReader(`{"resourceType":"Patient","name":[{"family":"Smith"}]}`)

	_, err := client.UploadNDJSON("test.ndjson", reader)
	require.NoError(t, err)

	entries := receivedBundle["entry"].([]any)
	require.Len(t, entries, 1)

	entry := entries[0].(map[string]any)
	request := entry["request"].(map[string]any)
	assert.Equal(t, "POST", request["method"])
	assert.Equal(t, "Patient", request["url"])
}

func TestFHIRClient_CreateTransactionBundle_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1}, logger)
	config := models.SendConfig{
		SendAs:    models.SendModeDirectResourceLoad,
		URL:       server.URL,
		BatchSize: 100,
	}
	client := services.NewFHIRClient(config, httpClient, logger)

	// Invalid JSON in the NDJSON line - JSON marshaling of raw message will fail
	reader := strings.NewReader(`{invalid json}`)

	_, err := client.UploadNDJSON("test.ndjson", reader)
	// The invalid JSON will cause marshal error
	require.Error(t, err)
	assert.Contains(t, err.Error(), "marshal")
}

func TestFHIRClient_CreateTransactionBundle_EmptyID(t *testing.T) {
	var receivedBundle map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedBundle)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1}, logger)
	config := models.SendConfig{
		SendAs:    models.SendModeDirectResourceLoad,
		URL:       server.URL,
		BatchSize: 100,
	}
	client := services.NewFHIRClient(config, httpClient, logger)

	// Resource with empty ID should use POST
	reader := strings.NewReader(`{"resourceType":"Patient","id":""}`)

	_, err := client.UploadNDJSON("test.ndjson", reader)
	require.NoError(t, err)

	entries := receivedBundle["entry"].([]any)
	require.Len(t, entries, 1)

	entry := entries[0].(map[string]any)
	request := entry["request"].(map[string]any)
	assert.Equal(t, "POST", request["method"])
	assert.Equal(t, "Patient", request["url"])
}

func TestFHIRError_Error(t *testing.T) {
	err := &services.FHIRError{
		StatusCode: 400,
		Message:    "Invalid resource",
		ErrorType:  models.ErrorTypeNonTransient,
	}

	assert.Equal(t, "FHIR server error: HTTP 400: Invalid resource", err.Error())
}

func TestFHIRError_IsRetryable(t *testing.T) {
	tests := []struct {
		name      string
		errorType models.ErrorType
		retryable bool
	}{
		{
			name:      "transient error is retryable",
			errorType: models.ErrorTypeTransient,
			retryable: true,
		},
		{
			name:      "non-transient error is not retryable",
			errorType: models.ErrorTypeNonTransient,
			retryable: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := &services.FHIRError{
				StatusCode: 500,
				Message:    "Server error",
				ErrorType:  tt.errorType,
			}
			assert.Equal(t, tt.retryable, err.IsRetryable())
		})
	}
}

func TestFHIRClient_UploadNDJSON_URLTrailingSlash(t *testing.T) {
	var receivedPath string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1}, logger)

	// URL with trailing slash
	config := models.SendConfig{
		SendAs:    models.SendModeDirectResourceLoad,
		URL:       server.URL + "/",
		BatchSize: 100,
	}
	client := services.NewFHIRClient(config, httpClient, logger)

	reader := strings.NewReader(`{"resourceType":"Patient","id":"1"}`)

	_, err := client.UploadNDJSON("test.ndjson", reader)
	require.NoError(t, err)
	// Trailing slash should be removed
	assert.Equal(t, "/", receivedPath)
}

func TestFHIRClient_UploadNDJSON_MixedResources(t *testing.T) {
	var receivedBundle map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedBundle)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1}, logger)
	config := models.SendConfig{
		SendAs:    models.SendModeDirectResourceLoad,
		URL:       server.URL,
		BatchSize: 100,
	}
	client := services.NewFHIRClient(config, httpClient, logger)

	// Mix of resources with and without IDs
	ndjsonContent := `{"resourceType":"Patient","id":"p1"}
{"resourceType":"Observation"}
{"resourceType":"Condition","id":"c1"}
`
	reader := strings.NewReader(ndjsonContent)

	stats, err := client.UploadNDJSON("test.ndjson", reader)
	require.NoError(t, err)
	assert.Equal(t, 3, stats.ResourcesUploaded)

	entries := receivedBundle["entry"].([]any)
	require.Len(t, entries, 3)

	// First: Patient with ID -> PUT
	entry1 := entries[0].(map[string]any)
	req1 := entry1["request"].(map[string]any)
	assert.Equal(t, "PUT", req1["method"])
	assert.Equal(t, "Patient/p1", req1["url"])

	// Second: Observation without ID -> POST
	entry2 := entries[1].(map[string]any)
	req2 := entry2["request"].(map[string]any)
	assert.Equal(t, "POST", req2["method"])
	assert.Equal(t, "Observation", req2["url"])

	// Third: Condition with ID -> PUT
	entry3 := entries[2].(map[string]any)
	req3 := entry3["request"].(map[string]any)
	assert.Equal(t, "PUT", req3["method"])
	assert.Equal(t, "Condition/c1", req3["url"])
}

func TestFHIRClient_UploadNDJSON_4xxError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad request"}`))
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1}, logger)
	config := models.SendConfig{
		SendAs:    models.SendModeDirectResourceLoad,
		URL:       server.URL,
		BatchSize: 100,
	}
	client := services.NewFHIRClient(config, httpClient, logger)

	reader := strings.NewReader(`{"resourceType":"Patient","id":"1"}`)

	_, err := client.UploadNDJSON("test.ndjson", reader)
	require.Error(t, err)

	// Check it's a FHIR error with correct type
	var fhirErr *services.FHIRError
	require.ErrorAs(t, err, &fhirErr)
	assert.Equal(t, 400, fhirErr.StatusCode)
	assert.Equal(t, models.ErrorTypeNonTransient, fhirErr.ErrorType)
}

func TestFHIRClient_UploadNDJSON_5xxError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"service unavailable"}`))
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1}, logger)
	config := models.SendConfig{
		SendAs:    models.SendModeDirectResourceLoad,
		URL:       server.URL,
		BatchSize: 100,
	}
	client := services.NewFHIRClient(config, httpClient, logger)

	reader := strings.NewReader(`{"resourceType":"Patient","id":"1"}`)

	_, err := client.UploadNDJSON("test.ndjson", reader)
	require.Error(t, err)
	// The error is wrapped by HTTP client retry logic
	assert.Contains(t, err.Error(), "503")
}

func TestFHIRClient_UploadNDJSON_LargeResource(t *testing.T) {
	var receivedSize int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		receivedSize = len(body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1}, logger)
	config := models.SendConfig{
		SendAs:    models.SendModeDirectResourceLoad,
		URL:       server.URL,
		BatchSize: 100,
	}
	client := services.NewFHIRClient(config, httpClient, logger)

	// Create a resource with a large text field (>64KB default scanner buffer)
	largeText := strings.Repeat("x", 100000)
	resource := `{"resourceType":"DocumentReference","id":"1","text":"` + largeText + `"}`
	reader := strings.NewReader(resource)

	stats, err := client.UploadNDJSON("test.ndjson", reader)
	require.NoError(t, err)
	assert.Equal(t, 1, stats.ResourcesUploaded)
	assert.Greater(t, receivedSize, 100000)
}

// errorReader is a reader that returns an error after reading some data
type errorReader struct {
	err error
}

func (r *errorReader) Read(p []byte) (n int, err error) {
	// Return some valid data first, then error
	if len(p) > 0 {
		copy(p, []byte(`{"resourceType":"Patient","id":"1"}`))
		n = 36
	}
	return n, r.err
}

// TestFHIRUploadStats verifies the struct fields
func TestFHIRUploadStats(t *testing.T) {
	stats := &services.FHIRUploadStats{
		ResourcesUploaded: 100,
		BatchesSent:       5,
	}

	assert.Equal(t, 100, stats.ResourcesUploaded)
	assert.Equal(t, 5, stats.BatchesSent)
}

func TestNewFHIRClient(t *testing.T) {
	logger := lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1}, logger)
	config := models.SendConfig{
		SendAs:    models.SendModeDirectResourceLoad,
		URL:       "http://localhost:8080",
		BatchSize: 50,
	}

	client := services.NewFHIRClient(config, httpClient, logger)
	assert.NotNil(t, client)
}

func TestFHIRClient_UploadNDJSON_EmptyFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Server should not be called for empty file")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1}, logger)
	config := models.SendConfig{
		SendAs:    models.SendModeDirectResourceLoad,
		URL:       server.URL,
		BatchSize: 100,
	}
	client := services.NewFHIRClient(config, httpClient, logger)

	// Empty file
	reader := strings.NewReader("")

	stats, err := client.UploadNDJSON("test.ndjson", reader)
	require.NoError(t, err)
	assert.Equal(t, 0, stats.ResourcesUploaded)
	assert.Equal(t, 0, stats.BatchesSent)
}

func TestFHIRClient_UploadNDJSON_OnlyEmptyLines(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Server should not be called for file with only empty lines")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1}, logger)
	config := models.SendConfig{
		SendAs:    models.SendModeDirectResourceLoad,
		URL:       server.URL,
		BatchSize: 100,
	}
	client := services.NewFHIRClient(config, httpClient, logger)

	// File with only empty/whitespace lines
	reader := strings.NewReader("\n\n   \n\t\n")

	stats, err := client.UploadNDJSON("test.ndjson", reader)
	require.NoError(t, err)
	assert.Equal(t, 0, stats.ResourcesUploaded)
	assert.Equal(t, 0, stats.BatchesSent)
}

func TestFHIRClient_UploadNDJSON_FinalBatchOnly(t *testing.T) {
	var receivedBundles []map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var bundle map[string]any
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &bundle)
		receivedBundles = append(receivedBundles, bundle)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1}, logger)
	config := models.SendConfig{
		SendAs:    models.SendModeDirectResourceLoad,
		URL:       server.URL,
		BatchSize: 100, // Larger than number of resources
	}
	client := services.NewFHIRClient(config, httpClient, logger)

	// Only 3 resources - should all go in final batch
	ndjsonContent := `{"resourceType":"Patient","id":"1"}
{"resourceType":"Patient","id":"2"}
{"resourceType":"Patient","id":"3"}
`
	reader := strings.NewReader(ndjsonContent)

	stats, err := client.UploadNDJSON("test.ndjson", reader)
	require.NoError(t, err)
	assert.Equal(t, 3, stats.ResourcesUploaded)
	assert.Equal(t, 1, stats.BatchesSent)
	require.Len(t, receivedBundles, 1)

	entries := receivedBundles[0]["entry"].([]any)
	assert.Len(t, entries, 3)
}

func TestFHIRClient_UploadNDJSON_FinalBatchError(t *testing.T) {
	requestCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"server error"}`))
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1}, logger)
	config := models.SendConfig{
		SendAs:    models.SendModeDirectResourceLoad,
		URL:       server.URL,
		BatchSize: 100, // All resources in final batch
	}
	client := services.NewFHIRClient(config, httpClient, logger)

	reader := strings.NewReader(`{"resourceType":"Patient","id":"1"}`)

	stats, err := client.UploadNDJSON("test.ndjson", reader)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to send final batch")
	assert.Equal(t, 0, stats.ResourcesUploaded)
	assert.Equal(t, 0, stats.BatchesSent)
}

func TestFHIRClient_UploadNDJSON_BytesReader(t *testing.T) {
	var receivedBundle map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedBundle)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1}, logger)
	config := models.SendConfig{
		SendAs:    models.SendModeDirectResourceLoad,
		URL:       server.URL,
		BatchSize: 100,
	}
	client := services.NewFHIRClient(config, httpClient, logger)

	// Use bytes.Reader instead of strings.Reader
	data := []byte(`{"resourceType":"Patient","id":"1"}`)
	reader := bytes.NewReader(data)

	stats, err := client.UploadNDJSON("test.ndjson", reader)
	require.NoError(t, err)
	assert.Equal(t, 1, stats.ResourcesUploaded)
}

func TestNewFHIRClientWithParams(t *testing.T) {
	logger := lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1}, logger)
	auth := models.AuthConfig{
		Username: "user",
		Password: "pass",
	}

	// Test with valid batch size
	client := services.NewFHIRClientWithParams("http://localhost:8080", 50, auth, httpClient, logger)
	assert.NotNil(t, client)

	// Test with zero batch size (should default to 100)
	clientDefault := services.NewFHIRClientWithParams("http://localhost:8080", 0, auth, httpClient, logger)
	assert.NotNil(t, clientDefault)

	// Test with negative batch size (should default to 100)
	clientNegative := services.NewFHIRClientWithParams("http://localhost:8080", -1, auth, httpClient, logger)
	assert.NotNil(t, clientNegative)
}

func TestFHIRClient_UploadNDJSON_OAuth2Auth(t *testing.T) {
	// Clear the OAuth2 cache before the test
	services.ClearOAuth2TokenCache()

	// Create a mock OAuth2 server
	oauthServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"access_token":"test-token","token_type":"Bearer","expires_in":3600}`))
	}))
	defer oauthServer.Close()

	var receivedAuthHeader string
	fhirServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuthHeader = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer fhirServer.Close()

	logger := lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1}, logger)
	config := models.SendConfig{
		SendAs:    models.SendModeDirectResourceLoad,
		URL:       fhirServer.URL,
		BatchSize: 100,
		Auth: models.AuthConfig{
			OAuthIssuerURI:    oauthServer.URL,
			OAuthClientID:     "test-client",
			OAuthClientSecret: "test-secret",
		},
	}
	client := services.NewFHIRClient(config, httpClient, logger)

	reader := strings.NewReader(`{"resourceType":"Patient","id":"1"}`)

	_, err := client.UploadNDJSON("test.ndjson", reader)
	require.NoError(t, err)
	assert.Equal(t, "Bearer test-token", receivedAuthHeader)
}

func TestFHIRClient_UploadNDJSON_OAuth2AuthError(t *testing.T) {
	// Clear the OAuth2 cache before the test
	services.ClearOAuth2TokenCache()

	// Create a mock OAuth2 server that returns an error
	oauthServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid_client"}`))
	}))
	defer oauthServer.Close()

	fhirServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("FHIR server should not be called when OAuth2 fails")
		w.WriteHeader(http.StatusOK)
	}))
	defer fhirServer.Close()

	logger := lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1}, logger)
	config := models.SendConfig{
		SendAs:    models.SendModeDirectResourceLoad,
		URL:       fhirServer.URL,
		BatchSize: 100,
		Auth: models.AuthConfig{
			OAuthIssuerURI:    oauthServer.URL,
			OAuthClientID:     "test-client",
			OAuthClientSecret: "test-secret",
		},
	}
	client := services.NewFHIRClient(config, httpClient, logger)

	reader := strings.NewReader(`{"resourceType":"Patient","id":"1"}`)

	_, err := client.UploadNDJSON("test.ndjson", reader)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "OAuth2")
}

func TestFHIRClient_UploadNDJSON_UnwrapCollectionBundle(t *testing.T) {
	var receivedBundle map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedBundle)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1}, logger)
	config := models.SendConfig{
		SendAs:    models.SendModeDirectResourceLoad,
		URL:       server.URL,
		BatchSize: 100,
	}
	client := services.NewFHIRClient(config, httpClient, logger)

	// A collection Bundle should be unwrapped, extracting its entries
	collectionBundle := `{"resourceType":"Bundle","type":"collection","entry":[{"resource":{"resourceType":"Patient","id":"p1"}},{"resource":{"resourceType":"Observation","id":"o1"}}]}`
	reader := strings.NewReader(collectionBundle)

	stats, err := client.UploadNDJSON("test.ndjson", reader)
	require.NoError(t, err)
	assert.Equal(t, 1, stats.ResourcesUploaded) // Bundle counts as 1 resource read

	// But the transaction bundle should contain the extracted resources
	entries := receivedBundle["entry"].([]any)
	assert.Len(t, entries, 2) // Two entries from the collection bundle

	// Verify both resources were extracted
	entry1 := entries[0].(map[string]any)
	resource1 := entry1["resource"].(map[string]any)
	assert.Equal(t, "Patient", resource1["resourceType"])

	entry2 := entries[1].(map[string]any)
	resource2 := entry2["resource"].(map[string]any)
	assert.Equal(t, "Observation", resource2["resourceType"])
}

func TestFHIRClient_UploadNDJSON_UnwrapSearchsetBundle(t *testing.T) {
	var receivedBundle map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedBundle)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1}, logger)
	config := models.SendConfig{
		SendAs:    models.SendModeDirectResourceLoad,
		URL:       server.URL,
		BatchSize: 100,
	}
	client := services.NewFHIRClient(config, httpClient, logger)

	// A searchset Bundle should also be unwrapped
	searchsetBundle := `{"resourceType":"Bundle","type":"searchset","entry":[{"resource":{"resourceType":"Condition","id":"c1"}}]}`
	reader := strings.NewReader(searchsetBundle)

	stats, err := client.UploadNDJSON("test.ndjson", reader)
	require.NoError(t, err)
	assert.Equal(t, 1, stats.ResourcesUploaded)

	entries := receivedBundle["entry"].([]any)
	assert.Len(t, entries, 1)

	entry := entries[0].(map[string]any)
	resource := entry["resource"].(map[string]any)
	assert.Equal(t, "Condition", resource["resourceType"])
	assert.Equal(t, "c1", resource["id"])
}

func TestFHIRClient_UploadNDJSON_UnwrapTransactionBundle(t *testing.T) {
	var receivedBundle map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedBundle)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1}, logger)
	config := models.SendConfig{
		SendAs:    models.SendModeDirectResourceLoad,
		URL:       server.URL,
		BatchSize: 100,
	}
	client := services.NewFHIRClient(config, httpClient, logger)

	// A transaction Bundle (like TORCH outputs) should be unwrapped
	transactionBundle := `{"resourceType":"Bundle","type":"transaction","entry":[{"resource":{"resourceType":"Patient","id":"p1"}},{"resource":{"resourceType":"Condition","id":"c1"}}]}`
	reader := strings.NewReader(transactionBundle)

	stats, err := client.UploadNDJSON("test.ndjson", reader)
	require.NoError(t, err)
	assert.Equal(t, 1, stats.ResourcesUploaded) // Bundle counts as 1 resource read

	// The transaction bundle should contain the extracted resources
	entries := receivedBundle["entry"].([]any)
	assert.Len(t, entries, 2) // Two entries from the transaction bundle

	// Verify both resources were extracted
	entry1 := entries[0].(map[string]any)
	resource1 := entry1["resource"].(map[string]any)
	assert.Equal(t, "Patient", resource1["resourceType"])

	entry2 := entries[1].(map[string]any)
	resource2 := entry2["resource"].(map[string]any)
	assert.Equal(t, "Condition", resource2["resourceType"])
}

func TestFHIRClient_UploadNDJSON_DocumentBundleNotUnwrapped(t *testing.T) {
	var receivedBundle map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedBundle)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1}, logger)
	config := models.SendConfig{
		SendAs:    models.SendModeDirectResourceLoad,
		URL:       server.URL,
		BatchSize: 100,
	}
	client := services.NewFHIRClient(config, httpClient, logger)

	// A document Bundle should NOT be unwrapped - it's treated as a single resource
	documentBundle := `{"resourceType":"Bundle","type":"document","id":"doc1","entry":[{"resource":{"resourceType":"Composition"}}]}`
	reader := strings.NewReader(documentBundle)

	stats, err := client.UploadNDJSON("test.ndjson", reader)
	require.NoError(t, err)
	assert.Equal(t, 1, stats.ResourcesUploaded)

	entries := receivedBundle["entry"].([]any)
	assert.Len(t, entries, 1) // Document bundle is kept as one entry

	entry := entries[0].(map[string]any)
	resource := entry["resource"].(map[string]any)
	assert.Equal(t, "Bundle", resource["resourceType"])
	assert.Equal(t, "document", resource["type"])
}

func TestFHIRClient_UploadNDJSON_CollectionBundleWithEmptyEntries(t *testing.T) {
	var receivedBundle map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedBundle)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1}, logger)
	config := models.SendConfig{
		SendAs:    models.SendModeDirectResourceLoad,
		URL:       server.URL,
		BatchSize: 100,
	}
	client := services.NewFHIRClient(config, httpClient, logger)

	// A collection Bundle with no entries - should result in empty transaction
	emptyCollectionBundle := `{"resourceType":"Bundle","type":"collection","entry":[]}`
	// Also include a regular resource to ensure we get a request
	ndjsonContent := emptyCollectionBundle + "\n" + `{"resourceType":"Patient","id":"p1"}`
	reader := strings.NewReader(ndjsonContent)

	stats, err := client.UploadNDJSON("test.ndjson", reader)
	require.NoError(t, err)
	assert.Equal(t, 2, stats.ResourcesUploaded)

	entries := receivedBundle["entry"].([]any)
	// Only the Patient resource should be in the transaction (empty bundle has no entries to extract)
	assert.Len(t, entries, 1)
}
