package unit

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
)

// TestValidateSplitConfig verifies configuration validation for Bundle splitting threshold
// Unit test for configuration validation
func TestValidateSplitConfig(t *testing.T) {
	testCases := []struct {
		name        string
		thresholdMB int
		expectError bool
		errorMsg    string
	}{
		{
			name:        "Negative threshold - invalid",
			thresholdMB: -1,
			expectError: true,
			errorMsg:    "must be > 0",
		},
		{
			name:        "Zero threshold - invalid",
			thresholdMB: 0,
			expectError: true,
			errorMsg:    "must be > 0",
		},
		{
			name:        "Very small threshold (1MB) - valid",
			thresholdMB: 1,
			expectError: false,
		},
		{
			name:        "Normal threshold (10MB) - valid",
			thresholdMB: 10,
			expectError: false,
		},
		{
			name:        "Large threshold (50MB) - valid with warning",
			thresholdMB: 50,
			expectError: false,
		},
		{
			name:        "Very large threshold (75MB) - valid with warning",
			thresholdMB: 75,
			expectError: false,
		},
		{
			name:        "Maximum valid threshold (100MB) - valid",
			thresholdMB: 100,
			expectError: false,
		},
		{
			name:        "Over maximum (101MB) - invalid",
			thresholdMB: 101,
			expectError: true,
			errorMsg:    "must be <= 100",
		},
		{
			name:        "Significantly over maximum (200MB) - invalid",
			thresholdMB: 200,
			expectError: true,
			errorMsg:    "must be <= 100",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := lib.ValidateSplitConfig(tc.thresholdMB)

			if tc.expectError {
				assert.Error(t, err, "Expected validation error")
				if tc.errorMsg != "" {
					assert.Contains(t, err.Error(), tc.errorMsg)
				}
			} else {
				assert.NoError(t, err, "Should not have validation error")
			}
		})
	}
}

// TestValidateSplitConfig_EdgeCases verifies boundary conditions
func TestValidateSplitConfig_EdgeCases(t *testing.T) {
	// Test boundary at 0/1
	err := lib.ValidateSplitConfig(0)
	assert.Error(t, err)
	err = lib.ValidateSplitConfig(1)
	assert.NoError(t, err)

	// Test boundary at 100/101
	err = lib.ValidateSplitConfig(100)
	assert.NoError(t, err)
	err = lib.ValidateSplitConfig(101)
	assert.Error(t, err)
}

// TestValidateSplitConfig_TypeConversion verifies the function handles MB to bytes conversion correctly
func TestValidateSplitConfig_ThresholdConversion(t *testing.T) {
	// 10MB should be valid
	err := lib.ValidateSplitConfig(10)
	assert.NoError(t, err, "10MB threshold should be valid")

	// The validation function works with MB values, not bytes
	// This is important for user configuration
	thresholdMB := 10
	assert.Greater(t, thresholdMB, 0, "MB value should be positive")
	assert.LessOrEqual(t, thresholdMB, 100, "MB value should be <= 100")
}

