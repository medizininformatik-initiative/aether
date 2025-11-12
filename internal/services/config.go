package services

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/spf13/viper"
	"github.com/medizininformatik-initiative/aether/internal/models"
)

// ExpandEnvVars expands environment variables in the format ${VAR} or $VAR
func ExpandEnvVars(s string) string {
	// Match ${VAR} pattern
	re := regexp.MustCompile(`\$\{([A-Z_][A-Z0-9_]*)\}`)
	expanded := re.ReplaceAllStringFunc(s, func(match string) string {
		// Extract variable name (remove ${ and })
		varName := strings.TrimSuffix(strings.TrimPrefix(match, "${"), "}")
		// Get environment variable value, return empty string if not set
		return os.Getenv(varName)
	})
	return expanded
}

// LoadConfig loads configuration from file and merges with CLI flags
// Priority order (highest to lowest):
//  1. CLI flags (via viper bindings)
//  2. Environment variables
//  3. Configuration file
//  4. Default values
func LoadConfig(configFile string) (*models.ProjectConfig, error) {
	// Set config file path if provided
	if configFile != "" {
		viper.SetConfigFile(configFile)
	} else {
		// Search for config in standard locations
		viper.SetConfigName("aether")
		viper.SetConfigType("yaml")
		viper.AddConfigPath(".")
		viper.AddConfigPath("$HOME/.config/aether")
		viper.AddConfigPath("/etc/aether")
	}

	// Enable environment variable override with AETHER_ prefix
	viper.SetEnvPrefix("AETHER")
	viper.AutomaticEnv()

	// Read config file (optional - don't fail if not found)
	configFound := true
	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			// Config file was found but couldn't be read
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}
		// Config file not found - use defaults only
		configFound = false
	}

	// Build config manually from viper values
	// (Viper.Unmarshal has issues with nested structs in some versions)
	// Expand environment variables in string values
	config := models.ProjectConfig{
		Services: models.ServiceConfig{
			TORCH: models.TORCHConfig{
				BaseURL:                   ExpandEnvVars(viper.GetString("services.torch.base_url")),
				Username:                  ExpandEnvVars(viper.GetString("services.torch.username")),
				Password:                  ExpandEnvVars(viper.GetString("services.torch.password")),
				ExtractionTimeoutMinutes:  viper.GetInt("services.torch.extraction_timeout_minutes"),
				PollingIntervalSeconds:    viper.GetInt("services.torch.polling_interval_seconds"),
				MaxPollingIntervalSeconds: viper.GetInt("services.torch.max_polling_interval_seconds"),
			},
			DIMP: models.DIMPConfig{
				URL:                    ExpandEnvVars(viper.GetString("services.dimp.url")),
				BundleSplitThresholdMB: viper.GetInt("services.dimp.bundle_split_threshold_mb"),
			},
			CSVConversion: models.CSVConversionConfig{
				URL: ExpandEnvVars(viper.GetString("services.csv_conversion.url")),
			},
			ParquetConversion: models.ParquetConversionConfig{
				URL: ExpandEnvVars(viper.GetString("services.parquet_conversion.url")),
			},
		},
		Retry: models.RetryConfig{
			MaxAttempts:      viper.GetInt("retry.max_attempts"),
			InitialBackoffMs: viper.GetInt64("retry.initial_backoff_ms"),
			MaxBackoffMs:     viper.GetInt64("retry.max_backoff_ms"),
		},
		JobsDir: ExpandEnvVars(viper.GetString("jobs_dir")),
	}

	// Get enabled steps
	enabledSteps := viper.GetStringSlice("pipeline.enabled_steps")
	for _, stepStr := range enabledSteps {
		config.Pipeline.EnabledSteps = append(config.Pipeline.EnabledSteps, models.StepName(stepStr))
	}

	// Apply defaults for missing fields only if config wasn't found
	if !configFound {
		defaults := models.DefaultConfig()
		if len(config.Pipeline.EnabledSteps) == 0 {
			config.Pipeline.EnabledSteps = defaults.Pipeline.EnabledSteps
		}
		if config.Services.DIMP.URL == "" && defaults.Services.DIMP.URL != "" {
			config.Services.DIMP.URL = defaults.Services.DIMP.URL
		}
		if config.Services.DIMP.BundleSplitThresholdMB == 0 {
			config.Services.DIMP.BundleSplitThresholdMB = defaults.Services.DIMP.BundleSplitThresholdMB
		}
		if config.Retry.MaxAttempts == 0 {
			config.Retry = defaults.Retry
		}
		if config.JobsDir == "" {
			config.JobsDir = defaults.JobsDir
		}
	} else {
		// Config was loaded, apply defaults only for truly missing values
		if config.Retry.MaxAttempts == 0 {
			config.Retry.MaxAttempts = 5
		}
		if config.Retry.InitialBackoffMs == 0 {
			config.Retry.InitialBackoffMs = 1000
		}
		if config.Retry.MaxBackoffMs == 0 {
			config.Retry.MaxBackoffMs = 30000
		}
		if config.JobsDir == "" {
			config.JobsDir = "./jobs"
		}
		// Apply TORCH defaults for missing values
		if config.Services.TORCH.ExtractionTimeoutMinutes == 0 {
			config.Services.TORCH.ExtractionTimeoutMinutes = 30
		}
		if config.Services.TORCH.PollingIntervalSeconds == 0 {
			config.Services.TORCH.PollingIntervalSeconds = 5
		}
		if config.Services.TORCH.MaxPollingIntervalSeconds == 0 {
			config.Services.TORCH.MaxPollingIntervalSeconds = 30
		}
		// Apply DIMP Bundle split threshold default if not set
		if config.Services.DIMP.BundleSplitThresholdMB == 0 {
			config.Services.DIMP.BundleSplitThresholdMB = 10 // 10MB default
		}
	}

	// Validate the configuration
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	// Validate jobs directory exists and is writable
	if err := models.ValidateJobsDir(config.JobsDir); err != nil {
		// Try to create it if it doesn't exist
		if os.IsNotExist(err) {
			if createErr := os.MkdirAll(config.JobsDir, 0755); createErr != nil {
				return nil, fmt.Errorf("failed to create jobs directory: %w", createErr)
			}
		} else {
			return nil, err
		}
	}

	return &config, nil
}

// GetConfigFilePath returns the path to the config file that was loaded
func GetConfigFilePath() string {
	return viper.ConfigFileUsed()
}

// SetConfigValue allows runtime override of config values
// Useful for CLI flag overrides
func SetConfigValue(key string, value any) {
	viper.Set(key, value)
}

// BindFlagToConfig binds a CLI flag to a configuration key
// This allows CLI flags to override config file values
func BindFlagToConfig(flagName string, configKey string) error {
	return viper.BindPFlag(configKey, nil) // Will be bound by cobra command
}
