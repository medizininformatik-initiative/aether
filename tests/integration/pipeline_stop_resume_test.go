package integration

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/pipeline"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

func testStopLogger() *lib.Logger { return lib.NewLogger(lib.LogLevelError) }

// TestStopAndResume_SignalPath proves the story of the user who presses Ctrl+C:
// the job records stopped, `job list` shows stopped, and the job resumes.
func TestStopAndResume_SignalPath(t *testing.T) {
	jobsDir := t.TempDir()
	job := createCompletedImportJob(t, jobsDir, 3)

	// The signal handler runs while the import step is complete but the job is not.
	require.NoError(t, pipeline.MarkJobStopped(jobsDir, job.JobID))

	reloaded, err := pipeline.LoadJob(jobsDir, job.JobID)
	require.NoError(t, err)
	assert.Equal(t, models.JobStatusStopped, reloaded.Status,
		"the state file must record why the job ended")

	assert.Equal(t, models.JobStatusStopped, services.EffectiveJobStatus(jobsDir, *reloaded),
		"`job list` must show the job as stopped")

	// The import step keeps its result, thus the job continues from the next step.
	importStep, found := models.GetStepByName(*reloaded, models.StepLocalImport)
	require.True(t, found)
	assert.Equal(t, models.StepStatusCompleted, importStep.Status)
}

// TestStopAndResume_CrashPath proves the liveness probe covers a process that
// died without a chance to write, for example from SIGKILL or a power loss.
func TestStopAndResume_CrashPath(t *testing.T) {
	jobsDir := t.TempDir()
	job := createCompletedImportJob(t, jobsDir, 2)

	// A killed process leaves in_progress on disk and holds no lock.
	inProgress := models.UpdateJobStatus(*job, models.JobStatusInProgress)
	require.NoError(t, pipeline.UpdateJob(jobsDir, &inProgress))

	reloaded, err := pipeline.LoadJob(jobsDir, job.JobID)
	require.NoError(t, err)
	assert.Equal(t, models.JobStatusInProgress, reloaded.Status,
		"the state file keeps in_progress; the probe does not write")

	assert.Equal(t, models.JobStatusStopped, services.EffectiveJobStatus(jobsDir, *reloaded),
		"no process holds the lock, thus the job is stopped")
}

// TestStopAndResume_LiveJobStaysInProgress proves the probe does not call a
// running job stopped.
func TestStopAndResume_LiveJobStaysInProgress(t *testing.T) {
	jobsDir := t.TempDir()
	job := createCompletedImportJob(t, jobsDir, 1)

	inProgress := models.UpdateJobStatus(*job, models.JobStatusInProgress)
	require.NoError(t, pipeline.UpdateJob(jobsDir, &inProgress))

	lock, err := services.AcquireJobLock(jobsDir, job.JobID, testStopLogger())
	require.NoError(t, err)
	defer func() { _ = lock.Release() }()

	assert.Equal(t, models.JobStatusInProgress,
		services.EffectiveJobStatus(jobsDir, inProgress))
}
