package unit

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

// An API key goes in its own header, so it never touches Authorization.
func TestApplyAuth_APIKey(t *testing.T) {
	tests := []struct {
		name   string
		auth   models.AuthConfig
		header string
	}{
		{
			name:   "default header",
			auth:   models.AuthConfig{APIKey: "secret"},
			header: "x-api-key",
		},
		{
			name:   "custom header",
			auth:   models.AuthConfig{APIKey: "secret", APIKeyHeader: "X-Gateway-Key"},
			header: "X-Gateway-Key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest("POST", "http://example.org/fhir", nil)
			require.NoError(t, err)

			require.NoError(t, services.DefaultHTTPClient().ApplyAuth(req, tt.auth))

			assert.Equal(t, "secret", req.Header.Get(tt.header))
			assert.Empty(t, req.Header.Get("Authorization"))
		})
	}
}

// A gateway can need an API key while the service behind it needs basic auth.
func TestApplyAuth_APIKeyWithBasicAuth(t *testing.T) {
	auth := models.AuthConfig{Username: "user", Password: "pass", APIKey: "secret"}

	req, err := http.NewRequest("POST", "http://example.org/fhir", nil)
	require.NoError(t, err)

	require.NoError(t, services.DefaultHTTPClient().ApplyAuth(req, auth))

	assert.Equal(t, "Basic dXNlcjpwYXNz", req.Header.Get("Authorization"))
	assert.Equal(t, "secret", req.Header.Get("x-api-key"))
}
