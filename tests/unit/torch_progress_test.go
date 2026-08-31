package unit

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

const taskWithProgressJSON = `{
  "resourceType": "Task",
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "status": "in-progress",
  "extension": [
    {
      "url": "https://torch.mii.de/fhir/torch-job-progress",
      "extension": [
        { "url": "cohortSize", "valueInteger": 1200 },
        { "url": "batchSize", "valueInteger": 500 },
        { "url": "batchesTotal", "valueInteger": 3 },
        { "url": "batchesCompleted", "valueInteger": 1 },
        {
          "url": "activeBatch",
          "extension": [
            { "url": "batchId", "valueString": "3fa85f64-5717-4562-b3fc-2c963f66afa6" },
            { "url": "stage", "valueString": "REFERENCE_RESOLVE" }
          ]
        },
        {
          "url": "activeBatch",
          "extension": [
            { "url": "batchId", "valueString": "7c9e6679-7425-40de-944b-e07fc1f90ae7" },
            { "url": "stage", "valueString": "DIRECT_LOAD" }
          ]
        }
      ]
    }
  ]
}`

func newProgressTestClient(t *testing.T, baseURL string) *services.TORCHClient {
	t.Helper()
	logger := lib.NewLogger(lib.LogLevelDebug)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 3, InitialBackoffMs: 100, MaxBackoffMs: 1000}, models.TLSConfig{}, logger)
	cfg := models.TORCHConfig{
		BaseURL:            baseURL,
		ExtractionTimeout:  time.Minute,
		PollingInterval:    10 * time.Millisecond,
		MaxPollingInterval: 10 * time.Millisecond,
	}
	return services.NewTORCHClient(cfg, httpClient, logger)
}

func TestTORCHClient_FetchJobProgress_ParsesExtension(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/fhir/Task/550e8400-e29b-41d4-a716-446655440000", r.URL.Path)
		w.Header().Set("Content-Type", "application/fhir+json")
		_, _ = w.Write([]byte(taskWithProgressJSON))
	}))
	defer server.Close()

	client := newProgressTestClient(t, server.URL)

	progress := client.FetchJobProgress("550e8400-e29b-41d4-a716-446655440000")

	require.NotNil(t, progress)
	assert.Equal(t, 1200, progress.CohortSize)
	assert.Equal(t, 500, progress.BatchSize)
	assert.Equal(t, 3, progress.BatchesTotal)
	assert.Equal(t, 1, progress.BatchesCompleted)
	require.Len(t, progress.ActiveBatches, 2)
	assert.Equal(t, "3fa85f64-5717-4562-b3fc-2c963f66afa6", progress.ActiveBatches[0].BatchID)
	assert.Equal(t, "REFERENCE_RESOLVE", progress.ActiveBatches[0].Stage)
	assert.Equal(t, "7c9e6679-7425-40de-944b-e07fc1f90ae7", progress.ActiveBatches[1].BatchID)
	assert.Equal(t, "DIRECT_LOAD", progress.ActiveBatches[1].Stage)
}

