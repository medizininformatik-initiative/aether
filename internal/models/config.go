package models

import (
	"fmt"
	"net/url"
)

// ProjectConfig is the top-level configuration for the Aether pipeline
type ProjectConfig struct {
	Services    ServiceConfig     `yaml:"services" json:"services"`
	Pipeline    PipelineConfig    `yaml:"pipeline" json:"pipeline"`
	Retry       RetryConfig       `yaml:"retry" json:"retry"`
	Compression CompressionConfig `yaml:"compression" json:"compression"`
	JobsDir     string            `yaml:"jobs_dir" json:"jobs_dir"`
}

// CompressionConfig holds compression settings for pipeline output files.
type CompressionConfig struct {
	Enabled bool   `yaml:"enabled" json:"enabled"`
	Level   string `yaml:"level" json:"level"`
}

// DefaultCompressionConfig returns the default compression configuration.
// Compression is enabled by default with the "default" compression level.
func DefaultCompressionConfig() CompressionConfig {
	return CompressionConfig{
		Enabled: true,
		Level:   "default",
	}
}

// ServiceConfig contains connection details for external HTTP services
type ServiceConfig struct {
	DIMP              DIMPConfig              `yaml:"dimp" json:"dimp"`
	CSVConversion     CSVConversionConfig     `yaml:"csv_conversion" json:"csv_conversion"`
	ParquetConversion ParquetConversionConfig `yaml:"parquet_conversion" json:"parquet_conversion"`
	TORCH             TORCHConfig             `yaml:"torch" json:"torch"`
}

// DIMPConfig contains DIMP pseudonymization service settings
type DIMPConfig struct {
	URL                    string `yaml:"url" json:"url"`
	BundleSplitThresholdMB int    `yaml:"bundle_split_threshold_mb" json:"bundle_split_threshold_mb"` // Default 10MB - threshold for splitting large Bundles to prevent HTTP 413 errors
}

// CSVConversionConfig contains CSV conversion service settings
type CSVConversionConfig struct {
	URL string `yaml:"url" json:"url"`
}

// ParquetConversionConfig contains Parquet conversion service settings
type ParquetConversionConfig struct {
	URL string `yaml:"url" json:"url"`
}

// TORCHConfig contains TORCH server connection and extraction behavior settings
type TORCHConfig struct {
	BaseURL                   string `yaml:"base_url" json:"base_url"`
	Username                  string `yaml:"username" json:"username"`
	Password                  string `yaml:"password" json:"password"`
	ExtractionTimeoutMinutes  int    `yaml:"extraction_timeout_minutes" json:"extraction_timeout_minutes"`
	PollingIntervalSeconds    int    `yaml:"polling_interval_seconds" json:"polling_interval_seconds"`
	MaxPollingIntervalSeconds int    `yaml:"max_polling_interval_seconds" json:"max_polling_interval_seconds"`
}

// PipelineConfig defines which steps are enabled and their execution order
type PipelineConfig struct {
	EnabledSteps []StepName `yaml:"enabled_steps" json:"enabled_steps"`
}

// RetryConfig controls retry behavior for transient errors
type RetryConfig struct {
	MaxAttempts      int   `yaml:"max_attempts" json:"max_attempts"`
	InitialBackoffMs int64 `yaml:"initial_backoff_ms" json:"initial_backoff_ms"`
	MaxBackoffMs     int64 `yaml:"max_backoff_ms" json:"max_backoff_ms"`
}

// DefaultConfig returns a sensible default configuration
func DefaultConfig() ProjectConfig {
	return ProjectConfig{
		Services: ServiceConfig{
			DIMP: DIMPConfig{
				URL:                    "",
				BundleSplitThresholdMB: 10, // 10MB default threshold for Bundle splitting
			},
			CSVConversion: CSVConversionConfig{
				URL: "",
			},
			ParquetConversion: ParquetConversionConfig{
				URL: "",
			},
			TORCH: TORCHConfig{
				BaseURL:                   "",
				Username:                  "",
				Password:                  "",
				ExtractionTimeoutMinutes:  30,
				PollingIntervalSeconds:    5,
				MaxPollingIntervalSeconds: 30,
			},
		},
		Pipeline: PipelineConfig{
			EnabledSteps: []StepName{StepLocalImport, StepHttpImport},
		},
		Retry: RetryConfig{
			MaxAttempts:      5,
			InitialBackoffMs: 1000,
			MaxBackoffMs:     30000,
		},
		Compression: DefaultCompressionConfig(),
		JobsDir:     "./jobs",
	}
}

// Validate checks if the TORCHConfig has all required fields and valid values
func (c *TORCHConfig) Validate() error {
	if c.BaseURL == "" {
		return fmt.Errorf("TORCH base_url is required")
	}

	parsedURL, err := url.Parse(c.BaseURL)
	if err != nil {
		return fmt.Errorf("invalid TORCH base_url: %w", err)
	}

	// Require http or https scheme for TORCH service
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return fmt.Errorf("invalid TORCH base_url: must use http or https scheme, got '%s'", parsedURL.Scheme)
	}

	// Username and password are optional - TORCH may not require authentication in all environments

	if c.ExtractionTimeoutMinutes <= 0 {
		return fmt.Errorf("extraction_timeout_minutes must be > 0, got %d", c.ExtractionTimeoutMinutes)
	}

	if c.PollingIntervalSeconds <= 0 || c.PollingIntervalSeconds > 60 {
		return fmt.Errorf("polling_interval_seconds must be 1-60, got %d", c.PollingIntervalSeconds)
	}

	if c.MaxPollingIntervalSeconds < c.PollingIntervalSeconds {
		return fmt.Errorf("max_polling_interval_seconds (%d) must be >= polling_interval_seconds (%d)",
			c.MaxPollingIntervalSeconds, c.PollingIntervalSeconds)
	}

	return nil
}

// IsStepEnabled checks if a specific step is enabled in the pipeline configuration
func (c *PipelineConfig) IsStepEnabled(step StepName) bool {
	for _, enabled := range c.EnabledSteps {
		if enabled == step {
			return true
		}
	}
	return false
}

// GetNextStep returns the next enabled step after the current one, or empty string if no more steps
func (c *PipelineConfig) GetNextStep(current StepName) StepName {
	foundCurrent := false
	for _, step := range c.EnabledSteps {
		if foundCurrent {
			return step
		}
		if step == current {
			foundCurrent = true
		}
	}
	return "" // No next step
}

// HasServiceURL checks if a service URL is configured for a given step
func (c *ServiceConfig) HasServiceURL(step StepName) bool {
	switch step {
	case StepDIMP:
		return c.DIMP.URL != ""
	case StepCSVConversion:
		return c.CSVConversion.URL != ""
	case StepParquetConversion:
		return c.ParquetConversion.URL != ""
	default:
		return true // Import and validation don't require external services
	}
}

// GetServiceURL returns the service URL for a given step
func (c *ServiceConfig) GetServiceURL(step StepName) string {
	switch step {
	case StepDIMP:
		return c.DIMP.URL
	case StepCSVConversion:
		return c.CSVConversion.URL
	case StepParquetConversion:
		return c.ParquetConversion.URL
	default:
		return ""
	}
}
