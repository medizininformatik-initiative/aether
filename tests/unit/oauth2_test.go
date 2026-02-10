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

func TestFetchOAuth2Token_Success(t *testing.T) {
	// Clear cache before test
	services.ClearOAuth2TokenCache()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/protocol/openid-connect/token", r.URL.Path)
		assert.Equal(t, "application/x-www-form-urlencoded", r.Header.Get("Content-Type"))

		// Verify form data
		err := r.ParseForm()
		require.NoError(t, err)
		assert.Equal(t, "client_credentials", r.Form.Get("grant_type"))
		assert.Equal(t, "test-client", r.Form.Get("client_id"))
		assert.Equal(t, "test-secret", r.Form.Get("client_secret"))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"access_token":"test-token-123","token_type":"Bearer","expires_in":3600}`))
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1}, models.TLSConfig{}, logger)

	token, err := services.FetchOAuth2Token(server.URL, "test-client", "test-secret", httpClient)
	require.NoError(t, err)
	assert.Equal(t, "test-token-123", token)
}

func TestFetchOAuth2Token_CacheHit(t *testing.T) {
	// Clear cache before test
	services.ClearOAuth2TokenCache()

	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"access_token":"cached-token","token_type":"Bearer","expires_in":3600}`))
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1}, models.TLSConfig{}, logger)

	// First request - should hit the server
	token1, err := services.FetchOAuth2Token(server.URL, "test-client", "test-secret", httpClient)
	require.NoError(t, err)
	assert.Equal(t, "cached-token", token1)
	assert.Equal(t, 1, requestCount)

	// Second request - should use cache
	token2, err := services.FetchOAuth2Token(server.URL, "test-client", "test-secret", httpClient)
	require.NoError(t, err)
	assert.Equal(t, "cached-token", token2)
	assert.Equal(t, 1, requestCount) // Still 1, cache was used
}

func TestFetchOAuth2Token_HTTPError(t *testing.T) {
	// Clear cache before test
	services.ClearOAuth2TokenCache()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid_client","error_description":"Client authentication failed"}`))
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1}, models.TLSConfig{}, logger)

	_, err := services.FetchOAuth2Token(server.URL, "test-client", "test-secret", httpClient)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "401")
}

func TestFetchOAuth2Token_InvalidJSON(t *testing.T) {
	// Clear cache before test
	services.ClearOAuth2TokenCache()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{invalid json}`))
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1}, models.TLSConfig{}, logger)

	_, err := services.FetchOAuth2Token(server.URL, "test-client", "test-secret", httpClient)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse token response")
}

func TestFetchOAuth2Token_MissingAccessToken(t *testing.T) {
	// Clear cache before test
	services.ClearOAuth2TokenCache()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// Response without access_token
		_, _ = w.Write([]byte(`{"token_type":"Bearer","expires_in":3600}`))
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1}, models.TLSConfig{}, logger)

	_, err := services.FetchOAuth2Token(server.URL, "test-client", "test-secret", httpClient)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing access_token")
}

func TestFetchOAuth2Token_ShortExpiresIn(t *testing.T) {
	// Clear cache before test
	services.ClearOAuth2TokenCache()

	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// Token that expires in less than 30 seconds - should not be cached
		_, _ = w.Write([]byte(`{"access_token":"short-lived-token","token_type":"Bearer","expires_in":20}`))
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1}, models.TLSConfig{}, logger)

	// First request
	token1, err := services.FetchOAuth2Token(server.URL, "test-client", "test-secret", httpClient)
	require.NoError(t, err)
	assert.Equal(t, "short-lived-token", token1)
	assert.Equal(t, 1, requestCount)

	// Second request - should NOT use cache because token expires too soon
	token2, err := services.FetchOAuth2Token(server.URL, "test-client", "test-secret", httpClient)
	require.NoError(t, err)
	assert.Equal(t, "short-lived-token", token2)
	assert.Equal(t, 2, requestCount) // Request count increased, cache was not used
}

func TestFetchOAuth2Token_TrailingSlashInIssuerURI(t *testing.T) {
	// Clear cache before test
	services.ClearOAuth2TokenCache()

	var receivedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"access_token":"token","token_type":"Bearer","expires_in":3600}`))
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1}, models.TLSConfig{}, logger)

	// Issuer URI with trailing slash
	_, err := services.FetchOAuth2Token(server.URL+"/", "test-client", "test-secret", httpClient)
	require.NoError(t, err)
	// Should not have double slashes
	assert.Equal(t, "/protocol/openid-connect/token", receivedPath)
}

func TestClearOAuth2TokenCache(t *testing.T) {
	// First, populate the cache
	services.ClearOAuth2TokenCache()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"access_token":"token-to-clear","token_type":"Bearer","expires_in":3600}`))
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1}, models.TLSConfig{}, logger)

	// Populate cache
	_, _ = services.FetchOAuth2Token(server.URL, "test-client", "test-secret", httpClient)

	// Clear cache
	services.ClearOAuth2TokenCache()

	// Next request should hit the server again (cache was cleared)
	requestCount := 0
	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"access_token":"new-token","token_type":"Bearer","expires_in":3600}`))
	}))
	defer server2.Close()

	token, err := services.FetchOAuth2Token(server2.URL, "test-client", "test-secret", httpClient)
	require.NoError(t, err)
	assert.Equal(t, "new-token", token)
	assert.Equal(t, 1, requestCount) // Server was called because cache was cleared
}