func TestTORCHClient_FetchJobProgress_SilentFallback(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
	}{
		{"task without extension", http.StatusOK, `{"resourceType":"Task","id":"abc","status":"in-progress"}`},
		{"not found", http.StatusNotFound, `{"resourceType":"OperationOutcome"}`},
		{"server error", http.StatusInternalServerError, ``},
		{"bad json", http.StatusOK, `{not json`},
		{"wrong resource type", http.StatusOK, `{"resourceType":"OperationOutcome"}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()

			client := newProgressTestClient(t, server.URL)

			assert.Nil(t, client.FetchJobProgress("abc"))
		})
	}
}

func TestTORCHClient_FetchJobProgress_ServerUnreachable(t *testing.T) {
	client := newProgressTestClient(t, "http://127.0.0.1:1")

	assert.Nil(t, client.FetchJobProgress("abc"))
}

func TestTORCHClient_PollExtractionStatus_ReportsProgress(t *testing.T) {
	const jobID = "550e8400-e29b-41d4-a716-446655440000"

	var pollCount int
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/fhir/__status/" + jobID:
			pollCount++
			if pollCount < 3 {
				w.WriteHeader(http.StatusAccepted)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"requiresAccessToken":false,"output":[{"type":"Patient","url":"` + server.URL + `/output/f1.ndjson"}]}`))
		case "/fhir/Task/" + jobID:
			completed := pollCount
			w.Header().Set("Content-Type", "application/fhir+json")
			_, _ = fmt.Fprintf(w, `{
				"resourceType": "Task", "id": "%s", "status": "in-progress",
				"extension": [{
					"url": "https://torch.mii.de/fhir/torch-job-progress",
					"extension": [
						{"url": "cohortSize", "valueInteger": 1200},
						{"url": "batchSize", "valueInteger": 500},
						{"url": "batchesTotal", "valueInteger": 3},
						{"url": "batchesCompleted", "valueInteger": %d}
					]
				}]
			}`, jobID, completed)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := newProgressTestClient(t, server.URL)

	var reported []services.TORCHProgress
	client.SetProgressHandler(func(p services.TORCHProgress) {
		reported = append(reported, p)
	})

	fileURLs, err := client.PollExtractionStatus(server.URL+"/fhir/__status/"+jobID, false)

	require.NoError(t, err)
	assert.Len(t, fileURLs, 1)
	require.Len(t, reported, 2)
	assert.Equal(t, 1, reported[0].BatchesCompleted)
	assert.Equal(t, 2, reported[1].BatchesCompleted)
	assert.Equal(t, 1200, reported[0].CohortSize)
}

func TestTORCHClient_PollExtractionStatus_NoTaskRoute_NoProgress(t *testing.T) {
	const jobID = "abc123"

	var pollCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/fhir/__status/"+jobID {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		pollCount++
		if pollCount < 2 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"requiresAccessToken":false,"output":[{"type":"Patient","url":"/output/f1.ndjson"}]}`))
	}))
	defer server.Close()

	client := newProgressTestClient(t, server.URL)

	handlerCalls := 0
	client.SetProgressHandler(func(services.TORCHProgress) { handlerCalls++ })

	_, err := client.PollExtractionStatus(server.URL+"/fhir/__status/"+jobID, false)

	require.NoError(t, err)
	assert.Equal(t, 0, handlerCalls)
}

// A TORCH without the Task API gets one probe, not one request per poll.
func TestTORCHClient_PollExtractionStatus_UnsupportedTaskAPI_ProbedOnce(t *testing.T) {
	const jobID = "abc123"

	var pollCount, taskRequests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/fhir/__status/" + jobID:
			pollCount++
			if pollCount < 4 {
				w.WriteHeader(http.StatusAccepted)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"requiresAccessToken":false,"output":[{"type":"Patient","url":"/output/f1.ndjson"}]}`))
		case "/fhir/Task/" + jobID:
			taskRequests++
			w.WriteHeader(http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := newProgressTestClient(t, server.URL)

	_, err := client.PollExtractionStatus(server.URL+"/fhir/__status/"+jobID, false)

	require.NoError(t, err)
	assert.Equal(t, 1, taskRequests, "an unsupported Task API gets probed once, then skipped")
}

