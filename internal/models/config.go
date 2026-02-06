package models

import (
	"fmt"
	"net/url"
	"time"
)

// ProjectConfig is the top-level configuration for the Aether pipeline
type ProjectConfig struct {
	Services    ServiceConfig     `yaml:"services" json:"services"`
	Pipeline    PipelineConfig    `yaml:"pipeline" json:"pipeline"`
	Retry       RetryConfig       `yaml:"retry" json:"retry"`
	Compression CompressionConfig `yaml:"compression" json:"compression"`
	TLS         TLSConfig         `yaml:"tls" json:"tls" mapstructure:"tls"`
	JobsDir     string            `yaml:"jobs_dir" json:"jobs_dir"`
}

// TLSConfig holds TLS settings for outgoing HTTP connections.
// Used to trust custom/internal CA certificates common in hospital networks.
type TLSConfig struct {
	CACertPath         string `yaml:"ca_cert_path" json:"ca_cert_path" mapstructure:"ca_cert_path"`
	InsecureSkipVerify bool   `yaml:"insecure_skip_verify" json:"insecure_skip_verify" mapstructure:"insecure_skip_verify"`
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
	DIMP               DIMPConfig               `yaml:"dimp" json:"dimp"`
	CSVConversion      CSVConversionConfig      `yaml:"csv_conversion" json:"csv_conversion"`
	ParquetConversion  ParquetConversionConfig  `yaml:"parquet_conversion" json:"parquet_conversion"`
	TORCH              TORCHConfig              `yaml:"torch" json:"torch"`
	Flattening         FlatteningConfig         `yaml:"flattening" json:"flattening"`
	CRTDLPreprocessing CRTDLPreprocessingConfig `yaml:"crtdl_preprocessing" json:"crtdl_preprocessing" mapstructure:"crtdl_preprocessing"`
	Send               SendConfig               `yaml:"send" json:"send"`
	LocalImport        LocalImportConfig        `yaml:"local_import" json:"local_import" mapstructure:"local_import"`
	Validation         ValidationConfig         `yaml:"validation" json:"validation" mapstructure:"validation"`
}

// ValidationConfig contains FHIR validation service settings
type ValidationConfig struct {
	URL                   string `yaml:"url" json:"url" mapstructure:"url"`
	MaxConcurrentRequests int    `yaml:"max_concurrent_requests" json:"max_concurrent_requests" mapstructure:"max_concurrent_requests"`
	BundleChunkSizeMB     int    `yaml:"bundle_chunk_size_mb" json:"bundle_chunk_size_mb" mapstructure:"bundle_chunk_size_mb"`
	FailOnError           *bool  `yaml:"fail_on_error" json:"fail_on_error" mapstructure:"fail_on_error"`
}

// LocalImportConfig contains settings for local directory import
type LocalImportConfig struct {
	Dir string `yaml:"dir" json:"dir" mapstructure:"dir"` // Default directory path for local imports
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
	FileReadyRetries          int    `yaml:"file_ready_retries" json:"file_ready_retries"`
	FileReadyIntervalSeconds  int    `yaml:"file_ready_interval_seconds" json:"file_ready_interval_seconds"`
}

// SendMode defines the mode for sending data
type SendMode string

const (
	// SendModeDirectResourceLoad sends NDJSON directly to a FHIR server using transaction bundles
	SendModeDirectResourceLoad SendMode = "direct_resource_load"
	// SendModeTransferLoad prepares and sends to a transfer FHIR server using Binary/DocumentReference
	SendModeTransferLoad SendMode = "transfer_load"
	// SendModeS3Upload uploads files to an S3-compatible object store
	SendModeS3Upload SendMode = "s3_upload"
)

// AuthConfig holds unified authentication settings for send step
type AuthConfig struct {
	// Basic Auth
	Username string `yaml:"username" json:"username" mapstructure:"username"`
	Password string `yaml:"password" json:"password" mapstructure:"password"`
	// OAuth2 Client Credentials
	OAuthIssuerURI    string `yaml:"oauth_issuer_uri" json:"oauth_issuer_uri" mapstructure:"oauth_issuer_uri"`
	OAuthClientID     string `yaml:"oauth_client_id" json:"oauth_client_id" mapstructure:"oauth_client_id"`
	OAuthClientSecret string `yaml:"oauth_client_secret" json:"oauth_client_secret" mapstructure:"oauth_client_secret"`
}

// TransferConfig holds settings specific to transfer_load mode
type TransferConfig struct {
	ProjectIdentifier      string `yaml:"project_identifier" json:"project_identifier" mapstructure:"project_identifier"`
	OrganizationIdentifier string `yaml:"organization_identifier" json:"organization_identifier" mapstructure:"organization_identifier"`
}

