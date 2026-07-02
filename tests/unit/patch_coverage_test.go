package unit

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/pipeline"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

// -----------------------------------------------------------------------------
// Mock fallbacks: each test double returns a zero/echo value when its func is
// unset. These branches back the client seams used across the pipeline tests.
// -----------------------------------------------------------------------------

func TestMockPseudonymizer_EchoesResourceWhenFuncUnset(t *testing.T) {
	m := &services.MockPseudonymizer{}
	res := map[string]any{"resourceType": "Patient", "id": "p1"}

	out, err := m.Pseudonymize(res)

	require.NoError(t, err)
	assert.Equal(t, res, out)
	require.Len(t, m.Calls, 1)
}

func TestMockFlattener_ReturnsEmptyCSVWhenFuncUnset(t *testing.T) {
	m := &services.MockFlattener{}

	csv, err := m.Flatten(models.ViewDefinition{Name: "V"}, []map[string]any{{"resourceType": "Patient"}})

	require.NoError(t, err)
	assert.Empty(t, csv)
	assert.Equal(t, 1, m.Calls)
}

func TestMockResourceValidator_ReturnsIssueFreeOutcomeWhenFuncUnset(t *testing.T) {
	m := &services.MockResourceValidator{}
	res := map[string]any{"resourceType": "Patient", "id": "p1"}

	out, err := m.ValidateResource(res)

	require.NoError(t, err)
	require.NotNil(t, out)
	assert.Equal(t, "OperationOutcome", out.OperationOutcome["resourceType"])
	require.Len(t, m.Calls, 1)
}

func TestMockExtractor_ReturnsZeroValuesWhenFuncsUnset(t *testing.T) {
	m := &services.MockExtractor{}

	url, err := m.SubmitExtraction("crtdl.json")
	require.NoError(t, err)
	assert.Empty(t, url)

	urls, err := m.PollExtractionStatus("http://torch/status", false)
	require.NoError(t, err)
	assert.Nil(t, urls)

	files, err := m.DownloadExtractionFiles([]string{"http://torch/out.ndjson"}, t.TempDir(), false, false, "")
	require.NoError(t, err)
	assert.Nil(t, files)
}

// -----------------------------------------------------------------------------
// HTTPClient.DoFHIRJSON error and default-value branches.
// -----------------------------------------------------------------------------

