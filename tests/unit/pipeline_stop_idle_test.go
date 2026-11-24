package unit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/pipeline"
)

// idleJobWithStatus persists a job in the torch step with the given status and
// a TORCH server that counts the cancel requests.
func idleJobWithStatus(t *testing.T, status models.JobStatus, cancelStatus int) (*models.PipelineJob, *atomic.Int32) {
	t.Helper()
	cancels := new(atomic.Int32)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/fhir/Task/"+cancelTestTORCHJobID+"/$cancel" {
			cancels.Add(1)
		}
		w.WriteHeader(cancelStatus)
	}))
	t.Cleanup(server.Close)
	job := persistTORCHExtractionJob(t, server.URL)
	job.Status = status
	require.NoError(t, pipeline.UpdateJob(job.Config.JobsDir, job))
	return job, cancels
}

func TestStopIdleJob_StopsAndCancelsTORCHJob(t *testing.T) {
	for _, status := range []models.JobStatus{models.JobStatusInProgress, models.JobStatusStopped} {
		t.Run(string(status), func(t *testing.T) {
			job, cancels := idleJobWithStatus(t, status, http.StatusOK)

			err := pipeline.StopIdleJob(context.Background(), job.Config.JobsDir, job.JobID, seamLogger())

			require.NoError(t, err)
			assert.EqualValues(t, 1, cancels.Load())
			reloaded, err := pipeline.LoadJob(job.Config.JobsDir, job.JobID)
			require.NoError(t, err)
			assert.Equal(t, models.JobStatusStopped, reloaded.Status)
		})
	}
}

func TestStopIdleJob_WaitingJobIsStopped(t *testing.T) {
	job, cancels := idleJobWithStatus(t, models.JobStatusWaiting, http.StatusOK)
	job.Steps[0] = models.CompleteStep(job.Steps[0], 1, 1)
	job.CurrentStep = string(models.StepDIMP)
	require.NoError(t, pipeline.UpdateJob(job.Config.JobsDir, job))

	err := pipeline.StopIdleJob(context.Background(), job.Config.JobsDir, job.JobID, seamLogger())

	require.NoError(t, err)
	assert.Zero(t, cancels.Load())
	reloaded, err := pipeline.LoadJob(job.Config.JobsDir, job.JobID)
	require.NoError(t, err)
	assert.Equal(t, models.JobStatusStopped, reloaded.Status)
}

func TestStopIdleJob_EndedJobIsAnError(t *testing.T) {
	for _, status := range []models.JobStatus{models.JobStatusCompleted, models.JobStatusFailed} {
		t.Run(string(status), func(t *testing.T) {
			job, cancels := idleJobWithStatus(t, status, http.StatusOK)

			err := pipeline.StopIdleJob(context.Background(), job.Config.JobsDir, job.JobID, seamLogger())

			require.Error(t, err)
			assert.Contains(t, err.Error(), "is already "+string(status))
			assert.Zero(t, cancels.Load())
			reloaded, err := pipeline.LoadJob(job.Config.JobsDir, job.JobID)
			require.NoError(t, err)
			assert.Equal(t, status, reloaded.Status)
		})
	}
}

func TestStopIdleJob_FailedCancelIsReturnedAndStopStays(t *testing.T) {
	job, cancels := idleJobWithStatus(t, models.JobStatusInProgress, http.StatusServiceUnavailable)

	err := pipeline.StopIdleJob(context.Background(), job.Config.JobsDir, job.JobID, seamLogger())

	require.Error(t, err)
	assert.EqualValues(t, 1, cancels.Load())
	reloaded, err := pipeline.LoadJob(job.Config.JobsDir, job.JobID)
	require.NoError(t, err)
	assert.Equal(t, models.JobStatusStopped, reloaded.Status)
}

func TestStopIdleJob_ClearsHandleOnlyWhenNoTORCHJobRemains(t *testing.T) {
	tests := []struct {
		name         string
		cancelStatus int
		wantErr      bool
		keepsHandle  bool
	}{
		{"cancel succeeds", http.StatusOK, false, false},
		{"TORCH job is unknown", http.StatusNotFound, false, false},
		// A completed TORCH job also answers 409. Its result must stay available
		// to "pipeline continue".
		{"TORCH job in a final state keeps the handle", http.StatusConflict, false, true},
		{"TORCH job is gone", http.StatusGone, false, false},
		{"cancel fails keeps the handle", http.StatusInternalServerError, true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job, _ := idleJobWithStatus(t, models.JobStatusInProgress, tt.cancelStatus)

			err := pipeline.StopIdleJob(context.Background(), job.Config.JobsDir, job.JobID, seamLogger())

			assert.Equal(t, tt.wantErr, err != nil)
			reloaded, err := pipeline.LoadJob(job.Config.JobsDir, job.JobID)
			require.NoError(t, err)
			assert.Equal(t, models.JobStatusStopped, reloaded.Status)
			if tt.keepsHandle {
				assert.Equal(t, cancelTestTORCHJobID, reloaded.TORCHJobID)
				assert.NotEmpty(t, reloaded.TORCHExtractionURL)
			} else {
				assert.Empty(t, reloaded.TORCHJobID)
				assert.Empty(t, reloaded.TORCHExtractionURL)
			}
		})
	}
}
