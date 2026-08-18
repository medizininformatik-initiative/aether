package unit

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

// TestConfigLoading_MultipleEnabledSteps verifies that all enabled steps
// are loaded from YAML config file (regression test for viper.Unmarshal bug)
func TestConfigLoading_MultipleEnabledSteps(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	jobsDir := filepath.Join(tmpDir, "jobs")
	_ = os.MkdirAll(jobsDir, 0755)

	configContent := `
services:
  dimp:
    url: "http://localhost:32861/fhir"
  flattening:
    service_url: "http://localhost:8000"
    lookup_path: "/tmp/lookup.json"

pipeline:
  enabled_steps:
    - local_import
    - dimp
    - validation
    - flattening

retry:
  max_attempts: 5
  initial_backoff_ms: 1000
  max_backoff_ms: 30000

jobs_dir: "` + jobsDir + `"
`
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	config, err := services.LoadConfig(configFile)
	require.NoError(t, err, "Config should load without error")

	// Verify ALL steps are loaded
	assert.Len(t, config.Pipeline.EnabledSteps, 4, "Should have 4 enabled steps")
	assert.Equal(t, models.StepLocalImport, config.Pipeline.EnabledSteps[0])
	assert.Equal(t, models.StepDIMP, config.Pipeline.EnabledSteps[1])
	assert.Equal(t, models.StepValidation, config.Pipeline.EnabledSteps[2])
	assert.Equal(t, models.StepFlattening, config.Pipeline.EnabledSteps[3])
}

// TestConfigLoading_ServiceURLs verifies all service URLs are loaded correctly
func TestConfigLoading_ServiceURLs(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	jobsDir := filepath.Join(tmpDir, "jobs")
	_ = os.MkdirAll(jobsDir, 0755)

	expectedDIMPUrl := "http://dimp.example.com:8080/fhir"
	expectedFlatteningUrl := "http://flattener.example.com:8000"

	configContent := `
services:
  dimp:
    url: "` + expectedDIMPUrl + `"
  flattening:
    service_url: "` + expectedFlatteningUrl + `"
    lookup_path: "/tmp/lookup.json"

pipeline:
  enabled_steps:
    - local_import

retry:
  max_attempts: 3
  initial_backoff_ms: 500
  max_backoff_ms: 10000

jobs_dir: "` + jobsDir + `"
`
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	config, err := services.LoadConfig(configFile)
	require.NoError(t, err)

	// Verify all service URLs are loaded exactly as specified
	assert.Equal(t, expectedDIMPUrl, config.Services.DIMP.URL, "DIMP URL should be loaded correctly")
	assert.Equal(t, expectedFlatteningUrl, config.Services.Flattening.ServiceURL, "Flattening URL should be loaded correctly")
}

// TestConfigLoading_DIMPExperimentalV3 verifies the experimental v3 DIMP
// settings are loaded and default to empty.
func TestConfigLoading_DIMPExperimentalV3(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	jobsDir := filepath.Join(tmpDir, "jobs")
	_ = os.MkdirAll(jobsDir, 0755)

	configContent := `
services:
  dimp:
    url: "http://dimp.example.com:8080"
    experimental_v3:
      anonymization_config: "/etc/aether/anonymization.yaml"

pipeline:
  enabled_steps:
    - local_import
    - dimp

jobs_dir: "` + jobsDir + `"
`
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	config, err := services.LoadConfig(configFile)
	require.NoError(t, err)

	assert.Equal(t, "/etc/aether/anonymization.yaml", config.Services.DIMP.ExperimentalV3.AnonymizationConfig)

	defaults := models.DefaultConfig()
	assert.Empty(t, defaults.Services.DIMP.ExperimentalV3.AnonymizationConfig,
		"experimental v3 must be off by default")
}

// TestConfigLoading_RetrySettings verifies retry configuration is loaded
func TestConfigLoading_RetrySettings(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	jobsDir := filepath.Join(tmpDir, "jobs")
	_ = os.MkdirAll(jobsDir, 0755)

	configContent := `
services:
  dimp_url: ""

pipeline:
  enabled_steps:
    - local_import

retry:
  max_attempts: 7
  initial_backoff_ms: 2000
  max_backoff_ms: 60000

jobs_dir: "` + jobsDir + `"
`
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	config, err := services.LoadConfig(configFile)
	require.NoError(t, err)

	assert.Equal(t, 7, config.Retry.MaxAttempts, "MaxAttempts should be loaded")
	assert.Equal(t, int64(2000), config.Retry.InitialBackoffMs, "InitialBackoffMs should be loaded")
	assert.Equal(t, int64(60000), config.Retry.MaxBackoffMs, "MaxBackoffMs should be loaded")
}

// TestConfigLoading_JobsDirectory verifies jobs_dir is loaded
func TestConfigLoading_JobsDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	customJobsDir := filepath.Join(tmpDir, "custom_jobs_location")
	_ = os.MkdirAll(customJobsDir, 0755)

	configContent := `
services:
  dimp_url: ""

pipeline:
  enabled_steps:
    - local_import

retry:
  max_attempts: 5
  initial_backoff_ms: 1000
  max_backoff_ms: 30000

jobs_dir: "` + customJobsDir + `"
`
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	config, err := services.LoadConfig(configFile)
	require.NoError(t, err)

	assert.Equal(t, customJobsDir, config.JobsDir, "Custom jobs directory should be loaded")
}

// TestConfigLoading_EmptyServiceURLs verifies empty service URLs are preserved
func TestConfigLoading_EmptyServiceURLs(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	jobsDir := filepath.Join(tmpDir, "jobs")
	_ = os.MkdirAll(jobsDir, 0755)

	configContent := `
services:
  dimp_url: ""

pipeline:
  enabled_steps:
    - local_import

retry:
  max_attempts: 5
  initial_backoff_ms: 1000
  max_backoff_ms: 30000

jobs_dir: "` + jobsDir + `"
`
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	config, err := services.LoadConfig(configFile)
	require.NoError(t, err)

	assert.Empty(t, config.Services.DIMP.URL, "Empty DIMP URL should remain empty")
}

// TestConfigLoading_PartialServiceURLs verifies mixed empty and non-empty URLs
func TestConfigLoading_PartialServiceURLs(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	jobsDir := filepath.Join(tmpDir, "jobs")
	_ = os.MkdirAll(jobsDir, 0755)

	configContent := `
services:
  dimp:
    url: "http://localhost:32861/fhir"

pipeline:
  enabled_steps:
    - local_import
    - dimp

retry:
  max_attempts: 5
  initial_backoff_ms: 1000
  max_backoff_ms: 30000

jobs_dir: "` + jobsDir + `"
`
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	config, err := services.LoadConfig(configFile)
	require.NoError(t, err)

	assert.Equal(t, "http://localhost:32861/fhir", config.Services.DIMP.URL, "DIMP URL should be loaded")
}

// TestConfigLoading_NoConfigFile verifies error when specific config file doesn't exist
func TestConfigLoading_NoConfigFile(t *testing.T) {
	tmpDir := t.TempDir()
	nonExistentFile := filepath.Join(tmpDir, "does-not-exist.yaml")

	// Try to load non-existent config file - should error
	_, err := services.LoadConfig(nonExistentFile)
	require.Error(t, err, "Should error when specified config file doesn't exist")
	assert.Contains(t, err.Error(), "no such file or directory", "Error should indicate file not found")
}

// TestConfigLoading_StepOrder verifies steps are loaded in the order specified
func TestConfigLoading_StepOrder(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	jobsDir := filepath.Join(tmpDir, "jobs")
	_ = os.MkdirAll(jobsDir, 0755)

	// Deliberately specify steps in a specific order
	// Note: Steps that require services must have URLs configured
	configContent := `
services:
  dimp:
    url: "http://localhost:32861/fhir"
  flattening:
    service_url: "http://localhost:8000"
    lookup_path: "/tmp/lookup.json"

pipeline:
  enabled_steps:
    - local_import
    - validation
    - dimp
    - flattening

retry:
  max_attempts: 5
  initial_backoff_ms: 1000
  max_backoff_ms: 30000

jobs_dir: "` + jobsDir + `"
`
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	config, err := services.LoadConfig(configFile)
	require.NoError(t, err)

	// Verify steps are in the exact order specified in YAML
	require.Len(t, config.Pipeline.EnabledSteps, 4)
	assert.Equal(t, models.StepLocalImport, config.Pipeline.EnabledSteps[0])
	assert.Equal(t, models.StepValidation, config.Pipeline.EnabledSteps[1])
	assert.Equal(t, models.StepDIMP, config.Pipeline.EnabledSteps[2])
	assert.Equal(t, models.StepFlattening, config.Pipeline.EnabledSteps[3])
}

// TestConfigLoading_MinimalConfig verifies minimal valid config loads
func TestConfigLoading_MinimalConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	jobsDir := filepath.Join(tmpDir, "jobs")
	_ = os.MkdirAll(jobsDir, 0755)

	// Absolute minimal config
	configContent := `
pipeline:
  enabled_steps:
    - local_import

retry:
  max_attempts: 3
  initial_backoff_ms: 500
  max_backoff_ms: 5000

jobs_dir: "` + jobsDir + `"
`
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	config, err := services.LoadConfig(configFile)
	require.NoError(t, err, "Minimal config should load successfully")

	assert.Len(t, config.Pipeline.EnabledSteps, 1)
	assert.Equal(t, models.StepLocalImport, config.Pipeline.EnabledSteps[0])
	assert.Empty(t, config.Services.DIMP.URL)
}

