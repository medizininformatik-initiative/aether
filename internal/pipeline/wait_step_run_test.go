package pipeline

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
)

// waitRunTestJob builds a job with a [local_import, wait] pipeline whose wait
// step starts in the given status, plus its job dir. The import step is marked
// completed so WaitDir resolves against import/.
func waitRunTestJob(t *testing.T, waitStatus models.StepStatus) (*models.PipelineJob, string) {
	t.Helper()
	jobsBaseDir := t.TempDir()
	jobID := "wait-run-test"
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

func waitRunContext(job *models.PipelineJob, jobDir string) *StepContext {
	return &StepContext{Job: job, JobDir: jobDir, Logger: lib.NewLogger(lib.LogLevelError)}
}

func TestWaitStepRun_FirstArrivalPausesAndCreatesDir(t *testing.T) {
	job, jobDir := waitRunTestJob(t, models.StepStatusPending)

	err := runStep(waitStep{}, waitRunContext(job, jobDir))

	require.ErrorIs(t, err, ErrPaused)
	s, _ := models.GetStepByName(*job, models.StepWait)
	assert.Equal(t, models.StepStatusWaiting, s.Status)
	// import_wait directory created, empty
	info, statErr := os.Stat(filepath.Join(jobDir, "import_wait"))
	require.NoError(t, statErr)
	assert.True(t, info.IsDir())
}

func TestWaitStepRun_ResumeWithFilesCompletes(t *testing.T) {
	job, jobDir := waitRunTestJob(t, models.StepStatusWaiting)
	waitDir := filepath.Join(jobDir, "import_wait")
	require.NoError(t, os.MkdirAll(waitDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(waitDir, "edited.ndjson"), []byte("{}\n"), 0644))

	err := runStep(waitStep{}, waitRunContext(job, jobDir))

	require.NoError(t, err)
	s, _ := models.GetStepByName(*job, models.StepWait)
	assert.Equal(t, models.StepStatusCompleted, s.Status)
	assert.Equal(t, 1, s.FilesProcessed)
}

func TestWaitStepRun_ResumeStillEmptyPausesAgain(t *testing.T) {
	job, jobDir := waitRunTestJob(t, models.StepStatusWaiting)
	require.NoError(t, os.MkdirAll(filepath.Join(jobDir, "import_wait"), 0755))

	err := runStep(waitStep{}, waitRunContext(job, jobDir))

	require.ErrorIs(t, err, ErrPaused)
	s, _ := models.GetStepByName(*job, models.StepWait)
	assert.Equal(t, models.StepStatusWaiting, s.Status)
}
