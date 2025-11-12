package unit

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

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
  csv_conversion:
    url: "http://localhost:9000/csv"
  parquet_conversion:
    url: "http://localhost:9000/parquet"

pipeline:
  enabled_steps:
    - local_import
    - dimp
    - validation
    - csv_conversion
    - parquet_conversion

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
	assert.Len(t, config.Pipeline.EnabledSteps, 5, "Should have 5 enabled steps")
	assert.Equal(t, models.StepLocalImport, config.Pipeline.EnabledSteps[0])
	assert.Equal(t, models.StepDIMP, config.Pipeline.EnabledSteps[1])
	assert.Equal(t, models.StepValidation, config.Pipeline.EnabledSteps[2])
	assert.Equal(t, models.StepCSVConversion, config.Pipeline.EnabledSteps[3])
	assert.Equal(t, models.StepParquetConversion, config.Pipeline.EnabledSteps[4])
}

// TestConfigLoading_ServiceURLs verifies all service URLs are loaded correctly
func TestConfigLoading_ServiceURLs(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	jobsDir := filepath.Join(tmpDir, "jobs")
	_ = os.MkdirAll(jobsDir, 0755)

	expectedDIMPUrl := "http://dimp.example.com:8080/fhir"
	expectedCSVUrl := "http://csv.example.com:9000/convert"
	expectedParquetUrl := "http://parquet.example.com:9001/convert"

	configContent := `
services:
  dimp:
    url: "` + expectedDIMPUrl + `"
  csv_conversion:
    url: "` + expectedCSVUrl + `"
  parquet_conversion:
    url: "` + expectedParquetUrl + `"

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
	assert.Equal(t, expectedCSVUrl, config.Services.CSVConversion.URL, "CSV URL should be loaded correctly")
	assert.Equal(t, expectedParquetUrl, config.Services.ParquetConversion.URL, "Parquet URL should be loaded correctly")
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
  csv_conversion_url: ""
  parquet_conversion_url: ""

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
	assert.Empty(t, config.Services.CSVConversion.URL, "Empty CSV URL should remain empty")
	assert.Empty(t, config.Services.ParquetConversion.URL, "Empty Parquet URL should remain empty")
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
  csv_conversion:
    url: ""
  parquet_conversion:
    url: "http://localhost:9001/parquet"

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
	assert.Empty(t, config.Services.CSVConversion.URL, "Empty CSV URL should remain empty")
	assert.Equal(t, "http://localhost:9001/parquet", config.Services.ParquetConversion.URL, "Parquet URL should be loaded")
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
  csv_conversion:
    url: "http://localhost:9000/csv"
  parquet_conversion:
    url: "http://localhost:9001/parquet"

pipeline:
  enabled_steps:
    - local_import
    - validation
    - dimp
    - parquet_conversion
    - csv_conversion

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
	require.Len(t, config.Pipeline.EnabledSteps, 5)
	assert.Equal(t, models.StepLocalImport, config.Pipeline.EnabledSteps[0])
	assert.Equal(t, models.StepValidation, config.Pipeline.EnabledSteps[1])
	assert.Equal(t, models.StepDIMP, config.Pipeline.EnabledSteps[2])
	assert.Equal(t, models.StepParquetConversion, config.Pipeline.EnabledSteps[3])
	assert.Equal(t, models.StepCSVConversion, config.Pipeline.EnabledSteps[4])
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
	assert.Empty(t, config.Services.CSVConversion.URL)
	assert.Empty(t, config.Services.ParquetConversion.URL)
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
  csv_conversion:
    url: ""
  parquet_conversion:
    url: ""

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

// TestConfigValidation_InvalidCSVUrl verifies invalid CSV conversion service URL is rejected
func TestConfigValidation_InvalidCSVUrl(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	jobsDir := filepath.Join(tmpDir, "jobs")
	_ = os.MkdirAll(jobsDir, 0755)

	configContent := `
services:
  dimp:
    url: ""
  csv_conversion:
    url: "invalid://url format:"
  parquet_conversion:
    url: ""

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
	assert.Error(t, err, "Invalid CSV conversion URL should fail during config loading")
	assert.Nil(t, config)
	assert.Contains(t, err.Error(), "invalid csv_conversion url")
}