// TestConfigValidation_InvalidDIMPUrl verifies invalid DIMP service URL is rejected
func TestConfigValidation_InvalidDIMPUrl(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	jobsDir := filepath.Join(tmpDir, "jobs")
	_ = os.MkdirAll(jobsDir, 0755)

	configContent := `
services:
  dimp:
    url: "http://[invalid:url"

pipeline:
  enabled_steps:
    - local_import

retry:
  max_attempts: 3
  initial_backoff_ms: 500
  max_backoff_ms: 10000

jobs_dir: "` + jobsDir + `"
`
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	// LoadConfig validates during loading, so it should fail with invalid URL
	config, err := services.LoadConfig(configFile)
	assert.Error(t, err, "Invalid DIMP URL should fail during config loading")
	assert.Nil(t, config)
	assert.Contains(t, err.Error(), "invalid dimp url")
}

// TestConfigValidation_DIMPExperimentalV3WithoutConfigPath verifies an
// experimental_v3 block without an anonymization config path loads. The DIMP
// step then uses the default endpoint.
func TestConfigValidation_DIMPExperimentalV3WithoutConfigPath(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	jobsDir := filepath.Join(tmpDir, "jobs")
	_ = os.MkdirAll(jobsDir, 0755)

	configContent := `
services:
  dimp:
    url: "http://localhost:32861"
    experimental_v3:

pipeline:
  enabled_steps:
    - local_import
    - dimp

jobs_dir: "` + jobsDir + `"
`
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	config, err := services.LoadConfig(configFile)
	require.NoError(t, err)
	assert.Empty(t, config.Services.DIMP.ExperimentalV3.AnonymizationConfig)
}

// Unit tests for TORCHConfig validation

func TestTORCHConfig_ValidateSuccess(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	jobsDir := filepath.Join(tmpDir, "jobs")
	_ = os.MkdirAll(jobsDir, 0755)

	configContent := `
services:
  dimp:
    url: ""
  torch:
    base_url: "http://localhost:8080"
    auth:
      username: "testuser"
      password: "testpass"
    extraction_timeout: PT30M
    polling_interval: PT5S
    max_polling_interval: PT30S

pipeline:
  enabled_steps:
    - local_import

retry:
  max_attempts: 5
  initial_backoff_ms: 1000
  max_backoff_ms: 30000

jobs_dir: "` + jobsDir + `"
`
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	config, err := services.LoadConfig(configFile)
	require.NoError(t, err, "Config with TORCH should load successfully")

	// Verify TORCH config is loaded
	assert.Equal(t, "http://localhost:8080", config.Services.TORCH.BaseURL)
	assert.Equal(t, models.AuthConfig{Username: "testuser", Password: "testpass"},
		config.Services.TORCH.Auth)
	assert.Equal(t, 30*time.Minute, config.Services.TORCH.ExtractionTimeout)
	assert.Equal(t, 5*time.Second, config.Services.TORCH.PollingInterval)
	assert.Equal(t, 30*time.Second, config.Services.TORCH.MaxPollingInterval)

	// Test validation passes with valid config
	err = config.Services.TORCH.Validate()
	assert.NoError(t, err, "Valid TORCH config should pass validation")
}

func TestTORCHConfig_DownloadStallTimeoutLoading(t *testing.T) {
	writeConfig := func(t *testing.T, torchExtra string) *models.ProjectConfig {
		tmpDir := t.TempDir()
		configFile := filepath.Join(tmpDir, "config.yaml")
		jobsDir := filepath.Join(tmpDir, "jobs")
		_ = os.MkdirAll(jobsDir, 0755)

		configContent := `
services:
  torch:
    base_url: "http://localhost:8080"
    username: "testuser"
    password: "testpass"
` + torchExtra + `
pipeline:
  enabled_steps:
    - local_import

jobs_dir: "` + jobsDir + `"
`
		require.NoError(t, os.WriteFile(configFile, []byte(configContent), 0644))
		config, err := services.LoadConfig(configFile)
		require.NoError(t, err)
		return config
	}

	t.Run("explicit value parses", func(t *testing.T) {
		config := writeConfig(t, "    download_stall_timeout: PT2M\n")
		assert.Equal(t, 2*time.Minute, config.Services.TORCH.DownloadStallTimeout)
	})

	t.Run("defaults when omitted", func(t *testing.T) {
		config := writeConfig(t, "")
		assert.Equal(t, 60*time.Second, config.Services.TORCH.DownloadStallTimeout)
	})
}

func TestTORCHConfig_ValidateMissingBaseURL(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	jobsDir := filepath.Join(tmpDir, "jobs")
	_ = os.MkdirAll(jobsDir, 0755)

	configContent := `
services:
  torch:
    base_url: ""
    username: "testuser"
    password: "testpass"

pipeline:
  enabled_steps:
    - local_import

retry:
  max_attempts: 5
  initial_backoff_ms: 1000
  max_backoff_ms: 30000

jobs_dir: "` + jobsDir + `"
`
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	// LoadConfig validates config, so missing base_url should cause error
	_, err = services.LoadConfig(configFile)
	assert.Error(t, err, "Missing base_url should fail validation")
	assert.Contains(t, err.Error(), "base_url")
}

func TestTORCHConfig_ValidateInvalidURL(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	jobsDir := filepath.Join(tmpDir, "jobs")
	_ = os.MkdirAll(jobsDir, 0755)

	configContent := `
services:
  torch:
    base_url: "not-a-valid-url"
    username: "testuser"
    password: "testpass"

pipeline:
  enabled_steps:
    - local_import

retry:
  max_attempts: 5
  initial_backoff_ms: 1000
  max_backoff_ms: 30000

jobs_dir: "` + jobsDir + `"
`
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	// LoadConfig validates config, invalid URL (missing scheme) should cause error
	_, err = services.LoadConfig(configFile)
	assert.Error(t, err, "Invalid URL should fail validation")
	assert.Contains(t, err.Error(), "http")
}

func TestTORCHConfig_ValidateMissingCredentials(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	jobsDir := filepath.Join(tmpDir, "jobs")
	_ = os.MkdirAll(jobsDir, 0755)

	configContent := `
services:
  torch:
    base_url: "http://localhost:8080"
    username: ""
    password: ""

pipeline:
  enabled_steps:
    - local_import

retry:
  max_attempts: 5
  initial_backoff_ms: 1000
  max_backoff_ms: 30000

jobs_dir: "` + jobsDir + `"
`
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	// Credentials are optional - should load successfully even without them
	config, err := services.LoadConfig(configFile)
	require.NoError(t, err, "Missing credentials should be allowed for TORCH")
	assert.Equal(t, "http://localhost:8080", config.Services.TORCH.BaseURL)
	assert.Equal(t, models.AuthConfig{}, config.Services.TORCH.EffectiveAuth())
}

func TestTORCHConfig_ValidateInvalidTimeout(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	jobsDir := filepath.Join(tmpDir, "jobs")
	_ = os.MkdirAll(jobsDir, 0755)

	configContent := `
services:
  torch:
    base_url: "http://localhost:8080"
    username: "testuser"
    password: "testpass"
    extraction_timeout: -1m

pipeline:
  enabled_steps:
    - local_import

retry:
  max_attempts: 5
  initial_backoff_ms: 1000
  max_backoff_ms: 30000

jobs_dir: "` + jobsDir + `"
`
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	// LoadConfig validates config, negative timeout should cause error
	_, err = services.LoadConfig(configFile)
	assert.Error(t, err, "Zero timeout should fail validation")
	assert.Contains(t, err.Error(), "timeout")
}

func TestTORCHConfig_ValidateNegativeDownloadStallTimeout(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	jobsDir := filepath.Join(tmpDir, "jobs")
	_ = os.MkdirAll(jobsDir, 0755)

	configContent := `
services:
  torch:
    base_url: "http://localhost:8080"
    username: "testuser"
    password: "testpass"
    download_stall_timeout: -1s

pipeline:
  enabled_steps:
    - local_import

retry:
  max_attempts: 5
  initial_backoff_ms: 1000
  max_backoff_ms: 30000

jobs_dir: "` + jobsDir + `"
`
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	_, err = services.LoadConfig(configFile)
	assert.Error(t, err, "Negative download_stall_timeout should fail validation")
	assert.Contains(t, err.Error(), "download_stall_timeout")
}

func TestTORCHConfig_ValidateInvalidPollingInterval(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	jobsDir := filepath.Join(tmpDir, "jobs")
	_ = os.MkdirAll(jobsDir, 0755)

	configContent := `
services:
  torch:
    base_url: "http://localhost:8080"
    username: "testuser"
    password: "testpass"
    polling_interval: -1s

pipeline:
  enabled_steps:
    - local_import

retry:
  max_attempts: 5
  initial_backoff_ms: 1000
  max_backoff_ms: 30000

jobs_dir: "` + jobsDir + `"
`
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	// LoadConfig validates config, invalid polling interval should cause error
	_, err = services.LoadConfig(configFile)
	assert.Error(t, err, "Invalid polling interval should fail validation")
	assert.Contains(t, err.Error(), "polling_interval")
}

