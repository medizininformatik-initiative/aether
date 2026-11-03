package unit

import (
	"net/http"
	"net/http/httptest"
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

func newImportJob(t *testing.T, step models.StepName, inputType models.InputType, source string) *models.PipelineJob {
	t.Helper()
	return &models.PipelineJob{
		JobID:       "22222222-2222-2222-2222-222222222222",
		InputSource: source,
		InputType:   inputType,
		CurrentStep: string(step),
		Status:      models.JobStatusPending,
		Steps:       models.InitializeSteps([]models.StepName{step}),
		Config: models.ProjectConfig{
			JobsDir:  t.TempDir(),
			Pipeline: models.PipelineConfig{EnabledSteps: []models.StepName{step}},
			Retry:    models.RetryConfig{MaxAttempts: 1, InitialBackoffMs: 1, MaxBackoffMs: 1},
		},
	}
}

func TestImportStep_LocalImport_SetsJobTotals(t *testing.T) {
	logger := quietLogger()
	srcDir := t.TempDir()
	content := []byte("{\"resourceType\":\"Patient\",\"id\":\"1\"}\n")
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "a.ndjson"), content, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "b.ndjson"), content, 0o644))

	job := newImportJob(t, models.StepLocalImport, models.InputTypeLocal, srcDir)

	_, err := runImportStep(job, logger, nil, false)

	require.NoError(t, err)
	assert.Equal(t, 2, job.TotalFiles)
	assert.Equal(t, int64(2*len(content)), job.TotalBytes)
}

func TestImportStep_LocalImport_ConfigDirTakesPrecedence(t *testing.T) {
	logger := quietLogger()
	cfgDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(cfgDir, "a.ndjson"), []byte("{}\n"), 0o644))

	job := newImportJob(t, models.StepLocalImport, models.InputTypeLocal, "/nonexistent/input")
	job.Config.Services.LocalImport.Dir = cfgDir

	_, err := runImportStep(job, logger, nil, false)

	require.NoError(t, err)
	assert.Equal(t, 1, job.TotalFiles)
}

func TestImportStep_HttpImport_SetsTotalsWithAndWithoutProgress(t *testing.T) {
	body := "{\"resourceType\":\"Patient\",\"id\":\"1\"}\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	for name, showProgress := range map[string]bool{"plain": false, "progress": true} {
		t.Run(name, func(t *testing.T) {
			logger := quietLogger()
			job := newImportJob(t, models.StepHttpImport, models.InputTypeHTTP, server.URL+"/data.ndjson")
			httpClient := services.NewHTTPClient(0, job.Config.Retry, models.TLSConfig{}, logger)

			_, err := runImportStep(job, logger, httpClient, showProgress)

			require.NoError(t, err)
			assert.Equal(t, 1, job.TotalFiles)
			assert.Equal(t, int64(len(body)), job.TotalBytes)
		})
	}
}

func TestImportStep_TorchResultURL_DownloadsFromPolledURL(t *testing.T) {
	logger := quietLogger()
	var polledURL string
	fake := &servicestest.MockExtractor{
		PollFunc: func(url string, _ bool) ([]string, error) {
			polledURL = url
			return []string{"http://torch/out.ndjson"}, nil
		},
		DownloadFunc: func([]string, string, bool, bool, string) ([]models.FHIRDataFile, error) {
			return []models.FHIRDataFile{{FileName: "out.ndjson", FileSize: 7}, {FileName: "o2.ndjson", FileSize: 5}}, nil
		},
	}
	pipeline.SetExtractorFactoryForTesting(func(models.TORCHConfig, *services.HTTPClient, *lib.Logger) services.Extractor {
		return fake
	})
	defer pipeline.ResetExtractorFactory()

	job := newImportJob(t, models.StepTorchImport, models.InputTypeTORCHURL, "http://torch/fhir/__status/job-1")

	_, err := runImportStep(job, logger, nil, false)

	require.NoError(t, err)
	assert.Equal(t, "http://torch/fhir/__status/job-1", polledURL)
	assert.Equal(t, 2, job.TotalFiles)
	assert.Equal(t, int64(12), job.TotalBytes)
}
