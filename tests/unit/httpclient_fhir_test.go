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

func fhirTestClient(retry models.RetryConfig) *services.HTTPClient {
	return services.NewHTTPClient(5*time.Second, retry, models.TLSConfig{}, lib.NewLogger(lib.LogLevelError))
}

// AC5: the real HTTPClient wires auth + status-error + decode for a FHIR-JSON request.
func TestHTTPClient_DoFHIRJSON_SuccessDecodesAndSendsAuthAndContentType(t *testing.T) {
	var gotAuth, gotContentType string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"resourceType":"Patient","id":"p1"}`))
	}))
	defer server.Close()

	client := fhirTestClient(models.RetryConfig{MaxAttempts: 1, InitialBackoffMs: 1, MaxBackoffMs: 1})

	var out map[string]any
	err := client.DoFHIRJSON(services.FHIRRequest{
		Method:      "POST",
		URL:         server.URL,
		ContentType: "application/json",
		Body:        map[string]any{"resourceType": "Patient"},
		Auth:        models.AuthConfig{Username: "u", Password: "p"},
		Service:     "DIMP",
	}, &out)

	require.NoError(t, err)
	assert.Equal(t, "p1", out["id"])
	assert.Equal(t, "application/json", gotContentType)
	assert.Equal(t, "Basic dTpw", gotAuth) // base64("u:p")
}

// AC5: status-error is wired — 4xx yields a classified *ServiceError.
func TestHTTPClient_DoFHIRJSON_HTTPErrorReturnsServiceError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"issue":"bad"}`))
	}))
	defer server.Close()

	client := fhirTestClient(models.RetryConfig{MaxAttempts: 1, InitialBackoffMs: 1, MaxBackoffMs: 1})

	err := client.DoFHIRJSON(services.FHIRRequest{
		Method: "POST", URL: server.URL, ContentType: "application/fhir+json",
		Body: map[string]any{}, Service: "validation",
	}, nil)

	require.Error(t, err)
	var svcErr *services.ServiceError
	require.True(t, errors.As(err, &svcErr), "expected *ServiceError")
	assert.Equal(t, 422, svcErr.StatusCode)
	assert.Equal(t, models.ErrorTypeNonTransient, svcErr.ErrorType)
	assert.Contains(t, svcErr.Body, "bad")
}

// AC5: retry is wired — a transient 503 then 200 succeeds after one retry.
func TestHTTPClient_DoFHIRJSON_RetriesTransient(t *testing.T) {
	var hits int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&hits, 1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	client := fhirTestClient(models.RetryConfig{MaxAttempts: 3, InitialBackoffMs: 1, MaxBackoffMs: 1})

	var out map[string]any
	err := client.DoFHIRJSON(services.FHIRRequest{
		Method: "POST", URL: server.URL, ContentType: "application/json",
		Body: map[string]any{}, Service: "DIMP",
	}, &out)

	require.NoError(t, err)
	assert.Equal(t, true, out["ok"])
	assert.Equal(t, int32(2), atomic.LoadInt32(&hits), "should retry once then succeed")
}

// DoOnce performs exactly one request with no retry, even on a transient status.
func TestHTTPClient_DoOnce_NoRetry(t *testing.T) {
	var hits int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := fhirTestClient(models.RetryConfig{MaxAttempts: 5, InitialBackoffMs: 1, MaxBackoffMs: 1})

	req, err := http.NewRequest("GET", server.URL, nil)
	require.NoError(t, err)
	resp, err := client.DoOnce(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	assert.Equal(t, int32(1), atomic.LoadInt32(&hits), "DoOnce must not retry")
}

// -----------------------------------------------------------------------------
// DoFHIRJSON error and default-value branches.
// -----------------------------------------------------------------------------

func TestHTTPClient_DoFHIRJSON_MarshalError(t *testing.T) {
	client := fhirTestClient(models.RetryConfig{MaxAttempts: 1, InitialBackoffMs: 1, MaxBackoffMs: 1})

	// A channel cannot be JSON-marshaled, so marshaling fails before any request.
	err := client.DoFHIRJSON(services.FHIRRequest{
		Method:  "POST",
		URL:     "http://example.invalid",
		Body:    make(chan int),
		Service: "DIMP",
	}, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to marshal request")
}

func TestHTTPClient_DoFHIRJSON_NewRequestError(t *testing.T) {
	client := fhirTestClient(models.RetryConfig{MaxAttempts: 1, InitialBackoffMs: 1, MaxBackoffMs: 1})

	// A method containing a space is rejected by http.NewRequest.
	err := client.DoFHIRJSON(services.FHIRRequest{
		Method:  "invalid method",
		URL:     "http://example.invalid",
		Service: "DIMP",
	}, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create request")
}

func TestHTTPClient_DoFHIRJSON_DefaultsContentTypeWhenEmpty(t *testing.T) {
	var gotContentType string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	client := fhirTestClient(models.RetryConfig{MaxAttempts: 1, InitialBackoffMs: 1, MaxBackoffMs: 1})

	var out map[string]any
	err := client.DoFHIRJSON(services.FHIRRequest{
		Method:  "POST",
		URL:     server.URL,
		Body:    map[string]any{"resourceType": "Patient"},
		Service: "DIMP",
		// ContentType left empty -> defaults to application/fhir+json.
	}, &out)

	require.NoError(t, err)
	assert.Equal(t, "application/fhir+json", gotContentType)
	assert.Equal(t, true, out["ok"])
}

func TestHTTPClient_DoFHIRJSON_ApplyAuthError(t *testing.T) {
	services.ClearOAuth2TokenCache()
	defer services.ClearOAuth2TokenCache()

	tokenServer := oauthTokenServer(http.StatusInternalServerError)
	defer tokenServer.Close()

	client := fhirTestClient(models.RetryConfig{MaxAttempts: 1, InitialBackoffMs: 1, MaxBackoffMs: 1})

	err := client.DoFHIRJSON(services.FHIRRequest{
		Method:  "POST",
		URL:     "http://example.invalid",
		Body:    map[string]any{"resourceType": "Patient"},
		Service: "DIMP",
		Auth: models.AuthConfig{
			OAuthIssuerURI:    tokenServer.URL,
			OAuthClientID:     "id",
			OAuthClientSecret: "secret",
		},
	}, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to add auth header")
}
