package unit

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

// loadTORCHConfigFile writes torchBlock as the services.torch section of a
// config file and loads it.
func loadTORCHConfigFile(t *testing.T, torchBlock string) *models.ProjectConfig {
	t.Helper()
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	jobsDir := filepath.Join(tmpDir, "jobs")
	require.NoError(t, os.MkdirAll(jobsDir, 0755))

	content := "services:\n  torch:\n" + torchBlock + `
pipeline:
  enabled_steps:
    - torch

jobs_dir: "` + jobsDir + `"
`
	require.NoError(t, os.WriteFile(configFile, []byte(content), 0644))

	config, err := services.LoadConfig(configFile)
	require.NoError(t, err)
	return config
}

// validTORCHConfig returns a TORCH config that passes validation. Each test
// changes only the auth settings that it examines.
func validTORCHConfig() models.TORCHConfig {
	return models.TORCHConfig{
		BaseURL:            "http://torch.example",
		ExtractionTimeout:  30 * time.Minute,
		PollingInterval:    5 * time.Second,
		MaxPollingInterval: 30 * time.Second,
	}
}

// The auth block is the supported shape, so EffectiveAuth returns it unchanged.
func TestTORCHConfig_EffectiveAuth_UsesAuthBlock(t *testing.T) {
	cfg := models.TORCHConfig{
		BaseURL: "http://torch.example",
		Auth: models.AuthConfig{
			Username: "block-user",
			Password: "block-pass",
		},
	}

	assert.Equal(t, models.AuthConfig{Username: "block-user", Password: "block-pass"}, cfg.EffectiveAuth())
}

// Configuration files that predate the auth block keep the flat fields, so
// EffectiveAuth maps them to the auth block.
func TestTORCHConfig_EffectiveAuth_FallsBackToDeprecatedFlatFields(t *testing.T) {
	cfg := models.TORCHConfig{
		BaseURL:           "http://torch.example",
		OAuthIssuerURI:    "https://idp.example", //nolint:staticcheck // the test covers the deprecated shape
		OAuthClientID:     "flat-client",         //nolint:staticcheck // the test covers the deprecated shape
		OAuthClientSecret: "flat-secret",         //nolint:staticcheck // the test covers the deprecated shape
	}

	assert.Equal(t, models.AuthConfig{
		OAuthIssuerURI:    "https://idp.example",
		OAuthClientID:     "flat-client",
		OAuthClientSecret: "flat-secret",
	}, cfg.EffectiveAuth())
}

// A file that mixes both shapes is ambiguous, so validation refuses it and
// names the deprecated keys.
func TestTORCHConfig_Validate_RejectsBothAuthShapes(t *testing.T) {
	cfg := validTORCHConfig()
	cfg.Username = "flat-user" //nolint:staticcheck // the test covers the deprecated shape
	cfg.Auth = models.AuthConfig{Username: "block-user", Password: "block-pass"}

	err := cfg.Validate()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "username")
	assert.Contains(t, err.Error(), "auth")
}

// TORCH uses the shared auth rules, which refuse basic auth together with
// OAuth2 because both write the Authorization header.
func TestTORCHConfig_Validate_AppliesSharedAuthRules(t *testing.T) {
	cfg := validTORCHConfig()
	cfg.Auth = models.AuthConfig{
		Username:          "u",
		Password:          "p",
		OAuthIssuerURI:    "https://idp.example",
		OAuthClientID:     "c",
		OAuthClientSecret: "s",
	}

	err := cfg.Validate()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "torch auth")
}

// The shared rules also cover a config that still uses the flat fields.
func TestTORCHConfig_Validate_AppliesSharedAuthRulesToDeprecatedFields(t *testing.T) {
	cfg := validTORCHConfig()
	cfg.Username = "u" //nolint:staticcheck // the test covers the deprecated shape

	err := cfg.Validate()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "password is required")
}

// The auth block is the supported shape in a configuration file.
func TestConfigLoading_TORCHAuthBlock(t *testing.T) {
	config := loadTORCHConfigFile(t, `    base_url: "http://torch.example"
    auth:
      username: "block-user"
      password: "block-pass"
`)

	assert.Equal(t, "block-user", config.Services.TORCH.Auth.Username)
	assert.Equal(t, "block-pass", config.Services.TORCH.Auth.Password)
	assert.Equal(t, models.AuthConfig{Username: "block-user", Password: "block-pass"},
		config.Services.TORCH.EffectiveAuth())
}

// A configuration file that predates the auth block keeps working.
func TestConfigLoading_TORCHDeprecatedFlatAuth(t *testing.T) {
	config := loadTORCHConfigFile(t, `    base_url: "http://torch.example"
    username: "flat-user"
    password: "flat-pass"
`)

	assert.Equal(t, models.AuthConfig{Username: "flat-user", Password: "flat-pass"},
		config.Services.TORCH.EffectiveAuth())
}

// The auth block gets its own AETHER_* variables.
func TestConfigLoading_TORCHAuthBlockEnvOverride(t *testing.T) {
	t.Setenv("AETHER_SERVICES_TORCH_AUTH_USERNAME", "env-user")
	t.Setenv("AETHER_SERVICES_TORCH_AUTH_PASSWORD", "env-pass")

	config := loadTORCHConfigFile(t, `    base_url: "http://torch.example"
`)

	assert.Equal(t, models.AuthConfig{Username: "env-user", Password: "env-pass"},
		config.Services.TORCH.EffectiveAuth())
}

// The AETHER_SERVICES_TORCH_USERNAME style variables keep working.
func TestConfigLoading_TORCHDeprecatedEnvOverride(t *testing.T) {
	t.Setenv("AETHER_SERVICES_TORCH_USERNAME", "env-user")
	t.Setenv("AETHER_SERVICES_TORCH_PASSWORD", "env-pass")

	config := loadTORCHConfigFile(t, `    base_url: "http://torch.example"
`)

	assert.Equal(t, models.AuthConfig{Username: "env-user", Password: "env-pass"},
		config.Services.TORCH.EffectiveAuth())
}

// An auth block alone shows that the operator configured TORCH, so validation
// runs and reports the missing base_url.
func TestProjectConfig_Validate_TORCHAuthBlockCountsAsConfigured(t *testing.T) {
	config := models.DefaultConfig()
	config.Pipeline.EnabledSteps = []models.StepName{models.StepLocalImport}
	config.Services.TORCH = models.TORCHConfig{
		Auth: models.AuthConfig{Username: "u", Password: "p"},
	}

	err := config.Validate()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "base_url is required")
}
