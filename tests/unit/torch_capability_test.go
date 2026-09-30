package unit

import (
	"bytes"
	"fmt"
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

const torchCapabilityStatement = `{"resourceType":"CapabilityStatement","software":{"name":"Torch","version":"1.0.0"}}`

// capabilityStub serves body with status at /fhir/metadata and records the
// last request.
type capabilityStub struct {
	*httptest.Server
	request *http.Request
}

func capabilityServer(t *testing.T, status int, body string) *capabilityStub {
	t.Helper()
	stub := &capabilityStub{}
	stub.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/fhir/metadata" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		stub.request = r
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(stub.Close)
	return stub
}

func capabilityClient(config models.TORCHConfig, logger *lib.Logger) *services.TORCHClient {
	httpClient := services.NewHTTPClient(30*time.Second, models.RetryConfig{MaxAttempts: 1}, models.TLSConfig{}, logger)
	return services.NewTORCHClient(config, httpClient, logger)
}

func quietLogger() *lib.Logger { return lib.NewLogger(lib.LogLevelError) }

func TestTORCHClient_CheckCapabilityStatement_AcceptsTorch(t *testing.T) {
	server := capabilityServer(t, http.StatusOK, torchCapabilityStatement)
	client := capabilityClient(models.TORCHConfig{
		BaseURL: server.URL,
		Auth:    models.AuthConfig{Username: "user", Password: "pass"},
	}, quietLogger())

	require.NoError(t, client.CheckCapabilityStatement())
	require.NotNil(t, server.request)
	assert.Equal(t, http.MethodGet, server.request.Method)
	assert.Equal(t, "Basic dXNlcjpwYXNz", server.request.Header.Get("Authorization"))
}

func TestTORCHClient_CheckCapabilityStatement_Unreachable(t *testing.T) {
	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()
	client := capabilityClient(models.TORCHConfig{BaseURL: closed.URL}, quietLogger())

	err := client.CheckCapabilityStatement()

	require.Error(t, err)
	assert.Contains(t, err.Error(), closed.URL+"/fhir/metadata")
}

func TestTORCHClient_CheckCapabilityStatement_InvalidBaseURL(t *testing.T) {
	client := capabilityClient(models.TORCHConfig{BaseURL: "://no-scheme"}, quietLogger())

	err := client.CheckCapabilityStatement()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create metadata request")
}

func TestTORCHClient_CheckCapabilityStatement_AuthHeaderError(t *testing.T) {
	services.ClearOAuth2TokenCache()
	defer services.ClearOAuth2TokenCache()

	tokenServer := oauthTokenServer(http.StatusInternalServerError)
	defer tokenServer.Close()

	err := torchOAuthClient(tokenServer.URL).CheckCapabilityStatement()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to add auth header")
}

// A retry of the token request can hold the start for minutes, so the check
// sends the token request one time only.
func TestTORCHClient_CheckCapabilityStatement_DoesNotRetryTokenRequest(t *testing.T) {
	services.ClearOAuth2TokenCache()
	defer services.ClearOAuth2TokenCache()

	tokenServer := oauthTokenServer(http.StatusServiceUnavailable, http.StatusOK)
	defer tokenServer.Close()
	server := capabilityServer(t, http.StatusOK, torchCapabilityStatement)

	logger := quietLogger()
	httpClient := services.NewHTTPClient(30*time.Second,
		models.RetryConfig{MaxAttempts: 3, InitialBackoffMs: 1, MaxBackoffMs: 1}, models.TLSConfig{}, logger)
	client := services.NewTORCHClient(models.TORCHConfig{
		BaseURL: server.URL,
		Auth: models.AuthConfig{
			OAuthIssuerURI:    tokenServer.URL,
			OAuthClientID:     "id",
			OAuthClientSecret: "secret",
		},
	}, httpClient, logger)

	err := client.CheckCapabilityStatement()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "authentication failed")
	assert.Contains(t, err.Error(), server.URL+"/fhir/metadata")
}

