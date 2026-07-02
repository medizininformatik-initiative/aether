package unit

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/pipeline"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

// These tests exercise the interface seams: each pipeline step reaches its
// service client through a factory that can be swapped for a fake adapter, so
// the step is testable without a live service.

func TestDIMPStep_UsesFakePseudonymizer(t *testing.T) {
	mock := &services.MockPseudonymizer{
		PseudonymizeFunc: func(r map[string]any) (map[string]any, error) {
			r["id"] = "fake-" + fmt.Sprint(r["id"])
			return r, nil
		},
	}
	pipeline.SetPseudonymizerFactoryForTesting(func(_ string, _ *services.HTTPClient, _ *lib.Logger) services.Pseudonymizer {
		return mock
	})
	defer pipeline.ResetPseudonymizerFactory()

	tmpDir := t.TempDir()
	job := createDIMPTestJob("http://unused-by-fake")
	importDir := filepath.Join(tmpDir, "import")
	require.NoError(t, os.MkdirAll(importDir, 0755))
	writeDIMPNDJSON(t, filepath.Join(importDir, "patients.ndjson"), []map[string]any{
		{"resourceType": "Patient", "id": "p1"},
	})

	require.NoError(t, pipeline.ExecuteDIMPStep(job, tmpDir, createDIMPTestLogger()))

	require.NotEmpty(t, mock.Calls, "step must call the injected fake, not a real client")
	out := readDIMPNDJSON(t, filepath.Join(tmpDir, "dimp", "dimped_patients.ndjson"))
	require.Len(t, out, 1)
	assert.Equal(t, "fake-p1", out[0]["id"])
}

func TestValidationStep_UsesFakeValidator(t *testing.T) {
	mock := &services.MockResourceValidator{
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

	require.NoError(t, pipeline.ExecuteValidationStep(job, tmpDir, createValidationTestLogger()))
	assert.NotEmpty(t, mock.Calls, "step must call the injected fake validator")
}

func TestMockFlattener_RecordsCallsAndReturnsCannedCSV(t *testing.T) {
	var _ services.Flattener = (*services.MockFlattener)(nil)

	mock := &services.MockFlattener{
		FlattenFunc: func(models.ViewDefinition, []map[string]any) (string, error) {
			return "id\n1\n", nil
		},
	}
	csv, err := mock.Flatten(models.ViewDefinition{Name: "V"}, []map[string]any{{"resourceType": "Patient"}})
	require.NoError(t, err)
	assert.Equal(t, "id\n1\n", csv)
	assert.Equal(t, 1, mock.Calls)
}

func TestMockExtractor_ReturnsCannedExtractionResults(t *testing.T) {
	var _ services.Extractor = (*services.MockExtractor)(nil)

	mock := &services.MockExtractor{
		SubmitFunc: func(string) (string, error) { return "http://torch/status/1", nil },
		PollFunc:   func(string, bool) ([]string, error) { return []string{"http://torch/out.ndjson"}, nil },
		DownloadFunc: func([]string, string, bool, bool, string) ([]models.FHIRDataFile, error) {
			return []models.FHIRDataFile{{FileName: "out.ndjson"}}, nil
		},
	}

	url, err := mock.SubmitExtraction("crtdl.json")
	require.NoError(t, err)
	assert.Equal(t, "http://torch/status/1", url)

	urls, err := mock.PollExtractionStatus(url, false)
	require.NoError(t, err)
	assert.Equal(t, []string{"http://torch/out.ndjson"}, urls)

	files, err := mock.DownloadExtractionFiles(urls, t.TempDir(), false, false, "")
	require.NoError(t, err)
	require.Len(t, files, 1)
	assert.Equal(t, "out.ndjson", files[0].FileName)
}
