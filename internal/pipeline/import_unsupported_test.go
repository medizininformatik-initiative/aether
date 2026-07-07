package pipeline

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

// TestImportStep_UnsupportedName covers importStep.Run's defensive default: an
// importStep carrying a name that is not one of the three import variants is
// rejected. Registered production dispatch never reaches this branch (the
// registry binds importStep only to torch/local/http), so it is exercised
// white-box by constructing the step directly.
func TestImportStep_UnsupportedName(t *testing.T) {
	job := &models.PipelineJob{
		JobID:       "test-job-123",
		InputType:   models.InputTypeLocal,
		CurrentStep: string(models.StepDIMP),
		Config: models.ProjectConfig{
			JobsDir: t.TempDir(),
			Retry:   models.RetryConfig{MaxAttempts: 3, InitialBackoffMs: 500, MaxBackoffMs: 5000},
		},
	}
	ctx := &StepContext{Job: job, Layout: services.NewJobLayoutForDir(t.TempDir(), nil), Logger: lib.NewLogger(lib.LogLevelInfo)}

	_, err := importStep{name: models.StepDIMP}.Run(ctx)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported import step")
}
