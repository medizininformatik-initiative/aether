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
)

func stopTestLogger() *lib.Logger { return lib.NewLogger(lib.LogLevelError) }

func TestStopJob_StopsJobWithoutProcess(t *testing.T) {
	jobsDir := persistRunningJob(t)
	var out bytes.Buffer

	err := stopJob(jobsDir, "aether.yaml", watcherTestJobID, time.Second, 10*time.Millisecond, &out, stopTestLogger())

	require.NoError(t, err)
	assert.Equal(t, models.JobStatusStopped, jobStatus(t, jobsDir))
	assert.Contains(t, out.String(), "Job "+watcherTestJobID+" stopped")
	assert.Contains(t, out.String(), "Continue it with: aether pipeline continue aether.yaml "+watcherTestJobID)
}

func TestStopJob_UnknownJobIsAnError(t *testing.T) {
	var out bytes.Buffer

	err := stopJob(t.TempDir(), "aether.yaml", watcherTestJobID, time.Second, 10*time.Millisecond, &out, stopTestLogger())

	require.Error(t, err)
	assert.Empty(t, out.String())
}

func TestStopJob_EndedJobIsAnError(t *testing.T) {
	jobsDir := persistRunningJob(t)
	job, err := pipeline.LoadJob(jobsDir, watcherTestJobID)
	require.NoError(t, err)
	require.NoError(t, pipeline.UpdateJob(jobsDir, ptr(models.UpdateJobStatus(*job, models.JobStatusCompleted))))
	var out bytes.Buffer

	err = stopJob(jobsDir, "aether.yaml", watcherTestJobID, time.Second, 10*time.Millisecond, &out, stopTestLogger())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "is already completed")
	assert.Equal(t, models.JobStatusCompleted, jobStatus(t, jobsDir))
}

func ptr[T any](v T) *T { return &v }

// holdLockUntilStopRequest simulates the process that runs the job. It holds the
// job lock until the stop marker appears, and then runs end and releases the lock.
func holdLockUntilStopRequest(t *testing.T, jobsDir string, end func()) {
	t.Helper()
	lock, err := services.AcquireJobLock(jobsDir, watcherTestJobID, stopTestLogger())
	require.NoError(t, err)
	testDone := make(chan struct{})
	t.Cleanup(func() { close(testDone) })
	go func() {
		defer func() { _ = lock.Release() }()
		for !services.StopRequestedSince(jobsDir, watcherTestJobID, time.Time{}) {
			select {
			case <-testDone:
				return
			case <-time.After(5 * time.Millisecond):
			}
		}
		end()
	}()
}

func stopMarkerExists(jobsDir string) bool {
	_, err := os.Stat(filepath.Join(services.GetJobDir(jobsDir, watcherTestJobID), ".stop"))
	return err == nil
}

func TestStopJob_AsksRunningProcessToStop(t *testing.T) {
	jobsDir := persistRunningJob(t)
	holdLockUntilStopRequest(t, jobsDir, func() {
		_ = pipeline.MarkJobStopped(jobsDir, watcherTestJobID)
	})
	var out bytes.Buffer

	err := stopJob(jobsDir, "aether.yaml", watcherTestJobID, 5*time.Second, 10*time.Millisecond, &out, stopTestLogger())

	require.NoError(t, err)
	assert.Contains(t, out.String(), "Job "+watcherTestJobID+" stopped")
	assert.Contains(t, out.String(), "Continue it with: aether pipeline continue aether.yaml "+watcherTestJobID)
	assert.False(t, stopMarkerExists(jobsDir))
}

func TestStopJob_JobThatCompletedWhileStoppingIsAnError(t *testing.T) {
	jobsDir := persistRunningJob(t)
	holdLockUntilStopRequest(t, jobsDir, func() {
		job, err := pipeline.LoadJob(jobsDir, watcherTestJobID)
		if err == nil {
			_ = pipeline.UpdateJob(jobsDir, ptr(models.UpdateJobStatus(*job, models.JobStatusCompleted)))
		}
	})
	var out bytes.Buffer

	err := stopJob(jobsDir, "aether.yaml", watcherTestJobID, 5*time.Second, 10*time.Millisecond, &out, stopTestLogger())

	var ended *pipeline.JobEndedError
	require.ErrorAs(t, err, &ended)
	assert.Contains(t, err.Error(), "is already completed")
	assert.NotContains(t, out.String(), "Continue it with")
	assert.Equal(t, models.JobStatusCompleted, jobStatus(t, jobsDir))
	assert.False(t, stopMarkerExists(jobsDir))
}

