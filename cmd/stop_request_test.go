package cmd

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/pipeline"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

const watcherTestJobID = "550e8400-e29b-41d4-a716-446655440000"

func persistRunningJob(t *testing.T) string {
	t.Helper()
	jobsDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(jobsDir, watcherTestJobID), 0o755))
	steps := []models.StepName{models.StepLocalImport, models.StepDIMP}
	job := &models.PipelineJob{
		JobID:       watcherTestJobID,
		InputSource: "/tmp/x",
		InputType:   models.InputTypeLocal,
		Status:      models.JobStatusInProgress,
		CurrentStep: string(steps[0]),
		Steps:       models.InitializeSteps(steps),
		Config: models.ProjectConfig{
			JobsDir:  jobsDir,
			Pipeline: models.PipelineConfig{EnabledSteps: steps},
		},
	}
	require.NoError(t, pipeline.UpdateJob(jobsDir, job))
	return jobsDir
}

func jobStatus(t *testing.T, jobsDir string) models.JobStatus {
	t.Helper()
	job, err := pipeline.LoadJob(jobsDir, watcherTestJobID)
	require.NoError(t, err)
	return job.Status
}

func TestStopRequestWatcher_StopsJobAndExitsOnFreshRequest(t *testing.T) {
	jobsDir := persistRunningJob(t)
	exits := make(chan int, 1)
	stop := watchStopRequests(jobsDir, watcherTestJobID, lib.NewLogger(lib.LogLevelError),
		time.Now().Add(-time.Minute), 5*time.Millisecond, func(code int) { exits <- code })
	defer stop()

	require.NoError(t, services.RequestStop(jobsDir, watcherTestJobID))

	select {
	case code := <-exits:
		assert.Equal(t, 143, code)
	case <-time.After(5 * time.Second):
		t.Fatal("watcher did not exit")
	}
	assert.Equal(t, models.JobStatusStopped, jobStatus(t, jobsDir))
}

func TestStopRequestWatcher_IgnoresStaleRequest(t *testing.T) {
	jobsDir := persistRunningJob(t)
	require.NoError(t, services.RequestStop(jobsDir, watcherTestJobID))
	exits := make(chan int, 1)
	stop := watchStopRequests(jobsDir, watcherTestJobID, lib.NewLogger(lib.LogLevelError),
		time.Now().Add(time.Minute), 5*time.Millisecond, func(code int) { exits <- code })
	defer stop()

	select {
	case <-exits:
		t.Fatal("watcher exited for a stale request")
	case <-time.After(100 * time.Millisecond):
	}
	assert.Equal(t, models.JobStatusInProgress, jobStatus(t, jobsDir))
}

func TestStopRequestWatcher_StopFunctionEndsWatcher(t *testing.T) {
	jobsDir := persistRunningJob(t)
	exits := make(chan int, 1)
	stop := watchStopRequests(jobsDir, watcherTestJobID, lib.NewLogger(lib.LogLevelError),
		time.Now().Add(-time.Minute), time.Millisecond, func(code int) { exits <- code })

	// The ticker is fast, thus a tick is often ready at the same time as the end
	// of the watcher. The watcher must not act on it.
	stop()
	require.NoError(t, services.RequestStop(jobsDir, watcherTestJobID))

	select {
	case <-exits:
		t.Fatal("watcher exited after it was stopped")
	case <-time.After(100 * time.Millisecond):
	}
	assert.Equal(t, models.JobStatusInProgress, jobStatus(t, jobsDir))
}

func TestStopRequestWatcher_DoesNotExitForJobThatEnded(t *testing.T) {
	jobsDir := persistRunningJob(t)
	job, err := pipeline.LoadJob(jobsDir, watcherTestJobID)
	require.NoError(t, err)
	require.NoError(t, pipeline.UpdateJob(jobsDir, ptr(models.UpdateJobStatus(*job, models.JobStatusCompleted))))
	exits := make(chan int, 1)
	stop := watchStopRequests(jobsDir, watcherTestJobID, lib.NewLogger(lib.LogLevelError),
		time.Now().Add(-time.Minute), 5*time.Millisecond, func(code int) { exits <- code })
	defer stop()

	require.NoError(t, services.RequestStop(jobsDir, watcherTestJobID))

	select {
	case code := <-exits:
		t.Fatalf("watcher exited with %d, but the run ends with its own exit code", code)
	case <-time.After(200 * time.Millisecond):
	}
	assert.Equal(t, models.JobStatusCompleted, jobStatus(t, jobsDir))
}

func TestInstallStopTriggers_RemovalEndsTheStopRequestWatcher(t *testing.T) {
	jobsDir := persistRunningJob(t)
	remove := installStopTriggers(jobsDir, watcherTestJobID, lib.NewLogger(lib.LogLevelError))

	remove()
	require.NoError(t, services.RequestStop(jobsDir, watcherTestJobID))

	// A watcher that is still active would stop the job and end this process
	// within one poll interval.
	time.Sleep(stopRequestPollInterval + 300*time.Millisecond)
	assert.Equal(t, models.JobStatusInProgress, jobStatus(t, jobsDir))
}

func TestStopRequestWatcher_StopsJobAndExitsWhenTORCHCancelFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	logger := lib.NewLogger(lib.LogLevelError)
	job := persistTORCHJob(t, server.URL, logger)
	job.TORCHJobID = "torch-job-1"
	job.TORCHExtractionURL = server.URL + "/fhir/__status/torch-job-1"
	require.NoError(t, pipeline.UpdateJob(job.Config.JobsDir, job))
	exits := make(chan int, 1)
	stop := watchStopRequests(job.Config.JobsDir, job.JobID, logger,
		time.Now().Add(-time.Minute), 5*time.Millisecond, func(code int) { exits <- code })
	defer stop()

	require.NoError(t, services.RequestStop(job.Config.JobsDir, job.JobID))

	select {
	case code := <-exits:
		assert.Equal(t, 143, code)
	case <-time.After(5 * time.Second):
		t.Fatal("the watcher did not exit")
	}
	stored, err := pipeline.LoadJob(job.Config.JobsDir, job.JobID)
	require.NoError(t, err)
	assert.Equal(t, models.JobStatusStopped, stored.Status)
}
