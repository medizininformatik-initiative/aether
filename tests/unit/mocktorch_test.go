package unit

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/testsupport/mocktorch"
)

// submitJob kicks off an extraction on the mock and returns the job id.
func submitJob(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	resp, err := http.Post(srv.URL+"/fhir/$extract-data", "application/fhir+json", strings.NewReader("{}"))
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusAccepted, resp.StatusCode)

	loc := resp.Header.Get("Content-Location")
	require.NotEmpty(t, loc)
	idx := strings.Index(loc, "/fhir/__status/")
	require.GreaterOrEqual(t, idx, 0, "Content-Location must point at the status route")
	return loc[idx+len("/fhir/__status/"):]
}

// taskProgress reads the torch-job-progress extension of the Task resource and
// returns its sub-extensions keyed by url.
func taskProgress(t *testing.T, srv *httptest.Server, jobID string) map[string]any {
	t.Helper()
	resp, err := http.Get(srv.URL + "/fhir/Task/" + jobID)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var task struct {
		Extension []struct {
			URL       string `json:"url"`
			Extension []struct {
				URL          string `json:"url"`
				ValueInteger *int   `json:"valueInteger"`
				ValueString  string `json:"valueString"`
			} `json:"extension"`
		} `json:"extension"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&task))
	require.Len(t, task.Extension, 1)

	out := map[string]any{}
	for _, e := range task.Extension[0].Extension {
		if e.ValueInteger != nil {
			out[e.URL] = *e.ValueInteger
		} else {
			out[e.URL] = e.ValueString
		}
	}
	return out
}

func newMock(t *testing.T, cfg mocktorch.Config) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(mocktorch.New(cfg).Handler())
	t.Cleanup(srv.Close)
	return srv
}

// A zero BatchSize and PollsPerBatch must fall back to 1 instead of dividing by
// zero, so each patient is its own batch and each poll completes one batch.
func TestMockTORCH_ZeroBatchSizeAndPollsFallBackToOne(t *testing.T) {
	srv := newMock(t, mocktorch.Config{CohortSize: 3})
	jobID := submitJob(t, srv)

	progress := taskProgress(t, srv, jobID)
	assert.Equal(t, 1, progress["batchSize"])
	assert.Equal(t, 3, progress["batchesTotal"])
	assert.Equal(t, 0, progress["batchesCompleted"])

	resp, err := http.Get(srv.URL + "/fhir/__status/" + jobID)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	assert.Equal(t, http.StatusAccepted, resp.StatusCode)

	// One poll per batch: after a single status poll one batch is done.
	assert.Equal(t, 1, taskProgress(t, srv, jobID)["batchesCompleted"])
}

// Extra status polls after the extraction finished must not report more
// completed batches than exist, and no batch stays active.
func TestMockTORCH_CompletedBatchesClampedToTotal(t *testing.T) {
	srv := newMock(t, mocktorch.Config{CohortSize: 2, BatchSize: 1, PollsPerBatch: 1})
	jobID := submitJob(t, srv)

	for i := 0; i < 5; i++ {
		resp, err := http.Get(srv.URL + "/fhir/__status/" + jobID)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
	}

	progress := taskProgress(t, srv, jobID)
	assert.Equal(t, 2, progress["batchesTotal"])
	assert.Equal(t, 2, progress["batchesCompleted"])
	assert.NotContains(t, progress, "activeBatch")
}

// A TORCH without the progress extension has no Task route, so the mock must
// answer 404 there for a job that exists.
func TestMockTORCH_DisabledTaskAPIIsNotFound(t *testing.T) {
	srv := newMock(t, mocktorch.Config{CohortSize: 1, DisableTaskAPI: true})
	jobID := submitJob(t, srv)

	resp, err := http.Get(srv.URL + "/fhir/Task/" + jobID)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

// The completed status response points at an output file, and that file serves
// NDJSON with an explicit Content-Length, which the download path probes with
// HEAD.
func TestMockTORCH_OutputIsDownloadable(t *testing.T) {
	srv := newMock(t, mocktorch.Config{CohortSize: 2, BatchSize: 2, PollsPerBatch: 1})
	jobID := submitJob(t, srv)

	resp, err := http.Get(srv.URL + "/fhir/__status/" + jobID)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var manifest struct {
		Output []struct {
			Type string `json:"type"`
			URL  string `json:"url"`
		} `json:"output"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&manifest))
	require.Len(t, manifest.Output, 1)
	assert.Equal(t, "Patient", manifest.Output[0].Type)

	head, err := http.Head(manifest.Output[0].URL)
	require.NoError(t, err)
	require.NoError(t, head.Body.Close())
	assert.Equal(t, "application/fhir+ndjson", head.Header.Get("Content-Type"))
	assert.Positive(t, head.ContentLength)

	body, err := http.Get(manifest.Output[0].URL)
	require.NoError(t, err)
	defer func() { _ = body.Body.Close() }()
	data, err := io.ReadAll(body.Body)
	require.NoError(t, err)
	assert.Equal(t, int64(len(data)), head.ContentLength)

	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	assert.Len(t, lines, 2)
	assert.Contains(t, lines[0], `"id":"mock-patient-1"`)
}

// The output body has one patient for each member of the cohort, so the
// download agrees with the cohort size that the Task API reports.
func TestMockTORCH_OutputHasOnePatientForEachCohortMember(t *testing.T) {
	srv := newMock(t, mocktorch.Config{CohortSize: 150, BatchSize: 150, PollsPerBatch: 1})
	jobID := submitJob(t, srv)

	resp, err := http.Get(srv.URL + "/fhir/__status/" + jobID)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, http.StatusOK, resp.StatusCode)

	out, err := http.Get(srv.URL + "/output/" + jobID + "/patients.ndjson")
	require.NoError(t, err)
	defer func() { _ = out.Body.Close() }()
	data, err := io.ReadAll(out.Body)
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	assert.Len(t, lines, 150)
	assert.Contains(t, lines[149], `"id":"mock-patient-150"`)
}

func TestMockTORCH_SubmitRejectsNonPost(t *testing.T) {
	srv := newMock(t, mocktorch.Config{CohortSize: 1})

	resp, err := http.Get(srv.URL + "/fhir/$extract-data")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
}

func TestMockTORCH_UnknownJobIsNotFound(t *testing.T) {
	srv := newMock(t, mocktorch.Config{CohortSize: 1})

	for _, path := range []string{"/fhir/__status/no-such-job", "/fhir/Task/no-such-job"} {
		resp, err := http.Get(srv.URL + path)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
		assert.Equal(t, http.StatusNotFound, resp.StatusCode, "path %s", path)
	}
}

// The root route answers so a reachability probe succeeds, while any other
// unrouted path is a 404.
func TestMockTORCH_RootIsReachableAndOtherPathsAre404(t *testing.T) {
	srv := newMock(t, mocktorch.Config{CohortSize: 1})

	resp, err := http.Get(srv.URL + "/")
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	resp, err = http.Get(srv.URL + "/not-a-torch-route")
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}
