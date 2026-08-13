package unit

import (
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

// newDIMPTestClient builds a DIMP client against url with a single-attempt
// retry policy, so a failing test fails fast instead of backing off.
func newDIMPTestClient(url string, auth models.AuthConfig) *services.DIMPClient {
	logger := lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(
		5*time.Second,
		models.RetryConfig{MaxAttempts: 1, InitialBackoffMs: 1, MaxBackoffMs: 10},
		models.TLSConfig{},
		logger,
	)
	return services.NewDIMPClient(models.DIMPConfig{URL: url, Auth: auth}, httpClient, logger)
}

// The DIMP client passes its configured auth to the shared HTTP client, so each
// scheme reaches the service in the header that scheme uses.
func TestDIMPClient_Pseudonymize_SendsConfiguredAuth(t *testing.T) {
	tests := []struct {
		name   string
		auth   models.AuthConfig
		header string
		want   string
	}{
		{
			name:   "basic auth",
			auth:   models.AuthConfig{Username: "u", Password: "p"},
			header: "Authorization",
			want:   "Basic dTpw", // base64("u:p")
		},
		{
			name:   "api key in the default header",
			auth:   models.AuthConfig{APIKey: "secret"},
			header: "x-api-key",
			want:   "secret",
		},
		{
			name:   "api key in a custom header",
			auth:   models.AuthConfig{APIKey: "secret", APIKeyHeader: "X-Gateway-Key"},
			header: "X-Gateway-Key",
			want:   "secret",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = r.Header.Get(tt.header)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"resourceType":"Patient","id":"p1"}`))
			}))
			defer server.Close()

			client := newDIMPTestClient(server.URL, tt.auth)

			_, err := client.Pseudonymize(map[string]any{"resourceType": "Patient"})

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// OAuth2 needs a token endpoint, so it does not fit the table above.
func TestDIMPClient_Pseudonymize_SendsOAuth2BearerToken(t *testing.T) {
	services.ClearOAuth2TokenCache()

	tokenServer := oauthTokenServer(http.StatusOK)
	defer tokenServer.Close()

	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"resourceType":"Patient","id":"p1"}`))
	}))
	defer server.Close()

	client := newDIMPTestClient(server.URL, models.AuthConfig{
		OAuthIssuerURI:    tokenServer.URL,
		OAuthClientID:     "aether",
		OAuthClientSecret: "secret",
	})

	_, err := client.Pseudonymize(map[string]any{"resourceType": "Patient"})

	require.NoError(t, err)
	assert.Equal(t, "Bearer tok", gotAuth)
}
