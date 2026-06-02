package services

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/viper"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
)

// getDuration reads a viper key as a string and parses it as a duration,
// supporting both ISO 8601 (e.g. "PT30M") and Go format (e.g. "30m").
// Returns zero duration if the key is not set or empty.
func getDuration(key string) time.Duration {
	s := viper.GetString(key)
	if s == "" {
		return 0
	}
	d, err := lib.ParseDuration(s)
	if err != nil {
		// Fall back to viper's built-in parsing for numeric values
		return viper.GetDuration(key)
	}
	return d
}

// viperGetBoolPtr returns a *bool if the key is explicitly set in config, or nil otherwise.
func viperGetBoolPtr(key string) *bool {
	if !viper.IsSet(key) {
		return nil
	}
	v := viper.GetBool(key)
	return &v
}

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

// LoadConfig loads configuration from the given file and merges with CLI flags.
// The config file path is required; auto-discovery of ./aether.yaml or
// ~/.config/aether/aether.yaml is intentionally not performed.
// Priority order (highest to lowest):
//  1. CLI flags (via viper bindings)
//  2. Environment variables
//  3. Configuration file
//  4. Default values
func LoadConfig(configFile string) (*models.ProjectConfig, error) {
	if configFile == "" {
		return nil, fmt.Errorf("config file is required: pass aether.yaml as the first positional argument (e.g. `aether pipeline start aether.yaml crtdl.json`)")
	}
	viper.SetConfigFile(configFile)

	// Enable environment variable override with AETHER_ prefix
	viper.SetEnvPrefix("AETHER")
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read config file %q: %w", configFile, err)
	}

	// Build config manually from viper values
	// (Viper.Unmarshal has issues with nested structs in some versions)
	// Expand environment variables in string values
	config := models.ProjectConfig{
		Services: models.ServiceConfig{
			TORCH: models.TORCHConfig{
				BaseURL:            ExpandEnvVars(viper.GetString("services.torch.base_url")),
				Username:           ExpandEnvVars(viper.GetString("services.torch.username")),
				Password:           ExpandEnvVars(viper.GetString("services.torch.password")),
				ExtractionTimeout:  getDuration("services.torch.extraction_timeout"),
				PollingInterval:    getDuration("services.torch.polling_interval"),
				MaxPollingInterval: getDuration("services.torch.max_polling_interval"),
				FileReadyRetries:   viper.GetInt("services.torch.file_ready_retries"),
				FileReadyInterval:  getDuration("services.torch.file_ready_interval"),
			},
			DIMP: models.DIMPConfig{
				URL:                    ExpandEnvVars(viper.GetString("services.dimp.url")),
				BundleSplitThresholdMB: viper.GetInt("services.dimp.bundle_split_threshold_mb"),
			},
			Validation: models.ValidationConfig{
				URL:                   ExpandEnvVars(viper.GetString("services.validation.url")),
				MaxConcurrentRequests: viper.GetInt("services.validation.max_concurrent_requests"),
				BundleChunkSizeMB:     viper.GetInt("services.validation.bundle_chunk_size_mb"),
				FailOnError:           viperGetBoolPtr("services.validation.fail_on_error"),
			},
			Flattening: models.FlatteningConfig{
				ServiceURL:  ExpandEnvVars(viper.GetString("services.flattening.service_url")),
				LookupPath:  ExpandEnvVars(viper.GetString("services.flattening.lookup_path")),
				Formats:     viper.GetStringSlice("services.flattening.formats"),
				Timeout:     getDuration("services.flattening.timeout"),
				BatchSizeMB: viper.GetInt("services.flattening.batch_size_mb"),
			},
			Send: models.SendConfig{
				URL:       ExpandEnvVars(viper.GetString("services.send.url")),
				SendAs:    models.SendMode(viper.GetString("services.send.send_as")),
				BatchSize: viper.GetInt("services.send.batch_size"),
				Auth: models.AuthConfig{
					Username:          ExpandEnvVars(viper.GetString("services.send.auth.username")),
					Password:          ExpandEnvVars(viper.GetString("services.send.auth.password")),
					OAuthIssuerURI:    ExpandEnvVars(viper.GetString("services.send.auth.oauth_issuer_uri")),
					OAuthClientID:     ExpandEnvVars(viper.GetString("services.send.auth.oauth_client_id")),
					OAuthClientSecret: ExpandEnvVars(viper.GetString("services.send.auth.oauth_client_secret")),
				},
				Transfer: models.TransferConfig{
					ProjectIdentifier:      ExpandEnvVars(viper.GetString("services.send.transfer.project_identifier")),
					OrganizationIdentifier: ExpandEnvVars(viper.GetString("services.send.transfer.organization_identifier")),
				},
				S3: models.S3Config{
					Endpoint:        ExpandEnvVars(viper.GetString("services.send.s3.endpoint")),
					Region:          ExpandEnvVars(viper.GetString("services.send.s3.region")),
					Bucket:          ExpandEnvVars(viper.GetString("services.send.s3.bucket")),
					AccessKeyID:     ExpandEnvVars(viper.GetString("services.send.s3.access_key_id")),
					SecretAccessKey: ExpandEnvVars(viper.GetString("services.send.s3.secret_access_key")),
					UsePathStyle:    viper.GetBool("services.send.s3.use_path_style"),
					Timeout:         getDuration("services.send.s3.timeout"),
				},
			},
			LocalImport: models.LocalImportConfig{
				Dir: ExpandEnvVars(viper.GetString("services.local_import.dir")),
			},
		},
	}

	// Load CRTDL preprocessing configuration separately
	// Using UnmarshalKey for complex nested structures (enrichments array)
	var crtdlPreprocessing models.CRTDLPreprocessingConfig
	if err := viper.UnmarshalKey("services.crtdl_preprocessing", &crtdlPreprocessing); err == nil {
		config.Services.CRTDLPreprocessing = crtdlPreprocessing
	}

	// Continue building the rest of the config
	config.Retry = models.RetryConfig{
		MaxAttempts:      viper.GetInt("retry.max_attempts"),
		InitialBackoffMs: viper.GetInt64("retry.initial_backoff_ms"),
		MaxBackoffMs:     viper.GetInt64("retry.max_backoff_ms"),
	}
	config.Compression = models.CompressionConfig{
		Enabled: viper.GetBool("compression.enabled"),
		Level:   viper.GetString("compression.level"),
	}
	config.TLS = models.TLSConfig{
		CACertPath:         ExpandEnvVars(viper.GetString("tls.ca_cert_path")),
		InsecureSkipVerify: viper.GetBool("tls.insecure_skip_verify"),
	}
	config.JobsDir = ExpandEnvVars(viper.GetString("jobs_dir"))

	// Handle compression default: enabled=true unless explicitly set to false
	// viper.GetBool returns false for missing keys, but we want true as default
	if !viper.IsSet("compression.enabled") {
		config.Compression.Enabled = true
	}

	// Get enabled steps
	enabledSteps := viper.GetStringSlice("pipeline.enabled_steps")
	for _, stepStr := range enabledSteps {
		config.Pipeline.EnabledSteps = append(config.Pipeline.EnabledSteps, models.StepName(stepStr))
	}

	// Apply defaults for any values missing from the loaded config file.
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
	if config.Services.TORCH.ExtractionTimeout == 0 {
		config.Services.TORCH.ExtractionTimeout = 30 * time.Minute
	}
	if config.Services.TORCH.PollingInterval == 0 {
		config.Services.TORCH.PollingInterval = 5 * time.Second
	}
	if config.Services.TORCH.MaxPollingInterval == 0 {
		config.Services.TORCH.MaxPollingInterval = 30 * time.Second
	}
	if config.Services.TORCH.FileReadyRetries == 0 {
		config.Services.TORCH.FileReadyRetries = 10
	}
	if config.Services.TORCH.FileReadyInterval == 0 {
		config.Services.TORCH.FileReadyInterval = 10 * time.Second
	}
	if config.Services.DIMP.BundleSplitThresholdMB == 0 {
		config.Services.DIMP.BundleSplitThresholdMB = 10
	}
	if config.Services.Validation.MaxConcurrentRequests == 0 {
		config.Services.Validation.MaxConcurrentRequests = 4
	}
	if config.Services.Validation.BundleChunkSizeMB == 0 {
		config.Services.Validation.BundleChunkSizeMB = 10
	}
	if config.Services.Validation.FailOnError == nil {
		defaultFailOnError := true
		config.Services.Validation.FailOnError = &defaultFailOnError
	}
	if config.Compression.Level == "" {
		config.Compression.Level = "default"
	}
	if config.Services.Flattening.Timeout == 0 {
		config.Services.Flattening.Timeout = 30 * time.Minute
	}
	if len(config.Services.Flattening.Formats) == 0 {
		config.Services.Flattening.Formats = []string{"csv"}
	}
	if config.Services.Flattening.BatchSizeMB == 0 {
		config.Services.Flattening.BatchSizeMB = 500
	}
	if config.Services.Send.BatchSize == 0 {
		config.Services.Send.BatchSize = 100
	}
	if config.Services.Send.S3.Region == "" {
		config.Services.Send.S3.Region = "eu-central-1"
	}
	if config.Services.Send.S3.Timeout == 0 {
		config.Services.Send.S3.Timeout = 30 * time.Minute
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
