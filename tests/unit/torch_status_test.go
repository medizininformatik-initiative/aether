package unit

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

func fastPollConfig(serverURL string) models.TORCHConfig {
	return models.TORCHConfig{
		BaseURL:            serverURL,
		ExtractionTimeout:  10 * time.Second,
		PollingInterval:    5 * time.Millisecond,
		MaxPollingInterval: 5 * time.Millisecond,
	}
}

func newPollTestClient(t *testing.T, cfg models.TORCHConfig) *services.TORCHClient {
	t.Helper()
	logger := lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(
		5*time.Second,
		models.RetryConfig{MaxAttempts: 1, InitialBackoffMs: 1, MaxBackoffMs: 1},
		models.TLSConfig{},
		logger,
	)
	return services.NewTORCHClient(cfg, httpClient, logger)
}

// Slice 4: a 500 from the status endpoint is a terminal job failure — fail
// immediately, do not retry until timeout.
func TestPollExtractionStatus_500FailsTerminally(t *testing.T) {
	var polls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&polls, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := newPollTestClient(t, fastPollConfig(server.URL))
	_, err := client.PollExtractionStatus(server.URL+"/fhir/__status/job-x", false)

	require.Error(t, err)
	assert.False(t, errors.Is(err, services.ErrExtractionTimeout), "must fail terminally, not by timeout")
	var torchErr *services.TORCHError
	require.ErrorAs(t, err, &torchErr)
	assert.Equal(t, models.ErrorTypeNonTransient, torchErr.ErrorType, "500 is a terminal failure")
	assert.Equal(t, int32(1), atomic.LoadInt32(&polls), "500 must not be retried")
}

// Slice 4: a 503 is transient — keep polling until the job responds.
func TestPollExtractionStatus_503RetriesThenSucceeds(t *testing.T) {
	var polls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&polls, 1)
		if n <= 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"output":[{"type":"Patient","url":"http://` + r.Host + `/out.ndjson"}]}`))
	}))
	defer server.Close()

	client := newPollTestClient(t, fastPollConfig(server.URL))
	fileURLs, err := client.PollExtractionStatus(server.URL+"/fhir/__status/job-x", false)

	require.NoError(t, err)
	assert.Len(t, fileURLs, 1)
	assert.GreaterOrEqual(t, atomic.LoadInt32(&polls), int32(3), "503 should be retried until success")
}

// Slice 5: extraction_timeout is a liveness (no-progress) window. A job that
// keeps emitting 202 resets the window on every contact, so it must NOT time
// out even when it runs longer than extraction_timeout.
func TestPollExtractionStatus_LivenessResetsOn202(t *testing.T) {
	var polls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&polls, 1)
		if n <= 5 {
			w.WriteHeader(http.StatusAccepted) // 202 in-progress, keeps contact alive
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"output":[{"type":"Patient","url":"http://` + r.Host + `/out.ndjson"}]}`))
	}))
	defer server.Close()

	cfg := models.TORCHConfig{
		BaseURL:            server.URL,
		ExtractionTimeout:  80 * time.Millisecond, // shorter than the total run below
		PollingInterval:    30 * time.Millisecond,
		MaxPollingInterval: 30 * time.Millisecond,
	}
	client := newPollTestClient(t, cfg)

	fileURLs, err := client.PollExtractionStatus(server.URL+"/fhir/__status/job-x", false)

	require.NoError(t, err, "periodic 202 must reset the liveness window")
	assert.Len(t, fileURLs, 1)
}

// Slice 5 (companion): true silence — TORCH never makes progress (continuous
// 503, no 200/202) — must trip the liveness timeout rather than poll forever.
func TestPollExtractionStatus_SilencePastWindowTimesOut(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable) // never any 200/202 contact
	}))
	defer server.Close()

	cfg := models.TORCHConfig{
		BaseURL:            server.URL,
		ExtractionTimeout:  50 * time.Millisecond,
		PollingInterval:    20 * time.Millisecond,
		MaxPollingInterval: 20 * time.Millisecond,
	}
	client := newPollTestClient(t, cfg)

	_, err := client.PollExtractionStatus(server.URL+"/fhir/__status/job-x", false)

	require.Error(t, err)
	assert.ErrorIs(t, err, services.ErrExtractionTimeout, "silence past the window must time out")
}

// TestJobIDFromStatusURL covers handle extraction from status URLs, including
// the degenerate inputs that must yield an empty job ID rather than a bogus
// path segment.
func TestJobIDFromStatusURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{"status url with job id", "https://torch.example/fhir/__status/job-42", "job-42"},
		{"trailing slash trimmed", "https://torch.example/fhir/__status/job-42/", "job-42"},
		{"empty string", "", ""},
		{"only slashes", "///", ""},
		{"dot path yields no segment", ".", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, services.JobIDFromStatusURL(tt.url))
		})
	}
}
