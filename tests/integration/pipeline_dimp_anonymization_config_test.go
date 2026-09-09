package integration

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/pipeline"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

const testAnonymizationYAML = `fhirVersion: R4
fhirPathRules:
  - path: Patient.name
    method: redact
`

// mockDeIdentifyServer records the requests that the $de-identify operation
// gets, and the shape of each request body.
type mockDeIdentifyServer struct {
	mu                 sync.Mutex
	requestPaths       []string
	configPayload      [][]byte
	parametersRequests int
}

// createMockDeIdentifyServer starts a mock FHIR-Pseudonymizer. It accepts both
// body shapes of the stable operation: a bare resource, and a Parameters
// resource with a config part. It adds a "pseudo-" prefix to the resource ID
// and returns the resource.
func createMockDeIdentifyServer(t *testing.T) (*httptest.Server, *mockDeIdentifyServer) {
	recorder := &mockDeIdentifyServer{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorder.mu.Lock()
		recorder.requestPaths = append(recorder.requestPaths, r.URL.Path)
		recorder.mu.Unlock()

		if r.URL.Path != "/fhir/$de-identify" {
			http.Error(w, "unexpected path", http.StatusNotFound)
			return
		}

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "failed to decode request", http.StatusBadRequest)
			return
		}

		resource := body
		if body["resourceType"] == "Parameters" {
			recorder.mu.Lock()
			recorder.parametersRequests++
			recorder.mu.Unlock()

			parts, ok := body["parameter"].([]any)
			if !ok {
				http.Error(w, "parameter must be a list", http.StatusBadRequest)
				return
			}

			resource = nil
			for _, p := range parts {
				part, ok := p.(map[string]any)
				if !ok {
					continue
				}
				switch part["name"] {
				case "config":
					attachment, ok := part["valueAttachment"].(map[string]any)
					if !ok {
						http.Error(w, "config part must have a valueAttachment", http.StatusBadRequest)
						return
					}
					data, err := base64.StdEncoding.DecodeString(attachment["data"].(string))
					if err != nil {
						http.Error(w, "config attachment is not base64", http.StatusBadRequest)
						return
					}
					recorder.mu.Lock()
					recorder.configPayload = append(recorder.configPayload, data)
					recorder.mu.Unlock()
				case "resource":
					resource, _ = part["resource"].(map[string]any)
				}
			}

			if resource == nil {
				http.Error(w, "Parameters must have a resource part", http.StatusBadRequest)
				return
			}
		}

		pseudonymizeResource(resource)

		w.Header().Set("Content-Type", "application/fhir+json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resource)
	}))

	t.Cleanup(server.Close)

	return server, recorder
}

func (m *mockDeIdentifyServer) paths() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.requestPaths...)
}

func (m *mockDeIdentifyServer) configs() [][]byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([][]byte(nil), m.configPayload...)
}

// parametersBodies gives the number of requests that sent a Parameters
// resource. Every other request sent the resource alone.
func (m *mockDeIdentifyServer) parametersBodies() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.parametersRequests
}

// readMaybeCompressedNDJSON reads an NDJSON file. It decompresses the file if
// it has the .zst extension.
func readMaybeCompressedNDJSON(t *testing.T, path string) []map[string]any {
	reader, err := lib.OpenFileForReading(path)
	require.NoError(t, err)
	defer func() {
		_ = reader.Close()
	}()

	var resources []map[string]any
	decoder := json.NewDecoder(reader)
	for decoder.More() {
		var resource map[string]any
		require.NoError(t, decoder.Decode(&resource))
		resources = append(resources, resource)
	}
	return resources
}

