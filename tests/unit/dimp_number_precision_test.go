package unit

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/models"
)

const numberFixture = `"a":1.50,"b":0.000000120,"c":12345678901234567891,"d":100.0,` +
	`"e":-0.50,"f":1.0E-5,"g":[1.10,[2.20]]`

var numberLiterals = []string{
	"1.50", "0.000000120", "12345678901234567891", "100.0", "-0.50", "1.0E-5", "[1.10,[2.20]]",
}

// assertNumberLiterals checks that each literal of numberFixture occurs count times in text.
func assertNumberLiterals(t *testing.T, text string, count int) {
	t.Helper()
	for _, lit := range numberLiterals {
		assert.Equal(t, count, strings.Count(text, lit), "occurrences of %s", lit)
	}
}

func readDIMPOutput(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "dimp", "dimped_patients.ndjson"))
	require.NoError(t, err)
	return string(data)
}

// runDIMPInDir writes content to import/patients.ndjson in tmpDir and runs
// the DIMP step against dimpURL.
func runDIMPInDir(t *testing.T, dimpURL, tmpDir, content string) {
	t.Helper()
	importDir := filepath.Join(tmpDir, "import")
	require.NoError(t, os.MkdirAll(importDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(importDir, "patients.ndjson"), []byte(content), 0644))
	require.NoError(t, runPipelineStep(models.StepDIMP, createDIMPTestJob(dimpURL), tmpDir, createDIMPTestLogger()))
}

func TestExecuteDIMPStep_KeepsNumberTextOfDIMPResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/fhir+json")
		_, _ = w.Write([]byte(`{"resourceType":"Patient","id":"p1",` + numberFixture + `}`))
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	runDIMPInDir(t, server.URL, tmpDir, `{"resourceType":"Patient","id":"p1"}`+"\n")

	assertNumberLiterals(t, readDIMPOutput(t, tmpDir), 1)
}

func TestExecuteDIMPStep_KeepsNumberTextOfSplitBundle(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/fhir+json")
		_, _ = w.Write(body)
	}))
	defer server.Close()

	padding := strings.Repeat("x", 100*1024)
	var entries []string
	for i := 0; i < 20; i++ {
		entries = append(entries, fmt.Sprintf(
			`{"fullUrl":"urn:uuid:c-%d","resource":{"resourceType":"Observation","id":"o-%d","pad":"%s",%s}}`,
			i, i, padding, numberFixture))
	}
	bundle := `{"resourceType":"Bundle","id":"b1","type":"collection","entry":[` + strings.Join(entries, ",") + "]}\n"

	tmpDir := t.TempDir()
	importDir := filepath.Join(tmpDir, "import")
	require.NoError(t, os.MkdirAll(importDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(importDir, "bundles.ndjson"), []byte(bundle), 0644))
	job := createDIMPTestJob(server.URL)
	job.Config.Services.DIMP.BundleSplitThresholdMB = 1
	require.NoError(t, runPipelineStep(models.StepDIMP, job, tmpDir, createDIMPTestLogger()))

	out, err := os.ReadFile(filepath.Join(tmpDir, "dimp", "dimped_bundles.ndjson"))
	require.NoError(t, err)
	assert.Greater(t, requests.Load(), int32(1), "the Bundle must be split into chunks")
	assertNumberLiterals(t, string(out), 20)
}

func TestExecuteValidationStep_KeepsNumberTextForValidator(t *testing.T) {
	var received atomic.Value
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		received.Store(string(body))
		w.Header().Set("Content-Type", "application/fhir+json")
		_, _ = w.Write([]byte(`{"resourceType":"OperationOutcome","issue":[{"severity":"information","code":"informational"}]}`))
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	importDir := filepath.Join(tmpDir, "import")
	require.NoError(t, os.MkdirAll(importDir, 0755))
	content := `{"resourceType":"Observation","id":"o1",` + numberFixture + "}\n"
	require.NoError(t, os.WriteFile(filepath.Join(importDir, "Observation.ndjson"), []byte(content), 0644))

	job := createValidationTestJob(server.URL)
	require.NoError(t, runPipelineStep(models.StepValidation, job, tmpDir, createValidationTestLogger()))

	body, _ := received.Load().(string)
	assertNumberLiterals(t, body, 1)
}

func TestExecuteDIMPStep_KeepsNumberTextOfInputFile(t *testing.T) {
	var received atomic.Value
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		received.Store(string(body))
		w.Header().Set("Content-Type", "application/fhir+json")
		_, _ = w.Write(body)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	runDIMPInDir(t, server.URL, tmpDir, `{"resourceType":"Patient","id":"p1",`+numberFixture+"}\n")

	requestBody, _ := received.Load().(string)
	assertNumberLiterals(t, requestBody, 1)
	assertNumberLiterals(t, readDIMPOutput(t, tmpDir), 1)
}
