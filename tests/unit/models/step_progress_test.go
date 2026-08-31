package models_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/models"
)

func TestPipelineStep_Progress_JSONRoundTrip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	step := models.PipelineStep{
		Name:   models.StepTorchImport,
		Status: models.StepStatusInProgress,
		Progress: &models.StepProgress{
			Message:   "1/3 batches (500/1200 patients)",
			Completed: 1,
			Total:     3,
			UpdatedAt: now,
		},
	}

	data, err := json.Marshal(step)
	require.NoError(t, err)

	var decoded models.PipelineStep
	require.NoError(t, json.Unmarshal(data, &decoded))
	require.NotNil(t, decoded.Progress)
	assert.Equal(t, "1/3 batches (500/1200 patients)", decoded.Progress.Message)
	assert.Equal(t, 1, decoded.Progress.Completed)
	assert.Equal(t, 3, decoded.Progress.Total)
	assert.Equal(t, now, decoded.Progress.UpdatedAt)
}

func TestPipelineStep_Progress_AbsentInOldJobFiles(t *testing.T) {
	old := `{"name":"torch","status":"completed","files_processed":2,"bytes_processed":100}`

	var step models.PipelineStep
	require.NoError(t, json.Unmarshal([]byte(old), &step))
	assert.Nil(t, step.Progress)

	data, err := json.Marshal(step)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "progress")
}