func TestTORCHConfig_ValidateMaxPollingLessThanMin(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	jobsDir := filepath.Join(tmpDir, "jobs")
	_ = os.MkdirAll(jobsDir, 0755)

	configContent := `
services:
  torch:
    base_url: "http://localhost:8080"
    username: "testuser"
    password: "testpass"
    polling_interval: PT10S
    max_polling_interval: PT5S

pipeline:
  enabled_steps:
    - local_import

retry:
  max_attempts: 5
  initial_backoff_ms: 1000
  max_backoff_ms: 30000

jobs_dir: "` + jobsDir + `"
`
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	// LoadConfig validates config, max < min should cause error
	_, err = services.LoadConfig(configFile)
	assert.Error(t, err, "Max polling less than min should fail validation")
	assert.Contains(t, err.Error(), "max_polling_interval")
}

func TestTORCHConfig_WithDefaults(t *testing.T) {
	// Test that TORCH config uses sensible defaults
	config := models.DefaultConfig()

	// Verify TORCH section exists with defaults
	assert.Equal(t, "", config.Services.TORCH.BaseURL, "BaseURL should default to empty")
	assert.Equal(t, models.AuthConfig{}, config.Services.TORCH.Auth, "Auth should default to empty")
	assert.Equal(t, 30*time.Minute, config.Services.TORCH.ExtractionTimeout, "Should default to 30 minutes")
	assert.Equal(t, 5*time.Second, config.Services.TORCH.PollingInterval, "Should default to 5 seconds")
	assert.Equal(t, 30*time.Second, config.Services.TORCH.MaxPollingInterval, "Should default to 30 seconds")
	assert.Equal(t, 10, config.Services.TORCH.FileReadyRetries, "Should default to 10 retries")
	assert.Equal(t, 10*time.Second, config.Services.TORCH.FileReadyInterval, "Should default to 10 seconds")
}

func TestTORCHConfig_FileReadySettings(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	jobsDir := filepath.Join(tmpDir, "jobs")
	_ = os.MkdirAll(jobsDir, 0755)

	configContent := `
services:
  torch:
    base_url: "http://localhost:8080"
    auth:
      username: "testuser"
      password: "testpass"
    extraction_timeout: PT30M
    polling_interval: PT5S
    max_polling_interval: PT30S
    file_ready_retries: 5
    file_ready_interval: PT3S

pipeline:
  enabled_steps:
    - local_import

retry:
  max_attempts: 5
  initial_backoff_ms: 1000
  max_backoff_ms: 30000

jobs_dir: "` + jobsDir + `"
`
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	config, err := services.LoadConfig(configFile)
	require.NoError(t, err, "Config should load without error")

	assert.Equal(t, 5, config.Services.TORCH.FileReadyRetries, "FileReadyRetries should be loaded")
	assert.Equal(t, 3*time.Second, config.Services.TORCH.FileReadyInterval, "FileReadyInterval should be loaded")
}

func TestTORCHConfig_FileReadySettingsDefault(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	jobsDir := filepath.Join(tmpDir, "jobs")
	_ = os.MkdirAll(jobsDir, 0755)

	// Config without file_ready settings — should get defaults
	configContent := `
services:
  torch:
    base_url: "http://localhost:8080"
    username: "testuser"
    password: "testpass"

pipeline:
  enabled_steps:
    - local_import

retry:
  max_attempts: 5
  initial_backoff_ms: 1000
  max_backoff_ms: 30000

jobs_dir: "` + jobsDir + `"
`
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	config, err := services.LoadConfig(configFile)
	require.NoError(t, err, "Config should load without error")

	assert.Equal(t, 10, config.Services.TORCH.FileReadyRetries, "FileReadyRetries should default to 10")
	assert.Equal(t, 10*time.Second, config.Services.TORCH.FileReadyInterval, "FileReadyInterval should default to 10")
}

func TestTORCHConfig_ValidateMalformedURL(t *testing.T) {
	// Test URL that causes url.Parse to fail (contains invalid characters)
	config := models.TORCHConfig{
		BaseURL:            "http://[invalid",
		Username:           "user",
		Password:           "pass",
		ExtractionTimeout:  30 * time.Minute,
		PollingInterval:    5 * time.Second,
		MaxPollingInterval: 30 * time.Second,
	}

	err := config.Validate()
	assert.Error(t, err, "Malformed URL should fail validation")
	assert.Contains(t, err.Error(), "invalid TORCH base_url")
}

func TestTORCHConfig_ValidateLargePollingInterval(t *testing.T) {
	// Arbitrarily large polling intervals are now allowed
	config := models.TORCHConfig{
		BaseURL:            "http://localhost:8080",
		Username:           "user",
		Password:           "pass",
		ExtractionTimeout:  30 * time.Minute,
		PollingInterval:    5 * time.Minute,
		MaxPollingInterval: 5 * time.Minute,
	}

	err := config.Validate()
	assert.NoError(t, err, "Large polling interval should be valid")
}

func TestTORCHConfig_ValidateEdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		config  models.TORCHConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "Polling interval exactly 1 - valid",
			config: models.TORCHConfig{
				BaseURL:            "http://localhost:8080",
				ExtractionTimeout:  1 * time.Minute,
				PollingInterval:    1 * time.Second,
				MaxPollingInterval: 1 * time.Second,
			},
			wantErr: false,
		},
		{
			name: "Polling interval exactly 60 - valid",
			config: models.TORCHConfig{
				BaseURL:            "http://localhost:8080",
				ExtractionTimeout:  1 * time.Minute,
				PollingInterval:    60 * time.Second,
				MaxPollingInterval: 60 * time.Second,
			},
			wantErr: false,
		},
		{
			name: "Max polling equals min polling - valid",
			config: models.TORCHConfig{
				BaseURL:            "http://localhost:8080",
				ExtractionTimeout:  1 * time.Minute,
				PollingInterval:    10 * time.Second,
				MaxPollingInterval: 10 * time.Second,
			},
			wantErr: false,
		},
		{
			name: "HTTPS URL - valid",
			config: models.TORCHConfig{
				BaseURL:            "https://torch.example.com",
				ExtractionTimeout:  30 * time.Minute,
				PollingInterval:    5 * time.Second,
				MaxPollingInterval: 30 * time.Second,
			},
			wantErr: false,
		},
		{
			name: "FTP URL - invalid scheme",
			config: models.TORCHConfig{
				BaseURL:            "ftp://localhost",
				ExtractionTimeout:  30 * time.Minute,
				PollingInterval:    5 * time.Second,
				MaxPollingInterval: 30 * time.Second,
			},
			wantErr: true,
			errMsg:  "must use http or https scheme",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestGetServiceURL verifies service URL retrieval for different steps
func TestGetServiceURL(t *testing.T) {
	config := models.ProjectConfig{
		Services: models.ServiceConfig{
			DIMP: models.DIMPConfig{
				URL: "http://dimp.example.com:8080",
			},
			Flattening: models.FlatteningConfig{
				ServiceURL: "http://flattener.example.com:8000",
			},
			Send: models.SendConfig{
				URL:    "http://fhir.example.com:8080/fhir",
				SendAs: models.SendModeTransferLoad,
				Transfer: models.TransferConfig{
					ProjectIdentifier:      "TEST-PROJECT",
					OrganizationIdentifier: "test.org",
				},
			},
		},
	}

	// Test DIMP URL
	assert.Equal(t, "http://dimp.example.com:8080", config.Services.GetServiceURL(models.StepDIMP))

	// Test Flattening URL
	assert.Equal(t, "http://flattener.example.com:8000", config.Services.GetServiceURL(models.StepFlattening))

	// Test Send URL
	assert.Equal(t, "http://fhir.example.com:8080/fhir", config.Services.GetServiceURL(models.StepSend))

	// Test unknown step
	assert.Equal(t, "", config.Services.GetServiceURL(models.StepLocalImport))
	assert.Equal(t, "", config.Services.GetServiceURL(models.StepValidation))
}

// TestHasServiceURLFlattening verifies HasServiceURL for flattening step
func TestHasServiceURLFlattening(t *testing.T) {
	t.Run("flattening with service URL", func(t *testing.T) {
		config := models.ServiceConfig{
			Flattening: models.FlatteningConfig{
				ServiceURL: "http://flattener.example.com:8000",
			},
		}
		assert.True(t, config.HasServiceURL(models.StepFlattening))
	})

	t.Run("flattening without service URL", func(t *testing.T) {
		config := models.ServiceConfig{
			Flattening: models.FlatteningConfig{
				ServiceURL: "",
			},
		}
		assert.False(t, config.HasServiceURL(models.StepFlattening))
	})
}

// TestGetNextStep verifies pipeline step progression
func TestGetNextStep(t *testing.T) {
	config := models.ProjectConfig{
		Pipeline: models.PipelineConfig{
			EnabledSteps: []models.StepName{
				models.StepLocalImport,
				models.StepValidation,
				models.StepDIMP,
				models.StepFlattening,
			},
		},
	}

	// Test getting next step from import
	assert.Equal(t, models.StepValidation, config.Pipeline.GetNextStep(models.StepLocalImport))

	// Test getting next step from validation
	assert.Equal(t, models.StepDIMP, config.Pipeline.GetNextStep(models.StepValidation))

	// Test getting next step from DIMP
	assert.Equal(t, models.StepFlattening, config.Pipeline.GetNextStep(models.StepDIMP))

	// Test getting next step from last step (should return empty)
	assert.Equal(t, models.StepName(""), config.Pipeline.GetNextStep(models.StepFlattening))
}

// TestHasServiceURL verifies service URL presence checks
func TestHasServiceURL(t *testing.T) {
	testCases := []struct {
		name   string
		config models.ServiceConfig
		step   models.StepName
		hasURL bool
	}{
		{
			name: "DIMP with URL",
			config: models.ServiceConfig{
				DIMP: models.DIMPConfig{URL: "http://dimp.example.com"},
			},
			step:   models.StepDIMP,
			hasURL: true,
		},
		{
			name: "DIMP without URL",
			config: models.ServiceConfig{
				DIMP: models.DIMPConfig{URL: ""},
			},
			step:   models.StepDIMP,
			hasURL: false,
		},
		{
			name:   "Import step (no external service needed)",
			config: models.ServiceConfig{},
			step:   models.StepLocalImport,
			hasURL: true,
		},
		{
			name:   "Validation step (no external service needed)",
			config: models.ServiceConfig{},
			step:   models.StepValidation,
			hasURL: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.hasURL, tc.config.HasServiceURL(tc.step))
		})
	}
}

