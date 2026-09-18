package pipeline

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
)

// The torch step must persist TORCH batch progress into the job file while
// polling, so `aether pipeline status` can show it from another process.
func TestExecuteTORCHExtraction_PersistsProgress(t *testing.T) {
	const torchJobID = "progress-job-uuid"
	var pollCount int32
	var progressOnDiskAtCompletion string

	logger := lib.NewLogger(lib.LogLevelError)
	job := newReattachJob(t, models.TORCHConfig{})
	job.CRTDLPath = writeTestCRTDL(t)
	job.Steps = []models.PipelineStep{{Name: models.StepTorchImport, Status: models.StepStatusInProgress}}

	mux := http.NewServeMux()
	mux.HandleFunc("/fhir/$extract-data", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Location", "http://"+r.Host+"/fhir/__status/"+torchJobID)
		w.WriteHeader(http.StatusAccepted)
	})
	mux.HandleFunc("/fhir/__status/"+torchJobID, func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&pollCount, 1)
		if count < 3 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		// On completion, the progress from the previous poll must be on disk.
		if reloaded, err := LoadJob(job.Config.JobsDir, job.JobID); err == nil {
			if step, found := models.GetStepByName(*reloaded, models.StepTorchImport); found && step.Progress != nil {
				progressOnDiskAtCompletion = step.Progress.Message
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"output":[{"type":"Patient","url":"http://` + r.Host + `/output/p.ndjson"}]}`))
	})
	mux.HandleFunc("/fhir/Task/"+torchJobID, func(w http.ResponseWriter, r *http.Request) {
		completed := atomic.LoadInt32(&pollCount)
		w.Header().Set("Content-Type", "application/fhir+json")
		_, _ = fmt.Fprintf(w, `{
			"resourceType": "Task", "id": "%s", "status": "in-progress",
			"extension": [{
				"url": "https://torch.mii.de/fhir/torch-job-progress",
				"extension": [
					{"url": "cohortSize", "valueInteger": 100},
					{"url": "batchSize", "valueInteger": 50},
					{"url": "batchesTotal", "valueInteger": 2},
					{"url": "batchesCompleted", "valueInteger": %d}
				]
			}]
		}`, torchJobID, completed)
	})
	mux.HandleFunc("/output/p.ndjson", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"resourceType":"Patient","id":"p1"}` + "\n"))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	job.Config.Services.TORCH = newTORCHTestClientConfig(server.URL)
	require.NoError(t, UpdateJob(job.Config.JobsDir, job))

	files, err := executeTORCHExtraction(job, t.TempDir(), newTestHTTPClient(logger), logger, false, false, "")

	require.NoError(t, err)
	assert.Len(t, files, 1)

	step, found := models.GetStepByName(*job, models.StepTorchImport)
	require.True(t, found)
	require.NotNil(t, step.Progress)
	assert.Equal(t, 2, step.Progress.Total)
	assert.NotEmpty(t, step.Progress.Message)
	assert.NotZero(t, step.Progress.UpdatedAt)

	assert.Equal(t, "2/2 batches (100/100 patients)", progressOnDiskAtCompletion)
}
