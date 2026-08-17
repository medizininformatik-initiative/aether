package models

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

// ProjectConfig is the top-level configuration for the Aether pipeline
type ProjectConfig struct {
	Services    ServiceConfig     `yaml:"services" json:"services" mapstructure:"services"`
	Pipeline    PipelineConfig    `yaml:"pipeline" json:"pipeline" mapstructure:"pipeline"`
	Retry       RetryConfig       `yaml:"retry" json:"retry" mapstructure:"retry"`
	Compression CompressionConfig `yaml:"compression" json:"compression" mapstructure:"compression"`
	TLS         TLSConfig         `yaml:"tls" json:"tls" mapstructure:"tls"`
	JobsDir     string            `yaml:"jobs_dir" json:"jobs_dir" mapstructure:"jobs_dir"`
}

// TLSConfig holds TLS settings for outgoing HTTP connections.
// Used to trust custom/internal CA certificates common in hospital networks.
type TLSConfig struct {
	CACertPath         string `yaml:"ca_cert_path" json:"ca_cert_path" mapstructure:"ca_cert_path"`
	InsecureSkipVerify bool   `yaml:"insecure_skip_verify" json:"insecure_skip_verify" mapstructure:"insecure_skip_verify"`
}

// CompressionConfig holds compression settings for pipeline output files.
type CompressionConfig struct {
	Enabled bool   `yaml:"enabled" json:"enabled" mapstructure:"enabled"`
	Level   string `yaml:"level" json:"level" mapstructure:"level"`
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
	DIMP               DIMPConfig               `yaml:"dimp" json:"dimp" mapstructure:"dimp"`
	TORCH              TORCHConfig              `yaml:"torch" json:"torch" mapstructure:"torch"`
	Flattening         FlatteningConfig         `yaml:"flattening" json:"flattening" mapstructure:"flattening"`
	CRTDLPreprocessing CRTDLPreprocessingConfig `yaml:"crtdl_preprocessing" json:"crtdl_preprocessing" mapstructure:"crtdl_preprocessing"`
	Send               SendConfig               `yaml:"send" json:"send" mapstructure:"send"`
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
	// Recursive opts into scanning subdirectories of Dir for NDJSON files.
	// Defaults to false since local_import flattens matches into a single
	// destination directory keyed by basename, which is only safe when the
	// whole source tree already has unique basenames.
	Recursive bool `yaml:"recursive" json:"recursive" mapstructure:"recursive"`
}

// DIMPConfig contains DIMP pseudonymization service settings
type DIMPConfig struct {
	URL                    string `yaml:"url" json:"url" mapstructure:"url"`
	BundleSplitThresholdMB int    `yaml:"bundle_split_threshold_mb" json:"bundle_split_threshold_mb" mapstructure:"bundle_split_threshold_mb"` // Default 10MB - threshold for splitting large Bundles to prevent HTTP 413 errors
	// Auth holds authentication settings for the DIMP service
	Auth AuthConfig `yaml:"auth" json:"auth" mapstructure:"auth"`
}

// Validate checks that the DIMP config is well-formed. An empty URL is allowed
// here; ProjectConfig.Validate refuses it only when the dimp step is enabled.
func (c *DIMPConfig) Validate() error {
	if c.URL != "" {
		if _, err := url.Parse(c.URL); err != nil {
			return fmt.Errorf("invalid dimp url: %w", err)
		}
	}
	return c.Auth.Validate("dimp")
}

