package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
)

// renderCSVCell is the last step between a value decoded from the flattener's
// NDJSON response and a CSV cell. Its default branch also guards against a
// value that encoding/json cannot marshal. The decoder never produces such a
// value, so only a direct call from this package reaches that guard.
func TestRenderCSVCell(t *testing.T) {
	tests := map[string]struct {
		value    any
		expected string
	}{
		"null becomes an empty cell":     {nil, ""},
		"string stays verbatim":          {"Doe, John", "Doe, John"},
		"integer keeps its source text":  {json.Number("42"), "42"},
		"decimal keeps trailing zeros":   {json.Number("1.500"), "1.500"},
		"true becomes true":              {true, "true"},
		"false becomes false":            {false, "false"},
		"object becomes compact JSON":    {map[string]any{"code": "abc"}, `{"code":"abc"}`},
		"array becomes compact JSON":     {[]any{"a", json.Number("1")}, `["a",1]`},
		"empty string stays empty":       {"", ""},
		"nested object becomes one cell": {map[string]any{"a": map[string]any{"b": "c"}}, `{"a":{"b":"c"}}`},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.expected, renderCSVCell(tt.value))
		})
	}

	t.Run("unmarshalable value falls back to its Go rendering", func(t *testing.T) {
		// A channel has no JSON representation. encoding/json never decodes
		// one, so this asserts the defensive branch, not a real response.
		value := make(chan int)

		assert.Equal(t, fmt.Sprint(value), renderCSVCell(value))
	})
}

func newLivenessTestClient(serverURL string, logger *lib.Logger) *FlattenerClient {
	client := NewFlattenerClient(
		models.FlatteningConfig{ServiceURL: serverURL, Timeout: time.Minute},
		models.RetryConfig{MaxAttempts: 3, InitialBackoffMs: 1, MaxBackoffMs: 1},
		nil,
		logger,
	)
	client.liveness = livenessConfig{interval: 20 * time.Millisecond, timeout: 50 * time.Millisecond, maxFailures: defaultLiveness.maxFailures}
	return client
}

func livenessTestViewDefinition() models.ViewDefinition {
	return models.ViewDefinition{
		ResourceType: "https://sql-on-fhir.org/ig/StructureDefinition/ViewDefinition",
		Name:         "TestView",
		Resource:     "Patient",
		Status:       "draft",
		Select: []models.SelectClause{{
			Column: []models.ColumnDefinition{{Name: "id", Path: "id"}},
		}},
	}
}

func livenessTestResources() []map[string]any {
	return []map[string]any{{"resourceType": "Patient", "id": "1"}}
}

// livenessServer answers the run route with runHandler and the metadata route
// with metadataHandler. The run handler ends when the client closes the
// connection, so the server can close at the end of the test.
func livenessServer(t *testing.T, runHandler func(w http.ResponseWriter, r *http.Request), metadataHandler func(w http.ResponseWriter, r *http.Request)) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == metadataPath {
			metadataHandler(w, r)
			return
		}
		runHandler(w, r)
	}))
	t.Cleanup(server.Close)
	return server
}

// hangUntilClientLeaves reads the request body first, because the server
// detects a closed connection only after it has read the whole body.
func hangUntilClientLeaves(_ http.ResponseWriter, r *http.Request) {
	_, _ = io.Copy(io.Discard, r.Body)
	<-r.Context().Done()
}

func TestFlattenerClient_Flatten_StoppedFlattenerIsDetected(t *testing.T) {
	tests := map[string]http.HandlerFunc{
		"metadata route answers HTTP 500": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		},
		"metadata route hangs": func(_ http.ResponseWriter, r *http.Request) {
			<-r.Context().Done()
		},
	}

	for name, metadata := range tests {
		t.Run(name, func(t *testing.T) {
			var runRequests atomic.Int32
			server := livenessServer(t, func(w http.ResponseWriter, r *http.Request) {
				runRequests.Add(1)
				hangUntilClientLeaves(w, r)
			}, metadata)
			var logs bytes.Buffer
			client := newLivenessTestClient(server.URL, lib.NewLoggerWithWriter(lib.LogLevelError, &logs))

			start := time.Now()
			_, err := client.Flatten(livenessTestViewDefinition(), livenessTestResources())

			require.Error(t, err)
			assert.ErrorIs(t, err, ErrFlattenerStopped)
			assert.Contains(t, err.Error(), "flattener stopped answering")
			assert.Contains(t, err.Error(), fmt.Sprintf("%d health checks at %s", defaultLiveness.maxFailures, server.URL+metadataPath))
			assert.NotContains(t, logs.String(), "Flattener HTTP request failed", "the watchdog logs its own cause")
			assert.Less(t, time.Since(start), 5*time.Second)
			assert.Equal(t, int32(1), runRequests.Load(), "a cancelled request must not be retried")
		})
	}
}

func okMetadata(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}

// slowRun answers after the given time with one NDJSON row.
func slowRun(delay time.Duration) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case <-time.After(delay):
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte("{\"id\":\"1\"}\n"))
	}
}

func TestFlattenerClient_Flatten_SlowFlattenerThatAnswersProbesIsNotCancelled(t *testing.T) {
	var probes atomic.Int32
	server := livenessServer(t, slowRun(300*time.Millisecond), func(w http.ResponseWriter, r *http.Request) {
		probes.Add(1)
		okMetadata(w, r)
	})
	var logs strings.Builder
	client := newLivenessTestClient(server.URL, lib.NewLoggerWithWriter(lib.LogLevelInfo, &logs))

	rows, err := client.Flatten(livenessTestViewDefinition(), livenessTestResources())

	require.NoError(t, err)
	assert.Equal(t, [][]string{{"1"}}, rows)
	assert.GreaterOrEqual(t, probes.Load(), int32(2), "the request must span several liveness intervals")
	assert.Contains(t, logs.String(), "Flattener request still in progress")
	assert.Contains(t, logs.String(), "viewDefinition TestView")
}

