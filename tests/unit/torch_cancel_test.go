package unit

import (
	"context"
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

func TestTORCHClient_CancelExtraction_PostsCancelOperation(t *testing.T) {
	var method, path string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		w.Header().Set("Content-Type", "application/fhir+json")
		_, _ = w.Write([]byte(`{"resourceType":"Task","status":"cancelled"}`))
	}))
	defer server.Close()

	client := newProgressTestClient(t, server.URL)

	require.NoError(t, client.CancelExtraction(context.Background(), "job-1"))
	assert.Equal(t, http.MethodPost, method)
	assert.Equal(t, "/fhir/Task/job-1/$cancel", path)
}

func TestTORCHClient_CancelExtraction_RedirectIsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusMultipleChoices)
	}))
	defer server.Close()

	client := newProgressTestClient(t, server.URL)

	assert.Error(t, client.CancelExtraction(context.Background(), "job-1"))
}

func TestTORCHClient_CancelExtraction_RejectedIsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`cannot be $cancel: already completed`))
	}))
	defer server.Close()

	client := newProgressTestClient(t, server.URL)

	err := client.CancelExtraction(context.Background(), "job-1")

	var torchErr *services.TORCHError
	require.ErrorAs(t, err, &torchErr)
	assert.Equal(t, "cancel", torchErr.Operation)
	assert.Equal(t, http.StatusConflict, torchErr.StatusCode)
	assert.Contains(t, torchErr.Message, "already completed")
}

func TestTORCHClient_CancelExtraction_SendsCredentials(t *testing.T) {
	var user, password string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, _ = r.BasicAuth()
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelDebug)
	httpClient := services.NewHTTPClient(2*time.Second, models.RetryConfig{MaxAttempts: 1}, models.TLSConfig{}, logger)
	cfg := models.TORCHConfig{
		BaseURL: server.URL,
		Auth:    models.AuthConfig{Username: "aether", Password: "secret"},
	}

	err := services.NewTORCHClient(cfg, httpClient, logger).CancelExtraction(context.Background(), "job-1")

	require.NoError(t, err)
	assert.Equal(t, "aether", user)
	assert.Equal(t, "secret", password)
}

func TestTORCHClient_CancelExtraction_StopsAtContextDeadline(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	defer server.Close()
	defer close(release)

	client := newProgressTestClient(t, server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := client.CancelExtraction(ctx, "job-1")

	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, time.Since(start), 2*time.Second)
}

func TestTORCHClient_CancelExtraction_InvalidBaseURLIsError(t *testing.T) {
	client := torchOAuthClient("", func(cfg *models.TORCHConfig) {
		cfg.BaseURL = "http://example.com/%%"
		cfg.Auth = models.AuthConfig{}
	})

	err := client.CancelExtraction(context.Background(), "job-1")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create cancel request")
}

func TestTORCHClient_CancelExtraction_AuthErrorSendsNoRequest(t *testing.T) {
	services.ClearOAuth2TokenCache()
	defer services.ClearOAuth2TokenCache()

	tokenServer := oauthTokenServer(http.StatusInternalServerError)
	defer tokenServer.Close()

	cancelCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cancelCalled = true
	}))
	defer server.Close()

	client := torchOAuthClient(tokenServer.URL, func(cfg *models.TORCHConfig) {
		cfg.BaseURL = server.URL
	})

	err := client.CancelExtraction(context.Background(), "job-1")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to add auth header")
	assert.False(t, cancelCalled)
}