// TestConfigValidation_MissingServiceURLForEnabledStep verifies error when enabled step lacks service URL
func TestConfigValidation_MissingServiceURLForEnabledStep(t *testing.T) {
	testCases := []struct {
		name      string
		config    models.ProjectConfig
		errorText string
	}{
		{
			name: "DIMP enabled without URL",
			config: models.ProjectConfig{
				Services: models.ServiceConfig{
					DIMP: models.DIMPConfig{URL: ""},
				},
				Pipeline: models.PipelineConfig{
					EnabledSteps: []models.StepName{
						models.StepLocalImport,
						models.StepDIMP,
					},
				},
				Retry: models.RetryConfig{
					MaxAttempts:      5,
					InitialBackoffMs: 1000,
					MaxBackoffMs:     30000,
				},
				JobsDir: "/tmp/jobs",
			},
			errorText: "service URL required for enabled step 'dimp'",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.config.Validate()
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tc.errorText)
		})
	}
}

// TestValidateServiceConnectivity_AllServicesAvailable verifies connectivity check with all services available
func TestValidateServiceConnectivity_AllServicesAvailable(t *testing.T) {
	// Create mock servers for each service
	dimpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer dimpServer.Close()

	config := models.ProjectConfig{
		Services: models.ServiceConfig{
			DIMP: models.DIMPConfig{
				URL: dimpServer.URL,
			},
		},
		Pipeline: models.PipelineConfig{
			EnabledSteps: []models.StepName{
				models.StepLocalImport,
				models.StepDIMP,
			},
		},
		Retry: models.RetryConfig{
			MaxAttempts:      5,
			InitialBackoffMs: 1000,
			MaxBackoffMs:     30000,
		},
		JobsDir: "/tmp/jobs",
	}

	// ValidateServiceConnectivity should succeed when all services are reachable
	err := config.ValidateServiceConnectivity(nil)
	assert.NoError(t, err, "All services should be reachable")
}

// TestValidateServiceConnectivity_WithCustomTransport verifies connectivity check uses the provided transport
func TestValidateServiceConnectivity_WithCustomTransport(t *testing.T) {
	dimpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer dimpServer.Close()

	config := models.ProjectConfig{
		Services: models.ServiceConfig{
			DIMP: models.DIMPConfig{
				URL: dimpServer.URL,
			},
		},
		Pipeline: models.PipelineConfig{
			EnabledSteps: []models.StepName{
				models.StepLocalImport,
				models.StepDIMP,
			},
		},
		Retry: models.RetryConfig{
			MaxAttempts:      5,
			InitialBackoffMs: 1000,
			MaxBackoffMs:     30000,
		},
		JobsDir: "/tmp/jobs",
	}

	// Pass a non-nil transport to exercise the transport injection branch
	transport := &http.Transport{}
	err := config.ValidateServiceConnectivity(transport)
	assert.NoError(t, err, "Should succeed with custom transport")
}

// TestValidateServiceConnectivity_DIMMServiceUnreachable verifies error when DIMP service is unreachable
func TestValidateServiceConnectivity_DIMMServiceUnreachable(t *testing.T) {
	config := models.ProjectConfig{
		Services: models.ServiceConfig{
			DIMP: models.DIMPConfig{
				URL: "http://localhost:9999", // Unreachable
			},
		},
		Pipeline: models.PipelineConfig{
			EnabledSteps: []models.StepName{
				models.StepLocalImport,
				models.StepDIMP,
			},
		},
		Retry: models.RetryConfig{
			MaxAttempts:      5,
			InitialBackoffMs: 1000,
			MaxBackoffMs:     30000,
		},
		JobsDir: "/tmp/jobs",
	}

	// ValidateServiceConnectivity should fail when DIMP is unreachable
	err := config.ValidateServiceConnectivity(nil)
	assert.Error(t, err, "Should fail when DIMP service is unreachable")
	assert.Contains(t, err.Error(), "DIMP")
}

// TestValidateServiceConnectivity_SendServiceAvailable verifies connectivity check with Send service available
func TestValidateServiceConnectivity_SendServiceAvailable(t *testing.T) {
	sendServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer sendServer.Close()

	config := models.ProjectConfig{
		Services: models.ServiceConfig{
			Send: models.SendConfig{
				URL:    sendServer.URL,
				SendAs: models.SendModeTransferLoad,
				Transfer: models.TransferConfig{
					ProjectIdentifier:      "TEST-PROJECT",
					OrganizationIdentifier: "test.org",
				},
			},
		},
		Pipeline: models.PipelineConfig{
			EnabledSteps: []models.StepName{
				models.StepLocalImport,
				models.StepSend,
			},
		},
		Retry: models.RetryConfig{
			MaxAttempts:      5,
			InitialBackoffMs: 1000,
			MaxBackoffMs:     30000,
		},
		JobsDir: "/tmp/jobs",
	}

	// ValidateServiceConnectivity should succeed when Send service is reachable
	err := config.ValidateServiceConnectivity(nil)
	assert.NoError(t, err, "Send service should be reachable")
}

// TestValidateServiceConnectivity_SendServiceUnreachable verifies error when Send service is unreachable
func TestValidateServiceConnectivity_SendServiceUnreachable(t *testing.T) {
	config := models.ProjectConfig{
		Services: models.ServiceConfig{
			Send: models.SendConfig{
				URL:    "http://localhost:59998", // Unreachable
				SendAs: models.SendModeTransferLoad,
				Transfer: models.TransferConfig{
					ProjectIdentifier:      "TEST-PROJECT",
					OrganizationIdentifier: "test.org",
				},
			},
		},
		Pipeline: models.PipelineConfig{
			EnabledSteps: []models.StepName{
				models.StepLocalImport,
				models.StepSend,
			},
		},
		Retry: models.RetryConfig{
			MaxAttempts:      5,
			InitialBackoffMs: 1000,
			MaxBackoffMs:     30000,
		},
		JobsDir: "/tmp/jobs",
	}

	// ValidateServiceConnectivity should fail when Send service is unreachable
	err := config.ValidateServiceConnectivity(nil)
	assert.Error(t, err, "Should fail when Send service is unreachable")
	assert.Contains(t, err.Error(), "Send")
}

// TestValidateServiceConnectivity_ValidationServiceAvailable verifies the validation service
// connectivity branch in ValidateServiceConnectivity (covers the StepValidation case).
func TestValidateServiceConnectivity_ValidationServiceAvailable(t *testing.T) {
	validationServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer validationServer.Close()

	config := models.ProjectConfig{
		Services: models.ServiceConfig{
			Validation: models.ValidationConfig{
				URL: validationServer.URL,
			},
		},
		Pipeline: models.PipelineConfig{
			EnabledSteps: []models.StepName{models.StepLocalImport, models.StepValidation},
		},
		Retry: models.RetryConfig{
			MaxAttempts:      3,
			InitialBackoffMs: 500,
			MaxBackoffMs:     5000,
		},
		JobsDir: "/tmp/jobs",
	}

	err := config.ValidateServiceConnectivity(nil)
	assert.NoError(t, err, "Validation service should be reachable")
}

// TestValidateServiceConnectivity_ValidationServiceUnreachable verifies error when validation is unreachable
func TestValidateServiceConnectivity_ValidationServiceUnreachable(t *testing.T) {
	config := models.ProjectConfig{
		Services: models.ServiceConfig{
			Validation: models.ValidationConfig{
				URL: "http://localhost:59997", // Unreachable
			},
		},
		Pipeline: models.PipelineConfig{
			EnabledSteps: []models.StepName{models.StepLocalImport, models.StepValidation},
		},
		Retry: models.RetryConfig{
			MaxAttempts:      3,
			InitialBackoffMs: 500,
			MaxBackoffMs:     5000,
		},
		JobsDir: "/tmp/jobs",
	}

	err := config.ValidateServiceConnectivity(nil)
	assert.Error(t, err, "Should fail when validation service is unreachable")
	assert.Contains(t, err.Error(), "Validation")
}

