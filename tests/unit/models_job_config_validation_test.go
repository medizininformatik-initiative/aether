package unit

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/models"
)

const validJobID = "550e8400-e29b-41d4-a716-446655440000"

func validJob() models.PipelineJob {
	return models.PipelineJob{
		JobID:       validJobID,
		InputSource: "/data/in",
		InputType:   models.InputTypeLocal,
		Status:      models.JobStatusPending,
	}
}

func TestPipelineJob_Validate_Rules(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(j *models.PipelineJob)
		errMsg string
	}{
		{"valid job", func(j *models.PipelineJob) {}, ""},
		{"invalid job id", func(j *models.PipelineJob) { j.JobID = "" }, "job_id is required"},
		{"empty source", func(j *models.PipelineJob) { j.InputSource = "" }, "input_source is required"},
		{"empty source with CRTDL path", func(j *models.PipelineJob) {
			j.InputSource = ""
			j.CRTDLPath = "x.crtdl"
		}, ""},
		{"empty source with local config dir", func(j *models.PipelineJob) {
			j.InputSource = ""
			j.Config.Services.LocalImport.Dir = "/data"
		}, ""},
		{"empty source with config dir but not local type", func(j *models.PipelineJob) {
			j.InputSource = ""
			j.InputType = models.InputTypeCRTDL
			j.Config.Services.LocalImport.Dir = "/data"
		}, "input_source is required"},
		{"http without scheme", func(j *models.PipelineJob) {
			j.InputType = models.InputTypeHTTP
			j.InputSource = "ftp://host/x"
		}, "input_source must be a valid HTTP(S) URL when input_type is http_url"},
		{"http with http scheme", func(j *models.PipelineJob) {
			j.InputType = models.InputTypeHTTP
			j.InputSource = "http://host/x"
		}, ""},
		{"http with https scheme", func(j *models.PipelineJob) {
			j.InputType = models.InputTypeHTTP
			j.InputSource = "https://host/x"
		}, ""},
		{"http with unparsable URL", func(j *models.PipelineJob) {
			j.InputType = models.InputTypeHTTP
			j.InputSource = "http://host/%zz"
		}, "invalid input_source URL"},
		{"invalid input type", func(j *models.PipelineJob) { j.InputType = "bogus" }, "invalid input_type: bogus"},
		{"invalid status", func(j *models.PipelineJob) { j.Status = "bogus" }, "invalid status: bogus"},
		{"current step found", func(j *models.PipelineJob) {
			j.CurrentStep = "dimp"
			j.Steps = []models.PipelineStep{{Name: models.StepLocalImport}, {Name: models.StepDIMP}}
		}, ""},
		{"current step missing", func(j *models.PipelineJob) {
			j.CurrentStep = "send"
			j.Steps = []models.PipelineStep{{Name: models.StepLocalImport}}
		}, "current_step 'send' not found in job steps"},
		{"negative total files", func(j *models.PipelineJob) { j.TotalFiles = -1 }, "total_files cannot be negative"},
		{"negative total bytes", func(j *models.PipelineJob) { j.TotalBytes = -1 }, "total_bytes cannot be negative"},
		{"invalid input type reported before invalid status", func(j *models.PipelineJob) {
			j.InputType = "bogus"
			j.Status = "bogus"
		}, "invalid input_type"},
		{"invalid status reported before current step", func(j *models.PipelineJob) {
			j.Status = "bogus"
			j.CurrentStep = "send"
		}, "invalid status"},
		{"current step reported before totals", func(j *models.PipelineJob) {
			j.CurrentStep = "send"
			j.TotalFiles = -1
		}, "current_step"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			j := validJob()
			tt.mutate(&j)
			err := j.Validate()
			if tt.errMsg == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errMsg)
		})
	}
}

func validProjectConfig() models.ProjectConfig {
	c := models.DefaultConfig()
	c.Pipeline.EnabledSteps = []models.StepName{models.StepLocalImport}
	return c
}