// TestConfigValidation_InvalidParquetUrl verifies invalid Parquet conversion service URL is rejected
func TestConfigValidation_InvalidParquetUrl(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	jobsDir := filepath.Join(tmpDir, "jobs")
	_ = os.MkdirAll(jobsDir, 0755)

	configContent := `
services:
  dimp:
    url: ""
  csv_conversion:
    url: ""
  parquet_conversion:
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
	assert.Error(t, err, "Invalid Parquet conversion URL should fail during config loading")
	assert.Nil(t, config)
	assert.Contains(t, err.Error(), "invalid parquet_conversion url")
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
    username: "testuser"
    password: "testpass"
    extraction_timeout_minutes: 30
    polling_interval_seconds: 5
    max_polling_interval_seconds: 30

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
	assert.Equal(t, "testuser", config.Services.TORCH.Username)
	assert.Equal(t, "testpass", config.Services.TORCH.Password)
	assert.Equal(t, 30, config.Services.TORCH.ExtractionTimeoutMinutes)
	assert.Equal(t, 5, config.Services.TORCH.PollingIntervalSeconds)
	assert.Equal(t, 30, config.Services.TORCH.MaxPollingIntervalSeconds)

	// Test validation passes with valid config
	err = config.Services.TORCH.Validate()
	assert.NoError(t, err, "Valid TORCH config should pass validation")
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
	assert.Equal(t, "", config.Services.TORCH.Username)
	assert.Equal(t, "", config.Services.TORCH.Password)
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
    extraction_timeout_minutes: -1

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

	// LoadConfig validates config, zero timeout should cause error
	_, err = services.LoadConfig(configFile)
	assert.Error(t, err, "Zero timeout should fail validation")
	assert.Contains(t, err.Error(), "timeout")
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
    polling_interval_seconds: -1

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
    polling_interval_seconds: 10
    max_polling_interval_seconds: 5

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
	assert.Equal(t, "", config.Services.TORCH.Username, "Username should default to empty")
	assert.Equal(t, "", config.Services.TORCH.Password, "Password should default to empty")
	assert.Equal(t, 30, config.Services.TORCH.ExtractionTimeoutMinutes, "Should default to 30 minutes")
	assert.Equal(t, 5, config.Services.TORCH.PollingIntervalSeconds, "Should default to 5 seconds")
	assert.Equal(t, 30, config.Services.TORCH.MaxPollingIntervalSeconds, "Should default to 30 seconds")
}

func TestTORCHConfig_ValidateMalformedURL(t *testing.T) {
	// Test URL that causes url.Parse to fail (contains invalid characters)
	config := models.TORCHConfig{
		BaseURL:                   "http://[invalid",
		Username:                  "user",
		Password:                  "pass",
		ExtractionTimeoutMinutes:  30,
		PollingIntervalSeconds:    5,
		MaxPollingIntervalSeconds: 30,
	}

	err := config.Validate()
	assert.Error(t, err, "Malformed URL should fail validation")
	assert.Contains(t, err.Error(), "invalid TORCH base_url")
}

func TestTORCHConfig_ValidatePollingIntervalTooHigh(t *testing.T) {
	// Test polling interval above maximum (>60)
	config := models.TORCHConfig{
		BaseURL:                   "http://localhost:8080",
		Username:                  "user",
		Password:                  "pass",
		ExtractionTimeoutMinutes:  30,
		PollingIntervalSeconds:    61, // Invalid: > 60
		MaxPollingIntervalSeconds: 61,
	}

	err := config.Validate()
	assert.Error(t, err, "Polling interval > 60 should fail validation")
	assert.Contains(t, err.Error(), "polling_interval_seconds must be 1-60")
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
				BaseURL:                   "http://localhost:8080",
				ExtractionTimeoutMinutes:  1,
				PollingIntervalSeconds:    1,
				MaxPollingIntervalSeconds: 1,
			},
			wantErr: false,
		},
		{
			name: "Polling interval exactly 60 - valid",
			config: models.TORCHConfig{
				BaseURL:                   "http://localhost:8080",
				ExtractionTimeoutMinutes:  1,
				PollingIntervalSeconds:    60,
				MaxPollingIntervalSeconds: 60,
			},
			wantErr: false,
		},
		{
			name: "Max polling equals min polling - valid",
			config: models.TORCHConfig{
				BaseURL:                   "http://localhost:8080",
				ExtractionTimeoutMinutes:  1,
				PollingIntervalSeconds:    10,
				MaxPollingIntervalSeconds: 10,
			},
			wantErr: false,
		},
		{
			name: "HTTPS URL - valid",
			config: models.TORCHConfig{
				BaseURL:                   "https://torch.example.com",
				ExtractionTimeoutMinutes:  30,
				PollingIntervalSeconds:    5,
				MaxPollingIntervalSeconds: 30,
			},
			wantErr: false,
		},
		{
			name: "FTP URL - invalid scheme",
			config: models.TORCHConfig{
				BaseURL:                   "ftp://localhost",
				ExtractionTimeoutMinutes:  30,
				PollingIntervalSeconds:    5,
				MaxPollingIntervalSeconds: 30,
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
			CSVConversion: models.CSVConversionConfig{
				URL: "http://csv.example.com:9000",
			},
			ParquetConversion: models.ParquetConversionConfig{
				URL: "http://parquet.example.com:9001",
			},
		},
	}

	// Test DIMP URL
	assert.Equal(t, "http://dimp.example.com:8080", config.Services.GetServiceURL(models.StepDIMP))

	// Test CSV URL
	assert.Equal(t, "http://csv.example.com:9000", config.Services.GetServiceURL(models.StepCSVConversion))

	// Test Parquet URL
	assert.Equal(t, "http://parquet.example.com:9001", config.Services.GetServiceURL(models.StepParquetConversion))

	// Test unknown step
	assert.Equal(t, "", config.Services.GetServiceURL(models.StepLocalImport))
	assert.Equal(t, "", config.Services.GetServiceURL(models.StepValidation))
}

// TestGetNextStep verifies pipeline step progression
func TestGetNextStep(t *testing.T) {
	config := models.ProjectConfig{
		Pipeline: models.PipelineConfig{
			EnabledSteps: []models.StepName{
				models.StepLocalImport,
				models.StepValidation,
				models.StepDIMP,
				models.StepCSVConversion,
			},
		},
	}

	// Test getting next step from import
	assert.Equal(t, models.StepValidation, config.Pipeline.GetNextStep(models.StepLocalImport))

	// Test getting next step from validation
	assert.Equal(t, models.StepDIMP, config.Pipeline.GetNextStep(models.StepValidation))

	// Test getting next step from DIMP
	assert.Equal(t, models.StepCSVConversion, config.Pipeline.GetNextStep(models.StepDIMP))

	// Test getting next step from last step (should return empty)
	assert.Equal(t, models.StepName(""), config.Pipeline.GetNextStep(models.StepCSVConversion))

	// Test getting next step for non-existent step (should return empty)
	assert.Equal(t, models.StepName(""), config.Pipeline.GetNextStep(models.StepParquetConversion))
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
			name: "CSV Conversion with URL",
			config: models.ServiceConfig{
				CSVConversion: models.CSVConversionConfig{URL: "http://csv.example.com"},
			},
			step:   models.StepCSVConversion,
			hasURL: true,
		},
		{
			name: "CSV Conversion without URL",
			config: models.ServiceConfig{
				CSVConversion: models.CSVConversionConfig{URL: ""},
			},
			step:   models.StepCSVConversion,
			hasURL: false,
		},
		{
			name: "Parquet Conversion with URL",
			config: models.ServiceConfig{
				ParquetConversion: models.ParquetConversionConfig{URL: "http://parquet.example.com"},
			},
			step:   models.StepParquetConversion,
			hasURL: true,
		},
		{
			name: "Parquet Conversion without URL",
			config: models.ServiceConfig{
				ParquetConversion: models.ParquetConversionConfig{URL: ""},
			},
			step:   models.StepParquetConversion,
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
					CSVConversion: models.CSVConversionConfig{
						URL: "http://csv.example.com",
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
			},
			errorText: "service URL required for enabled step 'dimp'",
		},
		{
			name: "CSV Conversion enabled without URL",
			config: models.ProjectConfig{
				Services: models.ServiceConfig{
					DIMP: models.DIMPConfig{
						URL: "http://dimp.example.com",
					},
					CSVConversion: models.CSVConversionConfig{URL: ""},
				},
				Pipeline: models.PipelineConfig{
					EnabledSteps: []models.StepName{
						models.StepLocalImport,
						models.StepCSVConversion,
					},
				},
				Retry: models.RetryConfig{
					MaxAttempts:      5,
					InitialBackoffMs: 1000,
					MaxBackoffMs:     30000,
				},
				JobsDir: "/tmp/jobs",
			},
			errorText: "service URL required for enabled step 'csv_conversion'",
		},
		{
			name: "Parquet Conversion enabled without URL",
			config: models.ProjectConfig{
				Services: models.ServiceConfig{
					ParquetConversion: models.ParquetConversionConfig{URL: ""},
				},
				Pipeline: models.PipelineConfig{
					EnabledSteps: []models.StepName{
						models.StepLocalImport,
						models.StepParquetConversion,
					},
				},
				Retry: models.RetryConfig{
					MaxAttempts:      5,
					InitialBackoffMs: 1000,
					MaxBackoffMs:     30000,
				},
				JobsDir: "/tmp/jobs",
			},
			errorText: "service URL required for enabled step 'parquet_conversion'",
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

	csvServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer csvServer.Close()

	parquetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer parquetServer.Close()

	config := models.ProjectConfig{
		Services: models.ServiceConfig{
			DIMP: models.DIMPConfig{
				URL: dimpServer.URL,
			},
			CSVConversion: models.CSVConversionConfig{
				URL: csvServer.URL,
			},
			ParquetConversion: models.ParquetConversionConfig{
				URL: parquetServer.URL,
			},
		},
		Pipeline: models.PipelineConfig{
			EnabledSteps: []models.StepName{
				models.StepLocalImport,
				models.StepDIMP,
				models.StepCSVConversion,
				models.StepParquetConversion,
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
	err := config.ValidateServiceConnectivity()
	assert.NoError(t, err, "All services should be reachable")
}

// TestValidateServiceConnectivity_DIMMServiceUnreachable verifies error when DIMP service is unreachable
func TestValidateServiceConnectivity_DIMMServiceUnreachable(t *testing.T) {
	csvServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer csvServer.Close()

	config := models.ProjectConfig{
		Services: models.ServiceConfig{
			DIMP: models.DIMPConfig{
				URL: "http://localhost:9999", // Unreachable
			},
			CSVConversion: models.CSVConversionConfig{
				URL: csvServer.URL,
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
	err := config.ValidateServiceConnectivity()
	assert.Error(t, err, "Should fail when DIMP service is unreachable")
	assert.Contains(t, err.Error(), "DIMP")
}

// TestValidateServiceConnectivity_CSVServiceUnreachable verifies error when CSV service is unreachable
func TestValidateServiceConnectivity_CSVServiceUnreachable(t *testing.T) {
	dimpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer dimpServer.Close()

	config := models.ProjectConfig{
		Services: models.ServiceConfig{
			DIMP: models.DIMPConfig{
				URL: dimpServer.URL,
			},
			CSVConversion: models.CSVConversionConfig{
				URL: "http://localhost:9998", // Unreachable
			},
		},
		Pipeline: models.PipelineConfig{
			EnabledSteps: []models.StepName{
				models.StepLocalImport,
				models.StepCSVConversion,
			},
		},
		Retry: models.RetryConfig{
			MaxAttempts:      5,
			InitialBackoffMs: 1000,
			MaxBackoffMs:     30000,
		},
		JobsDir: "/tmp/jobs",
	}

	// ValidateServiceConnectivity should fail when CSV service is unreachable
	err := config.ValidateServiceConnectivity()
	assert.Error(t, err, "Should fail when CSV conversion service is unreachable")
	assert.Contains(t, err.Error(), "CSV Conversion")
}

// TestValidateServiceConnectivity_ParquetServiceUnreachable verifies error when Parquet service is unreachable
func TestValidateServiceConnectivity_ParquetServiceUnreachable(t *testing.T) {
	dimpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer dimpServer.Close()

	config := models.ProjectConfig{
		Services: models.ServiceConfig{
			DIMP: models.DIMPConfig{
				URL: dimpServer.URL,
			},
			ParquetConversion: models.ParquetConversionConfig{
				URL: "http://localhost:9997", // Unreachable
			},
		},
		Pipeline: models.PipelineConfig{
			EnabledSteps: []models.StepName{
				models.StepLocalImport,
				models.StepParquetConversion,
			},
		},
		Retry: models.RetryConfig{
			MaxAttempts:      5,
			InitialBackoffMs: 1000,
			MaxBackoffMs:     30000,
		},
		JobsDir: "/tmp/jobs",
	}

	// ValidateServiceConnectivity should fail when Parquet service is unreachable
	err := config.ValidateServiceConnectivity()
	assert.Error(t, err, "Should fail when Parquet conversion service is unreachable")
	assert.Contains(t, err.Error(), "Parquet Conversion")
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