// TestValidateServiceConnectivity_S3ModeSkipsHTTPCheck verifies that S3 upload mode
// skips the HTTP connectivity check (S3 requires AWS Signature V4, not plain HTTP HEAD).
func TestValidateServiceConnectivity_S3ModeSkipsHTTPCheck(t *testing.T) {
	config := models.ProjectConfig{
		Services: models.ServiceConfig{
			Send: models.SendConfig{
				SendAs: models.SendModeS3Upload,
				S3: models.S3Config{
					Region:          "eu-central-1",
					Bucket:          "test-bucket",
					AccessKeyID:     "AKIAIOSFODNN7EXAMPLE",
					SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
				},
			},
		},
		Pipeline: models.PipelineConfig{
			EnabledSteps: []models.StepName{
				models.StepLocalImport,
				models.StepSend,
			},
		},
		Retry: models.RetryConfig{
			MaxAttempts:      5,
			InitialBackoffMs: 1000,
			MaxBackoffMs:     30000,
		},
		JobsDir: "/tmp/jobs",
	}

	// S3 mode should skip the HTTP connectivity check entirely
	// (no URL to check, and S3 uses AWS SDK connectivity)
	err := config.ValidateServiceConnectivity(nil)
	assert.NoError(t, err, "S3 mode should skip connectivity check")
}

// TestConfigLoading_BundleSplitThreshold verifies bundle_split_threshold_mb is loaded correctly
func TestConfigLoading_BundleSplitThreshold(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	jobsDir := filepath.Join(tmpDir, "jobs")
	_ = os.MkdirAll(jobsDir, 0755)

	configContent := `
services:
  dimp:
    url: "http://localhost:32861/fhir"
    bundle_split_threshold_mb: 1

pipeline:
  enabled_steps:
    - local_import
    - dimp

retry:
  max_attempts: 5
  initial_backoff_ms: 1000
  max_backoff_ms: 30000

jobs_dir: "` + jobsDir + `"
`
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	config, err := services.LoadConfig(configFile)
	require.NoError(t, err, "Config should load without error")

	// Verify bundle split threshold is loaded correctly from DIMP config
	assert.Equal(t, 1, config.Services.DIMP.BundleSplitThresholdMB, "bundle_split_threshold_mb should be 1")
}

// TestConfigLoading_BundleSplitThresholdDefault verifies default value when not specified
func TestConfigLoading_BundleSplitThresholdDefault(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	jobsDir := filepath.Join(tmpDir, "jobs")
	_ = os.MkdirAll(jobsDir, 0755)

	configContent := `
services:
  dimp:
    url: "http://localhost:32861/fhir"

pipeline:
  enabled_steps:
    - local_import
    - dimp

retry:
  max_attempts: 5
  initial_backoff_ms: 1000
  max_backoff_ms: 30000

jobs_dir: "` + jobsDir + `"
`
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	config, err := services.LoadConfig(configFile)
	require.NoError(t, err, "Config should load without error")

	// Verify bundle split threshold defaults to 10MB in DIMP config
	assert.Equal(t, 10, config.Services.DIMP.BundleSplitThresholdMB, "bundle_split_threshold_mb should default to 10")
}

// TestConfigLoading_DIMPAuth verifies the dimp auth block loads from YAML.
func TestConfigLoading_DIMPAuth(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	jobsDir := filepath.Join(tmpDir, "jobs")
	_ = os.MkdirAll(jobsDir, 0755)

	configContent := `
services:
  dimp:
    url: "http://localhost:32861"
    auth:
      api_key: "dimp-secret"
      api_key_header: "X-Gateway-Key"

pipeline:
  enabled_steps:
    - local_import
    - dimp

jobs_dir: "` + jobsDir + `"
`
	require.NoError(t, os.WriteFile(configFile, []byte(configContent), 0644))

	config, err := services.LoadConfig(configFile)
	require.NoError(t, err, "Config should load without error")

	assert.Equal(t, "dimp-secret", config.Services.DIMP.Auth.APIKey)
	assert.Equal(t, "X-Gateway-Key", config.Services.DIMP.Auth.APIKeyHeader)
	assert.Equal(t, 10, config.Services.DIMP.BundleSplitThresholdMB, "default survives the auth overlay")
}

// TestConfigLoading_DIMPAuthIncomplete verifies LoadConfig refuses an
// inconsistent dimp auth block, not only a struct built in a test.
func TestConfigLoading_DIMPAuthIncomplete(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	jobsDir := filepath.Join(tmpDir, "jobs")
	_ = os.MkdirAll(jobsDir, 0755)

	configContent := `
services:
  dimp:
    url: "http://localhost:32861"
    auth:
      username: "dimp-user"

pipeline:
  enabled_steps:
    - local_import
    - dimp

jobs_dir: "` + jobsDir + `"
`
	require.NoError(t, os.WriteFile(configFile, []byte(configContent), 0644))

	config, err := services.LoadConfig(configFile)

	require.Error(t, err)
	assert.Nil(t, config)
	assert.Contains(t, err.Error(), "dimp auth: password is required")
}

// TestConfigLoading_CompressionDefault verifies compression defaults to enabled=true
func TestConfigLoading_CompressionDefault(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	jobsDir := filepath.Join(tmpDir, "jobs")
	_ = os.MkdirAll(jobsDir, 0755)

	// Config without compression section - should default to enabled
	configContent := `
pipeline:
  enabled_steps:
    - local_import

retry:
  max_attempts: 5
  initial_backoff_ms: 1000
  max_backoff_ms: 30000

jobs_dir: "` + jobsDir + `"
`
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	config, err := services.LoadConfig(configFile)
	require.NoError(t, err, "Config should load without error")

	// Verify compression defaults: enabled=true, level="default"
	assert.True(t, config.Compression.Enabled, "Compression should default to enabled")
	assert.Equal(t, "default", config.Compression.Level, "Compression level should default to 'default'")
}

// TestConfigLoading_CompressionExplicitlyEnabled verifies compression settings are loaded
func TestConfigLoading_CompressionExplicitlyEnabled(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	jobsDir := filepath.Join(tmpDir, "jobs")
	_ = os.MkdirAll(jobsDir, 0755)

	configContent := `
pipeline:
  enabled_steps:
    - local_import

compression:
  enabled: true
  level: "better"

retry:
  max_attempts: 5
  initial_backoff_ms: 1000
  max_backoff_ms: 30000

jobs_dir: "` + jobsDir + `"
`
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	config, err := services.LoadConfig(configFile)
	require.NoError(t, err, "Config should load without error")

	assert.True(t, config.Compression.Enabled, "Compression should be enabled")
	assert.Equal(t, "better", config.Compression.Level, "Compression level should be 'better'")
}

// TestConfigLoading_CompressionDisabled verifies compression can be explicitly disabled
func TestConfigLoading_CompressionDisabled(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	jobsDir := filepath.Join(tmpDir, "jobs")
	_ = os.MkdirAll(jobsDir, 0755)

	configContent := `
pipeline:
  enabled_steps:
    - local_import

compression:
  enabled: false

retry:
  max_attempts: 5
  initial_backoff_ms: 1000
  max_backoff_ms: 30000

jobs_dir: "` + jobsDir + `"
`
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	config, err := services.LoadConfig(configFile)
	require.NoError(t, err, "Config should load without error")

	assert.False(t, config.Compression.Enabled, "Compression should be disabled when explicitly set to false")
	assert.Equal(t, "default", config.Compression.Level, "Compression level should default to 'default'")
}

// TestConfigLoading_CompressionAllLevels verifies all compression levels can be loaded
func TestConfigLoading_CompressionAllLevels(t *testing.T) {
	levels := []string{"fastest", "default", "better", "best"}

	for _, level := range levels {
		t.Run(level, func(t *testing.T) {
			tmpDir := t.TempDir()
			configFile := filepath.Join(tmpDir, "config.yaml")
			jobsDir := filepath.Join(tmpDir, "jobs")
			_ = os.MkdirAll(jobsDir, 0755)

			configContent := `
pipeline:
  enabled_steps:
    - local_import

compression:
  enabled: true
  level: "` + level + `"

retry:
  max_attempts: 5
  initial_backoff_ms: 1000
  max_backoff_ms: 30000

jobs_dir: "` + jobsDir + `"
`
			err := os.WriteFile(configFile, []byte(configContent), 0644)
			require.NoError(t, err)

			config, err := services.LoadConfig(configFile)
			require.NoError(t, err, "Config should load without error")

			assert.True(t, config.Compression.Enabled)
			assert.Equal(t, level, config.Compression.Level)
		})
	}
}

