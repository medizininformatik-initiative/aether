package unit

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/pipeline"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

func TestExecuteSendStep_TransferPrintsOneBasedBinaryProgress(t *testing.T) {
	server := newSendOKServer(t)

	tmpDir := t.TempDir()
	jobID := "test-send-progress"
	jobDir := stageTransferSendInput(t, tmpDir, jobID)
	require.NoError(t, os.WriteFile(filepath.Join(jobDir, "csv", "second.csv"), []byte("id\n2\n"), 0o600))

	job := createSendTestJob(server.URL, jobID, tmpDir)

	stop := captureStdoutForTest(t)
	err := runPipelineStep(models.StepSend, job, jobDir, lib.NewLogger(lib.LogLevelError))
	out := stop()
	require.NoError(t, err)

	assert.Contains(t, out, "Uploading Binary 1/2...")
	assert.Contains(t, out, "Uploading Binary 2/2...")
}

func newSendOKServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/fhir+json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"resourceType":"Bundle","type":"transaction-response"}`))
	}))
	t.Cleanup(server.Close)
	return server
}

// runFHIRSendWithFiles writes one Patient resource to each named file and
// returns the stdout and the log output of the send step.
func runFHIRSendWithFiles(t *testing.T, fileNames ...string) (stdout, logs string) {
	t.Helper()
	server := newSendOKServer(t)

	tmpDir := t.TempDir()
	jobID := "test-fhir-headers"
	jobDir := filepath.Join(tmpDir, jobID)
	inputDir := filepath.Join(jobDir, "dimp")
	require.NoError(t, os.MkdirAll(inputDir, 0755))
	for _, name := range fileNames {
		require.NoError(t, os.WriteFile(filepath.Join(inputDir, name), []byte(`{"resourceType":"Patient","id":"p1"}`+"\n"), 0644))
	}

	job := createFHIRSendTestJob(server.URL, jobID, tmpDir)

	var logBuf bytes.Buffer
	stop := captureStdoutForTest(t)
	err := runPipelineStep(models.StepSend, job, jobDir, lib.NewLoggerWithWriter(lib.LogLevelDebug, &logBuf))
	stdout = stop()
	require.NoError(t, err)
	return stdout, logBuf.String()
}

func TestExecuteSendStep_FHIR_PrintsCoreHeaderThenOtherHeaderOnce(t *testing.T) {
	out, _ := runFHIRSendWithFiles(t, "core.ndjson", "Observation.ndjson", "Patient.ndjson")

	coreHeader := "Processing core files first (1 file(s)):"
	otherHeader := "Processing other files (2 file(s)):"
	require.Contains(t, out, coreHeader)
	require.Equal(t, 1, strings.Count(out, otherHeader), out)

	coreAt := strings.Index(out, coreHeader)
	coreFileAt := strings.Index(out, "✓ core.ndjson")
	otherAt := strings.Index(out, otherHeader)
	firstOtherFileAt := strings.Index(out, "✓ Observation.ndjson")
	assert.Less(t, coreAt, coreFileAt)
	assert.Less(t, coreFileAt, otherAt)
	assert.Less(t, otherAt, firstOtherFileAt)
}

func TestExecuteSendStep_FHIR_WithoutCoreFilePrintsNoGroupHeaders(t *testing.T) {
	out, _ := runFHIRSendWithFiles(t, "Observation.ndjson", "Patient.ndjson")

	assert.Contains(t, out, "✓ Observation.ndjson")
	assert.NotContains(t, out, "Processing core files first")
	assert.NotContains(t, out, "Processing other files")
}

func TestExecuteSendStep_FHIR_ClosesInputFilesWithoutWarning(t *testing.T) {
	_, logs := runFHIRSendWithFiles(t, "Patient.ndjson")

	require.Contains(t, logs, "Direct resource load send step completed")
	assert.NotContains(t, logs, "Failed to close file reader")
}

// runS3SendWithJobDirSetup runs the S3 send step with one input file. prepare
// changes the job directory before the step starts. It returns the log output
// and the uploaded S3 keys.
func runS3SendWithJobDirSetup(t *testing.T, prepare func(jobDir string)) (logs string, uploadedKeys []string) {
	t.Helper()
	mock := &services.MockS3Uploader{Bucket: "test-bucket"}
	pipeline.SetS3UploaderFactoryForTesting(func(_ models.S3Config, _ models.AuthConfig, _ models.TLSConfig, _ *lib.Logger) (services.S3Uploader, error) {
		return mock, nil
	})
	defer pipeline.ResetS3UploaderFactory()

	tmpDir := t.TempDir()
	jobID := "test-s3-manifest-log"
	jobDir := filepath.Join(tmpDir, jobID)
	inputDir := filepath.Join(jobDir, "dimp")
	require.NoError(t, os.MkdirAll(inputDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(inputDir, "Patient.ndjson"), []byte(`{"resourceType":"Patient","id":"1"}`+"\n"), 0644))
	prepare(jobDir)

	job := createS3SendTestJob(jobID, tmpDir)
	var logBuf bytes.Buffer
	require.NoError(t, runPipelineStep(models.StepSend, job, jobDir, lib.NewLoggerWithWriter(lib.LogLevelDebug, &logBuf)))
	return logBuf.String(), mock.UploadedKeys
}

func TestExecuteSendStep_S3_CorruptManifestLogsWarningAndStartsFresh(t *testing.T) {
	logs, keys := runS3SendWithJobDirSetup(t, func(jobDir string) {
		require.NoError(t, os.WriteFile(models.GetUploadManifestPath(jobDir), []byte("{not json"), 0644))
	})

	assert.Contains(t, logs, "Failed to load upload manifest, starting fresh")
	assert.Len(t, keys, 1)
}

func TestExecuteSendStep_S3_ManifestSaveErrorLogsWarningAndContinues(t *testing.T) {
	logs, keys := runS3SendWithJobDirSetup(t, func(jobDir string) {
		// A directory at the temp path makes the manifest write fail.
		require.NoError(t, os.MkdirAll(models.GetUploadManifestPath(jobDir)+".tmp", 0755))
	})

	assert.Contains(t, logs, "Failed to save upload manifest")
	assert.NotContains(t, logs, "Failed to load upload manifest")
	assert.Len(t, keys, 1)
}
