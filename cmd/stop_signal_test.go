package cmd

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/pipeline"
)

// fakeSignal is an os.Signal that is not a syscall.Signal, thus it exercises the
// fallback of exitCodeForSignal.
type fakeSignal struct{}

func (fakeSignal) String() string { return "fake" }
func (fakeSignal) Signal()        {}

func TestExitCodeForSignal(t *testing.T) {
	tests := []struct {
		name string
		sig  os.Signal
		want int
	}{
		{"SIGINT gives 130", syscall.SIGINT, 130},
		{"SIGTERM gives 143", syscall.SIGTERM, 143},
		{"unknown signal gives 1", fakeSignal{}, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, exitCodeForSignal(tt.sig))
		})
	}
}

// stopCall is one call of the stop function of handleStopSignals.
type stopCall struct {
	sig         os.Signal
	cancelTORCH bool
}

// runStopHandler sends the signals to handleStopSignals and returns its output
// and the calls of its stop function.
func runStopHandler(signals ...os.Signal) (string, []stopCall) {
	ch := make(chan os.Signal, len(signals))
	for _, sig := range signals {
		ch <- sig
	}
	close(ch)

	var out bytes.Buffer
	var calls []stopCall
	handleStopSignals(ch, &out, func(sig os.Signal, cancelTORCH bool) {
		calls = append(calls, stopCall{sig, cancelTORCH})
	})
	return out.String(), calls
}

func TestHandleStopSignals_FirstInterruptAsksForConfirmation(t *testing.T) {
	out, calls := runStopHandler(os.Interrupt)

	assert.Contains(t, out, "Press Ctrl+C again to quit")
	assert.Empty(t, calls, "the pipeline must continue")
}

func TestHandleStopSignals_SecondInterruptStopsAndCancelsTORCH(t *testing.T) {
	_, calls := runStopHandler(os.Interrupt, os.Interrupt)

	assert.Equal(t, []stopCall{{os.Interrupt, true}}, calls)
}

// A service manager sends SIGTERM and expects the process to end. The TORCH job
// continues, so `pipeline continue` can re-attach to it.
func TestHandleStopSignals_TerminateStopsWithoutConfirmation(t *testing.T) {
	for _, signals := range [][]os.Signal{
		{syscall.SIGTERM},
		{os.Interrupt, syscall.SIGTERM},
	} {
		_, calls := runStopHandler(signals...)

		assert.Equal(t, []stopCall{{syscall.SIGTERM, false}}, calls)
	}
}

// persistTORCHJob writes a job whose torch step waits for the TORCH job "old"
// on the server at baseURL.
func persistTORCHJob(t *testing.T, baseURL string) (jobsDir string, jobID string) {
	t.Helper()
	jobsDir = t.TempDir()
	steps := []models.StepName{models.StepTorchImport}
	job := &models.PipelineJob{
		JobID:              models.GenerateJobID(),
		InputSource:        "crtdl.json",
		InputType:          models.InputTypeCRTDL,
		Status:             models.JobStatusInProgress,
		CurrentStep:        string(models.StepTorchImport),
		Steps:              models.InitializeSteps(steps),
		TORCHJobID:         "old",
		TORCHExtractionURL: baseURL + "/fhir/__status/old",
		Config: models.ProjectConfig{
			JobsDir:  jobsDir,
			Pipeline: models.PipelineConfig{EnabledSteps: steps},
			Services: models.ServiceConfig{TORCH: models.TORCHConfig{BaseURL: baseURL}},
		},
	}
	job.Steps[0] = models.StartStep(job.Steps[0])
	require.NoError(t, os.MkdirAll(filepath.Join(jobsDir, job.JobID), 0755))
	require.NoError(t, pipeline.UpdateJob(jobsDir, job))
	return jobsDir, job.JobID
}

func TestStopJob_WithoutCancelSendsNoRequestAndKeepsHandle(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
	}))
	defer server.Close()
	jobsDir, jobID := persistTORCHJob(t, server.URL)

	stopJob(jobsDir, jobID, false, time.Second, lib.NewLogger(lib.LogLevelError))

	assert.Zero(t, requests)
	job, err := pipeline.LoadJob(jobsDir, jobID)
	require.NoError(t, err)
	assert.Equal(t, models.JobStatusStopped, job.Status)
	assert.Equal(t, "old", job.TORCHJobID)
}

func TestStopJob_WithCancelCancelsTORCHJob(t *testing.T) {
	var cancelPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cancelPath = r.URL.Path
	}))
	defer server.Close()
	jobsDir, jobID := persistTORCHJob(t, server.URL)

	stopJob(jobsDir, jobID, true, time.Second, lib.NewLogger(lib.LogLevelError))

	assert.Equal(t, "/fhir/Task/old/$cancel", cancelPath)
	job, err := pipeline.LoadJob(jobsDir, jobID)
	require.NoError(t, err)
	assert.Equal(t, models.JobStatusStopped, job.Status)
}

func TestStopJob_SlowTORCHDoesNotDelayTheStop(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	defer server.Close()
	defer close(release)
	jobsDir, jobID := persistTORCHJob(t, server.URL)

	start := time.Now()
	stopJob(jobsDir, jobID, true, 50*time.Millisecond, lib.NewLogger(lib.LogLevelError))

	assert.Less(t, time.Since(start), 2*time.Second)
	job, err := pipeline.LoadJob(jobsDir, jobID)
	require.NoError(t, err)
	assert.Equal(t, models.JobStatusStopped, job.Status)
	assert.Equal(t, "old", job.TORCHJobID, "an unconfirmed cancel keeps the handle")
}