// TestPipelineAnonymizationConfig_EndToEnd runs a full local_import + dimp
// pipeline from a configuration file that gives an anonymization config path.
// It shows that aether sends the anonymization YAML to the stable $de-identify
// operation and writes the pseudonymized output.
func TestPipelineAnonymizationConfig_EndToEnd(t *testing.T) {
	server, recorder := createMockDeIdentifyServer(t)

	tmpDir := t.TempDir()
	jobsDir := filepath.Join(tmpDir, "jobs")
	importDir := filepath.Join(tmpDir, "import_data")
	require.NoError(t, os.MkdirAll(jobsDir, 0755))
	require.NoError(t, os.MkdirAll(importDir, 0755))

	anonymizationPath := filepath.Join(tmpDir, "anonymization.yaml")
	require.NoError(t, os.WriteFile(anonymizationPath, []byte(testAnonymizationYAML), 0644))

	writeNDJSONToFile(t, filepath.Join(importDir, "patients.ndjson"), []map[string]any{
		{"resourceType": "Patient", "id": "patient1", "name": []map[string]string{{"family": "Smith"}}},
		{"resourceType": "Patient", "id": "patient2", "name": []map[string]string{{"family": "Jones"}}},
	})

	configPath := filepath.Join(tmpDir, "aether.yaml")
	configContent := `
services:
  dimp:
    url: "` + server.URL + `"
    anonymization_config: "` + anonymizationPath + `"

pipeline:
  enabled_steps:
    - local_import
    - dimp

retry:
  max_attempts: 3
  initial_backoff_ms: 100
  max_backoff_ms: 1000

jobs_dir: "` + jobsDir + `"
`
	require.NoError(t, os.WriteFile(configPath, []byte(configContent), 0644))

	config, err := services.LoadConfig(configPath)
	require.NoError(t, err, "config with anonymization_config should load")
	require.Equal(t, anonymizationPath, config.Services.DIMP.AnonymizationConfig)

	logger := lib.NewLogger(lib.LogLevelError)

	job, err := pipeline.CreateJob(models.GenerateJobID(), importDir, "", *config, logger)
	require.NoError(t, err)

	startedJob := pipeline.StartJob(job)
	require.NoError(t, pipeline.UpdateJob(jobsDir, startedJob))

	httpClient := services.NewHTTPClient(30*time.Second, config.Retry, models.TLSConfig{}, logger)
	importedJob, err := runImportStep(startedJob, logger, httpClient, false)
	require.NoError(t, err)
	require.NoError(t, pipeline.UpdateJob(jobsDir, importedJob))

	advancedJob, err := pipeline.AdvanceToNextStep(importedJob)
	require.NoError(t, err)
	require.Equal(t, string(models.StepDIMP), advancedJob.CurrentStep)

	jobDir := services.GetJobDir(jobsDir, job.JobID)
	require.NoError(t, runPipelineStep(models.StepDIMP, advancedJob, jobDir, logger))
	require.NoError(t, pipeline.UpdateJob(jobsDir, advancedJob))

	dimpStep, found := models.GetStepByName(*advancedJob, models.StepDIMP)
	require.True(t, found, "job should have a DIMP step")
	assert.Equal(t, models.StepStatusCompleted, dimpStep.Status)

	entries, err := os.ReadDir(filepath.Join(jobDir, "dimp"))
	require.NoError(t, err)
	require.Len(t, entries, 1, "DIMP step should write one output file")

	resources := readMaybeCompressedNDJSON(t, filepath.Join(jobDir, "dimp", entries[0].Name()))
	require.Len(t, resources, 2, "output should have both patients")
	assert.Equal(t, "pseudo-patient1", resources[0]["id"])
	assert.Equal(t, "pseudo-patient2", resources[1]["id"])

	paths := recorder.paths()
	require.Len(t, paths, 2, "each resource goes to the DIMP service once")
	for _, path := range paths {
		assert.Equal(t, "/fhir/$de-identify", path, "requests must go to the stable operation")
	}

	assert.Equal(t, 2, recorder.parametersBodies(), "each request must send a Parameters resource")

	configs := recorder.configs()
	require.Len(t, configs, 2, "each request must include the anonymization config")
	for _, payload := range configs {
		assert.Equal(t, testAnonymizationYAML, string(payload),
			"the service must get the content of the anonymization YAML file")
	}
}