// TORCHConfig contains TORCH server connection and extraction behavior settings
type TORCHConfig struct {
	BaseURL string `yaml:"base_url" json:"base_url" mapstructure:"base_url"`
	// Auth holds authentication settings for the TORCH server.
	Auth AuthConfig `yaml:"auth" json:"auth" mapstructure:"auth"`
	// Deprecated: put these fields in the Auth block. They stay for
	// compatibility with configuration files that predate the auth block.
	Username string `yaml:"username" json:"username" mapstructure:"username"`
	// Deprecated: use Auth.Password.
	Password string `yaml:"password" json:"password" mapstructure:"password"`
	// Deprecated: use Auth.OAuthIssuerURI.
	OAuthIssuerURI string `yaml:"oauth_issuer_uri" json:"oauth_issuer_uri" mapstructure:"oauth_issuer_uri"`
	// Deprecated: use Auth.OAuthClientID.
	OAuthClientID string `yaml:"oauth_client_id" json:"oauth_client_id" mapstructure:"oauth_client_id"`
	// Deprecated: use Auth.OAuthClientSecret.
	OAuthClientSecret string `yaml:"oauth_client_secret" json:"oauth_client_secret" mapstructure:"oauth_client_secret"`

	ExtractionTimeout  time.Duration `yaml:"extraction_timeout" json:"extraction_timeout" mapstructure:"extraction_timeout"`
	PollingInterval    time.Duration `yaml:"polling_interval" json:"polling_interval" mapstructure:"polling_interval"`
	MaxPollingInterval time.Duration `yaml:"max_polling_interval" json:"max_polling_interval" mapstructure:"max_polling_interval"`
	FileReadyRetries   int           `yaml:"file_ready_retries" json:"file_ready_retries" mapstructure:"file_ready_retries"`
	FileReadyInterval  time.Duration `yaml:"file_ready_interval" json:"file_ready_interval" mapstructure:"file_ready_interval"`
	// DownloadStallTimeout bounds inactivity while streaming an extraction file:
	// the download aborts only when no bytes arrive for this long, so a large but
	// steadily-progressing NDJSON finishes regardless of total size, while a hung
	// connection fails fast. Unlike a whole-request deadline, it never has to be
	// sized to the largest expected download.
	DownloadStallTimeout time.Duration `yaml:"download_stall_timeout" json:"download_stall_timeout" mapstructure:"download_stall_timeout"`
}

// EffectiveAuth returns the authentication settings for the TORCH server. It
// maps the deprecated flat fields to the auth block, so that configuration
// files of both shapes work. Validate refuses a file that uses both shapes.
func (c *TORCHConfig) EffectiveAuth() AuthConfig {
	deprecated := AuthConfig{
		Username:          c.Username,
		Password:          c.Password,
		OAuthIssuerURI:    c.OAuthIssuerURI,
		OAuthClientID:     c.OAuthClientID,
		OAuthClientSecret: c.OAuthClientSecret,
	}
	if deprecated != (AuthConfig{}) {
		return deprecated
	}
	return c.Auth
}