func TestFlattenerClient_Flatten_FailuresNotInSequenceDoNotCancel(t *testing.T) {
	var probes atomic.Int32
	server := livenessServer(t, slowRun(600*time.Millisecond), func(w http.ResponseWriter, _ *http.Request) {
		if probes.Add(1)%2 == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	client := newLivenessTestClient(server.URL, lib.NewLogger(lib.LogLevelError))
	client.liveness.maxFailures = 2

	rows, err := client.Flatten(livenessTestViewDefinition(), livenessTestResources())

	require.NoError(t, err)
	assert.Equal(t, [][]string{{"1"}}, rows)
	assert.GreaterOrEqual(t, probes.Load(), int32(4), "the probes must alternate over several intervals")
}

func TestFlattenerClient_Flatten_WatchdogStopsWhenFlattenReturns(t *testing.T) {
	var probes atomic.Int32
	server := livenessServer(t, slowRun(100*time.Millisecond), func(w http.ResponseWriter, r *http.Request) {
		probes.Add(1)
		okMetadata(w, r)
	})
	client := newLivenessTestClient(server.URL, lib.NewLogger(lib.LogLevelError))

	_, err := client.Flatten(livenessTestViewDefinition(), livenessTestResources())
	require.NoError(t, err)

	// Flatten waits for the watchdog, so no probe starts after it returns.
	probesAtReturn := probes.Load()
	time.Sleep(10 * client.liveness.interval)
	assert.Equal(t, probesAtReturn, probes.Load())
}

func TestFlattenerClient_Flatten_ReturnsWithoutWaitingForProbeInFlight(t *testing.T) {
	server := livenessServer(t, slowRun(100*time.Millisecond), func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})
	client := newLivenessTestClient(server.URL, lib.NewLogger(lib.LogLevelError))
	client.liveness.timeout = time.Minute
	client.liveness.maxFailures = 100

	start := time.Now()
	rows, err := client.Flatten(livenessTestViewDefinition(), livenessTestResources())

	require.NoError(t, err)
	assert.Equal(t, [][]string{{"1"}}, rows)
	assert.Less(t, time.Since(start), 5*time.Second)
}

func TestFlattenerClient_Flatten_WatchdogEndsRetryBackoff(t *testing.T) {
	var runRequests atomic.Int32
	server := livenessServer(t, func(w http.ResponseWriter, r *http.Request) {
		runRequests.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusServiceUnavailable)
	}, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	client := NewFlattenerClient(
		models.FlatteningConfig{ServiceURL: server.URL, Timeout: time.Minute},
		models.RetryConfig{MaxAttempts: 3, InitialBackoffMs: 5000, MaxBackoffMs: 10000},
		nil,
		lib.NewLogger(lib.LogLevelError),
	)
	client.liveness = livenessConfig{interval: 20 * time.Millisecond, timeout: 50 * time.Millisecond, maxFailures: 3}

	start := time.Now()
	_, err := client.Flatten(livenessTestViewDefinition(), livenessTestResources())

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrFlattenerStopped)
	assert.Less(t, time.Since(start), 2*time.Second)
	assert.Equal(t, int32(1), runRequests.Load())
}

func TestFlattenerClient_Flatten_WatchdogCancelsWhileBodyStreams(t *testing.T) {
	var runRequests atomic.Int32
	server := livenessServer(t, func(w http.ResponseWriter, r *http.Request) {
		runRequests.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte("{\"id\":\"1\"}\n"))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	client := newLivenessTestClient(server.URL, lib.NewLogger(lib.LogLevelError))

	_, err := client.Flatten(livenessTestViewDefinition(), livenessTestResources())

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrFlattenerStopped)
	assert.NotContains(t, err.Error(), "timed out")
	assert.Equal(t, int32(1), runRequests.Load())
}

func TestFlattenerClient_HealthCheck_HangingMetadataRouteFailsAtLivenessTimeout(t *testing.T) {
	server := livenessServer(t, hangUntilClientLeaves, func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})
	client := newLivenessTestClient(server.URL, lib.NewLogger(lib.LogLevelError))

	start := time.Now()
	err := client.HealthCheck()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "flattener does not answer at "+server.URL+metadataPath)
	assert.Less(t, time.Since(start), 2*time.Second)
}

// Flatten builds its own request from the same base URL before the watchdog
// starts, so only a direct call reaches the request error in probe.
func TestProbe_MalformedURLFailsWithoutRequest(t *testing.T) {
	client := newLivenessTestClient("http://flattener", lib.NewLogger(lib.LogLevelError))

	err := probe(t.Context(), client.httpClient, "http://bad host"+metadataPath)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid character")
}

func TestFlattenerClient_Flatten_LogsFailureThatWatchdogDidNotCause(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	server.Close()
	var logs bytes.Buffer
	client := newLivenessTestClient(server.URL, lib.NewLoggerWithWriter(lib.LogLevelError, &logs))

	_, err := client.Flatten(livenessTestViewDefinition(), livenessTestResources())

	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrFlattenerStopped)
	assert.Contains(t, logs.String(), "Flattener HTTP request failed")
}
