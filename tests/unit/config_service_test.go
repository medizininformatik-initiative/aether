package unit

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/services"
)

// TestExpandEnvVars tests environment variable expansion
func TestExpandEnvVars(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		envVars  map[string]string
		expected string
	}{
		{
			name:     "Simple variable expansion",
			input:    "${MYVAR}",
			envVars:  map[string]string{"MYVAR": "value"},
			expected: "value",
		},
		{
			name:     "Variable with surrounding text",
			input:    "prefix_${MYVAR}_suffix",
			envVars:  map[string]string{"MYVAR": "test"},
			expected: "prefix_test_suffix",
		},
		{
			name:     "Multiple variables",
			input:    "${VAR1}_${VAR2}",
			envVars:  map[string]string{"VAR1": "first", "VAR2": "second"},
			expected: "first_second",
		},
		{
			name:     "Missing variable",
			input:    "${MISSING}",
			envVars:  map[string]string{},
			expected: "",
		},
		{
			name:     "No variables",
			input:    "just_plain_text",
			envVars:  map[string]string{},
			expected: "just_plain_text",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variables
			oldVars := make(map[string]string)
			for k, v := range tt.envVars {
				oldVars[k] = os.Getenv(k)
				require.NoError(t, os.Setenv(k, v))
			}

			// Call the function and assert result
			result := services.ExpandEnvVars(tt.input)
			assert.Equal(t, tt.expected, result)

			// Clean up environment
			for k := range tt.envVars {
				require.NoError(t, os.Setenv(k, oldVars[k]))
			}
		})
	}
}

// TestLoadConfig_NoConfigFile tests loading with default values when no config file exists
func TestLoadConfig_NoConfigFile(t *testing.T) {
	// Clear any existing config
	viper.Reset()

	// Use an existing directory so viper looks for config in standard locations
	tmpDir := t.TempDir()
	jobsDir := filepath.Join(tmpDir, "jobs")
	require.NoError(t, os.MkdirAll(jobsDir, 0755))

	// Load config - since it won't find one in standard locations, it should use defaults
	config, err := services.LoadConfig("")
	require.NoError(t, err)
	require.NotNil(t, config)

	// Verify default values are applied
	assert.NotNil(t, config.Pipeline.EnabledSteps)
}

// TestLoadConfig_ConfigFileNotFound_UsesDefaults tests that defaults are used when file not found
func TestLoadConfig_ConfigFileNotFound_UsesDefaults(t *testing.T) {
	// Clear any existing config
	viper.Reset()

	config, err := services.LoadConfig("")
	require.NoError(t, err)
	require.NotNil(t, config)

	// Verify defaults are applied
	assert.True(t, len(config.Pipeline.EnabledSteps) > 0)
	assert.Greater(t, config.Services.DIMP.BundleSplitThresholdMB, 0)
	assert.Greater(t, config.Retry.MaxAttempts, 0)
}

// TestLoadConfig_CreateJobsDir tests that jobs directory is created if it doesn't exist
func TestLoadConfig_CreateJobsDir(t *testing.T) {
	tmpDir := t.TempDir()
	jobsDir := filepath.Join(tmpDir, "new_jobs")

	// Ensure dir doesn't exist
	assert.NoDirExists(t, jobsDir)

	// Create a minimal config file pointing to this directory
	configFile := filepath.Join(tmpDir, "config.yaml")
	configContent := `jobs_dir: ` + jobsDir + `
pipeline:
  enabled_steps:
    - local_import
    - dimp
services:
  dimp:
    url: "http://localhost:8080"
    bundle_split_threshold_mb: 10
`
	require.NoError(t, os.WriteFile(configFile, []byte(configContent), 0644))

	viper.Reset()

	// Load config
	config, err := services.LoadConfig(configFile)
	require.NoError(t, err)
	require.NotNil(t, config)

	// Verify directory was created
	assert.DirExists(t, jobsDir)
	assert.Equal(t, jobsDir, config.JobsDir)
}

// TestLoadConfig_InvalidJobsDir tests error handling for invalid jobs directory
func TestLoadConfig_InvalidJobsDir(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a file where we want a directory
	jobsDir := filepath.Join(tmpDir, "jobs")
	f, err := os.Create(jobsDir)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	// Create a config file
	configFile := filepath.Join(tmpDir, "config.yaml")
	configContent := `jobs_dir: ` + jobsDir + `
services:
  dimp:
    url: "http://localhost:8080"
`
	require.NoError(t, os.WriteFile(configFile, []byte(configContent), 0644))

	viper.Reset()

	// Load config - should fail because jobs_dir is a file, not a directory
	_, loadErr := services.LoadConfig(configFile)
	assert.Error(t, loadErr)
}

