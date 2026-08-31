// Package mocktorch is a small TORCH stand-in for tests and demos. It serves
// the bulk-data kick-off and status endpoints and, like TORCH with the
// torch-job-progress extension, batch progress on the Task API. Progress
// advances on each status poll, so the extraction duration is
// polls x polling_interval of the client.
package mocktorch

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
)

// Config controls the shape and speed of the simulated extraction.
type Config struct {
	CohortSize    int
	BatchSize     int
	PollsPerBatch int
	// DisableTaskAPI simulates a TORCH without the progress extension: the
	// Task route returns 404.
	DisableTaskAPI bool
}

// batchStages is the TORCH batch pipeline in order.
var batchStages = []string{"CONSENT_FETCH", "DIRECT_LOAD", "REFERENCE_RESOLVE", "CASCADING_DELETE", "COPY_REDACT"}

type jobState struct {
	polls int
}

// Server simulates TORCH for one or more extraction jobs.
type Server struct {
	cfg  Config
	mu   sync.Mutex
	jobs map[string]*jobState
	next int
}

// New creates a mock TORCH with the given extraction shape.
func New(cfg Config) *Server {
	if cfg.PollsPerBatch < 1 {
		cfg.PollsPerBatch = 1
	}
	if cfg.BatchSize < 1 {
		cfg.BatchSize = 1
	}
	return &Server{cfg: cfg, jobs: map[string]*jobState{}}
}

// batchesTotal returns the number of batches for the configured cohort.
func (s *Server) batchesTotal() int {
	return (s.cfg.CohortSize + s.cfg.BatchSize - 1) / s.cfg.BatchSize
}

// Handler returns the HTTP handler that serves the TORCH API surface.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/fhir/$extract-data", s.handleSubmit)
	mux.HandleFunc("/fhir/__status/", s.handleStatus)
	mux.HandleFunc("/fhir/Task/", s.handleTask)
	mux.HandleFunc("/output/", s.handleOutput)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	return mux
}

func (s *Server) handleSubmit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	s.mu.Lock()
	s.next++
	jobID := fmt.Sprintf("mock-job-%d", s.next)
	s.jobs[jobID] = &jobState{}
	s.mu.Unlock()

	w.Header().Set("Content-Location", "http://"+r.Host+"/fhir/__status/"+jobID)
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	jobID := strings.TrimPrefix(r.URL.Path, "/fhir/__status/")

	s.mu.Lock()
	job, ok := s.jobs[jobID]
	if !ok {
		s.mu.Unlock()
		w.WriteHeader(http.StatusNotFound)
		return
	}
	job.polls++
	done := job.polls/s.cfg.PollsPerBatch >= s.batchesTotal()
	s.mu.Unlock()

	if !done {
		w.Header().Set("Content-Type", "application/fhir+json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = fmt.Fprintf(w, `{"resourceType":"OperationOutcome","issue":[{"severity":"information","code":"informational","diagnostics":"cohort Size: %d"}]}`, s.cfg.CohortSize)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = fmt.Fprintf(w, `{"requiresAccessToken":false,"output":[{"type":"Patient","url":"http://%s/output/%s/patients.ndjson"}]}`, r.Host, jobID)
}

func (s *Server) handleTask(w http.ResponseWriter, r *http.Request) {
	if s.cfg.DisableTaskAPI {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	jobID := strings.TrimPrefix(r.URL.Path, "/fhir/Task/")

	s.mu.Lock()
	job, ok := s.jobs[jobID]
	if !ok {
		s.mu.Unlock()
		w.WriteHeader(http.StatusNotFound)
		return
	}
	polls := job.polls
	s.mu.Unlock()

	total := s.batchesTotal()
	completed := polls / s.cfg.PollsPerBatch
	if completed > total {
		completed = total
	}

	var active string
	if completed < total {
		// Map the position inside the current batch to one of the 5 stages.
		pollInBatch := polls % s.cfg.PollsPerBatch
		stage := batchStages[pollInBatch*len(batchStages)/s.cfg.PollsPerBatch]
		active = fmt.Sprintf(`,{"url":"activeBatch","extension":[{"url":"batchId","valueString":"batch-%d"},{"url":"stage","valueString":"%s"}]}`, completed+1, stage)
	}

	w.Header().Set("Content-Type", "application/fhir+json")
	_, _ = fmt.Fprintf(w, `{
		"resourceType": "Task", "id": "%s", "status": "in-progress",
		"extension": [{
			"url": "https://torch.mii.de/fhir/torch-job-progress",
			"extension": [
				{"url": "cohortSize", "valueInteger": %d},
				{"url": "batchSize", "valueInteger": %d},
				{"url": "batchesTotal", "valueInteger": %d},
				{"url": "batchesCompleted", "valueInteger": %d}%s
			]
		}]
	}`, jobID, s.cfg.CohortSize, s.cfg.BatchSize, total, completed, active)
}

func (s *Server) handleOutput(w http.ResponseWriter, r *http.Request) {
	count := s.cfg.CohortSize
	if count > 100 {
		count = 100
	}
	var body strings.Builder
	for i := 1; i <= count; i++ {
		fmt.Fprintf(&body, `{"resourceType":"Patient","id":"mock-patient-%d"}`+"\n", i)
	}
	w.Header().Set("Content-Type", "application/fhir+ndjson")
	// The download path probes availability with HEAD and requires a positive
	// Content-Length, so set it explicitly.
	w.Header().Set("Content-Length", fmt.Sprintf("%d", body.Len()))
	_, _ = io.WriteString(w, body.String())
}