// A Task without the progress extension (cohort query still running) is not
// "unsupported": the client keeps asking on later polls.
func TestTORCHClient_PollExtractionStatus_ExtensionAppearsLater(t *testing.T) {
	const jobID = "abc123"

	var pollCount int
	var reported []services.TORCHProgress
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/fhir/__status/" + jobID:
			pollCount++
			if pollCount < 3 {
				w.WriteHeader(http.StatusAccepted)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"requiresAccessToken":false,"output":[{"type":"Patient","url":"/output/f1.ndjson"}]}`))
		case "/fhir/Task/" + jobID:
			w.Header().Set("Content-Type", "application/fhir+json")
			if pollCount < 2 {
				// Cohort query still running: Task exists, no extension yet.
				_, _ = fmt.Fprintf(w, `{"resourceType":"Task","id":"%s","status":"in-progress"}`, jobID)
				return
			}
			_, _ = fmt.Fprintf(w, `{"resourceType":"Task","id":"%s","status":"in-progress","extension":[{"url":"https://torch.mii.de/fhir/torch-job-progress","extension":[{"url":"cohortSize","valueInteger":10},{"url":"batchSize","valueInteger":10},{"url":"batchesTotal","valueInteger":1},{"url":"batchesCompleted","valueInteger":0}]}]}`, jobID)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := newProgressTestClient(t, server.URL)
	client.SetProgressHandler(func(p services.TORCHProgress) { reported = append(reported, p) })

	_, err := client.PollExtractionStatus(server.URL+"/fhir/__status/"+jobID, false)

	require.NoError(t, err)
	require.NotEmpty(t, reported, "progress must arrive once the extension appears")
	assert.Equal(t, 10, reported[0].CohortSize)
}

func TestTORCHProgress_Fraction(t *testing.T) {
	progress := services.TORCHProgress{
		CohortSize:       1200,
		BatchSize:        500,
		BatchesTotal:     3,
		BatchesCompleted: 1,
		ActiveBatches: []services.TORCHActiveBatch{
			{BatchID: "a", Stage: "DIRECT_LOAD"},       // stage 2 of 5
			{BatchID: "b", Stage: "REFERENCE_RESOLVE"}, // stage 3 of 5
		},
	}

	// (1 + 2/5 + 3/5) / 3 = 2/3
	assert.InDelta(t, 2.0/3.0, progress.Fraction(), 0.001)
}

func TestTORCHProgress_Fraction_Boundaries(t *testing.T) {
	assert.Equal(t, 0.0, services.TORCHProgress{}.Fraction())

	done := services.TORCHProgress{BatchesTotal: 3, BatchesCompleted: 3}
	assert.Equal(t, 1.0, done.Fraction())

	unknownStage := services.TORCHProgress{
		BatchesTotal:  2,
		ActiveBatches: []services.TORCHActiveBatch{{BatchID: "a", Stage: "SOMETHING_NEW"}},
	}
	assert.Equal(t, 0.0, unknownStage.Fraction())
}

func TestTORCHProgress_PatientsDone(t *testing.T) {
	progress := services.TORCHProgress{CohortSize: 1200, BatchSize: 500, BatchesTotal: 3, BatchesCompleted: 1}
	assert.Equal(t, 500, progress.PatientsDone())

	// The last batch is smaller: cap at the cohort size.
	full := services.TORCHProgress{CohortSize: 1200, BatchSize: 500, BatchesTotal: 3, BatchesCompleted: 3}
	assert.Equal(t, 1200, full.PatientsDone())
}

func TestTORCHProgress_Summary(t *testing.T) {
	progress := services.TORCHProgress{
		CohortSize:       1200,
		BatchSize:        500,
		BatchesTotal:     3,
		BatchesCompleted: 1,
		ActiveBatches: []services.TORCHActiveBatch{
			{BatchID: "a", Stage: "DIRECT_LOAD"},
			{BatchID: "b", Stage: "REFERENCE_RESOLVE"},
		},
	}

	assert.Equal(t,
		"1/3 batches (500/1200 patients), active: DIRECT_LOAD (2/5), REFERENCE_RESOLVE (3/5)",
		progress.Summary())

	noActive := services.TORCHProgress{CohortSize: 1200, BatchSize: 500, BatchesTotal: 3, BatchesCompleted: 3}
	assert.Equal(t, "3/3 batches (1200/1200 patients)", noActive.Summary())
}

func TestTORCHProgress_TerminalLine(t *testing.T) {
	progress := services.TORCHProgress{
		CohortSize:       1200,
		BatchSize:        500,
		BatchesTotal:     3,
		BatchesCompleted: 1,
		ActiveBatches: []services.TORCHActiveBatch{
			{BatchID: "a", Stage: "DIRECT_LOAD"},
			{BatchID: "b", Stage: "REFERENCE_RESOLVE"},
		},
	}

	// Fraction 2/3 -> 66%, bar width 16 -> 10 filled.
	assert.Equal(t,
		"TORCH extraction [##########......] 66% — 1/3 batches (500/1200 patients), active: DIRECT_LOAD (2/5), REFERENCE_RESOLVE (3/5)",
		progress.TerminalLine())
}