// DeprecatedAuthFields returns the names of the flat auth keys that the config
// sets. Validate uses it to name the keys in the error that refuses a file
// which uses both shapes.
func (c *TORCHConfig) DeprecatedAuthFields() []string {
	var names []string
	for _, field := range []struct {
		name  string
		value string
	}{
		{"username", c.Username},
		{"password", c.Password},
		{"oauth_issuer_uri", c.OAuthIssuerURI},
		{"oauth_client_id", c.OAuthClientID},
		{"oauth_client_secret", c.OAuthClientSecret},
	} {
		if field.value != "" {
			names = append(names, field.name)
		}
	}
	return names
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

// AuthConfig holds unified authentication settings for a service client.
// Use one scheme only: basic auth, OAuth2 client credentials, or an API key.
type AuthConfig struct {
	// Basic Auth
	Username string `yaml:"username" json:"username" mapstructure:"username"`
	Password string `yaml:"password" json:"password" mapstructure:"password"`
	// OAuth2 Client Credentials
	OAuthIssuerURI    string `yaml:"oauth_issuer_uri" json:"oauth_issuer_uri" mapstructure:"oauth_issuer_uri"`
	OAuthClientID     string `yaml:"oauth_client_id" json:"oauth_client_id" mapstructure:"oauth_client_id"`
	OAuthClientSecret string `yaml:"oauth_client_secret" json:"oauth_client_secret" mapstructure:"oauth_client_secret"`
	// API Key, sent in its own header instead of Authorization
	APIKey string `yaml:"api_key" json:"api_key" mapstructure:"api_key"`
	// APIKeyHeader names the header that carries APIKey. Empty means DefaultAPIKeyHeader.
	APIKeyHeader string `yaml:"api_key_header" json:"api_key_header" mapstructure:"api_key_header"`
}

// DefaultAPIKeyHeader is the conventional header for an API key.
const DefaultAPIKeyHeader = "x-api-key"

// APIKeyHeaderName returns the header that carries the API key.
func (c *AuthConfig) APIKeyHeaderName() string {
	if c.APIKeyHeader != "" {
		return c.APIKeyHeader
	}
	return DefaultAPIKeyHeader
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
	// Timeout defaults to 30m, but an explicit zero or negative value yields an
	// already-expired context.WithTimeout at upload time, so reject it here.
	if c.S3.Timeout <= 0 {
		return fmt.Errorf("send s3 timeout must be > 0 for s3_upload mode")
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
	return c.Auth.Validate("send")
}

// Validate checks that the authentication configuration is consistent. It
// refuses basic auth together with OAuth2, and an incomplete set of fields for
// one scheme. An API key can accompany basic auth or OAuth2, because it uses
// its own header. The scope names the owning config block in the error
// message, e.g. "send".
func (c *AuthConfig) Validate(scope string) error {
	hasBasicAuth := c.Username != "" || c.Password != ""
	hasOAuth2 := c.OAuthIssuerURI != "" || c.OAuthClientID != "" || c.OAuthClientSecret != ""

	// Basic auth and OAuth2 both write the Authorization header
	if hasBasicAuth && hasOAuth2 {
		return fmt.Errorf("%s auth: cannot configure basic auth and OAuth2 together; use one only", scope)
	}

	// If using Basic Auth, both username and password must be set
	if hasBasicAuth {
		if c.Username == "" {
			return fmt.Errorf("%s auth: username is required when using basic auth", scope)
		}
		if c.Password == "" {
			return fmt.Errorf("%s auth: password is required when using basic auth", scope)
		}
	}

	// If using OAuth2, all three fields must be set
	if hasOAuth2 {
		if c.OAuthIssuerURI == "" {
			return fmt.Errorf("%s auth: oauth_issuer_uri is required when using OAuth2", scope)
		}
		if c.OAuthClientID == "" {
			return fmt.Errorf("%s auth: oauth_client_id is required when using OAuth2", scope)
		}
		if c.OAuthClientSecret == "" {
			return fmt.Errorf("%s auth: oauth_client_secret is required when using OAuth2", scope)
		}
	}

	// api_key_header only names the header; it needs a key to send
	if c.APIKeyHeader != "" && c.APIKey == "" {
		return fmt.Errorf("%s auth: api_key is required when api_key_header is set", scope)
	}

	if c.APIKeyHeader != "" && !isValidHeaderFieldName(c.APIKeyHeader) {
		return fmt.Errorf("%s auth: api_key_header is not a valid HTTP header name: %q", scope, c.APIKeyHeader)
	}

	// One scheme per header: the API key would overwrite basic auth or OAuth2
	if strings.EqualFold(c.APIKeyHeader, "Authorization") && (hasBasicAuth || hasOAuth2) {
		return fmt.Errorf("%s auth: api_key_header cannot be Authorization together with basic auth or OAuth2; they write the same header", scope)
	}

	return nil
}

// isValidHeaderFieldName reports whether name is a token as RFC 9110, section
// 5.6.2 defines it. net/http refuses any other name when it writes the request.
func isValidHeaderFieldName(name string) bool {
	const separators = "!#$%&'*+-.^_`|~"

	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case 'a' <= c && c <= 'z', 'A' <= c && c <= 'Z', '0' <= c && c <= '9':
		case strings.IndexByte(separators, c) >= 0:
		default:
			return false
		}
	}

	return name != ""
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
	// SendAuthAPIKey indicates an API key sent in its own header
	SendAuthAPIKey
)