func TestTORCHClient_CheckCapabilityStatement_RejectsNonSuccessStatus(t *testing.T) {
	statuses := []int{http.StatusMultipleChoices, http.StatusInternalServerError, http.StatusBadGateway}
	for _, status := range statuses {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := capabilityServer(t, status, torchCapabilityStatement)
			client := capabilityClient(models.TORCHConfig{BaseURL: server.URL}, quietLogger())

			err := client.CheckCapabilityStatement()

			require.Error(t, err)
			assert.Contains(t, err.Error(), server.URL+"/fhir/metadata")
			assert.Contains(t, err.Error(), fmt.Sprintf("HTTP %d", status))
		})
	}
}

func TestTORCHClient_CheckCapabilityStatement_RejectsWrongPath(t *testing.T) {
	server := capabilityServer(t, http.StatusOK, torchCapabilityStatement)
	client := capabilityClient(models.TORCHConfig{BaseURL: server.URL + "/wrong"}, quietLogger())

	err := client.CheckCapabilityStatement()

	require.Error(t, err)
	assert.Contains(t, err.Error(), server.URL+"/wrong/fhir/metadata")
	assert.Contains(t, err.Error(), "HTTP 404")
}

func TestTORCHClient_CheckCapabilityStatement_ReportsAuthError(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := capabilityServer(t, status, "")
			client := capabilityClient(models.TORCHConfig{BaseURL: server.URL}, quietLogger())

			err := client.CheckCapabilityStatement()

			require.Error(t, err)
			assert.Contains(t, err.Error(), "authentication failed")
			assert.Contains(t, err.Error(), server.URL+"/fhir/metadata")
			assert.Contains(t, err.Error(), fmt.Sprintf("HTTP %d", status))
		})
	}
}

func TestTORCHClient_CheckCapabilityStatement_RejectsOtherBody(t *testing.T) {
	bodies := map[string]string{
		"html page":      "<html><body>Login</body></html>",
		"other resource": `{"resourceType":"OperationOutcome"}`,
		"empty body":     "",
		"truncated":      `{"resourceType":"CapabilityStatement"`,
	}
	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			server := capabilityServer(t, http.StatusOK, body)
			client := capabilityClient(models.TORCHConfig{BaseURL: server.URL}, quietLogger())

			err := client.CheckCapabilityStatement()

			require.Error(t, err)
			assert.Contains(t, err.Error(), "CapabilityStatement")
			assert.Contains(t, err.Error(), server.URL+"/fhir/metadata")
			assert.Contains(t, err.Error(), "HTTP 200")
		})
	}
}

func TestTORCHClient_CheckCapabilityStatement_WarnsOnOtherSoftware(t *testing.T) {
	server := capabilityServer(t, http.StatusOK,
		`{"resourceType":"CapabilityStatement","software":{"name":"Blaze"}}`)
	var logs bytes.Buffer
	client := capabilityClient(models.TORCHConfig{BaseURL: server.URL}, lib.NewLoggerWithWriter(lib.LogLevelInfo, &logs))

	require.NoError(t, client.CheckCapabilityStatement())
	assert.Contains(t, logs.String(), "WARN")
	assert.Contains(t, logs.String(), "Blaze")
}

// The configured request timeout of TORCH can be many minutes. The startup
// check must not wait that long.
func TestTORCHClient_CheckCapabilityStatement_TimesOutAfterFiveSeconds(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(server.Close)
	t.Cleanup(func() { close(release) })

	client := capabilityClient(models.TORCHConfig{BaseURL: server.URL, RequestTimeout: time.Hour}, quietLogger())

	start := time.Now()
	done := make(chan error, 1)
	go func() { done <- client.CheckCapabilityStatement() }()

	select {
	case err := <-done:
		require.Error(t, err)
		assert.Contains(t, err.Error(), server.URL+"/fhir/metadata")
		assert.GreaterOrEqual(t, time.Since(start), 4*time.Second)
	case <-time.After(10 * time.Second):
		t.Fatal("capability check did not time out")
	}
}

func TestTORCHClient_CheckCapabilityStatement_NoWarningForTorch(t *testing.T) {
	server := capabilityServer(t, http.StatusOK, torchCapabilityStatement)
	var logs bytes.Buffer
	client := capabilityClient(models.TORCHConfig{BaseURL: server.URL}, lib.NewLoggerWithWriter(lib.LogLevelInfo, &logs))

	require.NoError(t, client.CheckCapabilityStatement())
	assert.NotContains(t, logs.String(), "WARN")
}
