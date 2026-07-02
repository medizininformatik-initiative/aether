package pipeline

import (
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
	"github.com/medizininformatik-initiative/aether/internal/services"
)

func writeTestCRTDL(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "query.crtdl")
	require.NoError(t, os.WriteFile(path, []byte(`{"cohortDefinition":{},"dataExtraction":{}}`), 0o644))
	return path
}

// reattachTestServer serves a TORCH stand-in. It records whether the
// $extract-data kick-off endpoint was hit so tests can assert re-attach
// behavior (poll an existing job without re-submitting).
type reattachTestServer struct {
	server      *httptest.Server
	submitCount int32
}

func (s *reattachTestServer) submits() int { return int(atomic.LoadInt32(&s.submitCount)) }

func (s *reattachTestServer) baseURL() string { return s.server.URL }

func (s *reattachTestServer) close() { s.server.Close() }

func newTORCHTestClientConfig(baseURL string) models.TORCHConfig {
	return models.TORCHConfig{
		BaseURL:            baseURL,
		ExtractionTimeout:  1 * time.Minute,
		PollingInterval:    1 * time.Second,
		MaxPollingInterval: 1 * time.Second,
	}
}

func newReattachJob(t *testing.T, torchCfg models.TORCHConfig) *models.PipelineJob {
	t.Helper()
	cfg := models.DefaultConfig()
	cfg.JobsDir = t.TempDir()
	cfg.Services.TORCH = torchCfg
	cfg.Compression.Enabled = false
	return &models.PipelineJob{
		JobID:     "550e8400-e29b-41d4-a716-446655440000",
		InputType: models.InputTypeCRTDL,
		Status:    models.JobStatusInProgress,
		Config:    cfg,
	}
}

func newTestHTTPClient(logger *lib.Logger) *services.HTTPClient {
	return services.NewHTTPClient(
		10*time.Second,
		models.RetryConfig{MaxAttempts: 3, InitialBackoffMs: 10, MaxBackoffMs: 100},
		models.TLSConfig{},
		logger,
	)
}

// Slice 1: resuming with a stored TORCHJobID must re-attach (poll the
// existing job) and must NOT submit a new extraction.
func TestExecuteTORCHExtraction_ReattachSkipsSubmit(t *testing.T) {
	rt := &reattachTestServer{}
	mux := http.NewServeMux()
	mux.HandleFunc("/fhir/$extract-data", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&rt.submitCount, 1)
		w.Header().Set("Content-Location", rt.baseURL()+"/fhir/__status/should-not-happen")
		w.WriteHeader(http.StatusAccepted)
	})
	mux.HandleFunc("/fhir/__status/existing-job", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"output":[{"type":"Patient","url":"` + rt.baseURL() + `/output/patients.ndjson"}]}`))
	})
	mux.HandleFunc("/output/patients.ndjson", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"resourceType":"Patient","id":"p1"}` + "\n"))
	})
	rt.server = httptest.NewServer(mux)
	defer rt.close()

	logger := lib.NewLogger(lib.LogLevelError)
	job := newReattachJob(t, newTORCHTestClientConfig(rt.baseURL()))
	job.TORCHJobID = "existing-job"
	job.TORCHExtractionURL = rt.baseURL() + "/fhir/__status/existing-job"

	files, err := executeTORCHExtraction(job, t.TempDir(), newTestHTTPClient(logger), logger, false, false, "")

	require.NoError(t, err)
	assert.Equal(t, 0, rt.submits(), "re-attach must not POST $extract-data")
	assert.Len(t, files, 1, "should download the file from the re-attached job")
}

// Slice 2: after a fresh submit, the job handle (TORCHJobID + URL) must be
// persisted to disk BEFORE the long poll begins, so a crash mid-extraction
// leaves a recoverable handle.
func TestExecuteTORCHExtraction_PersistsHandleBeforePolling(t *testing.T) {
	const torchJobID = "fresh-job-uuid-123"
	var handleOnDiskAtPoll string

	logger := lib.NewLogger(lib.LogLevelError)
	job := newReattachJob(t, models.TORCHConfig{}) // BaseURL filled in below
	job.CRTDLPath = writeTestCRTDL(t)

	mux := http.NewServeMux()
	mux.HandleFunc("/fhir/$extract-data", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Location", "http://"+r.Host+"/fhir/__status/"+torchJobID)
		w.WriteHeader(http.StatusAccepted)
	})
	mux.HandleFunc("/fhir/__status/"+torchJobID, func(w http.ResponseWriter, r *http.Request) {
		// Polling has begun — the handle must already be on disk by now.
		reloaded, err := LoadJob(job.Config.JobsDir, job.JobID)
		if err == nil {
			handleOnDiskAtPoll = reloaded.TORCHJobID
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"output":[{"type":"Patient","url":"http://` + r.Host + `/output/p.ndjson"}]}`))
	})
	mux.HandleFunc("/output/p.ndjson", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"resourceType":"Patient","id":"p1"}` + "\n"))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	job.Config.Services.TORCH = newTORCHTestClientConfig(server.URL)
	require.NoError(t, UpdateJob(job.Config.JobsDir, job)) // initial on-disk state, no handle yet

	files, err := executeTORCHExtraction(job, t.TempDir(), newTestHTTPClient(logger), logger, false, false, "")

	require.NoError(t, err)
	assert.Len(t, files, 1)
	assert.Equal(t, torchJobID, job.TORCHJobID, "handle set in memory")
	assert.Equal(t, torchJobID, handleOnDiskAtPoll, "handle persisted to disk before polling")
}

