package unit

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/pipeline"
)

// persistJobWithStatus writes a minimal valid job to disk at the given status.
func persistJobWithStatus(t *testing.T, status models.JobStatus) (jobsDir, jobID string) {
	t.Helper()
	job := seamLoopJob(t, nil)
	job.Status = status
	require.NoError(t, pipeline.UpdateJob(job.Config.JobsDir, job))
	return job.Config.JobsDir, job.JobID
}

// TestMarkJobStopped_InProgressBecomesStopped proves the signal path records why
// the job ended: a user who presses Ctrl+C sees stopped, not in_progress.
func TestMarkJobStopped_InProgressBecomesStopped(t *testing.T) {
	jobsDir, jobID := persistJobWithStatus(t, models.JobStatusInProgress)

	require.NoError(t, pipeline.MarkJobStopped(jobsDir, jobID))

	reloaded, err := pipeline.LoadJob(jobsDir, jobID)
	require.NoError(t, err)
	assert.Equal(t, models.JobStatusStopped, reloaded.Status)
}

// TestMarkJobStopped_TerminalStatusUnchanged proves a signal that arrives after
// the loop already settled the job does not overwrite the real outcome.
func TestMarkJobStopped_TerminalStatusUnchanged(t *testing.T) {
	for _, status := range []models.JobStatus{
		models.JobStatusCompleted,
		models.JobStatusFailed,
		models.JobStatusWaiting,
	} {
		t.Run(string(status), func(t *testing.T) {
			jobsDir, jobID := persistJobWithStatus(t, status)

			require.NoError(t, pipeline.MarkJobStopped(jobsDir, jobID))

			reloaded, err := pipeline.LoadJob(jobsDir, jobID)
			require.NoError(t, err)
			assert.Equal(t, status, reloaded.Status)
		})
	}
}

// TestMarkJobStopped_UnknownJob proves an unreadable job gives an error instead
// of writing a state file for a job that does not exist.
func TestMarkJobStopped_UnknownJob(t *testing.T) {
	assert.Error(t, pipeline.MarkJobStopped(t.TempDir(), "no-such-job"))
}

// TestMarkJobStopped_LaterWriteKeepsStopped proves the stop wins the race with
// the run loop. The signal handler and the loop write the same state file from
// two goroutines. The process stops immediately after the handler writes, thus a
// step that finishes in that moment must not put the job back to in_progress.
func TestMarkJobStopped_LaterWriteKeepsStopped(t *testing.T) {
	jobsDir, jobID := persistJobWithStatus(t, models.JobStatusInProgress)

	require.NoError(t, pipeline.MarkJobStopped(jobsDir, jobID))

	// The run loop finishes its step and saves, as it does after every step.
	job, err := pipeline.LoadJob(jobsDir, jobID)
	require.NoError(t, err)
	running := models.UpdateJobStatus(*job, models.JobStatusInProgress)
	require.NoError(t, pipeline.UpdateJob(jobsDir, &running))

	reloaded, err := pipeline.LoadJob(jobsDir, jobID)
	require.NoError(t, err)
	assert.Equal(t, models.JobStatusStopped, reloaded.Status,
		"a write after the stop must not undo it")
}

// TestMarkJobStopped_ConcurrentWritesDoNotLoseTheStop runs the two writers at the
// same time, which is what the signal handler and the run loop really do.
func TestMarkJobStopped_ConcurrentWritesDoNotLoseTheStop(t *testing.T) {
	jobsDir, jobID := persistJobWithStatus(t, models.JobStatusInProgress)

	job, err := pipeline.LoadJob(jobsDir, jobID)
	require.NoError(t, err)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_ = pipeline.MarkJobStopped(jobsDir, jobID)
	}()
	go func() {
		defer wg.Done()
		for range 50 {
			running := models.UpdateJobStatus(*job, models.JobStatusInProgress)
			_ = pipeline.UpdateJob(jobsDir, &running)
		}
	}()
	wg.Wait()

	reloaded, err := pipeline.LoadJob(jobsDir, jobID)
	require.NoError(t, err)
	assert.Equal(t, models.JobStatusStopped, reloaded.Status)
}
