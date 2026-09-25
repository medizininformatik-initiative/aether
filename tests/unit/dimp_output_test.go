package unit

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
)

// runDIMPStepWithInput writes content to import/patients.ndjson, runs the
// DIMP step against dimpURL and returns the stdout, the log output and the
// step error.
func runDIMPStepWithInput(t *testing.T, dimpURL, content string) (stdout, logs string, err error) {
	t.Helper()
	tmpDir := t.TempDir()
	importDir := filepath.Join(tmpDir, "import")
	require.NoError(t, os.MkdirAll(importDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(importDir, "patients.ndjson"), []byte(content), 0644))

	var logBuf bytes.Buffer
	stop := captureStdoutForTest(t)
	err = runPipelineStep(models.StepDIMP, createDIMPTestJob(dimpURL), tmpDir, lib.NewLoggerWithWriter(lib.LogLevelDebug, &logBuf))
	stdout = stop()
	return stdout, logBuf.String(), err
}

// The resource count fails for a file that ends with invalid JSON, so the
// step processes the valid resources without a progress bar.
func TestExecuteDIMPStep_InvalidJSONAfterValidResourcesReportsItsOneBasedPosition(t *testing.T) {
	server := createMockDIMPServer()
	defer server.Close()

	content := `{"resourceType":"Patient","id":"p1"}` + "\n" +
		`{"resourceType":"Patient","id":"p2"}` + "\n" +
		"{invalid json\n"
	_, logs, err := runDIMPStepWithInput(t, server.URL, content)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse resource 3:")
	assert.Contains(t, logs, "Failed to parse FHIR resource | [file patients.ndjson resource_number 3 ")
	assert.Contains(t, logs, "Processing FHIR resource | [file patients.ndjson resource_number 1 resourceType Patient id p1]")
	assert.Contains(t, logs, "Processing FHIR resource | [file patients.ndjson resource_number 2 resourceType Patient id p2]")
}

func TestExecuteDIMPStep_DIMPErrorWithoutProgressBarReportsOneBasedPositions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error": "bad request"}`))
	}))
	defer server.Close()

	content := `{"resourceType":"Patient","id":"p1"}` + "\n" + "{invalid json\n"
	stdout, logs, err := runDIMPStepWithInput(t, server.URL, content)

	require.Error(t, err)
	assert.Contains(t, stdout, "File: patients.ndjson (resource 1)")
	assert.Contains(t, logs, "Failed to process FHIR file | [filename patients.ndjson file_number 1 total_files 1 ")
}

func TestExecuteDIMPStep_EmptyFileRunsWithoutProgressBar(t *testing.T) {
	server := createMockDIMPServer()
	defer server.Close()

	_, logs, err := runDIMPStepWithInput(t, server.URL, "")

	require.NoError(t, err)
	assert.Contains(t, logs, "Processing FHIR resources (unknown count)")
}

func TestExecuteDIMPStep_ValidFileHasKnownResourceCount(t *testing.T) {
	server := createMockDIMPServer()
	defer server.Close()

	_, logs, err := runDIMPStepWithInput(t, server.URL, `{"resourceType":"Patient","id":"p1"}`+"\n")

	require.NoError(t, err)
	require.Contains(t, logs, "DIMP step completed")
	assert.NotContains(t, logs, "Processing FHIR resources (unknown count)")
}

func TestExecuteDIMPStep_ResumeCountsResourcesOfAlreadyProcessedFiles(t *testing.T) {
	server := createMockDIMPServer()
	defer server.Close()

	tmpDir := t.TempDir()
	importDir := filepath.Join(tmpDir, "import")
	dimpDir := filepath.Join(tmpDir, "dimp")
	require.NoError(t, os.MkdirAll(importDir, 0755))
	require.NoError(t, os.MkdirAll(dimpDir, 0755))
	writeDIMPNDJSON(t, filepath.Join(importDir, "done.ndjson"), []map[string]any{
		{"resourceType": "Patient", "id": "p1"},
		{"resourceType": "Patient", "id": "p2"},
	})
	writeDIMPNDJSON(t, filepath.Join(dimpDir, "dimped_done.ndjson"), []map[string]any{
		{"resourceType": "Patient", "id": "pseudo-p1"},
		{"resourceType": "Patient", "id": "pseudo-p2"},
	})
	writeDIMPNDJSON(t, filepath.Join(importDir, "new.ndjson"), []map[string]any{
		{"resourceType": "Patient", "id": "p3"},
	})

	var logBuf bytes.Buffer
	stop := captureStdoutForTest(t)
	err := runPipelineStep(models.StepDIMP, createDIMPTestJob(server.URL), tmpDir, lib.NewLoggerWithWriter(lib.LogLevelDebug, &logBuf))
	stop()

	require.NoError(t, err)
	assert.Contains(t, logBuf.String(), "DIMP step completed | [files_processed 2 resources_processed 3 ")
}
