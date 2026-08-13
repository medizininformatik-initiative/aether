package unit

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/services"
)

// writeEnvTestConfig writes a config file with the given body plus a valid
// jobs_dir and returns the config file path.
func writeEnvTestConfig(t *testing.T, body string) string {
	t.Helper()
	tmpDir := t.TempDir()
	jobsDir := filepath.Join(tmpDir, "jobs")
	require.NoError(t, os.MkdirAll(jobsDir, 0755))
	configFile := filepath.Join(tmpDir, "config.yaml")
	content := body + "\njobs_dir: " + jobsDir + "\n"
	require.NoError(t, os.WriteFile(configFile, []byte(content), 0644))
	return configFile
}

// TestEnvOverride_NestedTorchBaseURL_AbsentFromFile is the canonical regression
// for issue #435: a nested key (services.torch.base_url) omitted from the YAML
// file is supplied entirely via AETHER_SERVICES_TORCH_BASE_URL. The torch step
// is enabled, so without the override TORCH.Validate would reject the empty
// base_url — proving the env value reaches both the struct and validation.
func TestEnvOverride_NestedTorchBaseURL_AbsentFromFile(t *testing.T) {
	t.Setenv("AETHER_SERVICES_TORCH_BASE_URL", "http://torch.env.example:9000")

	configFile := writeEnvTestConfig(t, `
pipeline:
  enabled_steps:
    - torch
retry:
  max_attempts: 5
  initial_backoff_ms: 1000
  max_backoff_ms: 30000`)

	config, err := services.LoadConfig(configFile)
	require.NoError(t, err, "env-provided base_url should satisfy validation")
	assert.Equal(t, "http://torch.env.example:9000", config.Services.TORCH.BaseURL)
}

// TestEnvOverride_NestedDIMPURL_AbsentFromFile covers a second nested key in a
// different service block (services.dimp.url). The dimp step requires a service
// URL, so the override is what lets validation pass.
func TestEnvOverride_NestedDIMPURL_AbsentFromFile(t *testing.T) {
	t.Setenv("AETHER_SERVICES_DIMP_URL", "http://dimp.env.example:8080/fhir")

	configFile := writeEnvTestConfig(t, `
pipeline:
  enabled_steps:
    - local_import
    - dimp
retry:
  max_attempts: 5
  initial_backoff_ms: 1000
  max_backoff_ms: 30000`)

	config, err := services.LoadConfig(configFile)
	require.NoError(t, err, "env-provided dimp url should satisfy validation")
	assert.Equal(t, "http://dimp.env.example:8080/fhir", config.Services.DIMP.URL)
}

// TestEnvOverride_DIMPAuthAPIKey verifies the dimp auth block is bindable from
// the environment, so credentials stay out of the config file.
func TestEnvOverride_DIMPAuthAPIKey(t *testing.T) {
	t.Setenv("AETHER_SERVICES_DIMP_AUTH_API_KEY", "env-api-key")

	configFile := writeEnvTestConfig(t, `
services:
  dimp:
    url: "http://dimp.example:8080"
pipeline:
  enabled_steps:
    - local_import
    - dimp
retry:
  max_attempts: 5
  initial_backoff_ms: 1000
  max_backoff_ms: 30000`)

	config, err := services.LoadConfig(configFile)
	require.NoError(t, err)
	assert.Equal(t, "env-api-key", config.Services.DIMP.Auth.APIKey)
}

// TestEnvOverride_DeeplyNestedKey verifies the reflection walk binds keys more
// than two levels deep (services.send.s3.bucket).
func TestEnvOverride_DeeplyNestedKey(t *testing.T) {
	t.Setenv("AETHER_SERVICES_SEND_S3_BUCKET", "env-bucket")

	configFile := writeEnvTestConfig(t, `
pipeline:
  enabled_steps:
    - local_import
retry:
  max_attempts: 5
  initial_backoff_ms: 1000
  max_backoff_ms: 30000`)

	config, err := services.LoadConfig(configFile)
	require.NoError(t, err)
	assert.Equal(t, "env-bucket", config.Services.Send.S3.Bucket)
}

// TestEnvOverride_TypedKeys verifies env overrides decode into non-string field
// types (int, bool, duration), proving the "all keys" scope is type-correct.
func TestEnvOverride_TypedKeys(t *testing.T) {
	t.Setenv("AETHER_RETRY_MAX_ATTEMPTS", "9")                      // int, non-service block
	t.Setenv("AETHER_TLS_INSECURE_SKIP_VERIFY", "true")             // bool
	t.Setenv("AETHER_SERVICES_TORCH_EXTRACTION_TIMEOUT", "PT45M")   // duration
	t.Setenv("AETHER_SERVICES_DIMP_BUNDLE_SPLIT_THRESHOLD_MB", "5") // int, service block

	configFile := writeEnvTestConfig(t, `
pipeline:
  enabled_steps:
    - local_import
retry:
  max_attempts: 5
  initial_backoff_ms: 1000
  max_backoff_ms: 30000`)

	config, err := services.LoadConfig(configFile)
	require.NoError(t, err)
	assert.Equal(t, 9, config.Retry.MaxAttempts, "int override")
	assert.True(t, config.TLS.InsecureSkipVerify, "bool override")
	assert.Equal(t, 45*time.Minute, config.Services.TORCH.ExtractionTimeout, "duration override")
	assert.Equal(t, 5, config.Services.DIMP.BundleSplitThresholdMB, "service-block int override")
}

// TestEnvOverride_UnsetKeepsDefaults verifies that binding env keys does not
// clobber struct defaults when the variables are unset — fields omitted from the
// file retain their models.DefaultConfig() values.
func TestEnvOverride_UnsetKeepsDefaults(t *testing.T) {
	configFile := writeEnvTestConfig(t, `
pipeline:
  enabled_steps:
    - local_import
retry:
  max_attempts: 5
  initial_backoff_ms: 1000
  max_backoff_ms: 30000`)

	config, err := services.LoadConfig(configFile)
	require.NoError(t, err)
	assert.Equal(t, 30*time.Minute, config.Services.TORCH.ExtractionTimeout, "duration default preserved")
	assert.Equal(t, 10, config.Services.DIMP.BundleSplitThresholdMB, "int default preserved")
	assert.True(t, config.Compression.Enabled, "bool default preserved")
	assert.Equal(t, "default", config.Compression.Level, "string default preserved")
}

// TestEnvOverride_EnvWinsOverFileValue verifies precedence: an env override
// takes priority over a value present in the config file.
func TestEnvOverride_EnvWinsOverFileValue(t *testing.T) {
	t.Setenv("AETHER_SERVICES_DIMP_URL", "http://dimp.env.example:8080/fhir")

	configFile := writeEnvTestConfig(t, `
services:
  dimp:
    url: "http://dimp.file.example:1234/fhir"
pipeline:
  enabled_steps:
    - local_import
    - dimp
retry:
  max_attempts: 5
  initial_backoff_ms: 1000
  max_backoff_ms: 30000`)

	config, err := services.LoadConfig(configFile)
	require.NoError(t, err)
	assert.Equal(t, "http://dimp.env.example:8080/fhir", config.Services.DIMP.URL, "env should win over file")
}
