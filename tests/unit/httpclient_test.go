package unit

import (
	"io"
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

// TestHTTPClient_Put_Success tests the Put method with a successful response
func TestHTTPClient_Put_Success(t *testing.T) {
	var receivedMethod string
	var receivedContentType string
	var receivedBody []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		receivedContentType = r.Header.Get("Content-Type")
		receivedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelDebug)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1, InitialBackoffMs: 100, MaxBackoffMs: 1000}, logger)

	body := []byte(`{"resourceType":"Binary","id":"123"}`)
	resp, err := httpClient.Put(server.URL, "application/fhir+json", body)

	require.NoError(t, err)
	require.NotNil(t, resp)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, "PUT", receivedMethod)
	assert.Equal(t, "application/fhir+json", receivedContentType)
	assert.Equal(t, body, receivedBody)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// TestHTTPClient_Put_ClientError tests the Put method with a 4xx client error (non-retryable)
func TestHTTPClient_Put_ClientError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad request"}`))
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelDebug)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1, InitialBackoffMs: 100, MaxBackoffMs: 1000}, logger)

	body := []byte(`{"data":"test"}`)
	resp, err := httpClient.Put(server.URL, "application/json", body)

	// 4xx client errors are non-retryable, response is returned for caller to handle
	require.NoError(t, err)
	require.NotNil(t, resp)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

// TestHTTPClient_Put_InvalidURL tests the Put method with an invalid URL
func TestHTTPClient_Put_InvalidURL(t *testing.T) {
	logger := lib.NewLogger(lib.LogLevelDebug)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1, InitialBackoffMs: 100, MaxBackoffMs: 1000}, logger)

	body := []byte(`{"data":"test"}`)
	// Use an invalid URL scheme to trigger NewRequest error
	resp, err := httpClient.Put("://invalid-url", "application/json", body)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "failed to create request")
}

// TestHTTPClient_Put_EmptyBody tests the Put method with an empty body
func TestHTTPClient_Put_EmptyBody(t *testing.T) {
	var receivedBody []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelDebug)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1, InitialBackoffMs: 100, MaxBackoffMs: 1000}, logger)

	resp, err := httpClient.Put(server.URL, "application/json", []byte{})

	require.NoError(t, err)
	require.NotNil(t, resp)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Empty(t, receivedBody)
}

// TestHTTPClient_Put_LargeBody tests the Put method with a large body
func TestHTTPClient_Put_LargeBody(t *testing.T) {
	var receivedBodyLen int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		receivedBodyLen = len(body)
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelDebug)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1, InitialBackoffMs: 100, MaxBackoffMs: 1000}, logger)

	// Create a large body (1MB)
	largeBody := make([]byte, 1024*1024)
	for i := range largeBody {
		largeBody[i] = byte('A' + (i % 26))
	}

	resp, err := httpClient.Put(server.URL, "application/octet-stream", largeBody)

	require.NoError(t, err)
	require.NotNil(t, resp)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusCreated, resp.StatusCode)
	assert.Equal(t, len(largeBody), receivedBodyLen)
}

// TestHTTPClient_Put_CustomContentType tests the Put method with various content types
func TestHTTPClient_Put_CustomContentType(t *testing.T) {
	contentTypes := []string{
		"application/fhir+json",
		"text/plain",
		"application/xml",
		"multipart/form-data",
	}

	for _, expectedCT := range contentTypes {
		t.Run(expectedCT, func(t *testing.T) {
			var receivedContentType string

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				receivedContentType = r.Header.Get("Content-Type")
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			logger := lib.NewLogger(lib.LogLevelDebug)
			httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1, InitialBackoffMs: 100, MaxBackoffMs: 1000}, logger)

			resp, err := httpClient.Put(server.URL, expectedCT, []byte("test"))

			require.NoError(t, err)
			require.NotNil(t, resp)
			_ = resp.Body.Close()

			assert.Equal(t, expectedCT, receivedContentType)
		})
	}
}
