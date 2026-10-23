package unit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

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
	var cancelPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cancelPath = r.URL.Path
	}))
	defer server.Close()
	job := persistTORCHExtractionJob(t, server.URL)

	err := pipeline.StopJobAndCancelExtraction(context.Background(), job.Config.JobsDir, job.JobID, seamLogger())

	require.NoError(t, err)
	assert.Equal(t, "/fhir/Task/"+cancelTestTORCHJobID+"/$cancel", cancelPath)
	reloaded, err := pipeline.LoadJob(job.Config.JobsDir, job.JobID)
	require.NoError(t, err)
	assert.Equal(t, models.JobStatusStopped, reloaded.Status)
}

func TestStopJobAndCancelExtraction_FailedCancelStillStops(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	job := persistTORCHExtractionJob(t, server.URL)
	job.Config.Retry = models.RetryConfig{MaxAttempts: 3, InitialBackoffMs: 1, MaxBackoffMs: 1}
	require.NoError(t, pipeline.UpdateJob(job.Config.JobsDir, job))

	err := pipeline.StopJobAndCancelExtraction(context.Background(), job.Config.JobsDir, job.JobID, seamLogger())

	assert.Error(t, err)
	assert.Equal(t, 1, requests, "the stop must not wait for retries")
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
		{
			name:       "job already completed",
			modify:     func(job *models.PipelineJob) { job.Status = models.JobStatusCompleted },
			wantStatus: models.JobStatusCompleted,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var requests int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
			}))
			defer server.Close()
			job := persistTORCHExtractionJob(t, server.URL)
			tt.modify(job)
			require.NoError(t, pipeline.UpdateJob(job.Config.JobsDir, job))

			err := pipeline.StopJobAndCancelExtraction(context.Background(), job.Config.JobsDir, job.JobID, seamLogger())

			require.NoError(t, err)
			assert.Zero(t, requests)
			reloaded, err := pipeline.LoadJob(job.Config.JobsDir, job.JobID)
			require.NoError(t, err)
			assert.Equal(t, tt.wantStatus, reloaded.Status)
		})
	}
}
