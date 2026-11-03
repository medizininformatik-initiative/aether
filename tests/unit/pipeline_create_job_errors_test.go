package unit

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/pipeline"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

func createJobConfig(jobsDir string) models.ProjectConfig {
	return models.ProjectConfig{
		JobsDir:  jobsDir,
		Pipeline: models.PipelineConfig{EnabledSteps: []models.StepName{models.StepLocalImport}},
	}
}

func TestCreateJob_ErrorWraps(t *testing.T) {
	logger := quietLogger()

	t.Run("source with .json extension that is no CRTDL", func(t *testing.T) {
		notCRTDL := filepath.Join(t.TempDir(), "x.json")
		require.NoError(t, os.WriteFile(notCRTDL, []byte(`{}`), 0o644))

		_, err := pipeline.CreateJob(models.GenerateJobID(), notCRTDL, "", createJobConfig(t.TempDir()), logger)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to detect input type: ")
	})

	t.Run("invalid job", func(t *testing.T) {
		_, err := pipeline.CreateJob("not-a-uuid", "", "", createJobConfig(t.TempDir()), logger)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create valid job: ")
	})

	t.Run("job directories cannot be created", func(t *testing.T) {
		blocker := filepath.Join(t.TempDir(), "file")
		require.NoError(t, os.WriteFile(blocker, nil, 0o644))

		_, err := pipeline.CreateJob(models.GenerateJobID(), "", "crtdl.json", createJobConfig(filepath.Join(blocker, "jobs")), logger)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create job directories: ")
	})

	t.Run("initial job state cannot be saved", func(t *testing.T) {
		jobsDir := t.TempDir()
		jobID := models.GenerateJobID()
		blockedState := services.GetStateFilePath(jobsDir, jobID)
		require.NoError(t, os.MkdirAll(filepath.Join(blockedState, "keep"), 0o755))

		_, err := pipeline.CreateJob(jobID, "", "crtdl.json", createJobConfig(jobsDir), logger)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to save initial job state: ")
	})

	t.Run("CRTDL cannot be prepared", func(t *testing.T) {
		missing := filepath.Join(t.TempDir(), "missing.json")

		_, err := pipeline.CreateJob(models.GenerateJobID(), "", missing, createJobConfig(t.TempDir()), logger)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to prepare CRTDL: ")
	})
}
