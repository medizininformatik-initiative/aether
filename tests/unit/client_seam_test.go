package unit

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/pipeline"
	"github.com/medizininformatik-initiative/aether/internal/services"
	"github.com/medizininformatik-initiative/aether/internal/services/servicestest"
)

// These tests exercise the interface seams: each pipeline step reaches its
// service client through a factory that can be swapped for a fake adapter, so
// the step is testable without a live service.

func TestDIMPStep_UsesFakeDIMPProcessor(t *testing.T) {
	mock := &servicestest.MockDIMPProcessor{
		PseudonymizeFunc: func(r map[string]any) (map[string]any, error) {
			r["id"] = "fake-" + fmt.Sprint(r["id"])
			return r, nil
		},
	}
	pipeline.SetDIMPFactoryForTesting(func(_ models.DIMPConfig, _ *services.HTTPClient, _ *lib.Logger) services.DIMPProcessor {
		return mock
	})
	defer pipeline.ResetDIMPFactory()

	tmpDir := t.TempDir()
	job := createDIMPTestJob("http://unused-by-fake")
	importDir := filepath.Join(tmpDir, "import")
	require.NoError(t, os.MkdirAll(importDir, 0755))
	writeDIMPNDJSON(t, filepath.Join(importDir, "patients.ndjson"), []map[string]any{
		{"resourceType": "Patient", "id": "p1"},
	})

	require.NoError(t, runPipelineStep(models.StepDIMP, job, tmpDir, createDIMPTestLogger()))

	require.NotEmpty(t, mock.Calls, "step must call the injected fake, not a real client")
	out := readDIMPNDJSON(t, filepath.Join(tmpDir, "dimp", "dimped_patients.ndjson"))
	require.Len(t, out, 1)
	assert.Equal(t, "fake-p1", out[0]["id"])
}

func TestValidationStep_UsesFakeValidator(t *testing.T) {
	mock := &servicestest.MockResourceValidator{
		ValidateFunc: func(map[string]any) (*services.ValidationResult, error) {
			return &services.ValidationResult{OperationOutcome: map[string]any{
				"resourceType": "OperationOutcome",
				"issue":        []map[string]any{{"severity": "information", "code": "informational"}},
			}}, nil
		},
	}
	pipeline.SetResourceValidatorFactoryForTesting(func(_ string, _ *services.HTTPClient, _ *lib.Logger) services.ResourceValidator {
		return mock
	})
	defer pipeline.ResetResourceValidatorFactory()

	tmpDir := t.TempDir()
	job := createValidationTestJob("http://unused-by-fake")
	importDir := filepath.Join(tmpDir, "import")
	require.NoError(t, os.MkdirAll(importDir, 0755))
	writeValidationNDJSON(t, filepath.Join(importDir, "Patient.ndjson"), []map[string]any{
		{"resourceType": "Patient", "id": "p1"},
	})

	require.NoError(t, runPipelineStep(models.StepValidation, job, tmpDir, createValidationTestLogger()))
	assert.NotEmpty(t, mock.Calls, "step must call the injected fake validator")
}

// TestImportStep_TorchExtraction_UsesFakeExtractor drives the TORCH import path
// through a fake Extractor and asserts the step's orchestration wiring: the
// submit URL is parsed into the persisted job handle, that handle drives the
// poll, and the poll's file URLs drive the download whose files surface in the
// completed step. Nothing here talks to a real TORCH server.
func TestImportStep_TorchExtraction_UsesFakeExtractor(t *testing.T) {
	tmpDir := t.TempDir()
	logger := lib.NewLogger(lib.LogLevelInfo)

	// The CRTDL file only needs to exist to pass import-source validation;
	// the fake extractor never reads its contents.
	crtdlPath := filepath.Join(tmpDir, "crtdl.json")
	require.NoError(t, os.WriteFile(crtdlPath, []byte(`{"resourceType":"Parameters"}`), 0644))

	var (
		submittedCRTDL string
		polledURL      string
		downloadedURLs []string
		downloadDir    string
	)
	fake := &servicestest.MockExtractor{
		SubmitFunc: func(path string) (string, error) {
			submittedCRTDL = path
			return "http://torch/fhir/__status/job-777", nil
		},
		PollFunc: func(url string, _ bool) ([]string, error) {
			polledURL = url
			return []string{"http://torch/out-1.ndjson"}, nil
		},
		DownloadFunc: func(urls []string, destDir string, _, _ bool, _ string) ([]models.FHIRDataFile, error) {
			downloadedURLs = urls
			downloadDir = destDir
			return []models.FHIRDataFile{{FileName: "out-1.ndjson", FileSize: 42}}, nil
		},
	}
	pipeline.SetExtractorFactoryForTesting(func(models.TORCHConfig, *services.HTTPClient, *lib.Logger) services.Extractor {
		return fake
	})
	defer pipeline.ResetExtractorFactory()

	job := &models.PipelineJob{
		JobID:       "11111111-1111-1111-1111-111111111111",
		InputType:   models.InputTypeCRTDL,
		CRTDLPath:   crtdlPath,
		CurrentStep: string(models.StepTorchImport),
		Status:      models.JobStatusPending,
		Steps:       models.InitializeSteps([]models.StepName{models.StepTorchImport}),
		Config: models.ProjectConfig{
			JobsDir: tmpDir,
			Pipeline: models.PipelineConfig{
				EnabledSteps: []models.StepName{models.StepTorchImport},
			},
		},
	}

	updatedJob, err := runImportStep(job, logger, nil, false)
	require.NoError(t, err)
	require.NotNil(t, updatedJob)

	// Submit received the CRTDL path, and its status URL was parsed and persisted
	// as the resumable job handle.
	assert.Equal(t, crtdlPath, submittedCRTDL)
	assert.Equal(t, "job-777", job.TORCHJobID)
	assert.Equal(t, "http://torch/fhir/__status/job-777", job.TORCHExtractionURL)

	// The persisted URL drove the poll, and the poll's URLs drove the download
	// into the step's import directory.
	assert.Equal(t, "http://torch/fhir/__status/job-777", polledURL)
	assert.Equal(t, []string{"http://torch/out-1.ndjson"}, downloadedURLs)
	assert.NotEmpty(t, downloadDir)

	// The downloaded file surfaces in the completed import step.
	importStep, found := models.GetStepByName(*updatedJob, models.StepTorchImport)
	require.True(t, found)
	assert.Equal(t, models.StepStatusCompleted, importStep.Status)
	assert.Equal(t, 1, importStep.FilesProcessed)
}