// Slice 3: a dead handle (404/410) on re-attach must clear the stale handle
// and re-submit a fresh extraction.
func TestExecuteTORCHExtraction_DeadHandleResubmits(t *testing.T) {
	const newJobID = "fresh-after-410"
	rt := &reattachTestServer{}

	mux := http.NewServeMux()
	mux.HandleFunc("/fhir/__status/stale-job", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusGone) // 410 — handle is dead
	})
	mux.HandleFunc("/fhir/$extract-data", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&rt.submitCount, 1)
		w.Header().Set("Content-Location", "http://"+r.Host+"/fhir/__status/"+newJobID)
		w.WriteHeader(http.StatusAccepted)
	})
	mux.HandleFunc("/fhir/__status/"+newJobID, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"output":[{"type":"Patient","url":"http://` + r.Host + `/output/p.ndjson"}]}`))
	})
	mux.HandleFunc("/output/p.ndjson", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"resourceType":"Patient","id":"p1"}` + "\n"))
	})
	rt.server = httptest.NewServer(mux)
	defer rt.close()

	logger := lib.NewLogger(lib.LogLevelError)
	job := newReattachJob(t, newTORCHTestClientConfig(rt.baseURL()))
	job.CRTDLPath = writeTestCRTDL(t)
	job.TORCHJobID = "stale-job"
	job.TORCHExtractionURL = rt.baseURL() + "/fhir/__status/stale-job"

	files, err := executeTORCHExtraction(job, t.TempDir(), newTestHTTPClient(logger), logger, false, false, "")

	require.NoError(t, err)
	assert.Equal(t, 1, rt.submits(), "dead handle must trigger exactly one re-submit")
	assert.Equal(t, newJobID, job.TORCHJobID, "handle replaced with the fresh job ID")
	assert.Len(t, files, 1)
}

// Slice 4: a fresh extraction whose submit TORCH rejects must surface the
// wrapped submit error — there is no handle to persist and nothing to poll.
func TestExecuteTORCHExtraction_FreshSubmitError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/fhir/$extract-data", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelError)
	job := newReattachJob(t, newTORCHTestClientConfig(server.URL))
	job.CRTDLPath = writeTestCRTDL(t)

	files, err := executeTORCHExtraction(job, t.TempDir(), newTestHTTPClient(logger), logger, false, false, "")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to submit TORCH extraction")
	assert.Empty(t, files)
}

// Slice 5: a dead handle clears cleanly, but if the follow-up re-submit is
// then rejected the re-submit error must surface (not the stale-handle state).
func TestExecuteTORCHExtraction_DeadHandleResubmitError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/fhir/__status/stale-job", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusGone) // 410 — handle is dead
	})
	mux.HandleFunc("/fhir/$extract-data", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelError)
	job := newReattachJob(t, newTORCHTestClientConfig(server.URL))
	job.CRTDLPath = writeTestCRTDL(t)
	job.TORCHJobID = "stale-job"
	job.TORCHExtractionURL = server.URL + "/fhir/__status/stale-job"

	files, err := executeTORCHExtraction(job, t.TempDir(), newTestHTTPClient(logger), logger, false, false, "")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to submit TORCH extraction")
	assert.Empty(t, files)
}

// Slice 6: a submit that succeeds but whose handle cannot be persisted
// (unwritable JobsDir) must surface the persist error before any polling —
// the whole point of persisting is to fail loudly if the handle is unsafe.
func TestExecuteTORCHExtraction_PersistHandleError(t *testing.T) {
	const torchJobID = "fresh-job-uuid-999"
	mux := http.NewServeMux()
	mux.HandleFunc("/fhir/$extract-data", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Location", "http://"+r.Host+"/fhir/__status/"+torchJobID)
		w.WriteHeader(http.StatusAccepted)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelError)
	job := newReattachJob(t, newTORCHTestClientConfig(server.URL))
	job.CRTDLPath = writeTestCRTDL(t)

	// Point JobsDir at a regular file so SaveJobState's MkdirAll fails.
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o644))
	job.Config.JobsDir = blocker

	files, err := executeTORCHExtraction(job, t.TempDir(), newTestHTTPClient(logger), logger, false, false, "")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to persist TORCH job handle")
	assert.Empty(t, files)
}
