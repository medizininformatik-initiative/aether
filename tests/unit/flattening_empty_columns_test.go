package unit

import (
	"bytes"
	"encoding/json"
	"fmt"
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
)

// writeTwoColumnCRTDL writes a CRTDL with one attribute group that has two
// attributes: id and gender.
func writeTwoColumnCRTDL(t *testing.T, path, groupID, groupName, groupRef string) {
	crtdl := map[string]any{
		"dataExtraction": map[string]any{
			"attributeGroups": []map[string]any{
				{
					"id":             groupID,
					"name":           groupName,
					"groupReference": groupRef,
					"attributes": []map[string]any{
						{"attributeRef": groupName + ".id", "mustHave": true},
						{"attributeRef": groupName + ".gender", "mustHave": false},
					},
				},
			},
		},
	}
	data, err := json.Marshal(crtdl)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0644))
}

// writeTwoColumnLookupTable writes a lookup table that maps the id and gender
// elements to the columns id and gender.
func writeTwoColumnLookupTable(t *testing.T, path, profileURL, resourceType string) {
	lookup := []map[string]any{
		{
			"url":          profileURL,
			"resourceType": resourceType,
			"elements": map[string]any{
				resourceType + ".id": map[string]any{
					"viewDefinition": map[string]any{
						"select": []map[string]any{
							{"column": []map[string]any{{"name": "id", "path": "id"}}},
						},
					},
				},
				resourceType + ".gender": map[string]any{
					"viewDefinition": map[string]any{
						"select": []map[string]any{
							{"column": []map[string]any{{"name": "gender", "path": "gender"}}},
						},
					},
				},
			},
		},
	}
	data, err := json.Marshal(lookup)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0644))
}

// countEmptyColumnWarnings counts the log lines that report columns with no data.
func countEmptyColumnWarnings(logOutput string) int {
	count := 0
	for _, line := range strings.Split(logOutput, "\n") {
		if strings.Contains(line, "no data") && strings.Contains(line, "column") {
			count++
		}
	}
	return count
}

// TestExecuteFlatteningStep_EmptyColumnWarningOncePerGroup verifies that a
// column with no data in the full job causes only one warning, although the
// step sends many batches to the flattener.
func TestExecuteFlatteningStep_EmptyColumnWarningOncePerGroup(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/fhir/ViewDefinition/$run" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		callCount++
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.WriteHeader(http.StatusOK)
		// Every row has id. No row has gender.
		_, _ = fmt.Fprintf(w, `{"id":"patient-%d"}`+"\n", callCount)
	}))
	defer server.Close()

	tempDir := t.TempDir()
	jobDir := filepath.Join(tempDir, "jobs", "test-empty-columns")
	inputDir := filepath.Join(jobDir, "import")
	require.NoError(t, os.MkdirAll(inputDir, 0755))

	profileURL := "https://example.com/Patient"
	groupID := "group-patient"

	crtdlPath := filepath.Join(tempDir, "test.json")
	writeTwoColumnCRTDL(t, crtdlPath, groupID, "Patient", profileURL)

	lookupPath := filepath.Join(tempDir, "lookup.json")
	writeTwoColumnLookupTable(t, lookupPath, profileURL, "Patient")

	writeTestNDJSON(t, filepath.Join(inputDir, "patients.ndjson"), makeLargePatientBundles(profileURL, groupID))

	job := createFlatteningTestJob(server.URL, lookupPath, crtdlPath)
	job.Config.Services.Flattening.BatchSizeMB = 1

	var logOutput bytes.Buffer
	logger := lib.NewLoggerWithWriter(lib.LogLevelDebug, &logOutput)

	require.NoError(t, runPipelineStep(models.StepFlattening, job, jobDir, logger))
	require.Greater(t, callCount, 1, "test needs more than one batch")

	assert.Equal(t, 1, countEmptyColumnWarnings(logOutput.String()),
		"expected one warning for the full job")
	assert.Contains(t, logOutput.String(), "gender")
}

// TestExecuteFlatteningStep_NoWarningIfColumnHasDataInSomeRows verifies that a
// column with data in only some rows causes no warning. FHIR elements are
// optional, thus this condition is normal.
func TestExecuteFlatteningStep_NoWarningIfColumnHasDataInSomeRows(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/fhir/ViewDefinition/$run" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		callCount++
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.WriteHeader(http.StatusOK)
		// The first batch has no gender. All later batches have gender.
		if callCount == 1 {
			_, _ = fmt.Fprint(w, `{"id":"patient-1"}`+"\n")
			return
		}
		_, _ = fmt.Fprintf(w, `{"id":"patient-%d","gender":"female"}`+"\n", callCount)
	}))
	defer server.Close()

	tempDir := t.TempDir()
	jobDir := filepath.Join(tempDir, "jobs", "test-partial-columns")
	inputDir := filepath.Join(jobDir, "import")
	require.NoError(t, os.MkdirAll(inputDir, 0755))

	profileURL := "https://example.com/Patient"
	groupID := "group-patient"

	crtdlPath := filepath.Join(tempDir, "test.json")
	writeTwoColumnCRTDL(t, crtdlPath, groupID, "Patient", profileURL)

	lookupPath := filepath.Join(tempDir, "lookup.json")
	writeTwoColumnLookupTable(t, lookupPath, profileURL, "Patient")

	writeTestNDJSON(t, filepath.Join(inputDir, "patients.ndjson"), makeLargePatientBundles(profileURL, groupID))

	job := createFlatteningTestJob(server.URL, lookupPath, crtdlPath)
	job.Config.Services.Flattening.BatchSizeMB = 1

	var logOutput bytes.Buffer
	logger := lib.NewLoggerWithWriter(lib.LogLevelDebug, &logOutput)

	require.NoError(t, runPipelineStep(models.StepFlattening, job, jobDir, logger))
	require.Greater(t, callCount, 1, "test needs more than one batch")

	assert.Equal(t, 0, countEmptyColumnWarnings(logOutput.String()))
}