// TestPipelineAnonymizationConfig_WithoutPathSendsBareResource shows that a
// configuration without an anonymization_config sends the resource alone. The
// service then uses its own anonymization file.
func TestPipelineAnonymizationConfig_WithoutPathSendsBareResource(t *testing.T) {
	server, recorder := createMockDeIdentifyServer(t)

	tmpDir := t.TempDir()
	jobsDir := filepath.Join(tmpDir, "jobs")
	importDir := filepath.Join(tmpDir, "import_data")
	require.NoError(t, os.MkdirAll(jobsDir, 0755))
	require.NoError(t, os.MkdirAll(importDir, 0755))

	writeNDJSONToFile(t, filepath.Join(importDir, "patients.ndjson"), []map[string]any{
		{"resourceType": "Patient", "id": "patient1"},
	})

	configPath := filepath.Join(tmpDir, "aether.yaml")
	configContent := `
services:
  dimp:
    url: "` + server.URL + `"

pipeline:
  enabled_steps:
    - local_import
    - dimp

retry:
  max_attempts: 3
  initial_backoff_ms: 100
  max_backoff_ms: 1000

jobs_dir: "` + jobsDir + `"
`
	require.NoError(t, os.WriteFile(configPath, []byte(configContent), 0644))

	config, err := services.LoadConfig(configPath)
	require.NoError(t, err)
	require.Empty(t, config.Services.DIMP.AnonymizationConfig)

	logger := lib.NewLogger(lib.LogLevelError)

	job, err := pipeline.CreateJob(models.GenerateJobID(), importDir, "", *config, logger)
	require.NoError(t, err)

	startedJob := pipeline.StartJob(job)
	require.NoError(t, pipeline.UpdateJob(jobsDir, startedJob))

	httpClient := services.NewHTTPClient(30*time.Second, config.Retry, models.TLSConfig{}, logger)
	importedJob, err := runImportStep(startedJob, logger, httpClient, false)
	require.NoError(t, err)
	require.NoError(t, pipeline.UpdateJob(jobsDir, importedJob))

	advancedJob, err := pipeline.AdvanceToNextStep(importedJob)
	require.NoError(t, err)

	jobDir := services.GetJobDir(jobsDir, job.JobID)
	require.NoError(t, runPipelineStep(models.StepDIMP, advancedJob, jobDir, logger))

	entries, err := os.ReadDir(filepath.Join(jobDir, "dimp"))
	require.NoError(t, err)
	require.Len(t, entries, 1)

	resources := readMaybeCompressedNDJSON(t, filepath.Join(jobDir, "dimp", entries[0].Name()))
	require.Len(t, resources, 1)
	assert.Equal(t, "pseudo-patient1", resources[0]["id"])

	require.Len(t, recorder.paths(), 1)
	assert.Zero(t, recorder.parametersBodies(), "the request must send the resource alone")
	assert.Empty(t, recorder.configs(), "no anonymization config must go to the service")
}

// TestPipelineAnonymizationConfig_UnreadableFileFailsTheStep shows that the
// DIMP step fails early if it cannot read the anonymization YAML.
func TestPipelineAnonymizationConfig_UnreadableFileFailsTheStep(t *testing.T) {
	server, recorder := createMockDeIdentifyServer(t)

	tmpDir := t.TempDir()
	jobDir := filepath.Join(tmpDir, "jobs", "job-missing-anonymization")
	importDir := filepath.Join(jobDir, "import")
	require.NoError(t, os.MkdirAll(importDir, 0755))

	writeNDJSONToFile(t, filepath.Join(importDir, "patients.ndjson"), []map[string]any{
		{"resourceType": "Patient", "id": "patient1"},
	})

	job := &models.PipelineJob{
		JobID: "job-missing-anonymization",
		Config: models.ProjectConfig{
			Services: models.ServiceConfig{
				DIMP: models.DIMPConfig{
					URL:                    server.URL,
					BundleSplitThresholdMB: 10,
					AnonymizationConfig:    filepath.Join(tmpDir, "does-not-exist.yaml"),
				},
			},
			Pipeline: models.PipelineConfig{
				EnabledSteps: []models.StepName{models.StepLocalImport, models.StepDIMP},
			},
			Retry: models.RetryConfig{
				MaxAttempts:      3,
				InitialBackoffMs: 100,
				MaxBackoffMs:     1000,
			},
		},
		Steps: make([]models.PipelineStep, 0),
	}

	err := runPipelineStep(models.StepDIMP, job, jobDir, lib.NewLogger(lib.LogLevelError))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "anonymization config")
	assert.Empty(t, recorder.paths(), "the step must fail before it sends a request")
}
