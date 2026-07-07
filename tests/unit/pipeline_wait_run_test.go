package unit

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/pipeline"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

// waitRunJob builds a [local_import, wait] job whose wait step starts in the
// given status, plus its job dir. The import step is marked completed so
// WaitDir resolves against import/.
func waitRunJob(t *testing.T, waitStatus models.StepStatus) (*models.PipelineJob, string) {
	t.Helper()
	jobsBaseDir := t.TempDir()
	jobID := "wait-run-ext-test"
	jobDir := filepath.Join(jobsBaseDir, jobID)

	steps := []models.StepName{models.StepLocalImport, models.StepWait}
	job := &models.PipelineJob{
		JobID:       jobID,
		Status:      models.JobStatusInProgress,
		CurrentStep: string(models.StepWait),
		Steps:       models.InitializeSteps(steps),
		Config: models.ProjectConfig{
			JobsDir:  jobsBaseDir,
			Pipeline: models.PipelineConfig{EnabledSteps: steps},
		},
	}
	imp, _ := models.GetStepByName(*job, models.StepLocalImport)
	*job = models.ReplaceStep(*job, models.CompleteStep(imp, 1, 1))
	wait, _ := models.GetStepByName(*job, models.StepWait)
	wait.Status = waitStatus
	*job = models.ReplaceStep(*job, wait)
	return job, jobDir
}

func waitRunContext(job *models.PipelineJob, jobDir string) *pipeline.StepContext {
	layout := services.NewJobLayoutForDir(jobDir, job.Config.Pipeline.EnabledSteps)
	return &pipeline.StepContext{Job: job, Layout: layout, Logger: lib.NewLogger(lib.LogLevelError)}
}

// waitStepFromRegistry returns the real wait Step through the public seam.
func waitStepFromRegistry(t *testing.T) pipeline.Step {
	t.Helper()
	pipeline.ResetStepRegistry()
	step, ok := pipeline.StepFor(models.StepWait)
	require.True(t, ok)
	return step
}

func TestWaitStepRun_ReportsName(t *testing.T) {
	assert.Equal(t, models.StepWait, waitStepFromRegistry(t).Name())
}

func TestWaitStepRun_FirstArrivalPausesAndCreatesDir(t *testing.T) {
	job, jobDir := waitRunJob(t, models.StepStatusInProgress)

	_, err := waitStepFromRegistry(t).Run(waitRunContext(job, jobDir))

	require.ErrorIs(t, err, pipeline.ErrPaused)
	info, statErr := os.Stat(filepath.Join(jobDir, "import_wait"))
	require.NoError(t, statErr)
	assert.True(t, info.IsDir(), "wait directory must be created")
}

func TestWaitStepRun_ResumeWithFilesCompletes(t *testing.T) {
	job, jobDir := waitRunJob(t, models.StepStatusInProgress)
	waitDir := filepath.Join(jobDir, "import_wait")
	require.NoError(t, os.MkdirAll(waitDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(waitDir, "edited.ndjson"), []byte("{}\n"), 0644))

	result, err := waitStepFromRegistry(t).Run(waitRunContext(job, jobDir))

	require.NoError(t, err)
	assert.Equal(t, 1, result.FilesProcessed)
}

func TestWaitStepRun_ResumeStillEmptyPausesAgain(t *testing.T) {
	job, jobDir := waitRunJob(t, models.StepStatusInProgress)
	require.NoError(t, os.MkdirAll(filepath.Join(jobDir, "import_wait"), 0755))

	_, err := waitStepFromRegistry(t).Run(waitRunContext(job, jobDir))

	require.ErrorIs(t, err, pipeline.ErrPaused)
}

func TestWaitStepRun_NoWaitInProgressErrors(t *testing.T) {
	// Wait step is pending, not in progress: GetCurrentWaitStepIndex fails.
	job, jobDir := waitRunJob(t, models.StepStatusPending)

	_, err := waitStepFromRegistry(t).Run(waitRunContext(job, jobDir))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no wait step currently in progress")
}

func TestWaitStepRun_CreateDirErrorSurfaces(t *testing.T) {
	job, jobDir := waitRunJob(t, models.StepStatusInProgress)

	original := pipeline.FileOpsProvider
	pipeline.FileOpsProvider = &mockFileOps{
		mkdirFunc: func(string, os.FileMode) error { return os.ErrPermission },
	}
	defer func() { pipeline.FileOpsProvider = original }()

	_, err := waitStepFromRegistry(t).Run(waitRunContext(job, jobDir))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create wait directory")
}

func TestWaitStepRun_CountFilesErrorSurfaces(t *testing.T) {
	job, jobDir := waitRunJob(t, models.StepStatusInProgress)

	original := pipeline.FileOpsProvider
	pipeline.FileOpsProvider = &mockFileOps{
		// MkdirAll succeeds (nil func -> real os.MkdirAll), but reading the wait
		// directory fails with a non-NotExist error.
		readDirFunc: func(string) ([]os.DirEntry, error) { return nil, os.ErrPermission },
	}
	defer func() { pipeline.FileOpsProvider = original }()

	_, err := waitStepFromRegistry(t).Run(waitRunContext(job, jobDir))

	require.Error(t, err)
	assert.ErrorIs(t, err, os.ErrPermission)
}
