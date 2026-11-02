package unit

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

func newCSVImportTestJob(t *testing.T, serverURL, sourceInput string) *models.PipelineJob {
	t.Helper()
	steps := []models.StepName{models.StepCSVImport, models.StepSend}
	return &models.PipelineJob{
		JobID:       "csv-job",
		InputSource: sourceInput,
		InputType:   models.InputTypeLocal,
		CurrentStep: string(models.StepCSVImport),
		Status:      models.JobStatusPending,
		Steps:       models.InitializeSteps(steps),
		Config: models.ProjectConfig{
			JobsDir:  t.TempDir(),
			Pipeline: models.PipelineConfig{EnabledSteps: steps},
			Services: models.ServiceConfig{
				Send: models.SendConfig{
					URL:    serverURL,
					SendAs: models.SendModeTransferLoad,
					Transfer: models.TransferConfig{
						ProjectIdentifier:      "TEST-PROJECT",
						OrganizationIdentifier: "test.hospital.de",
					},
				},
			},
			Retry: FastRetryConfig(),
		},
	}
}

func runCSVImport(job *models.PipelineJob) (*models.PipelineJob, error) {
	logger := lib.NewLogger(lib.LogLevelInfo)
	httpClient := services.NewHTTPClient(5*time.Second, job.Config.Retry, models.TLSConfig{}, logger)
	return runImportStep(job, logger, httpClient, false)
}

func TestCSVImportStep_CopiesFilesIntoCSVDirAndSetsTotals(t *testing.T) {
	source := t.TempDir()
	writeTestFile(t, filepath.Join(source, "Patient.csv"), "id\n1\n")
	writeTestFile(t, filepath.Join(source, "sub", "Observation.csv"), "id\n2\n")
	writeTestFile(t, filepath.Join(source, "notes.txt"), "ignored")

	job := newCSVImportTestJob(t, "", source)

	job, err := runCSVImport(job)
	require.NoError(t, err)

	csvDir := filepath.Join(job.Config.JobsDir, job.JobID, "csv")
	assert.FileExists(t, filepath.Join(csvDir, "Patient.csv"))
	assert.FileExists(t, filepath.Join(csvDir, "sub", "Observation.csv"))
	assert.NoFileExists(t, filepath.Join(csvDir, "notes.txt"))
	assert.Equal(t, 2, job.TotalFiles)
	assert.Equal(t, int64(len("id\n1\n")+len("id\n2\n")), job.TotalBytes)

	step, found := models.GetStepByName(*job, models.StepCSVImport)
	require.True(t, found)
	assert.Equal(t, models.StepStatusCompleted, step.Status)
	assert.Equal(t, 2, step.FilesProcessed)
}

func TestCSVImportStep_ConfigDirTakesPrecedenceOverInputSource(t *testing.T) {
	configDir := t.TempDir()
	otherDir := t.TempDir()
	writeTestFile(t, filepath.Join(configDir, "FromConfig.csv"), "a\n")
	writeTestFile(t, filepath.Join(otherDir, "FromInput.csv"), "b\n")

	job := newCSVImportTestJob(t, "", otherDir)
	job.Config.Services.CSVImport.Dir = configDir

	job, err := runCSVImport(job)
	require.NoError(t, err)

	csvDir := filepath.Join(job.Config.JobsDir, job.JobID, "csv")
	assert.FileExists(t, filepath.Join(csvDir, "FromConfig.csv"))
	assert.NoFileExists(t, filepath.Join(csvDir, "FromInput.csv"))
}

func TestCSVImportStep_FailsWithoutCSVFiles(t *testing.T) {
	source := t.TempDir()
	writeTestFile(t, filepath.Join(source, "Patient.ndjson"), "{}\n")

	job := newCSVImportTestJob(t, "", source)

	_, err := runCSVImport(job)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no CSV files found")
	assert.Contains(t, err.Error(), source)
}

func TestCSVImportStep_FailsWithoutSourceDirectory(t *testing.T) {
	job := newCSVImportTestJob(t, "", "")

	_, err := runCSVImport(job)

	require.Error(t, err)
}

func TestCSVImportStep_SendTransfersImportedFiles(t *testing.T) {
	var receivedBinaries []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var resource map[string]any
		_ = json.Unmarshal(body, &resource)
		if resource["resourceType"] == "Binary" {
			receivedBinaries = append(receivedBinaries, resource)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	source := t.TempDir()
	writeTestFile(t, filepath.Join(source, "Patient.csv"), "id,gender\n1,male\n")

	job := newCSVImportTestJob(t, server.URL, source)
	job, err := runCSVImport(job)
	require.NoError(t, err)

	jobDir := filepath.Join(job.Config.JobsDir, job.JobID)
	logger := lib.NewLogger(lib.LogLevelInfo)
	require.NoError(t, runPipelineStep(models.StepSend, job, jobDir, logger))

	require.Len(t, receivedBinaries, 1)
	data, err := base64.StdEncoding.DecodeString(receivedBinaries[0]["data"].(string))
	require.NoError(t, err)
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	require.NoError(t, err)
	require.Len(t, archive.File, 1)
	assert.Equal(t, "Patient.csv", filepath.Base(archive.File[0].Name))

	_, statErr := os.Stat(filepath.Join(jobDir, "csv", "Patient.csv"))
	assert.NoError(t, statErr)
}
