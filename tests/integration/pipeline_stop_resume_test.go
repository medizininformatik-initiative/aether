package integration

import (
	"context"
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

// TestStopAndResume_CancelledTORCHJobRestartsExtraction proves the story of the
// user who confirms Ctrl+C during the TORCH extraction: aether cancels the TORCH
// job, and `pipeline continue` submits a new extraction instead of waiting for
// the cancelled one.
func TestStopAndResume_CancelledTORCHJobRestartsExtraction(t *testing.T) {
	var cancelled []string
	submitCount := 0
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/fhir/Task/old-job/$cancel":
			cancelled = append(cancelled, "old-job")
		case r.Method == http.MethodGet && r.URL.Path == "/fhir/__status/old-job":
			w.WriteHeader(http.StatusGone)
		case r.Method == http.MethodPost && r.URL.Path == "/fhir/$extract-data":
			submitCount++
			w.Header().Set("Content-Location", server.URL+"/fhir/__status/new-job")
			w.WriteHeader(http.StatusAccepted)
		case r.Method == http.MethodGet && r.URL.Path == "/fhir/__status/new-job":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"resourceType":"Parameters","parameter":[]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	tempDir := t.TempDir()
	jobsDir := filepath.Join(tempDir, "jobs")
	require.NoError(t, os.MkdirAll(jobsDir, 0755))
	crtdlPath := filepath.Join(tempDir, "crtdl.json")
	require.NoError(t, os.WriteFile(crtdlPath,
		[]byte(`{"cohortDefinition":{"version":"1.0.0","inclusionCriteria":[]},"dataExtraction":{"attributeGroups":[]}}`), 0644))
	config := models.ProjectConfig{
		Services: models.ServiceConfig{TORCH: models.TORCHConfig{
			BaseURL:            server.URL,
			ExtractionTimeout:  time.Minute,
			PollingInterval:    10 * time.Millisecond,
			MaxPollingInterval: 10 * time.Millisecond,
		}},
		Pipeline: models.PipelineConfig{EnabledSteps: []models.StepName{models.StepTorchImport}},
		Retry:    models.RetryConfig{MaxAttempts: 1, InitialBackoffMs: 1, MaxBackoffMs: 1},
		JobsDir:  jobsDir,
	}
	logger := testStopLogger()
	job, err := pipeline.CreateJob(models.GenerateJobID(), "", crtdlPath, config, logger)
	require.NoError(t, err)
	job = pipeline.StartJob(job)
	job.TORCHJobID = "old-job"
	job.TORCHExtractionURL = server.URL + "/fhir/__status/old-job"
	require.NoError(t, pipeline.UpdateJob(jobsDir, job))

	require.NoError(t, pipeline.StopJobAndCancelExtraction(context.Background(), jobsDir, job.JobID, logger))
	assert.Equal(t, []string{"old-job"}, cancelled)

	pipeline.ResetStoppedJobsForTesting()
	resumed, err := pipeline.LoadJob(jobsDir, job.JobID)
	require.NoError(t, err)
	httpClient := services.NewHTTPClient(2*time.Second, config.Retry, models.TLSConfig{}, logger)
	resumed, err = runImportStep(resumed, logger, httpClient, false)

	require.NoError(t, err)
	assert.Equal(t, 1, submitCount, "continue must submit a new extraction")
	assert.Equal(t, "new-job", resumed.TORCHJobID)
}

// TestStopAndResume_StopDuringSubmitCancelsTheNewTORCHJob covers a confirmed
// Ctrl+C while the submit to TORCH is in progress. The state file has no handle
// yet, thus the stop must wait for the submit and cancel the new TORCH job.
// Otherwise the TORCH job runs without a collector, and `pipeline continue`
// submits a second one.
func TestStopAndResume_StopDuringSubmitCancelsTheNewTORCHJob(t *testing.T) {
	submitStarted := make(chan struct{})
	releaseSubmit := make(chan struct{})
	cancelled := make(chan string, 1)
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/fhir/$extract-data":
			close(submitStarted)
			<-releaseSubmit
			w.Header().Set("Content-Location", server.URL+"/fhir/__status/new-job")
			w.WriteHeader(http.StatusAccepted)
		case r.Method == http.MethodPost && r.URL.Path == "/fhir/Task/new-job/$cancel":
			cancelled <- "new-job"
		default:
			w.WriteHeader(http.StatusGone)
		}
	}))
	defer server.Close()

	tempDir := t.TempDir()
	jobsDir := filepath.Join(tempDir, "jobs")
	require.NoError(t, os.MkdirAll(jobsDir, 0755))
	crtdlPath := filepath.Join(tempDir, "crtdl.json")
	require.NoError(t, os.WriteFile(crtdlPath,
		[]byte(`{"cohortDefinition":{"version":"1.0.0","inclusionCriteria":[]},"dataExtraction":{"attributeGroups":[]}}`), 0644))
	config := models.ProjectConfig{
		Services: models.ServiceConfig{TORCH: models.TORCHConfig{
			BaseURL:            server.URL,
			ExtractionTimeout:  time.Minute,
			PollingInterval:    10 * time.Millisecond,
			MaxPollingInterval: 10 * time.Millisecond,
		}},
		Pipeline: models.PipelineConfig{EnabledSteps: []models.StepName{models.StepTorchImport}},
		Retry:    models.RetryConfig{MaxAttempts: 1, InitialBackoffMs: 1, MaxBackoffMs: 1},
		JobsDir:  jobsDir,
	}
	logger := testStopLogger()
	job, err := pipeline.CreateJob(models.GenerateJobID(), "", crtdlPath, config, logger)
	require.NoError(t, err)
	job = pipeline.StartJob(job)
	require.NoError(t, pipeline.UpdateJob(jobsDir, job))

	importErr := make(chan error, 1)
	go func() {
		httpClient := services.NewHTTPClient(5*time.Second, config.Retry, models.TLSConfig{}, logger)
		_, err := runImportStep(job, logger, httpClient, false)
		importErr <- err
	}()
	<-submitStarted

	stopErr := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		stopErr <- pipeline.StopJobAndCancelExtraction(ctx, jobsDir, job.JobID, logger)
	}()
	require.Eventually(t, func() bool {
		stored, err := pipeline.LoadJob(jobsDir, job.JobID)
		return err == nil && stored.Status == models.JobStatusStopped
	}, 2*time.Second, 5*time.Millisecond)
	close(releaseSubmit)

	require.NoError(t, <-stopErr)
	select {
	case id := <-cancelled:
		assert.Equal(t, "new-job", id)
	default:
		t.Fatal("the stop must cancel the TORCH job that the submit created")
	}
	require.ErrorIs(t, <-importErr, pipeline.ErrJobStopped)
}
