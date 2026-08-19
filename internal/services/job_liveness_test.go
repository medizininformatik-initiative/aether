package services

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/models"
)

// TestEffectiveJobStatus_InProgressWithoutLiveProcess proves the liveness probe:
// an in_progress job whose lock no process holds reports stopped, because the
// process died (Ctrl+C, SIGKILL, crash) without writing a terminal status.
func TestEffectiveJobStatus_InProgressWithoutLiveProcess(t *testing.T) {
	jobsDir := t.TempDir()
	job := models.PipelineJob{JobID: "job-1", Status: models.JobStatusInProgress}

	assert.Equal(t, models.JobStatusStopped, EffectiveJobStatus(jobsDir, job))
}

// TestEffectiveJobStatus_InProgressWithLiveProcess proves a running job keeps
// in_progress while another lock holder is alive.
func TestEffectiveJobStatus_InProgressWithLiveProcess(t *testing.T) {
	jobsDir := t.TempDir()
	job := models.PipelineJob{JobID: "job-1", Status: models.JobStatusInProgress}

	lock, err := AcquireJobLock(jobsDir, job.JobID, testLockLogger())
	require.NoError(t, err)
	defer func() { _ = lock.Release() }()

	assert.Equal(t, models.JobStatusInProgress, EffectiveJobStatus(jobsDir, job))
}

// TestEffectiveJobStatus_OtherStatusesUnchanged proves the probe only reinterprets
// in_progress. A waiting job has no live process by design and must stay waiting.
func TestEffectiveJobStatus_OtherStatusesUnchanged(t *testing.T) {
	jobsDir := t.TempDir()

	for _, status := range []models.JobStatus{
		models.JobStatusPending,
		models.JobStatusCompleted,
		models.JobStatusFailed,
		models.JobStatusStopped,
		models.JobStatusWaiting,
	} {
		t.Run(string(status), func(t *testing.T) {
			job := models.PipelineJob{JobID: "job-1", Status: status}
			assert.Equal(t, status, EffectiveJobStatus(jobsDir, job))
		})
	}
}

// TestEffectiveJobStatusText_ProcessDied proves the user can tell a died process
// from a deliberate stop: the derived stop names its cause.
func TestEffectiveJobStatusText_ProcessDied(t *testing.T) {
	jobsDir := t.TempDir()
	job := models.PipelineJob{JobID: "job-1", Status: models.JobStatusInProgress}

	assert.Equal(t, "stopped (process died)", EffectiveJobStatusText(jobsDir, job))
}

// TestEffectiveJobStatusText_DeliberateStop proves a stop the user made shows
// without a cause, because the persisted status is the real outcome.
func TestEffectiveJobStatusText_DeliberateStop(t *testing.T) {
	jobsDir := t.TempDir()
	job := models.PipelineJob{JobID: "job-1", Status: models.JobStatusStopped}

	assert.Equal(t, "stopped", EffectiveJobStatusText(jobsDir, job))
}

// TestEffectiveJobStatusText_LiveProcess proves a job with a live lock holder
// shows in_progress, without a cause suffix.
func TestEffectiveJobStatusText_LiveProcess(t *testing.T) {
	jobsDir := t.TempDir()
	job := models.PipelineJob{JobID: "job-1", Status: models.JobStatusInProgress}

	lock, err := AcquireJobLock(jobsDir, job.JobID, testLockLogger())
	require.NoError(t, err)
	defer func() { _ = lock.Release() }()

	assert.Equal(t, "in_progress", EffectiveJobStatusText(jobsDir, job))
}