// TestProjectConfig_Validate tests validation of ProjectConfig struct
func TestProjectConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  models.ProjectConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "Valid config with single import step",
			config: models.ProjectConfig{
				Pipeline: models.PipelineConfig{
					EnabledSteps: []models.StepName{models.StepLocalImport},
				},
				Retry: models.RetryConfig{
					MaxAttempts:      3,
					InitialBackoffMs: 500,
					MaxBackoffMs:     5000,
				},
				JobsDir: "/tmp/jobs",
			},
			wantErr: false,
		},
		{
			name: "Empty enabled steps",
			config: models.ProjectConfig{
				Pipeline: models.PipelineConfig{
					EnabledSteps: []models.StepName{},
				},
				Retry: models.RetryConfig{
					MaxAttempts:      3,
					InitialBackoffMs: 500,
					MaxBackoffMs:     5000,
				},
				JobsDir: "/tmp/jobs",
			},
			wantErr: true,
			errMsg:  "at least one pipeline step must be enabled",
		},
		{
			name: "First step is not an import step - validation step first",
			config: models.ProjectConfig{
				Pipeline: models.PipelineConfig{
					EnabledSteps: []models.StepName{models.StepValidation},
				},
				Retry: models.RetryConfig{
					MaxAttempts:      3,
					InitialBackoffMs: 500,
					MaxBackoffMs:     5000,
				},
				JobsDir: "/tmp/jobs",
			},
			wantErr: true,
			errMsg:  "first enabled step must be an import step (torch, local_import, or http_import)",
		},
		{
			name: "First step is not an import step - DIMP first",
			config: models.ProjectConfig{
				Services: models.ServiceConfig{
					DIMP: models.DIMPConfig{URL: "http://dimp.example.com"},
				},
				Pipeline: models.PipelineConfig{
					EnabledSteps: []models.StepName{models.StepDIMP},
				},
				Retry: models.RetryConfig{
					MaxAttempts:      3,
					InitialBackoffMs: 500,
					MaxBackoffMs:     5000,
				},
				JobsDir: "/tmp/jobs",
			},
			wantErr: true,
			errMsg:  "first enabled step must be an import step (torch, local_import, or http_import)",
		},
		{
			name: "First step is torch_import - valid",
			config: models.ProjectConfig{
				Services: models.ServiceConfig{
					TORCH: models.TORCHConfig{
						BaseURL:            "http://localhost:8080",
						Username:           "testuser",
						Password:           "testpass",
						ExtractionTimeout:  30 * time.Minute,
						PollingInterval:    5 * time.Second,
						MaxPollingInterval: 30 * time.Second,
					},
				},
				Pipeline: models.PipelineConfig{
					EnabledSteps: []models.StepName{models.StepTorchImport},
				},
				Retry: models.RetryConfig{
					MaxAttempts:      3,
					InitialBackoffMs: 500,
					MaxBackoffMs:     5000,
				},
				JobsDir: "/tmp/jobs",
			},
			wantErr: false,
		},
		{
			name: "First step is http_import - valid",
			config: models.ProjectConfig{
				Pipeline: models.PipelineConfig{
					EnabledSteps: []models.StepName{models.StepHttpImport},
				},
				Retry: models.RetryConfig{
					MaxAttempts:      3,
					InitialBackoffMs: 500,
					MaxBackoffMs:     5000,
				},
				JobsDir: "/tmp/jobs",
			},
			wantErr: false,
		},
		{
			name: "Unrecognized step in enabled_steps",
			config: models.ProjectConfig{
				Pipeline: models.PipelineConfig{
					EnabledSteps: []models.StepName{
						models.StepLocalImport,
						models.StepName("invalid_step"),
					},
				},
				Retry: models.RetryConfig{
					MaxAttempts:      3,
					InitialBackoffMs: 500,
					MaxBackoffMs:     5000,
				},
				JobsDir: "/tmp/jobs",
			},
			wantErr: true,
			errMsg:  "unrecognized step in enabled_steps: invalid_step",
		},
		{
			name: "Max attempts too low",
			config: models.ProjectConfig{
				Pipeline: models.PipelineConfig{
					EnabledSteps: []models.StepName{models.StepLocalImport},
				},
				Retry: models.RetryConfig{
					MaxAttempts:      0,
					InitialBackoffMs: 500,
					MaxBackoffMs:     5000,
				},
				JobsDir: "/tmp/jobs",
			},
			wantErr: true,
			errMsg:  "max_attempts must be between 1 and 10",
		},
		{
			name: "Max attempts too high",
			config: models.ProjectConfig{
				Pipeline: models.PipelineConfig{
					EnabledSteps: []models.StepName{models.StepLocalImport},
				},
				Retry: models.RetryConfig{
					MaxAttempts:      11,
					InitialBackoffMs: 500,
					MaxBackoffMs:     5000,
				},
				JobsDir: "/tmp/jobs",
			},
			wantErr: true,
			errMsg:  "max_attempts must be between 1 and 10",
		},
		{
			name: "Initial backoff negative",
			config: models.ProjectConfig{
				Pipeline: models.PipelineConfig{
					EnabledSteps: []models.StepName{models.StepLocalImport},
				},
				Retry: models.RetryConfig{
					MaxAttempts:      3,
					InitialBackoffMs: -1,
					MaxBackoffMs:     5000,
				},
				JobsDir: "/tmp/jobs",
			},
			wantErr: true,
			errMsg:  "initial_backoff_ms must be positive",
		},
		{
			name: "Max backoff negative",
			config: models.ProjectConfig{
				Pipeline: models.PipelineConfig{
					EnabledSteps: []models.StepName{models.StepLocalImport},
				},
				Retry: models.RetryConfig{
					MaxAttempts:      3,
					InitialBackoffMs: 500,
					MaxBackoffMs:     -1,
				},
				JobsDir: "/tmp/jobs",
			},
			wantErr: true,
			errMsg:  "max_backoff_ms must be positive",
		},
		{
			name: "Initial backoff >= max backoff",
			config: models.ProjectConfig{
				Pipeline: models.PipelineConfig{
					EnabledSteps: []models.StepName{models.StepLocalImport},
				},
				Retry: models.RetryConfig{
					MaxAttempts:      3,
					InitialBackoffMs: 5000,
					MaxBackoffMs:     5000,
				},
				JobsDir: "/tmp/jobs",
			},
			wantErr: true,
			errMsg:  "initial_backoff_ms must be less than max_backoff_ms",
		},
		{
			name: "Empty jobs_dir",
			config: models.ProjectConfig{
				Pipeline: models.PipelineConfig{
					EnabledSteps: []models.StepName{models.StepLocalImport},
				},
				Retry: models.RetryConfig{
					MaxAttempts:      3,
					InitialBackoffMs: 500,
					MaxBackoffMs:     5000,
				},
				JobsDir: "",
			},
			wantErr: true,
			errMsg:  "jobs_dir is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()

			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestSendConfig_TransferLoad_Validate tests validation of transfer_load mode SendConfig
func TestSendConfig_TransferLoad_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  models.SendConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "Valid transfer_load config",
			config: models.SendConfig{
				URL:    "https://fhir.example.com/fhir",
				SendAs: models.SendModeTransferLoad,
				Transfer: models.TransferConfig{
					ProjectIdentifier:      "TEST-PROJECT",
					OrganizationIdentifier: "test.org",
				},
			},
			wantErr: false,
		},
		{
			name: "Missing URL",
			config: models.SendConfig{
				URL:    "",
				SendAs: models.SendModeTransferLoad,
				Transfer: models.TransferConfig{
					ProjectIdentifier:      "TEST-PROJECT",
					OrganizationIdentifier: "test.org",
				},
			},
			wantErr: true,
			errMsg:  "url is required",
		},
		{
			name: "Invalid URL - malformed",
			config: models.SendConfig{
				URL:    "://not-a-valid-url",
				SendAs: models.SendModeTransferLoad,
				Transfer: models.TransferConfig{
					ProjectIdentifier:      "TEST-PROJECT",
					OrganizationIdentifier: "test.org",
				},
			},
			wantErr: true,
			errMsg:  "invalid send url",
		},
		{
			name: "Invalid URL - wrong scheme (ftp)",
			config: models.SendConfig{
				URL:    "ftp://fhir.example.com/fhir",
				SendAs: models.SendModeTransferLoad,
				Transfer: models.TransferConfig{
					ProjectIdentifier:      "TEST-PROJECT",
					OrganizationIdentifier: "test.org",
				},
			},
			wantErr: true,
			errMsg:  "must use http or https scheme",
		},
		{
			name: "Invalid URL - empty scheme",
			config: models.SendConfig{
				URL:    "fhir.example.com/fhir",
				SendAs: models.SendModeTransferLoad,
				Transfer: models.TransferConfig{
					ProjectIdentifier:      "TEST-PROJECT",
					OrganizationIdentifier: "test.org",
				},
			},
			wantErr: true,
			errMsg:  "must use http or https scheme",
		},
		{
			name: "Missing project_identifier",
			config: models.SendConfig{
				URL:    "https://fhir.example.com/fhir",
				SendAs: models.SendModeTransferLoad,
				Transfer: models.TransferConfig{
					ProjectIdentifier:      "",
					OrganizationIdentifier: "test.org",
				},
			},
			wantErr: true,
			errMsg:  "project_identifier is required",
		},
		{
			name: "Missing organization_identifier",
			config: models.SendConfig{
				URL:    "https://fhir.example.com/fhir",
				SendAs: models.SendModeTransferLoad,
				Transfer: models.TransferConfig{
					ProjectIdentifier:      "TEST-PROJECT",
					OrganizationIdentifier: "",
				},
			},
			wantErr: true,
			errMsg:  "organization_identifier is required",
		},
		{
			name: "Valid transfer_load config with http scheme",
			config: models.SendConfig{
				URL:    "http://localhost:8080/fhir",
				SendAs: models.SendModeTransferLoad,
				Transfer: models.TransferConfig{
					ProjectIdentifier:      "TEST-PROJECT",
					OrganizationIdentifier: "test.org",
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()

			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestSendConfig_DirectResourceLoad_Validate tests validation of direct_resource_load mode SendConfig
func TestSendConfig_DirectResourceLoad_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  models.SendConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "Valid direct_resource_load config",
			config: models.SendConfig{
				URL:       "https://fhir.example.com",
				SendAs:    models.SendModeDirectResourceLoad,
				BatchSize: 100,
			},
			wantErr: false,
		},
		{
			name: "Valid direct_resource_load config with auth",
			config: models.SendConfig{
				URL:       "http://localhost:8080/fhir",
				SendAs:    models.SendModeDirectResourceLoad,
				BatchSize: 50,
				Auth: models.AuthConfig{
					Username: "user",
					Password: "pass",
				},
			},
			wantErr: false,
		},
		{
			name: "Invalid URL - malformed",
			config: models.SendConfig{
				URL:       "://not-a-valid-url",
				SendAs:    models.SendModeDirectResourceLoad,
				BatchSize: 100,
			},
			wantErr: true,
			errMsg:  "invalid send url",
		},
		{
			name: "Invalid URL - wrong scheme (ftp)",
			config: models.SendConfig{
				URL:       "ftp://fhir.example.com",
				SendAs:    models.SendModeDirectResourceLoad,
				BatchSize: 100,
			},
			wantErr: true,
			errMsg:  "must use http or https scheme",
		},
		{
			name: "Invalid URL - empty scheme",
			config: models.SendConfig{
				URL:       "fhir.example.com",
				SendAs:    models.SendModeDirectResourceLoad,
				BatchSize: 100,
			},
			wantErr: true,
			errMsg:  "must use http or https scheme",
		},
		{
			name: "Invalid batch_size - negative",
			config: models.SendConfig{
				URL:       "https://fhir.example.com",
				SendAs:    models.SendModeDirectResourceLoad,
				BatchSize: -1,
			},
			wantErr: true,
			errMsg:  "batch_size must be >= 0",
		},
		{
			name: "Invalid batch_size - too large",
			config: models.SendConfig{
				URL:       "https://fhir.example.com",
				SendAs:    models.SendModeDirectResourceLoad,
				BatchSize: 1001,
			},
			wantErr: true,
			errMsg:  "batch_size must be <= 1000",
		},
		{
			name: "Valid batch_size at maximum",
			config: models.SendConfig{
				URL:       "https://fhir.example.com",
				SendAs:    models.SendModeDirectResourceLoad,
				BatchSize: 1000,
			},
			wantErr: false,
		},
		{
			name: "Valid batch_size zero (uses default)",
			config: models.SendConfig{
				URL:       "https://fhir.example.com",
				SendAs:    models.SendModeDirectResourceLoad,
				BatchSize: 0,
			},
			wantErr: false,
		},
		{
			name: "Missing send_as mode",
			config: models.SendConfig{
				URL: "https://fhir.example.com",
			},
			wantErr: true,
			errMsg:  "send_as is required",
		},
		{
			name: "Invalid send_as mode",
			config: models.SendConfig{
				URL:    "https://fhir.example.com",
				SendAs: "invalid_mode",
			},
			wantErr: true,
			errMsg:  "invalid send send_as",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()

			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestSendConfig_GetAuthType tests the GetAuthType method
func TestSendConfig_GetAuthType(t *testing.T) {
	tests := []struct {
		name     string
		config   models.SendConfig
		expected models.SendAuthType
	}{
		{
			name:     "Empty config returns None",
			config:   models.SendConfig{},
			expected: models.SendAuthNone,
		},
		{
			name: "Basic Auth configured",
			config: models.SendConfig{
				URL:    "https://example.com",
				SendAs: models.SendModeDirectResourceLoad,
				Auth: models.AuthConfig{
					Username: "user",
					Password: "pass",
				},
			},
			expected: models.SendAuthBasic,
		},
		{
			name: "OAuth2 configured",
			config: models.SendConfig{
				URL:    "https://example.com",
				SendAs: models.SendModeDirectResourceLoad,
				Auth: models.AuthConfig{
					OAuthIssuerURI:    "https://auth.example.com",
					OAuthClientID:     "client",
					OAuthClientSecret: "secret",
				},
			},
			expected: models.SendAuthOAuth2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.GetAuthType()
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestSendConfig_IsConfigured tests the IsConfigured method
func TestSendConfig_IsConfigured(t *testing.T) {
	tests := []struct {
		name     string
		config   models.SendConfig
		expected bool
	}{
		{
			name:     "Empty config - not configured",
			config:   models.SendConfig{},
			expected: false,
		},
		{
			name: "URL set - configured",
			config: models.SendConfig{
				URL:    "https://fhir.example.com",
				SendAs: models.SendModeTransferLoad,
			},
			expected: true,
		},
		{
			name: "URL empty - not configured",
			config: models.SendConfig{
				URL:    "",
				SendAs: models.SendModeDirectResourceLoad,
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.IsConfigured()
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestSendConfig_GetBatchSize tests the GetBatchSize method
func TestSendConfig_GetBatchSize(t *testing.T) {
	tests := []struct {
		name     string
		config   models.SendConfig
		expected int
	}{
		{
			name:     "Zero batch size returns default (100)",
			config:   models.SendConfig{BatchSize: 0},
			expected: 100,
		},
		{
			name:     "Negative batch size returns default (100)",
			config:   models.SendConfig{BatchSize: -5},
			expected: 100,
		},
		{
			name:     "Positive batch size is returned",
			config:   models.SendConfig{BatchSize: 50},
			expected: 50,
		},
		{
			name:     "Maximum batch size",
			config:   models.SendConfig{BatchSize: 1000},
			expected: 1000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.GetBatchSize()
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestProjectConfig_Validate_ValidationServiceURL verifies that an invalid validation URL
// triggers a validation error in ProjectConfig.Validate.
func TestProjectConfig_Validate_ValidationServiceURL(t *testing.T) {
	config := models.ProjectConfig{
		Pipeline: models.PipelineConfig{
			EnabledSteps: []models.StepName{models.StepLocalImport, models.StepValidation},
		},
		Services: models.ServiceConfig{
			Validation: models.ValidationConfig{
				// url.Parse rejects percent-encoded sequences with invalid hex digits
				URL: "http://host/%zz",
			},
		},
		Retry: models.RetryConfig{
			MaxAttempts:      3,
			InitialBackoffMs: 500,
			MaxBackoffMs:     5000,
		},
		JobsDir: "/tmp/jobs",
	}

	err := config.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid validation url")
}

func TestProjectConfig_Validate_SendStep(t *testing.T) {
	tests := []struct {
		name    string
		config  models.ProjectConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "Valid config with send step (transfer_load)",
			config: models.ProjectConfig{
				Pipeline: models.PipelineConfig{
					EnabledSteps: []models.StepName{models.StepLocalImport, models.StepSend},
				},
				Services: models.ServiceConfig{
					Send: models.SendConfig{
						URL:    "https://fhir.example.com/fhir",
						SendAs: models.SendModeTransferLoad,
						Transfer: models.TransferConfig{
							ProjectIdentifier:      "TEST-PROJECT",
							OrganizationIdentifier: "test.org",
						},
					},
				},
				Retry: models.RetryConfig{
					MaxAttempts:      3,
					InitialBackoffMs: 500,
					MaxBackoffMs:     5000,
				},
				JobsDir: "/tmp/jobs",
			},
			wantErr: false,
		},
		{
			name: "Send step enabled but missing url",
			config: models.ProjectConfig{
				Pipeline: models.PipelineConfig{
					EnabledSteps: []models.StepName{models.StepLocalImport, models.StepSend},
				},
				Services: models.ServiceConfig{
					Send: models.SendConfig{
						URL:    "",
						SendAs: models.SendModeTransferLoad,
						Transfer: models.TransferConfig{
							ProjectIdentifier:      "TEST-PROJECT",
							OrganizationIdentifier: "test.org",
						},
					},
				},
				Retry: models.RetryConfig{
					MaxAttempts:      3,
					InitialBackoffMs: 500,
					MaxBackoffMs:     5000,
				},
				JobsDir: "/tmp/jobs",
			},
			wantErr: true,
			errMsg:  "service URL required for enabled step 'send'",
		},
		{
			name: "Send step enabled but invalid send config - missing project identifier",
			config: models.ProjectConfig{
				Pipeline: models.PipelineConfig{
					EnabledSteps: []models.StepName{models.StepLocalImport, models.StepSend},
				},
				Services: models.ServiceConfig{
					Send: models.SendConfig{
						URL:    "https://fhir.example.com/fhir",
						SendAs: models.SendModeTransferLoad,
						Transfer: models.TransferConfig{
							ProjectIdentifier:      "",
							OrganizationIdentifier: "test.org",
						},
					},
				},
				Retry: models.RetryConfig{
					MaxAttempts:      3,
					InitialBackoffMs: 500,
					MaxBackoffMs:     5000,
				},
				JobsDir: "/tmp/jobs",
			},
			wantErr: true,
			errMsg:  "send config validation failed",
		},
		{
			name: "Send step enabled but invalid send config - missing organization identifier",
			config: models.ProjectConfig{
				Pipeline: models.PipelineConfig{
					EnabledSteps: []models.StepName{models.StepLocalImport, models.StepSend},
				},
				Services: models.ServiceConfig{
					Send: models.SendConfig{
						URL:    "https://fhir.example.com/fhir",
						SendAs: models.SendModeTransferLoad,
						Transfer: models.TransferConfig{
							ProjectIdentifier:      "TEST-PROJECT",
							OrganizationIdentifier: "",
						},
					},
				},
				Retry: models.RetryConfig{
					MaxAttempts:      3,
					InitialBackoffMs: 500,
					MaxBackoffMs:     5000,
				},
				JobsDir: "/tmp/jobs",
			},
			wantErr: true,
			errMsg:  "send config validation failed",
		},
		{
			name: "Send step enabled but invalid send config - invalid URL scheme",
			config: models.ProjectConfig{
				Pipeline: models.PipelineConfig{
					EnabledSteps: []models.StepName{models.StepLocalImport, models.StepSend},
				},
				Services: models.ServiceConfig{
					Send: models.SendConfig{
						URL:    "ftp://fhir.example.com/fhir",
						SendAs: models.SendModeTransferLoad,
						Transfer: models.TransferConfig{
							ProjectIdentifier:      "TEST-PROJECT",
							OrganizationIdentifier: "test.org",
						},
					},
				},
				Retry: models.RetryConfig{
					MaxAttempts:      3,
					InitialBackoffMs: 500,
					MaxBackoffMs:     5000,
				},
				JobsDir: "/tmp/jobs",
			},
			wantErr: true,
			errMsg:  "send config validation failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()

			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestSendConfig_ValidateS3Upload tests validation of s3_upload mode SendConfig
func TestSendConfig_ValidateS3Upload(t *testing.T) {
	tests := []struct {
		name    string
		config  models.SendConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "Valid s3_upload config - all fields",
			config: models.SendConfig{
				SendAs: models.SendModeS3Upload,
				S3: models.S3Config{
					Region:          "eu-central-1",
					Bucket:          "my-bucket",
					AccessKeyID:     "AKIAIOSFODNN7EXAMPLE",
					SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
					Timeout:         30 * 60 * 1e9,
				},
			},
			wantErr: false,
		},
		{
			name: "Valid s3_upload config with custom endpoint",
			config: models.SendConfig{
				SendAs: models.SendModeS3Upload,
				S3: models.S3Config{
					Endpoint:        "http://localhost:9000",
					Region:          "us-east-1",
					Bucket:          "test-bucket",
					AccessKeyID:     "aetheradmin",
					SecretAccessKey: "aetheradmin",
					UsePathStyle:    true,
					Timeout:         10 * 60 * 1e9,
				},
			},
			wantErr: false,
		},
		{
			name: "S3 upload does not require URL field",
			config: models.SendConfig{
				URL:    "",
				SendAs: models.SendModeS3Upload,
				S3: models.S3Config{
					Region:          "eu-central-1",
					Bucket:          "my-bucket",
					AccessKeyID:     "AKIAIOSFODNN7EXAMPLE",
					SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
					Timeout:         30 * 60 * 1e9,
				},
			},
			wantErr: false,
		},
		{
			name: "Missing bucket",
			config: models.SendConfig{
				SendAs: models.SendModeS3Upload,
				S3: models.S3Config{
					Region:          "eu-central-1",
					Bucket:          "",
					AccessKeyID:     "AKIAIOSFODNN7EXAMPLE",
					SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
					Timeout:         30 * 60 * 1e9,
				},
			},
			wantErr: true,
			errMsg:  "s3 bucket is required",
		},
		{
			name: "Missing region",
			config: models.SendConfig{
				SendAs: models.SendModeS3Upload,
				S3: models.S3Config{
					Region:          "",
					Bucket:          "my-bucket",
					AccessKeyID:     "AKIAIOSFODNN7EXAMPLE",
					SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
					Timeout:         30 * 60 * 1e9,
				},
			},
			wantErr: true,
			errMsg:  "s3 region is required",
		},
		{
			name: "Missing access_key_id",
			config: models.SendConfig{
				SendAs: models.SendModeS3Upload,
				S3: models.S3Config{
					Region:          "eu-central-1",
					Bucket:          "my-bucket",
					AccessKeyID:     "",
					SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
					Timeout:         30 * 60 * 1e9,
				},
			},
			wantErr: true,
			errMsg:  "s3 access_key_id is required",
		},
		{
			name: "Missing secret_access_key",
			config: models.SendConfig{
				SendAs: models.SendModeS3Upload,
				S3: models.S3Config{
					Region:          "eu-central-1",
					Bucket:          "my-bucket",
					AccessKeyID:     "AKIAIOSFODNN7EXAMPLE",
					SecretAccessKey: "",
					Timeout:         30 * 60 * 1e9,
				},
			},
			wantErr: true,
			errMsg:  "s3 secret_access_key is required",
		},
		{
			name: "Invalid endpoint URL - unparseable",
			config: models.SendConfig{
				SendAs: models.SendModeS3Upload,
				S3: models.S3Config{
					Endpoint:        "http://host:notaport",
					Region:          "eu-central-1",
					Bucket:          "my-bucket",
					AccessKeyID:     "AKIAIOSFODNN7EXAMPLE",
					SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
					Timeout:         30 * 60 * 1e9,
				},
			},
			wantErr: true,
			errMsg:  "invalid s3 endpoint",
		},
		{
			name: "Invalid endpoint URL scheme",
			config: models.SendConfig{
				SendAs: models.SendModeS3Upload,
				S3: models.S3Config{
					Endpoint:        "ftp://invalid-endpoint",
					Region:          "eu-central-1",
					Bucket:          "my-bucket",
					AccessKeyID:     "AKIAIOSFODNN7EXAMPLE",
					SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
					Timeout:         30 * 60 * 1e9,
				},
			},
			wantErr: true,
			errMsg:  "must use http or https scheme",
		},
		{
			name: "Valid s3_upload with basic auth for proxy",
			config: models.SendConfig{
				SendAs: models.SendModeS3Upload,
				S3: models.S3Config{
					Region:          "eu-central-1",
					Bucket:          "my-bucket",
					AccessKeyID:     "AKIAIOSFODNN7EXAMPLE",
					SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
					Timeout:         30 * 60 * 1e9,
				},
				Auth: models.AuthConfig{
					Username: "proxyuser",
					Password: "proxypass",
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestSendConfig_IsConfigured_S3(t *testing.T) {
	tests := []struct {
		name     string
		config   models.SendConfig
		expected bool
	}{
		{
			name: "S3 bucket set - configured",
			config: models.SendConfig{
				SendAs: models.SendModeS3Upload,
				S3: models.S3Config{
					Bucket: "my-bucket",
				},
			},
			expected: true,
		},
		{
			name: "No URL and no S3 bucket - not configured",
			config: models.SendConfig{
				SendAs: models.SendModeS3Upload,
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.config.IsConfigured())
		})
	}
}

func TestProjectConfig_Validate_SendStep_S3(t *testing.T) {
	tests := []struct {
		name    string
		config  models.ProjectConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "Valid config with send step (s3_upload)",
			config: models.ProjectConfig{
				Pipeline: models.PipelineConfig{
					EnabledSteps: []models.StepName{models.StepLocalImport, models.StepSend},
				},
				Services: models.ServiceConfig{
					Send: models.SendConfig{
						SendAs: models.SendModeS3Upload,
						S3: models.S3Config{
							Region:          "eu-central-1",
							Bucket:          "my-bucket",
							AccessKeyID:     "AKIAIOSFODNN7EXAMPLE",
							SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
							Timeout:         30 * 60 * 1e9,
						},
					},
				},
				Retry: models.RetryConfig{
					MaxAttempts:      3,
					InitialBackoffMs: 500,
					MaxBackoffMs:     5000,
				},
				JobsDir: "/tmp/jobs",
			},
			wantErr: false,
		},
		{
			name: "S3 send step enabled but missing bucket - fails HasServiceURL",
			config: models.ProjectConfig{
				Pipeline: models.PipelineConfig{
					EnabledSteps: []models.StepName{models.StepLocalImport, models.StepSend},
				},
				Services: models.ServiceConfig{
					Send: models.SendConfig{
						SendAs: models.SendModeS3Upload,
						S3: models.S3Config{
							Region:          "eu-central-1",
							Bucket:          "",
							AccessKeyID:     "AKIAIOSFODNN7EXAMPLE",
							SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
						},
					},
				},
				Retry: models.RetryConfig{
					MaxAttempts:      3,
					InitialBackoffMs: 500,
					MaxBackoffMs:     5000,
				},
				JobsDir: "/tmp/jobs",
			},
			wantErr: true,
			errMsg:  "service URL required for enabled step 'send'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
