package unit

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/models"
)

// Basic auth and OAuth2 both write Authorization, so only one of them is valid.
func TestAuthConfig_Validate_RejectsBasicWithOAuth2(t *testing.T) {
	auth := models.AuthConfig{
		Username:          "u",
		Password:          "p",
		OAuthIssuerURI:    "https://idp",
		OAuthClientID:     "c",
		OAuthClientSecret: "s",
	}

	err := auth.Validate("dimp")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "dimp auth:")
	assert.Contains(t, err.Error(), "cannot configure basic auth and OAuth2 together")
}

// An API key uses its own header, so it can accompany an Authorization scheme.
func TestAuthConfig_Validate_AcceptsAPIKeyWithAuthorizationScheme(t *testing.T) {
	tests := []struct {
		name string
		auth models.AuthConfig
	}{
		{
			name: "basic and api key",
			auth: models.AuthConfig{Username: "u", Password: "p", APIKey: "k"},
		},
		{
			name: "oauth2 and api key",
			auth: models.AuthConfig{
				OAuthIssuerURI:    "https://idp",
				OAuthClientID:     "c",
				OAuthClientSecret: "s",
				APIKey:            "k",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NoError(t, tt.auth.Validate("dimp"))
		})
	}
}

func TestAuthConfig_Validate_RejectsHeaderWithoutAPIKey(t *testing.T) {
	auth := models.AuthConfig{APIKeyHeader: "X-Gateway-Key"}

	err := auth.Validate("dimp")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "api_key is required")
}

// A static bearer token is a legal API key in the Authorization header. No
// other field can express it.
func TestAuthConfig_Validate_AcceptsAuthorizationAsAPIKeyHeaderAlone(t *testing.T) {
	auth := models.AuthConfig{APIKey: "Bearer abc", APIKeyHeader: "Authorization"}

	assert.NoError(t, auth.Validate("dimp"))
}

// One scheme per header: the API key would overwrite the credentials of basic
// auth or OAuth2.
func TestAuthConfig_Validate_RejectsAuthorizationAsAPIKeyHeaderWithScheme(t *testing.T) {
	schemes := map[string]models.AuthConfig{
		"basic auth": {Username: "u", Password: "p"},
		"oauth2": {
			OAuthIssuerURI:    "https://idp",
			OAuthClientID:     "c",
			OAuthClientSecret: "s",
		},
	}

	for scheme, auth := range schemes {
		for _, name := range []string{"Authorization", "authorization", "AUTHORIZATION"} {
			t.Run(scheme+" "+name, func(t *testing.T) {
				auth.APIKey = "k"
				auth.APIKeyHeader = name

				err := auth.Validate("dimp")

				require.Error(t, err)
				assert.Contains(t, err.Error(), "dimp auth:")
				assert.Contains(t, err.Error(), "api_key_header cannot be Authorization")
			})
		}
	}
}

// An invalid name fails when the client writes the request. Validate reports it
// at config load instead.
func TestAuthConfig_Validate_RejectsInvalidAPIKeyHeaderName(t *testing.T) {
	for _, name := range []string{"X Gateway Key", "X-Key:", "X-Key\nInjected: 1", "kéy"} {
		t.Run(name, func(t *testing.T) {
			auth := models.AuthConfig{APIKey: "k", APIKeyHeader: name}

			err := auth.Validate("dimp")

			require.Error(t, err)
			assert.Contains(t, err.Error(), "dimp auth:")
			assert.Contains(t, err.Error(), "api_key_header is not a valid HTTP header name")
		})
	}
}

func TestAuthConfig_Validate_AcceptsValidAPIKeyHeaderName(t *testing.T) {
	for _, name := range []string{"X-Gateway-Key", "x_api_key", "Api.Key1"} {
		t.Run(name, func(t *testing.T) {
			auth := models.AuthConfig{APIKey: "k", APIKeyHeader: name}

			assert.NoError(t, auth.Validate("dimp"))
		})
	}
}

func TestAuthConfig_Validate_AcceptsAPIKeyAlone(t *testing.T) {
	auth := models.AuthConfig{APIKey: "k"}

	assert.NoError(t, auth.Validate("dimp"))
}

func dimpAuthProjectConfig(auth models.AuthConfig) models.ProjectConfig {
	return models.ProjectConfig{
		Services: models.ServiceConfig{
			DIMP: models.DIMPConfig{URL: "http://localhost:32861", Auth: auth},
		},
		Pipeline: models.PipelineConfig{
			EnabledSteps: []models.StepName{models.StepLocalImport, models.StepDIMP},
		},
		Retry:   models.RetryConfig{MaxAttempts: 3, InitialBackoffMs: 500, MaxBackoffMs: 5000},
		JobsDir: "/tmp/jobs",
	}
}

func TestProjectConfig_Validate_RejectsInconsistentDIMPAuth(t *testing.T) {
	config := dimpAuthProjectConfig(models.AuthConfig{Username: "u"})

	err := config.Validate()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "dimp auth: password is required")
}

func TestProjectConfig_Validate_AcceptsDIMPAPIKeyAuth(t *testing.T) {
	config := dimpAuthProjectConfig(models.AuthConfig{APIKey: "k"})

	assert.NoError(t, config.Validate())
}