// GetAuthType returns the scheme that sets the Authorization header. It
// returns SendAuthAPIKey only when the API key is the sole scheme, because an
// API key can accompany basic auth or OAuth2.
func (c *SendConfig) GetAuthType() SendAuthType {
	if c.Auth.Username != "" && c.Auth.Password != "" {
		return SendAuthBasic
	}
	if c.Auth.OAuthIssuerURI != "" && c.Auth.OAuthClientID != "" && c.Auth.OAuthClientSecret != "" {
		return SendAuthOAuth2
	}
	if c.Auth.APIKey != "" {
		return SendAuthAPIKey
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

// PipelineConfig defines which steps are enabled and their execution order
type PipelineConfig struct {
	EnabledSteps []StepName `yaml:"enabled_steps" json:"enabled_steps" mapstructure:"enabled_steps"`
}

// RetryConfig controls retry behavior for transient errors
type RetryConfig struct {
	// MaxAttempts is the total number of attempts (initial try plus retries).
	// A value of 0 or less is treated as a single attempt with no retries.
	MaxAttempts      int   `yaml:"max_attempts" json:"max_attempts" mapstructure:"max_attempts"`
	InitialBackoffMs int64 `yaml:"initial_backoff_ms" json:"initial_backoff_ms" mapstructure:"initial_backoff_ms"`
	MaxBackoffMs     int64 `yaml:"max_backoff_ms" json:"max_backoff_ms" mapstructure:"max_backoff_ms"`
}

// DefaultConfig returns a sensible default configuration.
// It is the single source of truth for default values: config loading starts
// from this and overlays only the fields present in the YAML file, so a field
// omitted from YAML resolves to the default declared here.
func DefaultConfig() ProjectConfig {
	failOnError := true
	return ProjectConfig{
		Services: ServiceConfig{
			DIMP: DIMPConfig{
				URL:                    "",
				BundleSplitThresholdMB: 10, // 10MB default threshold for Bundle splitting
			},
			TORCH: TORCHConfig{
				BaseURL:              "",
				ExtractionTimeout:    30 * time.Minute,
				PollingInterval:      5 * time.Second,
				MaxPollingInterval:   30 * time.Second,
				FileReadyRetries:     10,
				FileReadyInterval:    10 * time.Second,
				DownloadStallTimeout: 60 * time.Second,
			},
			Flattening:         DefaultFlatteningConfig(),
			CRTDLPreprocessing: DefaultCRTDLPreprocessingConfig(),
			Validation: ValidationConfig{
				MaxConcurrentRequests: 4,
				BundleChunkSizeMB:     10,
				FailOnError:           &failOnError,
			},
			Send: SendConfig{
				BatchSize: 100,
				S3: S3Config{
					Timeout: 30 * time.Minute,
				},
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

	// Authentication is optional - TORCH may not require it in all environments.
	// Two shapes together are ambiguous, thus EffectiveAuth cannot resolve them.
	if deprecated := c.DeprecatedAuthFields(); len(deprecated) > 0 && c.Auth != (AuthConfig{}) {
		return fmt.Errorf("torch auth: set the auth block or the deprecated flat fields (%s), not both",
			strings.Join(deprecated, ", "))
	}

	auth := c.EffectiveAuth()
	if err := auth.Validate("torch"); err != nil {
		return err
	}

	if c.ExtractionTimeout <= 0 {
		return fmt.Errorf("extraction_timeout must be > 0, got %s", c.ExtractionTimeout)
	}

	if c.PollingInterval < time.Second {
		return fmt.Errorf("polling_interval must be >= 1s, got %s", c.PollingInterval)
	}

	if c.MaxPollingInterval < c.PollingInterval {
		return fmt.Errorf("max_polling_interval (%s) must be >= polling_interval (%s)",
			c.MaxPollingInterval, c.PollingInterval)
	}

	if c.FileReadyInterval < 0 {
		return fmt.Errorf("file_ready_interval must be >= 0, got %s", c.FileReadyInterval)
	}

	// A negative window is nonsensical; zero means "use the built-in default"
	// (applied when the TORCH client is constructed), mirroring file_ready_interval.
	if c.DownloadStallTimeout < 0 {
		return fmt.Errorf("download_stall_timeout must be >= 0, got %s", c.DownloadStallTimeout)
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
	case StepFlattening:
		return c.Flattening.ServiceURL
	case StepSend:
		return c.Send.URL
	default:
		return ""
	}
}
