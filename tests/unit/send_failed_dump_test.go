package unit

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

// newFailingFHIRServer returns a server that rejects every request with the
// given status and an OperationOutcome body.
func newFailingFHIRServer(t *testing.T, status int) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"resourceType":"OperationOutcome","issue":[{"severity":"error"}]}`))
	}))
	t.Cleanup(server.Close)
	return server
}

func TestRequestDump_WritesFailedTransactionBundle(t *testing.T) {
	server := newFailingFHIRServer(t, http.StatusBadRequest)
	dumpDir := t.TempDir()

	logger := lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1}, models.TLSConfig{}, logger)
	config := models.SendConfig{
		SendAs:    models.SendModeDirectResourceLoad,
		URL:       server.URL,
		BatchSize: 100,
	}
	client := services.NewFHIRClient(config, httpClient, logger)
	client.DumpFailedRequestsTo(services.NewRequestDump(dumpDir, logger))

	_, err := client.UploadNDJSON("patients.ndjson", strings.NewReader(`{"resourceType":"Patient","id":"1"}`))
	require.Error(t, err)

	matches, globErr := filepath.Glob(filepath.Join(dumpDir, "*.request.json"))
	require.NoError(t, globErr)
	require.Len(t, matches, 1, "the failing bundle must be written to disk")

	content, readErr := os.ReadFile(matches[0])
	require.NoError(t, readErr)

	var bundle map[string]any
	require.NoError(t, json.Unmarshal(content, &bundle))
	assert.Equal(t, "Bundle", bundle["resourceType"])
	assert.Equal(t, "transaction", bundle["type"])

	assert.Equal(t, filepath.Join(dumpDir, "patients-batch-1.request.json"), matches[0],
		"the file name must identify the source file and the batch")
}

func TestRequestDump_WritesServerResponseBesideRequest(t *testing.T) {
	server := newFailingFHIRServer(t, http.StatusUnprocessableEntity)
	dumpDir := t.TempDir()

	logger := lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1}, models.TLSConfig{}, logger)
	config := models.SendConfig{
		SendAs:    models.SendModeDirectResourceLoad,
		URL:       server.URL,
		BatchSize: 100,
	}
	client := services.NewFHIRClient(config, httpClient, logger)
	client.DumpFailedRequestsTo(services.NewRequestDump(dumpDir, logger))

	_, err := client.UploadNDJSON("patients.ndjson", strings.NewReader(`{"resourceType":"Patient","id":"1"}`))
	require.Error(t, err)

	content, readErr := os.ReadFile(filepath.Join(dumpDir, "patients-batch-1.response.txt"))
	require.NoError(t, readErr)

	assert.Contains(t, string(content), "422")
	assert.Contains(t, string(content), "OperationOutcome")
}

func TestRequestDump_WritesNothingWhenUploadSucceeds(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	dumpDir := filepath.Join(t.TempDir(), "failed")

	logger := lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1}, models.TLSConfig{}, logger)
	config := models.SendConfig{
		SendAs:    models.SendModeDirectResourceLoad,
		URL:       server.URL,
		BatchSize: 100,
	}
	client := services.NewFHIRClient(config, httpClient, logger)
	client.DumpFailedRequestsTo(services.NewRequestDump(dumpDir, logger))

	_, err := client.UploadNDJSON("patients.ndjson", strings.NewReader(`{"resourceType":"Patient","id":"1"}`))
	require.NoError(t, err)

	_, statErr := os.Stat(dumpDir)
	assert.True(t, os.IsNotExist(statErr), "a successful run must not leave patient data on disk")
}

func TestRequestDump_WarnsOnlyWhenResponseWriteFails(t *testing.T) {
	const warning = "Failed to write failed response"

	t.Run("write succeeds", func(t *testing.T) {
		var logs bytes.Buffer
		dump := services.NewRequestDump(t.TempDir(), lib.NewLoggerWithWriter(lib.LogLevelWarn, &logs))

		dump.Write("batch-1", []byte(`{}`), http.StatusBadRequest, []byte("rejected"))

		assert.NotContains(t, logs.String(), warning)
	})

	t.Run("write fails", func(t *testing.T) {
		dumpDir := t.TempDir()
		require.NoError(t, os.Mkdir(filepath.Join(dumpDir, "batch-1.response.txt"), 0o750))
		var logs bytes.Buffer
		dump := services.NewRequestDump(dumpDir, lib.NewLoggerWithWriter(lib.LogLevelWarn, &logs))

		dump.Write("batch-1", []byte(`{}`), http.StatusBadRequest, []byte("rejected"))

		assert.Contains(t, logs.String(), warning)
	})
}

// stageTransferSendInput writes one input file for a transfer_load send step and
// returns the job directory.
func stageTransferSendInput(t *testing.T, jobsDir, jobID string) string {
	t.Helper()
	jobDir := filepath.Join(jobsDir, jobID)
	inputDir := filepath.Join(jobDir, "csv")
	require.NoError(t, os.MkdirAll(inputDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(inputDir, "patient.csv"), []byte("id\n1\n"), 0o600))
	return jobDir
}

func TestSendStep_DumpsRejectedBinaryWithoutPayload(t *testing.T) {
	server := newFailingFHIRServer(t, http.StatusBadRequest)

	jobsDir := t.TempDir()
	jobID := "test-send-dump-binary"
	jobDir := stageTransferSendInput(t, jobsDir, jobID)

	job := createSendTestJob(server.URL, jobID, jobsDir)
	job.Config.Retry.MaxAttempts = 1
	job.Config.Services.Send.DumpFailedRequests = true

	err := runPipelineStep(models.StepSend, job, jobDir, lib.NewLogger(lib.LogLevelError))
	require.Error(t, err)

	matches, globErr := filepath.Glob(filepath.Join(jobDir, "send", "failed", "binary-*.request.json"))
	require.NoError(t, globErr)
	require.Len(t, matches, 1, "the rejected Binary must be written to disk")

	content, readErr := os.ReadFile(matches[0])
	require.NoError(t, readErr)

	var binary map[string]any
	require.NoError(t, json.Unmarshal(content, &binary))
	assert.Equal(t, "Binary", binary["resourceType"])
	assert.NotContains(t, binary, "data",
		"the base64 payload duplicates the input file and must not be written")
}

func TestSendStep_WritesNoDumpWhenDisabled(t *testing.T) {
	server := newFailingFHIRServer(t, http.StatusBadRequest)

	jobsDir := t.TempDir()
	jobID := "test-send-dump-disabled"
	jobDir := stageTransferSendInput(t, jobsDir, jobID)

	job := createSendTestJob(server.URL, jobID, jobsDir)
	job.Config.Retry.MaxAttempts = 1

	err := runPipelineStep(models.StepSend, job, jobDir, lib.NewLogger(lib.LogLevelError))
	require.Error(t, err)

	_, statErr := os.Stat(filepath.Join(jobDir, "send", "failed"))
	assert.True(t, os.IsNotExist(statErr), "the dump must stay off unless the config enables it")
}

func TestSendStep_DumpsRejectedDocumentReference(t *testing.T) {
	// The DocumentReference goes last, so let every Binary through first.
	var rejected bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/DocumentReference/") {
			rejected = true
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"resourceType":"OperationOutcome"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	jobsDir := t.TempDir()
	jobID := "test-send-dump-docref"
	jobDir := stageTransferSendInput(t, jobsDir, jobID)

	job := createSendTestJob(server.URL, jobID, jobsDir)
	job.Config.Retry.MaxAttempts = 1
	job.Config.Services.Send.DumpFailedRequests = true

	err := runPipelineStep(models.StepSend, job, jobDir, lib.NewLogger(lib.LogLevelError))
	require.Error(t, err)
	require.True(t, rejected, "the test must reach the DocumentReference upload")

	matches, globErr := filepath.Glob(filepath.Join(jobDir, "send", "failed", "documentreference-*.request.json"))
	require.NoError(t, globErr)
	require.Len(t, matches, 1, "the rejected DocumentReference must be written to disk")

	content, readErr := os.ReadFile(matches[0])
	require.NoError(t, readErr)

	var docRef map[string]any
	require.NoError(t, json.Unmarshal(content, &docRef))
	assert.Equal(t, "DocumentReference", docRef["resourceType"])
	assert.NotEmpty(t, docRef["content"], "the DocumentReference must be written complete")
}

func TestSendStep_DumpFailureKeepsTheSendError(t *testing.T) {
	server := newFailingFHIRServer(t, http.StatusBadRequest)

	jobsDir := t.TempDir()
	jobID := "test-send-dump-unwritable"
	jobDir := stageTransferSendInput(t, jobsDir, jobID)

	// A regular file where the dump directory belongs makes every write fail.
	require.NoError(t, os.MkdirAll(filepath.Join(jobDir, "send"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(jobDir, "send", "failed"), []byte("not a directory"), 0o600))

	job := createSendTestJob(server.URL, jobID, jobsDir)
	job.Config.Retry.MaxAttempts = 1
	job.Config.Services.Send.DumpFailedRequests = true

	err := runPipelineStep(models.StepSend, job, jobDir, lib.NewLogger(lib.LogLevelError))

	require.Error(t, err, "a failed dump must not hide the rejected upload")
	assert.Contains(t, err.Error(), "failed to upload Binary")
}

func TestSendStep_DumpsRejectedTransactionBundle(t *testing.T) {
	server := newFailingFHIRServer(t, http.StatusBadRequest)

	jobsDir := t.TempDir()
	jobID := "test-send-dump-bundle"
	jobDir := filepath.Join(jobsDir, jobID)
	inputDir := filepath.Join(jobDir, "csv")
	require.NoError(t, os.MkdirAll(inputDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(inputDir, "core.ndjson"),
		[]byte(`{"resourceType":"Patient","id":"1"}`+"\n"), 0o600))

	job := createSendTestJob(server.URL, jobID, jobsDir)
	job.Config.Retry.MaxAttempts = 1
	job.Config.Services.Send.SendAs = models.SendModeDirectResourceLoad
	job.Config.Services.Send.DumpFailedRequests = true

	err := runPipelineStep(models.StepSend, job, jobDir, lib.NewLogger(lib.LogLevelError))
	require.Error(t, err)

	content, readErr := os.ReadFile(filepath.Join(jobDir, "send", "failed", "core-batch-1.request.json"))
	require.NoError(t, readErr)

	var bundle map[string]any
	require.NoError(t, json.Unmarshal(content, &bundle))
	assert.Equal(t, "transaction", bundle["type"])
}