// S3Config holds settings for S3-compatible object store uploads
type S3Config struct {
	Endpoint        string        `yaml:"endpoint" json:"endpoint" mapstructure:"endpoint"`
	Region          string        `yaml:"region" json:"region" mapstructure:"region"`
	Bucket          string        `yaml:"bucket" json:"bucket" mapstructure:"bucket"`
	AccessKeyID     string        `yaml:"access_key_id" json:"access_key_id" mapstructure:"access_key_id"`
	SecretAccessKey string        `yaml:"secret_access_key" json:"secret_access_key" mapstructure:"secret_access_key"`
	UsePathStyle    bool          `yaml:"use_path_style" json:"use_path_style" mapstructure:"use_path_style"`
	Timeout         time.Duration `yaml:"timeout" json:"timeout" mapstructure:"timeout"`
}

// SendConfig contains settings for the send step
type SendConfig struct {
	// URL is the FHIR server base URL (required)
	URL string `yaml:"url" json:"url" mapstructure:"url"`
	// SendAs determines the send mode: "direct_resource_load" or "transfer_load"
	SendAs SendMode `yaml:"send_as" json:"send_as" mapstructure:"send_as"`
	// BatchSize is the number of resources per transaction bundle (only for direct_resource_load, default: 100)
	BatchSize int `yaml:"batch_size" json:"batch_size" mapstructure:"batch_size"`
	// Auth holds authentication settings (unified for all modes)
	Auth AuthConfig `yaml:"auth" json:"auth" mapstructure:"auth"`
	// Transfer holds settings specific to transfer_load mode
	Transfer TransferConfig `yaml:"transfer" json:"transfer" mapstructure:"transfer"`
	// S3 holds settings for s3_upload mode
	S3 S3Config `yaml:"s3" json:"s3" mapstructure:"s3"`
}

// Validate checks if SendConfig has all required fields
func (c *SendConfig) Validate() error {
	// Validate send_as mode
	if c.SendAs == "" {
		return fmt.Errorf("send send_as is required (must be 'direct_resource_load', 'transfer_load', or 's3_upload')")
	}

	if c.SendAs != SendModeDirectResourceLoad && c.SendAs != SendModeTransferLoad && c.SendAs != SendModeS3Upload {
		return fmt.Errorf("invalid send send_as: must be 'direct_resource_load', 'transfer_load', or 's3_upload', got '%s'", c.SendAs)
	}

	// URL is required for FHIR modes, not for S3
	if c.SendAs != SendModeS3Upload {
		if c.URL == "" {
			return fmt.Errorf("send url is required")
		}

		parsedURL, err := url.Parse(c.URL)
		if err != nil {
			return fmt.Errorf("invalid send url: %w", err)
		}

		if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
			return fmt.Errorf("invalid send url: must use http or https scheme, got '%s'", parsedURL.Scheme)
		}
	}

	// Mode-specific validation
	switch c.SendAs {
	case SendModeDirectResourceLoad:
		return c.validateDirectResourceLoad()
	case SendModeTransferLoad:
		return c.validateTransferLoad()
	case SendModeS3Upload:
		return c.validateS3Upload()
	}

	return nil
}

// validateDirectResourceLoad validates direct_resource_load mode settings
func (c *SendConfig) validateDirectResourceLoad() error {
	if c.BatchSize < 0 {
		return fmt.Errorf("send batch_size must be >= 0, got %d", c.BatchSize)
	}

	if c.BatchSize > 1000 {
		return fmt.Errorf("send batch_size must be <= 1000 (FHIR server limit), got %d", c.BatchSize)
	}

	// Validate auth
	return c.validateAuth()
}

// validateTransferLoad validates transfer_load mode settings
func (c *SendConfig) validateTransferLoad() error {
	if c.Transfer.ProjectIdentifier == "" {
		return fmt.Errorf("send transfer.project_identifier is required for transfer_load mode")
	}

	if c.Transfer.OrganizationIdentifier == "" {
		return fmt.Errorf("send transfer.organization_identifier is required for transfer_load mode")
	}

	// Validate auth
	return c.validateAuth()
}

// validateS3Upload validates s3_upload mode settings
func (c *SendConfig) validateS3Upload() error {
	if c.S3.Bucket == "" {
		return fmt.Errorf("send s3 bucket is required for s3_upload mode")
	}
	if c.S3.Region == "" {
		return fmt.Errorf("send s3 region is required for s3_upload mode")
	}
	if c.S3.AccessKeyID == "" {
		return fmt.Errorf("send s3 access_key_id is required for s3_upload mode")
	}
	if c.S3.SecretAccessKey == "" {
		return fmt.Errorf("send s3 secret_access_key is required for s3_upload mode")
	}

	// Validate endpoint URL scheme if provided
	if c.S3.Endpoint != "" {
		parsedURL, err := url.Parse(c.S3.Endpoint)
		if err != nil {
			return fmt.Errorf("invalid s3 endpoint: %w", err)
		}
		if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
			return fmt.Errorf("invalid s3 endpoint: must use http or https scheme, got '%s'", parsedURL.Scheme)
		}
	}

	// Auth is optional for S3 (used for proxy auth)
	return c.validateAuth()
}

