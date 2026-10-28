package integration

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/services/servicestest"
)

// TestExecuteFlatteningStep_ProvenanceRoutesOnlyOwnBundle verifies that a
// Provenance that targets a resource of another Bundle does not route that
// resource.
func TestExecuteFlatteningStep_ProvenanceRoutesOnlyOwnBundle(t *testing.T) {
	var body []byte
	server := servicestest.NewFlattenerServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/fhir/ViewDefinition/$run" {
			body, _ = io.ReadAll(r.Body)
			w.Header().Set("Content-Type", "application/x-ndjson")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"1","name":"Alice"}` + "\n"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	tempDir := t.TempDir()
	jobID := "test-own-bundle-routing"
	jobDir := filepath.Join(tempDir, "jobs", jobID)
	inputDir := filepath.Join(jobDir, "import")
	require.NoError(t, os.MkdirAll(inputDir, 0755))

	groupID := "group-patient-ob"
	crtdlPath := filepath.Join(tempDir, "test.json")
	require.NoError(t, os.WriteFile(crtdlPath, []byte(wrapCRTDL(`{
		"attributeGroups": [{
			"id": "`+groupID+`",
			"name": "Patients",
			"groupReference": "https://example.com/Patient",
			"attributes": [
				{"attributeRef": "Patient.id", "mustHave": true},
				{"attributeRef": "Patient.name", "mustHave": false}
			]
		}]
	}`)), 0644))

	lookupPath := filepath.Join(tempDir, "lookup.json")
	require.NoError(t, os.WriteFile(lookupPath, []byte(`[{
		"url": "https://example.com/Patient",
		"resourceType": "Patient",
		"elements": {
			"Patient.id": {"viewDefinition": {"select": [{"column": [{"name": "id", "path": "id"}]}]}},
			"Patient.name": {"viewDefinition": {"select": [{"column": [{"name": "name", "path": "name[0].text"}]}]}}
		}
	}]`), 0644))

	patient := func(id, name string) map[string]any {
		return map[string]any{
			"resourceType": "Patient", "id": id,
			"meta": map[string]any{"profile": []any{"https://example.com/Patient"}},
			"name": []any{map[string]any{"text": name}},
		}
	}
	prov := makeProvenance("prov-a", "Patient/1", groupID)
	prov["target"] = append(prov["target"].([]any), map[string]any{"reference": "Patient/2"})

	var lines []byte
	for _, b := range []map[string]any{
		makeBundle("bundle-a", patient("1", "Alice"), prov),
		makeBundle("bundle-b", patient("2", "Bob")),
	} {
		data, err := json.Marshal(b)
		require.NoError(t, err)
		lines = append(lines, append(data, '\n')...)
	}
	require.NoError(t, os.WriteFile(filepath.Join(inputDir, "bundles.ndjson"), lines, 0644))

	job := &models.PipelineJob{
		JobID:       jobID,
		Status:      models.JobStatusInProgress,
		InputSource: crtdlPath,
		InputType:   models.InputTypeCRTDL,
		CRTDLPath:   crtdlPath,
		Config: models.ProjectConfig{
			Services: models.ServiceConfig{
				Flattening: models.FlatteningConfig{
					ServiceURL: server.URL,
					LookupPath: lookupPath,
					Formats:    []string{"csv"},
					Timeout:    30 * time.Second,
				},
			},
			Pipeline: models.PipelineConfig{
				EnabledSteps: []models.StepName{models.StepLocalImport, models.StepFlattening},
			},
			Retry: models.RetryConfig{MaxAttempts: 1},
		},
	}

	err := runPipelineStep(models.StepFlattening, job, jobDir, lib.NewLogger(lib.LogLevelDebug))
	require.NoError(t, err)
	require.NotEmpty(t, body, "flattener service should have been called")
	assert.Contains(t, string(body), `"id":"1"`)
	assert.NotContains(t, string(body), `"id":"2"`)
}