func TestStopJob_ProcessThatDoesNotEndIsAnError(t *testing.T) {
	jobsDir := persistRunningJob(t)
	lock, err := services.AcquireJobLock(jobsDir, watcherTestJobID, stopTestLogger())
	require.NoError(t, err)
	defer func() { _ = lock.Release() }()
	var out bytes.Buffer

	err = stopJob(jobsDir, "aether.yaml", watcherTestJobID, 100*time.Millisecond, 10*time.Millisecond, &out, stopTestLogger())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "did not end")
	assert.False(t, stopMarkerExists(jobsDir))
	assert.Equal(t, models.JobStatusInProgress, jobStatus(t, jobsDir))
}

func TestStopJob_JobThatFailedWhileStoppingIsAnError(t *testing.T) {
	jobsDir := persistRunningJob(t)
	holdLockUntilStopRequest(t, jobsDir, func() {
		job, err := pipeline.LoadJob(jobsDir, watcherTestJobID)
		if err == nil {
			_ = pipeline.UpdateJob(jobsDir, ptr(models.UpdateJobStatus(*job, models.JobStatusFailed)))
		}
	})
	var out bytes.Buffer

	err := stopJob(jobsDir, "aether.yaml", watcherTestJobID, 5*time.Second, 10*time.Millisecond, &out, stopTestLogger())

	var ended *pipeline.JobEndedError
	require.ErrorAs(t, err, &ended)
	assert.Contains(t, err.Error(), "is already failed")
	assert.NotContains(t, out.String(), "Continue it with")
	assert.Equal(t, models.JobStatusFailed, jobStatus(t, jobsDir))
}

// A process that dies without a record, for example from SIGKILL, leaves the
// job in_progress. The stop then works as if no process held the lock.
func TestStopJob_StopsJobWhoseProcessDiedWithoutRecord(t *testing.T) {
	jobsDir := persistRunningJob(t)
	holdLockUntilStopRequest(t, jobsDir, func() {})
	var out bytes.Buffer

	err := stopJob(jobsDir, "aether.yaml", watcherTestJobID, 5*time.Second, 10*time.Millisecond, &out, stopTestLogger())

	require.NoError(t, err)
	assert.Equal(t, models.JobStatusStopped, jobStatus(t, jobsDir))
	assert.Contains(t, out.String(), "Job "+watcherTestJobID+" stopped")
	assert.Contains(t, out.String(), "Continue it with: aether pipeline continue aether.yaml "+watcherTestJobID)
	assert.False(t, stopMarkerExists(jobsDir))
}

func TestStopJob_FailedTORCHCancelKeepsStopAndIsAnError(t *testing.T) {
	var cancels atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cancels.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	logger := stopTestLogger()
	job := persistTORCHJob(t, server.URL, logger)
	job.TORCHJobID = "torch-job-1"
	job.TORCHExtractionURL = server.URL + "/fhir/__status/torch-job-1"
	require.NoError(t, pipeline.UpdateJob(job.Config.JobsDir, job))
	var out bytes.Buffer

	err := stopJob(job.Config.JobsDir, "aether.yaml", job.JobID, time.Second, 10*time.Millisecond, &out, logger)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "job "+job.JobID+" is stopped, but the TORCH cancel failed")
	assert.EqualValues(t, 1, cancels.Load())
	stored, loadErr := pipeline.LoadJob(job.Config.JobsDir, job.JobID)
	require.NoError(t, loadErr)
	assert.Equal(t, models.JobStatusStopped, stored.Status)
}

func TestJobStopCommand_StopsIdleJobThroughTheCLI(t *testing.T) {
	jobsDir := persistRunningJob(t)
	cfgPath := filepath.Join(t.TempDir(), "aether.yaml")
	cfg := "jobs_dir: " + jobsDir + "\npipeline:\n  enabled_steps:\n    - local_import\n    - dimp\nservices:\n  dimp:\n    url: http://localhost:1\n"
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfg), 0o600))
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	rootCmd.SetArgs([]string{"job", "stop", cfgPath, watcherTestJobID})
	t.Cleanup(func() {
		rootCmd.SetArgs(nil)
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
	})

	err := rootCmd.Execute()

	require.NoError(t, err)
	assert.Contains(t, out.String(), "Job "+watcherTestJobID+" stopped")
	assert.Contains(t, out.String(), "Continue it with: aether pipeline continue "+cfgPath+" "+watcherTestJobID)
	assert.Equal(t, models.JobStatusStopped, jobStatus(t, jobsDir))
}
