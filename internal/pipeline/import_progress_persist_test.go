package pipeline

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/services"
	"github.com/medizininformatik-initiative/aether/internal/services/servicestest"
)

// progressReporter is an Extractor that only records the progress handler the
// import step registers, so the handler can be driven directly.
type progressReporter struct {
	servicestest.MockExtractor
	handler func(services.TORCHProgress)
}

func (p *progressReporter) SetProgressHandler(fn func(services.TORCHProgress)) { p.handler = fn }

func progressJob(jobsDir string, steps ...models.StepName) *models.PipelineJob {
	cfg := models.DefaultConfig()
	cfg.JobsDir = jobsDir
	job := &models.PipelineJob{
		JobID:       "550e8400-e29b-41d4-a716-446655440000",
		InputType:   models.InputTypeCRTDL,
		InputSource: "crtdl.json",
		Status:      models.JobStatusInProgress,
		Config:      cfg,
	}
	for _, name := range steps {
		job.Steps = append(job.Steps, models.PipelineStep{Name: name, Status: models.StepStatusPending})
	}
	return job
}

var sampleProgress = services.TORCHProgress{CohortSize: 100, BatchSize: 50, BatchesTotal: 2, BatchesCompleted: 1}

// Progress belongs to the torch step only: steps in front of it in the job must
// keep their nil progress.
func TestAttachTORCHProgressPersistence_OnlyUpdatesTORCHStep(t *testing.T) {
	jobsDir := t.TempDir()
	job := progressJob(jobsDir, models.StepValidation, models.StepTorchImport)
	require.NoError(t, UpdateJob(jobsDir, job))

	reporter := &progressReporter{}
	attachTORCHProgressPersistence(job, reporter, lib.NewLogger(lib.LogLevelError))
	require.NotNil(t, reporter.handler)

	reporter.handler(sampleProgress)

	assert.Nil(t, job.Steps[0].Progress, "a non-torch step must not receive TORCH progress")
	require.NotNil(t, job.Steps[1].Progress)
	assert.Equal(t, 1, job.Steps[1].Progress.Completed)
	assert.Equal(t, 2, job.Steps[1].Progress.Total)
	assert.Equal(t, sampleProgress.Summary(), job.Steps[1].Progress.Message)

	reloaded, err := LoadJob(jobsDir, job.JobID)
	require.NoError(t, err)
	diskStep, found := models.GetStepByName(*reloaded, models.StepTorchImport)
	require.True(t, found)
	require.NotNil(t, diskStep.Progress)
	assert.Equal(t, 1, diskStep.Progress.Completed)
}

// Persistence is best effort: an unwritable jobs directory must not stop the
// in-memory progress update, which is what the running step reports.
func TestAttachTORCHProgressPersistence_SurvivesWriteFailure(t *testing.T) {
	blocked := filepath.Join(t.TempDir(), "jobs")
	require.NoError(t, os.WriteFile(blocked, []byte("not a directory"), 0600))

	job := progressJob(blocked, models.StepTorchImport)
	reporter := &progressReporter{}
	attachTORCHProgressPersistence(job, reporter, lib.NewLogger(lib.LogLevelError))
	require.NotNil(t, reporter.handler)

	assert.NotPanics(t, func() { reporter.handler(sampleProgress) })

	require.NotNil(t, job.Steps[0].Progress)
	assert.Equal(t, 1, job.Steps[0].Progress.Completed)

	_, err := LoadJob(blocked, job.JobID)
	assert.Error(t, err, "nothing can be persisted to an unwritable jobs directory")
}

// A client without a progress handler seam is accepted unchanged.
func TestAttachTORCHProgressPersistence_IgnoresClientWithoutSeam(t *testing.T) {
	job := progressJob(t.TempDir(), models.StepTorchImport)

	assert.NotPanics(t, func() {
		attachTORCHProgressPersistence(job, &servicestest.MockExtractor{}, lib.NewLogger(lib.LogLevelError))
	})
	assert.Nil(t, job.Steps[0].Progress)
}