// TestLoadConfig_EnvVarOverride tests environment variable override of config values
func TestLoadConfig_EnvVarOverride(t *testing.T) {
	tmpDir := t.TempDir()
	jobsDir := filepath.Join(tmpDir, "jobs")

	// Create config file
	configFile := filepath.Join(tmpDir, "config.yaml")
	configContent := `jobs_dir: ` + jobsDir + `
pipeline:
  enabled_steps:
    - local_import
services:
  dimp:
    url: "http://localhost:8080"
`
	require.NoError(t, os.WriteFile(configFile, []byte(configContent), 0644))
	require.NoError(t, os.MkdirAll(jobsDir, 0755))

	viper.Reset()

	// Set environment variable to override jobs_dir
	overrideJobsDir := filepath.Join(tmpDir, "override_jobs")
	require.NoError(t, os.MkdirAll(overrideJobsDir, 0755))
	require.NoError(t, os.Setenv("AETHER_JOBS_DIR", overrideJobsDir))
	defer func() { _ = os.Unsetenv("AETHER_JOBS_DIR") }()

	config, err := services.LoadConfig(configFile)
	require.NoError(t, err)

	// Verify environment variable was used
	assert.Equal(t, overrideJobsDir, config.JobsDir)
}

// TestGetConfigFilePath tests getting the loaded config file path
func TestGetConfigFilePath(t *testing.T) {
	viper.Reset()

	// Initially no config file
	path := services.GetConfigFilePath()
	assert.Equal(t, "", path)

	// After loading a config
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, os.WriteFile(configFile, []byte(""), 0644))
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "jobs"), 0755))

	viper.Reset()
	_, _ = services.LoadConfig(configFile)

	path = services.GetConfigFilePath()
	assert.Equal(t, configFile, path)
}

// TestSetConfigValue tests setting config values at runtime
func TestSetConfigValue(t *testing.T) {
	viper.Reset()

	// Set a value
	services.SetConfigValue("test.key", "test_value")

	// Verify it was set
	assert.Equal(t, "test_value", viper.GetString("test.key"))
}

// TestLoadConfig_ParsedPipeline tests that pipeline steps are correctly parsed
func TestLoadConfig_ParsedPipeline(t *testing.T) {
	tmpDir := t.TempDir()
	jobsDir := filepath.Join(tmpDir, "jobs")
	require.NoError(t, os.MkdirAll(jobsDir, 0755))

	configFile := filepath.Join(tmpDir, "config.yaml")
	configContent := `jobs_dir: ` + jobsDir + `
pipeline:
  enabled_steps:
    - local_import
    - dimp
    - validation
services:
  dimp:
    url: "http://localhost:8080"
    bundle_split_threshold_mb: 10
`
	require.NoError(t, os.WriteFile(configFile, []byte(configContent), 0644))

	viper.Reset()

	config, err := services.LoadConfig(configFile)
	require.NoError(t, err)

	// Verify pipeline steps were parsed
	assert.NotEmpty(t, config.Pipeline.EnabledSteps)
}

// TestLoadConfig_MultipleServices tests loading multiple service configurations
func TestLoadConfig_MultipleServices(t *testing.T) {
	tmpDir := t.TempDir()
	jobsDir := filepath.Join(tmpDir, "jobs")
	require.NoError(t, os.MkdirAll(jobsDir, 0755))

	configFile := filepath.Join(tmpDir, "config.yaml")
	configContent := `jobs_dir: ` + jobsDir + `
pipeline:
  enabled_steps:
    - local_import
    - dimp
services:
  dimp:
    url: "http://dimp:8080"
    bundle_split_threshold_mb: 20
  csv_conversion:
    url: "http://csv:8080"
  parquet_conversion:
    url: "http://parquet:8080"
`
	require.NoError(t, os.WriteFile(configFile, []byte(configContent), 0644))

	viper.Reset()

	config, err := services.LoadConfig(configFile)
	require.NoError(t, err)

	// Verify all services loaded
	assert.Equal(t, "http://dimp:8080", config.Services.DIMP.URL)
	assert.Equal(t, 20, config.Services.DIMP.BundleSplitThresholdMB)
	assert.Equal(t, "http://csv:8080", config.Services.CSVConversion.URL)
	assert.Equal(t, "http://parquet:8080", config.Services.ParquetConversion.URL)
}

// TestLoadConfig_ExplicitDIMPURL tests that explicit DIMP URL is preserved from config
func TestLoadConfig_ExplicitDIMPURL(t *testing.T) {
	tmpDir := t.TempDir()

	// Create config with explicit DIMP URL
	configFile := filepath.Join(tmpDir, "config.yaml")
	configContent := `jobs_dir: ` + filepath.Join(tmpDir, "custom_jobs") + `
pipeline:
  enabled_steps:
    - local_import
    - dimp
services:
  dimp:
    url: "http://my-dimp:9999"
    bundle_split_threshold_mb: 50
`
	require.NoError(t, os.WriteFile(configFile, []byte(configContent), 0644))

	viper.Reset()

	config, err := services.LoadConfig(configFile)
	require.NoError(t, err)
	require.NotNil(t, config)

	// Verify explicit DIMP URL is preserved
	assert.Equal(t, "http://my-dimp:9999", config.Services.DIMP.URL)
	assert.Equal(t, 50, config.Services.DIMP.BundleSplitThresholdMB)
}