// TestConfigLoading_CRTDLPreprocessing verifies CRTDL preprocessing configuration is loaded
func TestConfigLoading_CRTDLPreprocessing(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	jobsDir := filepath.Join(tmpDir, "jobs")
	_ = os.MkdirAll(jobsDir, 0755)

	configContent := `
services:
  torch:
    base_url: "http://localhost:8080"
  crtdl_preprocessing:
    enabled: true
    enrichments:
      - group_reference: "https://www.example.org/fhir/StructureDefinition/Patient"
        add_group_if_not_exists: false
        attributes_to_add:
          - attribute_ref: "Patient.identifier:PseudonymisierterIdentifier"
            must_have: true

pipeline:
  enabled_steps:
    - torch

retry:
  max_attempts: 5
  initial_backoff_ms: 1000
  max_backoff_ms: 30000

jobs_dir: "` + jobsDir + `"
`
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	config, err := services.LoadConfig(configFile)
	require.NoError(t, err, "Config should load without error")

	// Verify CRTDL preprocessing is loaded
	assert.True(t, config.Services.CRTDLPreprocessing.Enabled, "CRTDL preprocessing should be enabled")
	require.Len(t, config.Services.CRTDLPreprocessing.Enrichments, 1, "Should have 1 enrichment")

	enrichment := config.Services.CRTDLPreprocessing.Enrichments[0]
	assert.Equal(t, "https://www.example.org/fhir/StructureDefinition/Patient", enrichment.GroupReference)
	assert.Nil(t, enrichment.CreateIfNotExists, "CreateIfNotExists should be nil when not configured")
	require.Len(t, enrichment.AttributesToAdd, 1, "Should have 1 attribute to add")
	assert.Equal(t, "Patient.identifier:PseudonymisierterIdentifier", enrichment.AttributesToAdd[0].AttributeRef)
	assert.True(t, enrichment.AttributesToAdd[0].MustHave)
}

// TestConfigLoading_CRTDLPreprocessingDisabled verifies disabled CRTDL preprocessing
func TestConfigLoading_CRTDLPreprocessingDisabled(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	jobsDir := filepath.Join(tmpDir, "jobs")
	_ = os.MkdirAll(jobsDir, 0755)

	configContent := `
services:
  torch:
    base_url: "http://localhost:8080"
  crtdl_preprocessing:
    enabled: false

pipeline:
  enabled_steps:
    - torch

retry:
  max_attempts: 5
  initial_backoff_ms: 1000
  max_backoff_ms: 30000

jobs_dir: "` + jobsDir + `"
`
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	config, err := services.LoadConfig(configFile)
	require.NoError(t, err, "Config should load without error")

	assert.False(t, config.Services.CRTDLPreprocessing.Enabled, "CRTDL preprocessing should be disabled")
}

// TestConfigLoading_CRTDLPreprocessingNotSet verifies default when CRTDL preprocessing is not set
func TestConfigLoading_CRTDLPreprocessingNotSet(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	jobsDir := filepath.Join(tmpDir, "jobs")
	_ = os.MkdirAll(jobsDir, 0755)

	configContent := `
services:
  torch:
    base_url: "http://localhost:8080"

pipeline:
  enabled_steps:
    - torch

retry:
  max_attempts: 5
  initial_backoff_ms: 1000
  max_backoff_ms: 30000

jobs_dir: "` + jobsDir + `"
`
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	config, err := services.LoadConfig(configFile)
	require.NoError(t, err, "Config should load without error")

	// When not set, should default to disabled
	assert.False(t, config.Services.CRTDLPreprocessing.Enabled, "CRTDL preprocessing should default to disabled")
}

// TestConfigLoading_SendConfig verifies that send service configuration
// is properly loaded from YAML config file
func TestConfigLoading_SendConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	jobsDir := filepath.Join(tmpDir, "jobs")
	_ = os.MkdirAll(jobsDir, 0755)

	// Mock server for connectivity validation
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer mockServer.Close()

	configContent := `
services:
  send:
    url: "` + mockServer.URL + `"
    send_as: "transfer_load"
    transfer:
      project_identifier: "MII-TEST-PROJECT"
      organization_identifier: "test.hospital.de"

pipeline:
  enabled_steps:
    - local_import
    - send

retry:
  max_attempts: 5
  initial_backoff_ms: 1000
  max_backoff_ms: 30000

jobs_dir: "` + jobsDir + `"
`
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	config, err := services.LoadConfig(configFile)
	require.NoError(t, err, "Config should load without error")

	assert.Equal(t, mockServer.URL, config.Services.Send.URL)
	assert.Equal(t, "MII-TEST-PROJECT", config.Services.Send.Transfer.ProjectIdentifier)
	assert.Equal(t, "test.hospital.de", config.Services.Send.Transfer.OrganizationIdentifier)

	// Verify send step is in enabled steps
	assert.Contains(t, config.Pipeline.EnabledSteps, models.StepSend)
}

// TestConfigLoading_SendConfigWithBasicAuth verifies that Basic Auth configuration is loaded
func TestConfigLoading_SendConfigWithBasicAuth(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	jobsDir := filepath.Join(tmpDir, "jobs")
	_ = os.MkdirAll(jobsDir, 0755)

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer mockServer.Close()

	configContent := `
services:
  send:
    url: "` + mockServer.URL + `"
    send_as: "transfer_load"
    auth:
      username: "testuser"
      password: "testpass"
    transfer:
      project_identifier: "MII-TEST-PROJECT"
      organization_identifier: "test.hospital.de"

pipeline:
  enabled_steps:
    - local_import
    - send

retry:
  max_attempts: 5
  initial_backoff_ms: 1000
  max_backoff_ms: 30000

jobs_dir: "` + jobsDir + `"
`
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	config, err := services.LoadConfig(configFile)
	require.NoError(t, err, "Config should load without error")

	assert.Equal(t, "testuser", config.Services.Send.Auth.Username)
	assert.Equal(t, "testpass", config.Services.Send.Auth.Password)
	assert.Equal(t, models.SendAuthBasic, config.Services.Send.GetAuthType())
}

// TestConfigLoading_SendConfigWithOAuth2 verifies that OAuth2 configuration is loaded
func TestConfigLoading_SendConfigWithOAuth2(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	jobsDir := filepath.Join(tmpDir, "jobs")
	_ = os.MkdirAll(jobsDir, 0755)

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer mockServer.Close()

	configContent := `
services:
  send:
    url: "` + mockServer.URL + `"
    send_as: "transfer_load"
    auth:
      oauth_issuer_uri: "https://auth.example.com/realms/test"
      oauth_client_id: "my-client"
      oauth_client_secret: "my-secret"
    transfer:
      project_identifier: "MII-TEST-PROJECT"
      organization_identifier: "test.hospital.de"

pipeline:
  enabled_steps:
    - local_import
    - send

retry:
  max_attempts: 5
  initial_backoff_ms: 1000
  max_backoff_ms: 30000

jobs_dir: "` + jobsDir + `"
`
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	config, err := services.LoadConfig(configFile)
	require.NoError(t, err, "Config should load without error")

	assert.Equal(t, "https://auth.example.com/realms/test", config.Services.Send.Auth.OAuthIssuerURI)
	assert.Equal(t, "my-client", config.Services.Send.Auth.OAuthClientID)
	assert.Equal(t, "my-secret", config.Services.Send.Auth.OAuthClientSecret)
	assert.Equal(t, models.SendAuthOAuth2, config.Services.Send.GetAuthType())
}

// TestConfigLoading_SendConfigNoAuth verifies that no-auth configuration works
func TestConfigLoading_SendConfigNoAuth(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	jobsDir := filepath.Join(tmpDir, "jobs")
	_ = os.MkdirAll(jobsDir, 0755)

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer mockServer.Close()

	configContent := `
services:
  send:
    url: "` + mockServer.URL + `"
    send_as: "transfer_load"
    transfer:
      project_identifier: "MII-TEST-PROJECT"
      organization_identifier: "test.hospital.de"

pipeline:
  enabled_steps:
    - local_import
    - send

retry:
  max_attempts: 5
  initial_backoff_ms: 1000
  max_backoff_ms: 30000

jobs_dir: "` + jobsDir + `"
`
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	config, err := services.LoadConfig(configFile)
	require.NoError(t, err, "Config should load without error")

	assert.Empty(t, config.Services.Send.Auth.Username)
	assert.Empty(t, config.Services.Send.Auth.Password)
	assert.Empty(t, config.Services.Send.Auth.OAuthIssuerURI)
	assert.Equal(t, models.SendAuthNone, config.Services.Send.GetAuthType())
}

// TestSendConfig_ValidateMixedAuth verifies that mixing Basic Auth and OAuth2 is rejected
func TestSendConfig_ValidateMixedAuth(t *testing.T) {
	config := models.SendConfig{
		URL:    "https://example.com/fhir",
		SendAs: models.SendModeTransferLoad,
		Auth: models.AuthConfig{
			Username:          "user",
			Password:          "pass",
			OAuthIssuerURI:    "https://auth.example.com",
			OAuthClientID:     "client",
			OAuthClientSecret: "secret",
		},
		Transfer: models.TransferConfig{
			ProjectIdentifier:      "TEST",
			OrganizationIdentifier: "test.org",
		},
	}

	err := config.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cannot configure basic auth and OAuth2 together")
}

// TestSendConfig_ValidateIncompleteBasicAuth verifies partial Basic Auth is rejected
func TestSendConfig_ValidateIncompleteBasicAuth(t *testing.T) {
	tests := []struct {
		name     string
		username string
		password string
		errMsg   string
	}{
		{
			name:     "username only",
			username: "user",
			password: "",
			errMsg:   "password is required",
		},
		{
			name:     "password only",
			username: "",
			password: "pass",
			errMsg:   "username is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := models.SendConfig{
				URL:    "https://example.com/fhir",
				SendAs: models.SendModeTransferLoad,
				Auth: models.AuthConfig{
					Username: tt.username,
					Password: tt.password,
				},
				Transfer: models.TransferConfig{
					ProjectIdentifier:      "TEST",
					OrganizationIdentifier: "test.org",
				},
			}

			err := config.Validate()
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.errMsg)
		})
	}
}