func TestDoFHIRJSON_MarshalError(t *testing.T) {
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

func TestDoFHIRJSON_NewRequestError(t *testing.T) {
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

func TestDoFHIRJSON_DefaultsContentTypeWhenEmpty(t *testing.T) {
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

func TestDoFHIRJSON_ApplyAuthError(t *testing.T) {
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

// -----------------------------------------------------------------------------
// FlattenerClient: response body read failure.
// -----------------------------------------------------------------------------

func TestFlattenerClient_ReadResponseBodyError(t *testing.T) {
	// Promise more bytes than are sent, then close the connection so the client's
	// body read fails with an unexpected EOF.
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
		_, _ = buf.WriteString("HTTP/1.1 200 OK\r\nContent-Length: 100\r\nContent-Type: text/csv\r\n\r\nshort")
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
	assert.Contains(t, err.Error(), "failed to read response")
}

// -----------------------------------------------------------------------------
// Pipeline client-factory seams: the *ForTesting setters and their resets.
// -----------------------------------------------------------------------------

func TestFlattenerFactorySeam_SetAndReset(t *testing.T) {
	pipeline.SetFlattenerFactoryForTesting(
		func(models.FlatteningConfig, models.RetryConfig, *http.Transport, *lib.Logger) services.Flattener {
			return &services.MockFlattener{}
		},
	)
	// Restoring the default factory must not panic and leaves the package usable.
	pipeline.ResetFlattenerFactory()
}

func TestExtractorFactorySeam_SetAndReset(t *testing.T) {
	pipeline.SetExtractorFactoryForTesting(
		func(models.TORCHConfig, *services.HTTPClient, *lib.Logger) services.Extractor {
			return &services.MockExtractor{}
		},
	)
	pipeline.ResetExtractorFactory()
}

// -----------------------------------------------------------------------------
// TORCHClient auth-header application: OAuth2 token-fetch failures surface as
// "failed to add auth header" from each request-building path.
// -----------------------------------------------------------------------------

// oauthTokenServer returns a token endpoint that replies with the given HTTP
// statuses in sequence, repeating the last one for any further calls. A 200 is
// answered with a short-lived (uncacheable) token so each request re-fetches.
func oauthTokenServer(statuses ...int) *httptest.Server {
	var n int32
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i := int(atomic.AddInt32(&n, 1)) - 1
		status := statuses[len(statuses)-1]
		if i < len(statuses) {
			status = statuses[i]
		}
		if status == http.StatusOK {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"tok","expires_in":0,"token_type":"Bearer"}`))
			return
		}
		w.WriteHeader(status)
	}))
}

// torchOAuthClient builds a TORCHClient configured for OAuth2 against issuerURL,
// with no basic-auth credentials so ApplyAuth takes the OAuth path.
func torchOAuthClient(issuerURL string, cfgMods ...func(*models.TORCHConfig)) *services.TORCHClient {
	logger := lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(
		5*time.Second,
		models.RetryConfig{MaxAttempts: 1, InitialBackoffMs: 1, MaxBackoffMs: 1},
		models.TLSConfig{},
		logger,
	)
	cfg := models.TORCHConfig{
		BaseURL:            "http://torch.invalid",
		OAuthIssuerURI:     issuerURL,
		OAuthClientID:      "id",
		OAuthClientSecret:  "secret",
		ExtractionTimeout:  30 * time.Second,
		PollingInterval:    10 * time.Millisecond,
		MaxPollingInterval: 10 * time.Millisecond,
	}
	for _, mod := range cfgMods {
		mod(&cfg)
	}
	return services.NewTORCHClient(cfg, httpClient, logger)
}

func TestTORCHClient_SubmitExtraction_AuthError(t *testing.T) {
	services.ClearOAuth2TokenCache()
	defer services.ClearOAuth2TokenCache()

	tokenServer := oauthTokenServer(http.StatusInternalServerError)
	defer tokenServer.Close()

	crtdlPath := filepath.Join(t.TempDir(), "crtdl.json")
	require.NoError(t, os.WriteFile(crtdlPath,
		[]byte(`{"cohortDefinition":{"inclusionCriteria":[]},"dataExtraction":{"attributeGroups":[]}}`), 0644))

	client := torchOAuthClient(tokenServer.URL)

	_, err := client.SubmitExtraction(crtdlPath)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to add auth header")
}

func TestTORCHClient_SubmitExtractionWithContent_AuthError(t *testing.T) {
	services.ClearOAuth2TokenCache()
	defer services.ClearOAuth2TokenCache()

	tokenServer := oauthTokenServer(http.StatusInternalServerError)
	defer tokenServer.Close()

	client := torchOAuthClient(tokenServer.URL)

	_, err := client.SubmitExtractionWithContent(
		[]byte(`{"cohortDefinition":{"inclusionCriteria":[]},"dataExtraction":{"attributeGroups":[]}}`))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to add auth header")
}

func TestTORCHClient_Ping_AuthError(t *testing.T) {
	services.ClearOAuth2TokenCache()
	defer services.ClearOAuth2TokenCache()

	tokenServer := oauthTokenServer(http.StatusInternalServerError)
	defer tokenServer.Close()

	client := torchOAuthClient(tokenServer.URL)

	err := client.Ping()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to add auth header")
}

func TestTORCHClient_PollExtractionStatus_AuthError(t *testing.T) {
	services.ClearOAuth2TokenCache()
	defer services.ClearOAuth2TokenCache()

	tokenServer := oauthTokenServer(http.StatusInternalServerError)
	defer tokenServer.Close()

	client := torchOAuthClient(tokenServer.URL)

	_, err := client.PollExtractionStatus("http://torch.invalid/status", false)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to add auth header")
}

func TestTORCHClient_DownloadExtractionFiles_AvailabilityCheckAuthError(t *testing.T) {
	services.ClearOAuth2TokenCache()
	defer services.ClearOAuth2TokenCache()

	tokenServer := oauthTokenServer(http.StatusInternalServerError)
	defer tokenServer.Close()

	// FileReadyRetries=1 runs the HEAD availability check, whose auth fails first.
	client := torchOAuthClient(tokenServer.URL, func(c *models.TORCHConfig) { c.FileReadyRetries = 1 })

	_, err := client.DownloadExtractionFiles(
		[]string{"http://torch.invalid/out.ndjson"}, t.TempDir(), false, false, "")

	require.Error(t, err)
}

func TestTORCHClient_DownloadExtractionFiles_DownloadAuthError(t *testing.T) {
	services.ClearOAuth2TokenCache()
	defer services.ClearOAuth2TokenCache()

	tokenServer := oauthTokenServer(http.StatusInternalServerError)
	defer tokenServer.Close()

	// FileReadyRetries=0 skips the availability check, so downloadFile's auth is
	// the first application and its error branch is exercised.
	client := torchOAuthClient(tokenServer.URL, func(c *models.TORCHConfig) { c.FileReadyRetries = 0 })

	_, err := client.DownloadExtractionFiles(
		[]string{"http://torch.invalid/out.ndjson"}, t.TempDir(), false, false, "")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to add auth header")
}

func TestTORCHClient_DownloadExtractionFiles_RangeCheckAuthError(t *testing.T) {
	services.ClearOAuth2TokenCache()
	defer services.ClearOAuth2TokenCache()

	// The HEAD check's token fetch succeeds; the Range fallback's fetch fails.
	tokenServer := oauthTokenServer(http.StatusOK, http.StatusInternalServerError)
	defer tokenServer.Close()

	// File server rejects HEAD with 405 so checkFileAvailable falls back to a
	// Range GET, whose auth application then fails.
	fileServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer fileServer.Close()

	client := torchOAuthClient(tokenServer.URL, func(c *models.TORCHConfig) { c.FileReadyRetries = 1 })

	_, err := client.DownloadExtractionFiles(
		[]string{fileServer.URL + "/out.ndjson"}, t.TempDir(), false, false, "")

	require.Error(t, err)
}

// The Range-fallback availability check (used when HEAD is unsupported) succeeds
// and the file downloads: HEAD -> 405, a ranged GET -> 206, a full GET -> 200.
func TestTORCHClient_DownloadExtractionFiles_RangeCheckAvailable(t *testing.T) {
	fileServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodHead:
			w.WriteHeader(http.StatusMethodNotAllowed)
		case r.Header.Get("Range") != "":
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write([]byte("x"))
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"resourceType":"Patient","id":"p1"}` + "\n"))
		}
	}))
	defer fileServer.Close()

	logger := lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(
		5*time.Second,
		models.RetryConfig{MaxAttempts: 1, InitialBackoffMs: 1, MaxBackoffMs: 1},
		models.TLSConfig{},
		logger,
	)
	client := services.NewTORCHClient(models.TORCHConfig{
		BaseURL:          fileServer.URL,
		Username:         "u",
		Password:         "p",
		FileReadyRetries: 1,
	}, httpClient, logger)

	dir := t.TempDir()
	files, err := client.DownloadExtractionFiles(
		[]string{fileServer.URL + "/out.ndjson"}, dir, false, false, "")

	require.NoError(t, err)
	require.Len(t, files, 1)
}
