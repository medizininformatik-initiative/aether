package cmd

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/medizininformatik-initiative/aether/internal/models"
)

func TestStepProgressSuffix(t *testing.T) {
	inProgress := models.PipelineStep{
		Name:   models.StepTorchImport,
		Status: models.StepStatusInProgress,
		Progress: &models.StepProgress{
			Message:   "1/3 batches (500/1200 patients)",
			Completed: 1,
			Total:     3,
			UpdatedAt: time.Now(),
		},
	}
	assert.Equal(t, " — 1/3 batches (500/1200 patients)", stepProgressSuffix(inProgress))

	noProgress := models.PipelineStep{Name: models.StepTorchImport, Status: models.StepStatusInProgress}
	assert.Equal(t, "", stepProgressSuffix(noProgress))

	completed := inProgress
	completed.Status = models.StepStatusCompleted
	assert.Equal(t, "", stepProgressSuffix(completed))
}