// TestSendConfig_ValidateIncompleteOAuth2 verifies partial OAuth2 config is rejected
func TestSendConfig_ValidateIncompleteOAuth2(t *testing.T) {
	tests := []struct {
		name         string
		issuerURI    string
		clientID     string
		clientSecret string
		errMsg       string
	}{
		{
			name:         "missing issuer URI",
			issuerURI:    "",
			clientID:     "client",
			clientSecret: "secret",
			errMsg:       "oauth_issuer_uri is required",
		},
		{
			name:         "missing client ID",
			issuerURI:    "https://auth.example.com",
			clientID:     "",
			clientSecret: "secret",
			errMsg:       "oauth_client_id is required",
		},
		{
			name:         "missing client secret",
			issuerURI:    "https://auth.example.com",
			clientID:     "client",
			clientSecret: "",
			errMsg:       "oauth_client_secret is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := models.SendConfig{
				URL:    "https://example.com/fhir",
				SendAs: models.SendModeTransferLoad,
				Auth: models.AuthConfig{
					OAuthIssuerURI:    tt.issuerURI,
					OAuthClientID:     tt.clientID,
					OAuthClientSecret: tt.clientSecret,
				},
				Transfer: models.TransferConfig{
					ProjectIdentifier:      "TEST",
					OrganizationIdentifier: "test.org",
				},
			}

			err := config.Validate()
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.errMsg)
		})
	}
}

// TestConfigLoading_LocalImportDir verifies that services.local_import.dir is loaded correctly
func TestConfigLoading_LocalImportDir(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	jobsDir := filepath.Join(tmpDir, "jobs")
	dataDir := filepath.Join(tmpDir, "data")
	_ = os.MkdirAll(jobsDir, 0755)
	_ = os.MkdirAll(dataDir, 0755)

	configContent := `
services:
  local_import:
    dir: "` + dataDir + `"

pipeline:
  enabled_steps:
    - local_import

retry:
  max_attempts: 5
  initial_backoff_ms: 1000
  max_backoff_ms: 30000

jobs_dir: "` + jobsDir + `"
`
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	config, err := services.LoadConfig(configFile)
	require.NoError(t, err)

	assert.Equal(t, dataDir, config.Services.LocalImport.Dir, "LocalImport.Dir should be loaded from config")
}

// TestConfigLoading_LocalImportDirEmpty verifies empty local_import.dir is handled correctly
func TestConfigLoading_LocalImportDirEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	jobsDir := filepath.Join(tmpDir, "jobs")
	_ = os.MkdirAll(jobsDir, 0755)

	configContent := `
services:
  local_import:
    dir: ""

pipeline:
  enabled_steps:
    - local_import

retry:
  max_attempts: 5
  initial_backoff_ms: 1000
  max_backoff_ms: 30000

jobs_dir: "` + jobsDir + `"
`
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	config, err := services.LoadConfig(configFile)
	require.NoError(t, err)

	assert.Empty(t, config.Services.LocalImport.Dir, "LocalImport.Dir should be empty when not configured")
}

// TestConfigLoading_LocalImportDirEnvVar verifies environment variable expansion in local_import.dir
func TestConfigLoading_LocalImportDirEnvVar(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	jobsDir := filepath.Join(tmpDir, "jobs")
	dataDir := filepath.Join(tmpDir, "data")
	_ = os.MkdirAll(jobsDir, 0755)
	_ = os.MkdirAll(dataDir, 0755)

	// Set environment variable
	require.NoError(t, os.Setenv("TEST_FHIR_DATA_DIR", dataDir))
	defer func() { _ = os.Unsetenv("TEST_FHIR_DATA_DIR") }()

	configContent := `
services:
  local_import:
    dir: "${TEST_FHIR_DATA_DIR}"

pipeline:
  enabled_steps:
    - local_import

retry:
  max_attempts: 5
  initial_backoff_ms: 1000
  max_backoff_ms: 30000

jobs_dir: "` + jobsDir + `"
`
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	config, err := services.LoadConfig(configFile)
	require.NoError(t, err)

	assert.Equal(t, dataDir, config.Services.LocalImport.Dir, "LocalImport.Dir should expand environment variable")
}

// TestPipelineJob_ValidateWithEmptyInputSource_LocalImportConfigDir verifies that
// job validation allows empty InputSource when local_import uses config directory
func TestPipelineJob_ValidateWithEmptyInputSource_LocalImportConfigDir(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a job with empty InputSource but with config.Services.LocalImport.Dir set
	job := models.PipelineJob{
		JobID:       "550e8400-e29b-41d4-a716-446655440000",
		InputSource: "", // Empty - using config dir instead
		InputType:   models.InputTypeLocal,
		CurrentStep: string(models.StepLocalImport),
		Status:      models.JobStatusPending,
		Steps:       models.InitializeSteps([]models.StepName{models.StepLocalImport}),
		Config: models.ProjectConfig{
			JobsDir: tmpDir,
			Pipeline: models.PipelineConfig{
				EnabledSteps: []models.StepName{models.StepLocalImport},
			},
			Services: models.ServiceConfig{
				LocalImport: models.LocalImportConfig{
					Dir: "/configured/path", // Config directory is set
				},
			},
		},
	}

	// Validation should pass because local_import config dir is set
	err := job.Validate()
	assert.NoError(t, err, "Validation should pass with empty InputSource when local_import config dir is set")
}

// TestPipelineJob_ValidateWithEmptyInputSource_NoConfigDir verifies that
// job validation fails when InputSource is empty and no config directory is set
func TestPipelineJob_ValidateWithEmptyInputSource_NoConfigDir(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a job with empty InputSource and no config dir
	job := models.PipelineJob{
		JobID:       "550e8400-e29b-41d4-a716-446655440000",
		InputSource: "", // Empty - no config dir either
		InputType:   models.InputTypeLocal,
		CurrentStep: string(models.StepLocalImport),
		Status:      models.JobStatusPending,
		Steps:       models.InitializeSteps([]models.StepName{models.StepLocalImport}),
		Config: models.ProjectConfig{
			JobsDir: tmpDir,
			Pipeline: models.PipelineConfig{
				EnabledSteps: []models.StepName{models.StepLocalImport},
			},
			Services: models.ServiceConfig{
				LocalImport: models.LocalImportConfig{
					Dir: "", // No config directory
				},
			},
		},
	}

	// Validation should fail because neither InputSource nor config dir is set
	err := job.Validate()
	assert.Error(t, err, "Validation should fail with empty InputSource and no config dir")
	assert.Contains(t, err.Error(), "input_source is required", "Error should mention input_source")
}

// TestConfigLoading_TLSConfig verifies TLS settings are loaded from YAML
func TestConfigLoading_TLSConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	jobsDir := filepath.Join(tmpDir, "jobs")
	_ = os.MkdirAll(jobsDir, 0755)

	configContent := `
pipeline:
  enabled_steps:
    - local_import

tls:
  ca_cert_path: "/etc/ssl/custom/ca-bundle.pem"
  insecure_skip_verify: true

retry:
  max_attempts: 3
  initial_backoff_ms: 500
  max_backoff_ms: 5000

jobs_dir: "` + jobsDir + `"
`
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	config, err := services.LoadConfig(configFile)
	require.NoError(t, err, "Config should load without error")

	assert.Equal(t, "/etc/ssl/custom/ca-bundle.pem", config.TLS.CACertPath, "TLS CA cert path should be loaded")
	assert.True(t, config.TLS.InsecureSkipVerify, "InsecureSkipVerify should be true")
}

// TestConfigLoading_TLSConfigDefaults verifies TLS defaults when not specified
func TestConfigLoading_TLSConfigDefaults(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	jobsDir := filepath.Join(tmpDir, "jobs")
	_ = os.MkdirAll(jobsDir, 0755)

	configContent := `
pipeline:
  enabled_steps:
    - local_import

retry:
  max_attempts: 3
  initial_backoff_ms: 500
  max_backoff_ms: 5000

jobs_dir: "` + jobsDir + `"
`
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	config, err := services.LoadConfig(configFile)
	require.NoError(t, err, "Config should load without error")

	assert.Empty(t, config.TLS.CACertPath, "TLS CA cert path should be empty by default")
	assert.False(t, config.TLS.InsecureSkipVerify, "InsecureSkipVerify should default to false")
}

// TestConfigLoading_TLSConfigEnvVar verifies environment variable expansion in ca_cert_path
func TestConfigLoading_TLSConfigEnvVar(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	jobsDir := filepath.Join(tmpDir, "jobs")
	_ = os.MkdirAll(jobsDir, 0755)

	require.NoError(t, os.Setenv("TEST_CA_CERT_PATH", "/custom/certs/ca.pem"))
	defer func() { _ = os.Unsetenv("TEST_CA_CERT_PATH") }()

	configContent := `
pipeline:
  enabled_steps:
    - local_import

tls:
  ca_cert_path: "${TEST_CA_CERT_PATH}"

retry:
  max_attempts: 3
  initial_backoff_ms: 500
  max_backoff_ms: 5000

jobs_dir: "` + jobsDir + `"
`
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	config, err := services.LoadConfig(configFile)
	require.NoError(t, err, "Config should load without error")

	assert.Equal(t, "/custom/certs/ca.pem", config.TLS.CACertPath, "TLS CA cert path should expand env var")
}

