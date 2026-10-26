package cmd

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/pipeline"
	"github.com/medizininformatik-initiative/aether/internal/services"
	"github.com/medizininformatik-initiative/aether/internal/testsupport/mocktorch"
)

// countingTORCH wraps a mock TORCH whose extraction does not end, and counts the
// extraction submits.
func countingTORCH(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	submits := &atomic.Int32{}
	mock := mocktorch.New(mocktorch.Config{CohortSize: 10, BatchSize: 1, PollsPerBatch: 1_000_000}).Handler()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/fhir/$extract-data" {
			submits.Add(1)
		}
		mock.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	return server, submits
}

func torchStatusCode(t *testing.T, url string) int {
	t.Helper()
	resp, err := http.Get(url)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode
}

// runTORCHStepInBackground runs the torch step, as the job process does, and
// returns the channel with the result of the step.
func runTORCHStepInBackground(job *models.PipelineJob, logger *lib.Logger) <-chan error {
	result := make(chan error, 1)
	go func() {
		httpClient := services.NewHTTPClient(2*time.Second, job.Config.Retry, models.TLSConfig{}, logger)
		result <- pipeline.RunStep(models.StepTorchImport, &pipeline.StepContext{
			Job:        job,
			Layout:     services.NewJobLayout(job.Config.JobsDir, job.JobID, job.Config.Pipeline.EnabledSteps),
			Logger:     logger,
			HTTPClient: httpClient,
		})
	}()
	return result
}

func persistTORCHJob(t *testing.T, torchURL string, logger *lib.Logger) *models.PipelineJob {
	t.Helper()
	tempDir := t.TempDir()
	jobsDir := filepath.Join(tempDir, "jobs")
	require.NoError(t, os.MkdirAll(jobsDir, 0o755))
	crtdlPath := filepath.Join(tempDir, "crtdl.json")
	require.NoError(t, os.WriteFile(crtdlPath,
		[]byte(`{"cohortDefinition":{"version":"1.0.0","inclusionCriteria":[]},"dataExtraction":{"attributeGroups":[]}}`), 0o644))
	config := models.ProjectConfig{
		Services: models.ServiceConfig{TORCH: models.TORCHConfig{
			BaseURL:            torchURL,
			ExtractionTimeout:  time.Minute,
			PollingInterval:    10 * time.Millisecond,
			MaxPollingInterval: 10 * time.Millisecond,
		}},
		Pipeline: models.PipelineConfig{EnabledSteps: []models.StepName{models.StepTorchImport}},
		Retry:    models.RetryConfig{MaxAttempts: 1, InitialBackoffMs: 1, MaxBackoffMs: 1},
		JobsDir:  jobsDir,
	}
	job, err := pipeline.CreateJob(models.GenerateJobID(), "", crtdlPath, config, logger)
	require.NoError(t, err)
	job = pipeline.StartJob(job)
	require.NoError(t, pipeline.UpdateJob(jobsDir, job))
	return job
}

// TestStopJob_CancelsTORCHJobAndContinueSubmitsNewExtraction runs the torch step
// against a TORCH job that does not end, stops the job from "another process",
// and continues it.
func TestStopJob_CancelsTORCHJobAndContinueSubmitsNewExtraction(t *testing.T) {
	server, submits := countingTORCH(t)
	logger := stopTestLogger()
	job := persistTORCHJob(t, server.URL, logger)
	jobsDir := job.Config.JobsDir

	lock, err := services.AcquireJobLock(jobsDir, job.JobID, logger)
	require.NoError(t, err)
	exited := make(chan int, 1)
	stopWatcher := watchStopRequests(jobsDir, job.JobID, logger, time.Now().Add(-time.Minute), 10*time.Millisecond,
		func(code int) {
			_ = lock.Release()
			exited <- code
		})
	defer stopWatcher()
	stepResult := runTORCHStepInBackground(job, logger)
	require.Eventually(t, func() bool {
		current, err := pipeline.LoadJob(jobsDir, job.JobID)
		return err == nil && current.TORCHJobID != ""
	}, 5*time.Second, 5*time.Millisecond, "first extraction")
	current, err := pipeline.LoadJob(jobsDir, job.JobID)
	require.NoError(t, err)
	statusURL := current.TORCHExtractionURL
	require.Equal(t, http.StatusAccepted, torchStatusCode(t, statusURL))

	var out bytes.Buffer
	require.NoError(t, stopJob(jobsDir, "aether.yaml", job.JobID, 5*time.Second, 10*time.Millisecond, &out, logger))

	assert.Equal(t, 143, <-exited)
	assert.Contains(t, out.String(), "Job "+job.JobID+" stopped")
	assert.Equal(t, http.StatusGone, torchStatusCode(t, statusURL), "TORCH job must be cancelled")
	stopped, err := pipeline.LoadJob(jobsDir, job.JobID)
	require.NoError(t, err)
	assert.Equal(t, models.JobStatusStopped, stopped.Status)
	require.ErrorIs(t, <-stepResult, pipeline.ErrJobStopped)
	assert.EqualValues(t, 1, submits.Load(), "the stopped process must not submit again")

	pipeline.ResetStoppedJobsForTesting()
	resumed, err := pipeline.LoadJob(jobsDir, job.JobID)
	require.NoError(t, err)
	resumedResult := runTORCHStepInBackground(resumed, logger)
	require.Eventually(t, func() bool { return submits.Load() == 2 },
		5*time.Second, 5*time.Millisecond, "new extraction")

	require.NoError(t, stopJob(jobsDir, "aether.yaml", job.JobID, 5*time.Second, 10*time.Millisecond, &out, logger))
	// This test process runs both the step and the second stop, so the step sees
	// the cancelled handle before any stop flag.
	select {
	case err := <-resumedResult:
		require.ErrorContains(t, err, "TORCH job handle is gone")
	case <-time.After(10 * time.Second):
		t.Fatal("the resumed step did not end after the stop")
	}
}
