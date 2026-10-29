package unit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/pipeline"
)

const cancelTestTORCHJobID = "torch-job-7"

// persistTORCHExtractionJob writes a job whose torch step waits for the TORCH
// job cancelTestTORCHJobID on the server at baseURL.
func persistTORCHExtractionJob(t *testing.T, baseURL string) *models.PipelineJob {
	t.Helper()
	job := seamJobWithSteps(t, []models.StepName{models.StepTorchImport, models.StepDIMP}, nil)
	job.InputType = models.InputTypeCRTDL
	job.Steps[0] = models.StartStep(job.Steps[0])
	job.Config.Services.TORCH = models.TORCHConfig{BaseURL: baseURL}
	job.TORCHExtractionURL = baseURL + "/fhir/__status/" + cancelTestTORCHJobID
	job.TORCHJobID = cancelTestTORCHJobID
	require.NoError(t, pipeline.UpdateJob(job.Config.JobsDir, job))
	return job
}

func TestStopJobAndCancelExtraction_CancelsTORCHJob(t *testing.T) {
	var cancelPath atomic.Value
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cancelPath.Store(r.URL.Path)
	}))
	defer server.Close()
	job := persistTORCHExtractionJob(t, server.URL)

	err := pipeline.StopJobAndCancelExtraction(context.Background(), job.Config.JobsDir, job.JobID, seamLogger())

	require.NoError(t, err)
	assert.Equal(t, "/fhir/Task/"+cancelTestTORCHJobID+"/$cancel", cancelPath.Load())
	reloaded, err := pipeline.LoadJob(job.Config.JobsDir, job.JobID)
	require.NoError(t, err)
	assert.Equal(t, models.JobStatusStopped, reloaded.Status)
}

func TestStopJobAndCancelExtraction_FailedCancelStillStops(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	job := persistTORCHExtractionJob(t, server.URL)
	job.Config.Retry = models.RetryConfig{MaxAttempts: 3, InitialBackoffMs: 1, MaxBackoffMs: 1}
	require.NoError(t, pipeline.UpdateJob(job.Config.JobsDir, job))

	err := pipeline.StopJobAndCancelExtraction(context.Background(), job.Config.JobsDir, job.JobID, seamLogger())

	assert.Error(t, err)
	assert.EqualValues(t, 1, requests.Load(), "the stop must not wait for retries")
	reloaded, err := pipeline.LoadJob(job.Config.JobsDir, job.JobID)
	require.NoError(t, err)
	assert.Equal(t, models.JobStatusStopped, reloaded.Status)
}

func TestStopJobAndCancelExtraction_NoTORCHExtractionSendsNoRequest(t *testing.T) {
	tests := []struct {
		name       string
		modify     func(job *models.PipelineJob)
		wantStatus models.JobStatus
	}{
		{
			name: "another step runs",
			modify: func(job *models.PipelineJob) {
				job.Steps[0] = models.CompleteStep(job.Steps[0], 1, 1)
				job.Steps[1] = models.StartStep(job.Steps[1])
				job.CurrentStep = string(models.StepDIMP)
			},
			wantStatus: models.JobStatusStopped,
		},
		{
			name:       "torch step completed",
			modify:     func(job *models.PipelineJob) { job.Steps[0] = models.CompleteStep(job.Steps[0], 1, 1) },
			wantStatus: models.JobStatusStopped,
		},
		{
			name:       "current step is not torch",
			modify:     func(job *models.PipelineJob) { job.CurrentStep = string(models.StepDIMP) },
			wantStatus: models.JobStatusStopped,
		},
		{
			name:       "extraction not submitted",
			modify:     func(job *models.PipelineJob) { job.TORCHJobID = "" },
			wantStatus: models.JobStatusStopped,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
			}))
			defer server.Close()
			job := persistTORCHExtractionJob(t, server.URL)
			tt.modify(job)
			require.NoError(t, pipeline.UpdateJob(job.Config.JobsDir, job))

			err := pipeline.StopJobAndCancelExtraction(context.Background(), job.Config.JobsDir, job.JobID, seamLogger())

			require.NoError(t, err)
			assert.Zero(t, requests.Load())
			reloaded, err := pipeline.LoadJob(job.Config.JobsDir, job.JobID)
			require.NoError(t, err)
			assert.Equal(t, tt.wantStatus, reloaded.Status)
		})
	}
}

func TestStopJobAndCancelExtraction_StopsJobThatIsNotInProgress(t *testing.T) {
	for _, status := range []models.JobStatus{models.JobStatusStopped, models.JobStatusWaiting, models.JobStatusPending} {
		t.Run(string(status), func(t *testing.T) {
			var cancels atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/fhir/Task/"+cancelTestTORCHJobID+"/$cancel" {
					cancels.Add(1)
				}
			}))
			defer server.Close()
			job := persistTORCHExtractionJob(t, server.URL)
			job.Status = status
			require.NoError(t, pipeline.UpdateJob(job.Config.JobsDir, job))

			err := pipeline.StopJobAndCancelExtraction(context.Background(), job.Config.JobsDir, job.JobID, seamLogger())

			require.NoError(t, err)
			assert.EqualValues(t, 1, cancels.Load())
			reloaded, err := pipeline.LoadJob(job.Config.JobsDir, job.JobID)
			require.NoError(t, err)
			assert.Equal(t, models.JobStatusStopped, reloaded.Status)
		})
	}
}

func TestStopJobAndCancelExtraction_EndedJobIsAnError(t *testing.T) {
	for _, status := range []models.JobStatus{models.JobStatusCompleted, models.JobStatusFailed} {
		t.Run(string(status), func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
			}))
			defer server.Close()
			job := persistTORCHExtractionJob(t, server.URL)
			job.Status = status
			require.NoError(t, pipeline.UpdateJob(job.Config.JobsDir, job))

			err := pipeline.StopJobAndCancelExtraction(context.Background(), job.Config.JobsDir, job.JobID, seamLogger())

			var ended *pipeline.JobEndedError
			require.ErrorAs(t, err, &ended)
			assert.Equal(t, status, ended.Status)
			assert.Zero(t, requests.Load())
			reloaded, err := pipeline.LoadJob(job.Config.JobsDir, job.JobID)
			require.NoError(t, err)
			assert.Equal(t, status, reloaded.Status)
		})
	}
}

func TestStopJobAndCancelExtraction_UnresponsiveTORCHEndsAfterCancelTimeout(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	defer server.Close()
	defer close(release)
	job := persistTORCHExtractionJob(t, server.URL)

	result := make(chan error, 1)
	start := time.Now()
	go func() {
		result <- pipeline.StopJobAndCancelExtraction(context.Background(), job.Config.JobsDir, job.JobID, seamLogger())
	}()

	select {
	case err := <-result:
		require.Error(t, err)
		assert.GreaterOrEqual(t, time.Since(start), pipeline.TORCHCancelTimeout)
	case <-time.After(pipeline.TORCHCancelTimeout + 3*time.Second):
		t.Fatal("the cancel request must end after the cancel timeout")
	}
	reloaded, err := pipeline.LoadJob(job.Config.JobsDir, job.JobID)
	require.NoError(t, err)
	assert.Equal(t, models.JobStatusStopped, reloaded.Status)
}