// TestConfigLoading_ValidationConfig verifies that validation config including
// bundle_chunk_size_mb is properly loaded from YAML
func TestConfigLoading_ValidationConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	jobsDir := filepath.Join(tmpDir, "jobs")
	_ = os.MkdirAll(jobsDir, 0755)

	configContent := `
services:
  validation:
    url: "http://validator.example.com:8080"
    max_concurrent_requests: 8
    bundle_chunk_size_mb: 25

pipeline:
  enabled_steps:
    - local_import

retry:
  max_attempts: 3
  initial_backoff_ms: 500
  max_backoff_ms: 5000

jobs_dir: "` + jobsDir + `"
`
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	config, err := services.LoadConfig(configFile)
	require.NoError(t, err, "Config should load without error")

	assert.Equal(t, "http://validator.example.com:8080", config.Services.Validation.URL, "Validation URL should be loaded")
	assert.Equal(t, 8, config.Services.Validation.MaxConcurrentRequests, "MaxConcurrentRequests should be loaded")
	assert.Equal(t, 25, config.Services.Validation.BundleChunkSizeMB, "BundleChunkSizeMB should be loaded")
}

// TestConfigLoading_ValidationConfigDefaults verifies that defaults are applied
// when validation config fields are not set
func TestConfigLoading_ValidationConfigDefaults(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	jobsDir := filepath.Join(tmpDir, "jobs")
	_ = os.MkdirAll(jobsDir, 0755)

	configContent := `
services:
  validation:
    url: "http://validator.example.com:8080"

pipeline:
  enabled_steps:
    - local_import

retry:
  max_attempts: 3
  initial_backoff_ms: 500
  max_backoff_ms: 5000

jobs_dir: "` + jobsDir + `"
`
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	config, err := services.LoadConfig(configFile)
	require.NoError(t, err, "Config should load without error")

	assert.Equal(t, "http://validator.example.com:8080", config.Services.Validation.URL)
	assert.Equal(t, 4, config.Services.Validation.MaxConcurrentRequests, "MaxConcurrentRequests should default to 4")
	assert.Equal(t, 10, config.Services.Validation.BundleChunkSizeMB, "BundleChunkSizeMB should default to 10")
	require.NotNil(t, config.Services.Validation.FailOnError, "FailOnError should not be nil")
	assert.True(t, *config.Services.Validation.FailOnError, "FailOnError should default to true")
}

// TestConfigLoading_ValidationFailOnError verifies that fail_on_error: true is loaded from YAML
func TestConfigLoading_ValidationFailOnError(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	jobsDir := filepath.Join(tmpDir, "jobs")
	_ = os.MkdirAll(jobsDir, 0755)

	configContent := `
services:
  validation:
    url: "http://validator.example.com:8080"
    fail_on_error: true

pipeline:
  enabled_steps:
    - local_import

retry:
  max_attempts: 3
  initial_backoff_ms: 500
  max_backoff_ms: 5000

jobs_dir: "` + jobsDir + `"
`
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	config, err := services.LoadConfig(configFile)
	require.NoError(t, err, "Config should load without error")

	require.NotNil(t, config.Services.Validation.FailOnError, "FailOnError should not be nil")
	assert.True(t, *config.Services.Validation.FailOnError, "FailOnError should be true when set in config")
	assert.Equal(t, "http://validator.example.com:8080", config.Services.Validation.URL)
}

// TestConfigLoading_ValidationFailOnErrorExplicitFalse verifies that fail_on_error: false
// is respected and not overridden by the default (true).
func TestConfigLoading_ValidationFailOnErrorExplicitFalse(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	jobsDir := filepath.Join(tmpDir, "jobs")
	_ = os.MkdirAll(jobsDir, 0755)

	configContent := `
services:
  validation:
    url: "http://validator.example.com:8080"
    fail_on_error: false

pipeline:
  enabled_steps:
    - local_import

retry:
  max_attempts: 3
  initial_backoff_ms: 500
  max_backoff_ms: 5000

jobs_dir: "` + jobsDir + `"
`
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	config, err := services.LoadConfig(configFile)
	require.NoError(t, err, "Config should load without error")

	require.NotNil(t, config.Services.Validation.FailOnError, "FailOnError should not be nil")
	assert.False(t, *config.Services.Validation.FailOnError, "FailOnError should be false when explicitly set to false in config")
}

func TestConfigLoading_FlatteningBatchSizeMB(t *testing.T) {
	t.Run("loads batch_size_mb from config", func(t *testing.T) {
		tmpDir := t.TempDir()
		configFile := filepath.Join(tmpDir, "config.yaml")
		jobsDir := filepath.Join(tmpDir, "jobs")
		_ = os.MkdirAll(jobsDir, 0755)

		configContent := `
services:
  flattening:
    service_url: "http://localhost:8080"
    lookup_path: "/path/to/lookup.json"
    batch_size_mb: 25

pipeline:
  enabled_steps:
    - local_import

retry:
  max_attempts: 5
  initial_backoff_ms: 1000
  max_backoff_ms: 30000

jobs_dir: "` + jobsDir + `"
`
		err := os.WriteFile(configFile, []byte(configContent), 0644)
		require.NoError(t, err)

		config, err := services.LoadConfig(configFile)
		require.NoError(t, err)

		assert.Equal(t, 25, config.Services.Flattening.BatchSizeMB)
	})

	t.Run("defaults to 500 when not set", func(t *testing.T) {
		tmpDir := t.TempDir()
		configFile := filepath.Join(tmpDir, "config.yaml")
		jobsDir := filepath.Join(tmpDir, "jobs")
		_ = os.MkdirAll(jobsDir, 0755)

		configContent := `
services:
  flattening:
    service_url: "http://localhost:8080"
    lookup_path: "/path/to/lookup.json"

pipeline:
  enabled_steps:
    - local_import

retry:
  max_attempts: 5
  initial_backoff_ms: 1000
  max_backoff_ms: 30000

jobs_dir: "` + jobsDir + `"
`
		err := os.WriteFile(configFile, []byte(configContent), 0644)
		require.NoError(t, err)

		config, err := services.LoadConfig(configFile)
		require.NoError(t, err)

		assert.Equal(t, 500, config.Services.Flattening.BatchSizeMB)
	})
}

// TestConfigLoading_OmittedFieldsUseStructDefaults verifies the struct-driven
// loader: fields absent from YAML resolve to the defaults declared in
// models.DefaultConfig() across every value kind (duration, int, int64, string,
// bool, *bool, slice). The config supplies only the required pipeline step and
// jobs_dir; everything else — including the entire retry block — is omitted.
func TestConfigLoading_OmittedFieldsUseStructDefaults(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	jobsDir := filepath.Join(tmpDir, "jobs")
	require.NoError(t, os.MkdirAll(jobsDir, 0755))

	configContent := `
pipeline:
  enabled_steps:
    - local_import

jobs_dir: "` + jobsDir + `"
`
	require.NoError(t, os.WriteFile(configFile, []byte(configContent), 0644))

	config, err := services.LoadConfig(configFile)
	require.NoError(t, err)

	defaults := models.DefaultConfig()

	// Duration default
	assert.Equal(t, 5*time.Second, config.Services.TORCH.PollingInterval)
	// int default
	assert.Equal(t, defaults.Services.DIMP.BundleSplitThresholdMB, config.Services.DIMP.BundleSplitThresholdMB)
	// int64 defaults: the whole retry block was omitted yet still resolves
	assert.Equal(t, defaults.Retry, config.Retry)
	// string default
	assert.Equal(t, "default", config.Compression.Level)
	// bool default (true)
	assert.True(t, config.Compression.Enabled)
	// *bool default (true)
	require.NotNil(t, config.Services.Validation.FailOnError)
	assert.True(t, *config.Services.Validation.FailOnError)
	// slice default
	assert.Equal(t, []string{"csv"}, config.Services.Flattening.Formats)
	// nested send defaults: region is intentionally not defaulted so that
	// validateS3Upload fails fast when an s3_upload config omits it.
	assert.Empty(t, config.Services.Send.S3.Region)
	assert.Equal(t, 100, config.Services.Send.BatchSize)
}

// TestConfigLoading_DecodeError verifies LoadConfig surfaces a decode error when
// a duration field holds a string that is neither ISO 8601 nor Go format. The
// stringToDurationHookFunc fails to parse it, so viper.Unmarshal returns an error.
func TestConfigLoading_DecodeError(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	jobsDir := filepath.Join(tmpDir, "jobs")
	_ = os.MkdirAll(jobsDir, 0755)

	configContent := `
services:
  torch:
    base_url: "http://localhost:8080"
    extraction_timeout: "not-a-duration"

pipeline:
  enabled_steps:
    - local_import

retry:
  max_attempts: 5
  initial_backoff_ms: 1000
  max_backoff_ms: 30000

jobs_dir: "` + jobsDir + `"
`
	require.NoError(t, os.WriteFile(configFile, []byte(configContent), 0644))

	config, err := services.LoadConfig(configFile)
	assert.Error(t, err, "Unparseable duration should fail decoding")
	assert.Nil(t, config)
	assert.Contains(t, err.Error(), "failed to decode configuration")
}
