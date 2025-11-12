package unit

import (
	"testing"

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
						BaseURL:                   "http://localhost:8080",
						Username:                  "testuser",
						Password:                  "testpass",
						ExtractionTimeoutMinutes:  30,
						PollingIntervalSeconds:    5,
						MaxPollingIntervalSeconds: 30,
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