// validateAuth checks that authentication configuration is consistent
func (c *SendConfig) validateAuth() error {
	hasBasicAuth := c.Auth.Username != "" || c.Auth.Password != ""
	hasOAuth2 := c.Auth.OAuthIssuerURI != "" || c.Auth.OAuthClientID != "" || c.Auth.OAuthClientSecret != ""

	// Cannot mix both auth methods
	if hasBasicAuth && hasOAuth2 {
		return fmt.Errorf("send auth: cannot configure both basic auth and OAuth2; use one or the other")
	}

	// If using Basic Auth, both username and password must be set
	if hasBasicAuth {
		if c.Auth.Username == "" {
			return fmt.Errorf("send auth: username is required when using basic auth")
		}
		if c.Auth.Password == "" {
			return fmt.Errorf("send auth: password is required when using basic auth")
		}
	}

	// If using OAuth2, all three fields must be set
	if hasOAuth2 {
		if c.Auth.OAuthIssuerURI == "" {
			return fmt.Errorf("send auth: oauth_issuer_uri is required when using OAuth2")
		}
		if c.Auth.OAuthClientID == "" {
			return fmt.Errorf("send auth: oauth_client_id is required when using OAuth2")
		}
		if c.Auth.OAuthClientSecret == "" {
			return fmt.Errorf("send auth: oauth_client_secret is required when using OAuth2")
		}
	}

	return nil
}

// SendAuthType represents the type of authentication to use
type SendAuthType int

const (
	// SendAuthNone indicates no authentication
	SendAuthNone SendAuthType = iota
	// SendAuthBasic indicates Basic authentication
	SendAuthBasic
	// SendAuthOAuth2 indicates OAuth2 client credentials
	SendAuthOAuth2
)

// GetAuthType returns the authentication type based on configuration
func (c *SendConfig) GetAuthType() SendAuthType {
	if c.Auth.Username != "" && c.Auth.Password != "" {
		return SendAuthBasic
	}
	if c.Auth.OAuthIssuerURI != "" && c.Auth.OAuthClientID != "" && c.Auth.OAuthClientSecret != "" {
		return SendAuthOAuth2
	}
	return SendAuthNone
}

// IsConfigured returns true if the send step has minimum configuration
func (c *SendConfig) IsConfigured() bool {
	return c.URL != "" || c.S3.Bucket != ""
}

// GetBatchSize returns the batch size for direct_resource_load mode, with a default of 100
func (c *SendConfig) GetBatchSize() int {
	if c.BatchSize <= 0 {
		return 100 // default
	}
	return c.BatchSize
}

// DefaultMaxNDJSONLineSize is the default maximum line size for NDJSON scanning (100MB).
// FHIR Bundles with hundreds of resources can produce multi-megabyte lines.
const DefaultMaxNDJSONLineSize = 100 * 1024 * 1024

// PipelineConfig defines which steps are enabled and their execution order
type PipelineConfig struct {
	EnabledSteps        []StepName `yaml:"enabled_steps" json:"enabled_steps"`
	MaxNDJSONLineSizeMB int        `yaml:"max_ndjson_line_size_mb" json:"max_ndjson_line_size_mb" mapstructure:"max_ndjson_line_size_mb"`
}

// MaxNDJSONLineSize returns the maximum NDJSON line size in bytes.
// Defaults to DefaultMaxNDJSONLineSize (100MB) if not configured.
func (c PipelineConfig) MaxNDJSONLineSize() int {
	if c.MaxNDJSONLineSizeMB <= 0 {
		return DefaultMaxNDJSONLineSize
	}
	return c.MaxNDJSONLineSizeMB * 1024 * 1024
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
				FileReadyRetries:          10,
				FileReadyIntervalSeconds:  10,
			},
			Flattening:         DefaultFlatteningConfig(),
			CRTDLPreprocessing: DefaultCRTDLPreprocessingConfig(),
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
	case StepFlattening:
		return c.Flattening.ServiceURL != ""
	case StepSend:
		return c.Send.IsConfigured()
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
	case StepFlattening:
		return c.Flattening.ServiceURL
	case StepSend:
		return c.Send.URL
	default:
		return ""
	}
}
