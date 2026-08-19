package unit

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/pipeline"
)

// statusProbeStep reads the persisted job status while the step runs, so a test
// can see what `job list` would show during the run.
type statusProbeStep struct {
	name    models.StepName
	jobsDir string
	jobID   string
	seen    models.JobStatus
}

func (s *statusProbeStep) Name() models.StepName { return s.name }

func (s *statusProbeStep) Run(_ *pipeline.StepContext) (pipeline.StepResult, error) {
	if job, err := pipeline.LoadJob(s.jobsDir, s.jobID); err == nil {
		s.seen = job.Status
	}
	return pipeline.StepResult{}, nil
}

// TestRunLoop_ResumeClearsNonRunningStatus proves a resumed job reports in_progress
// while it runs. Without this, a job resumed after Ctrl+C keeps stopped on disk and
// `job list` shows a running job as stopped.
func TestRunLoop_ResumeClearsNonRunningStatus(t *testing.T) {
	for _, status := range []models.JobStatus{models.JobStatusStopped, models.JobStatusWaiting} {
		t.Run(string(status), func(t *testing.T) {
			probe := &statusProbeStep{name: models.StepLocalImport}
			dimp := &seamFakeStep{name: models.StepDIMP}
			job := seamLoopJob(t, map[models.StepName]pipeline.Step{
				models.StepLocalImport: probe,
				models.StepDIMP:        dimp,
			})
			probe.jobsDir, probe.jobID = job.Config.JobsDir, job.JobID

			job.Status = status
			require.NoError(t, pipeline.UpdateJob(job.Config.JobsDir, job))

			_, err := pipeline.RunLoop(job, seamLogger(), pipeline.RunOptions{})

			require.NoError(t, err)
			assert.Equal(t, models.JobStatusInProgress, probe.seen,
				"a job must report in_progress while a step runs")
		})
	}
}

// TestRunLoop_ResumeSaveErrorSurfaces proves the loop stops if it cannot record
// that it claimed the job. It must not run a step whose state it cannot persist.
func TestRunLoop_ResumeSaveErrorSurfaces(t *testing.T) {
	imp := &seamFakeStep{name: models.StepLocalImport}
	dimp := &seamFakeStep{name: models.StepDIMP}
	job := seamLoopJob(t, map[models.StepName]pipeline.Step{
		models.StepLocalImport: imp,
		models.StepDIMP:        dimp,
	})
	job.Status = models.JobStatusStopped
	require.NoError(t, pipeline.UpdateJob(job.Config.JobsDir, job))

	failSaveOnNth(t, 1) // the resume-claim save is the first save attempt

	_, err := pipeline.RunLoop(job, seamLogger(), pipeline.RunOptions{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to save job state")
	assert.Equal(t, 0, imp.calls, "no step may run if the claim cannot be saved")
}