func TestProjectConfig_Validate_Rules(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(c *models.ProjectConfig)
		errMsg string
	}{
		{"valid config", func(c *models.ProjectConfig) {}, ""},
		{"no steps", func(c *models.ProjectConfig) { c.Pipeline.EnabledSteps = nil }, "at least one pipeline step must be enabled"},
		{"first step not import", func(c *models.ProjectConfig) {
			c.Pipeline.EnabledSteps = []models.StepName{models.StepDIMP}
		}, "first enabled step must be an import step (torch, local_import, or http_import)"},
		{"first step torch", func(c *models.ProjectConfig) {
			c.Pipeline.EnabledSteps = []models.StepName{models.StepTorchImport}
			c.Services.TORCH.BaseURL = "http://torch"
		}, ""},
		{"first step http import", func(c *models.ProjectConfig) {
			c.Pipeline.EnabledSteps = []models.StepName{models.StepHttpImport}
		}, ""},
		{"unrecognized step", func(c *models.ProjectConfig) {
			c.Pipeline.EnabledSteps = []models.StepName{models.StepLocalImport, "bogus"}
		}, "unrecognized step in enabled_steps: bogus"},
		{"dimp without url", func(c *models.ProjectConfig) {
			c.Pipeline.EnabledSteps = []models.StepName{models.StepLocalImport, models.StepDIMP}
		}, "service URL required for enabled step 'dimp'"},
		{"send without url", func(c *models.ProjectConfig) {
			c.Pipeline.EnabledSteps = []models.StepName{models.StepLocalImport, models.StepSend}
		}, "service URL required for enabled step 'send'"},
		{"flattening without url is allowed", func(c *models.ProjectConfig) {
			c.Pipeline.EnabledSteps = []models.StepName{models.StepLocalImport, models.StepFlattening}
		}, ""},
		{"torch enabled without base url", func(c *models.ProjectConfig) {
			c.Pipeline.EnabledSteps = []models.StepName{models.StepTorchImport}
		}, "TORCH config validation failed: TORCH base_url is required"},
		{"torch configured via auth only", func(c *models.ProjectConfig) {
			c.Services.TORCH.Auth.Username = "user"
		}, "TORCH config validation failed"},
		{"torch configured via base url only", func(c *models.ProjectConfig) {
			c.Services.TORCH.BaseURL = "://bad"
		}, "TORCH config validation failed"},
		{"send enabled with invalid send config", func(c *models.ProjectConfig) {
			c.Pipeline.EnabledSteps = []models.StepName{models.StepLocalImport, models.StepSend}
			c.Services.Send.URL = "http://fhir"
		}, "send config validation failed"},
		{"invalid dimp url", func(c *models.ProjectConfig) {
			c.Services.DIMP.URL = "http://host/%zz"
		}, "invalid dimp url"},
		{"invalid validation url", func(c *models.ProjectConfig) {
			c.Services.Validation.URL = "http://host/%zz"
		}, "invalid validation url"},
		{"valid validation url", func(c *models.ProjectConfig) {
			c.Services.Validation.URL = "http://host"
		}, ""},
		{"max attempts too low", func(c *models.ProjectConfig) { c.Retry.MaxAttempts = 0 }, "max_attempts must be between 1 and 10"},
		{"max attempts too high", func(c *models.ProjectConfig) { c.Retry.MaxAttempts = 11 }, "max_attempts must be between 1 and 10"},
		{"max attempts lower bound", func(c *models.ProjectConfig) { c.Retry.MaxAttempts = 1 }, ""},
		{"max attempts upper bound", func(c *models.ProjectConfig) { c.Retry.MaxAttempts = 10 }, ""},
		{"initial backoff zero", func(c *models.ProjectConfig) { c.Retry.InitialBackoffMs = 0 }, "initial_backoff_ms must be positive"},
		{"max backoff zero", func(c *models.ProjectConfig) { c.Retry.MaxBackoffMs = 0 }, "max_backoff_ms must be positive"},
		{"initial equals max backoff", func(c *models.ProjectConfig) {
			c.Retry.InitialBackoffMs = 100
			c.Retry.MaxBackoffMs = 100
		}, "initial_backoff_ms must be less than max_backoff_ms"},
		{"initial below max backoff", func(c *models.ProjectConfig) {
			c.Retry.InitialBackoffMs = 99
			c.Retry.MaxBackoffMs = 100
		}, ""},
		{"empty jobs dir", func(c *models.ProjectConfig) { c.JobsDir = "" }, "jobs_dir is required"},
		{"retry reported before jobs dir", func(c *models.ProjectConfig) {
			c.Retry.MaxAttempts = 0
			c.JobsDir = ""
		}, "max_attempts"},
		{"service url reported before torch", func(c *models.ProjectConfig) {
			c.Pipeline.EnabledSteps = []models.StepName{models.StepTorchImport, models.StepDIMP}
		}, "service URL required for enabled step 'dimp'"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := validProjectConfig()
			tt.mutate(&c)
			err := c.Validate()
			if tt.errMsg == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errMsg)
		})
	}
}