// TestFlatteningStep_UsesFakeFlattener drives the flattening step through a fake
// Flattener and asserts the step's orchestration: the CRTDL group is compiled
// into a ViewDefinition, the provenance-linked resource is routed and batched to
// the flattener, and the flattener's rows are written to disk as CSV. The fake
// stands in for the HTTP flattener service.
func TestFlatteningStep_UsesFakeFlattener(t *testing.T) {
	var (
		gotViewDef models.ViewDefinition
		gotCount   int
	)
	fake := &servicestest.MockFlattener{
		FlattenFunc: func(vd models.ViewDefinition, resources []map[string]any) ([][]string, error) {
			gotViewDef = vd
			gotCount = len(resources)
			return [][]string{{"flattened-patient"}}, nil
		},
	}
	pipeline.SetFlattenerFactoryForTesting(
		func(models.FlatteningConfig, models.RetryConfig, *http.Transport, *lib.Logger) services.Flattener {
			return fake
		},
	)
	defer pipeline.ResetFlattenerFactory()

	tempDir := t.TempDir()
	jobDir := filepath.Join(tempDir, "jobs", "flatten-seam-job")
	inputDir := filepath.Join(jobDir, "import")
	require.NoError(t, os.MkdirAll(inputDir, 0755))

	profileURL := "https://example.com/Patient"
	groupID := "group-patient"
	crtdlPath := filepath.Join(tempDir, "crtdl.json")
	writeTestCRTDL(t, crtdlPath, groupID, "Patient", profileURL)
	lookupPath := filepath.Join(tempDir, "lookup.json")
	writeTestLookupTable(t, lookupPath, profileURL, "Patient")

	patient := map[string]any{
		"resourceType": "Patient",
		"id":           "1",
		"meta":         map[string]any{"profile": []any{profileURL}},
	}
	prov := makeProvenance("prov-1", "Patient/1", groupID)
	writeTestNDJSON(t, filepath.Join(inputDir, "patients.ndjson"), []map[string]any{makeBundle("b1", patient, prov)})

	// A dummy (well-formed but unused) service URL: config validation runs, but
	// the fake replaces the HTTP round-trip.
	job := createFlatteningTestJob("http://flattener.invalid", lookupPath, crtdlPath)

	require.NoError(t, runPipelineStep(models.StepFlattening, job, jobDir, createFlatteningTestLogger()))

	// The provenance-linked Patient was routed to the fake, carrying a
	// ViewDefinition compiled from the CRTDL group.
	assert.Equal(t, 1, fake.Calls)
	assert.Equal(t, 1, gotCount)
	assert.Equal(t, "Patient", gotViewDef.Resource, "flattener must receive a ViewDefinition compiled from the CRTDL group + lookup")

	// The fake's CSV output landed in the step's csv/ directory.
	csvFiles, err := filepath.Glob(filepath.Join(jobDir, "csv", "*.csv"))
	require.NoError(t, err)
	require.Len(t, csvFiles, 1)
	content, err := os.ReadFile(csvFiles[0])
	require.NoError(t, err)
	assert.Contains(t, string(content), "flattened-patient")
}

// The zero value of MockFlattener must be usable: a step under test that only
// needs the call count gets no rows and no error.
func TestMockFlattener_WithoutFlattenFuncReturnsNoRows(t *testing.T) {
	mock := &servicestest.MockFlattener{}

	rows, err := mock.Flatten(
		models.ViewDefinition{Name: "patients", Resource: "Patient"},
		[]map[string]any{{"resourceType": "Patient", "id": "1"}},
	)

	require.NoError(t, err)
	assert.Nil(t, rows)
	assert.Equal(t, 1, mock.Calls)
}
