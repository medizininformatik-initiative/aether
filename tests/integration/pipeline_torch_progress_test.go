package integration

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/mocktorch"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/pipeline"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

func writeMinimalCRTDL(t *testing.T, dir string) string {
	t.Helper()
	crtdlPath := filepath.Join(dir, "test.json")
	crtdlJSON, _ := json.Marshal(map[string]any{
		"cohortDefinition": map[string]any{"version": "1.0.0", "inclusionCriteria": []any{}},
		"dataExtraction":   map[string]any{"attributeGroups": []any{}},
	})
	require.NoError(t, os.WriteFile(crtdlPath, crtdlJSON, 0644))
	return crtdlPath
}

// E2E: aether runs the torch step against a mocked TORCH that reports batch
// progress via the Task API. The step must persist increasing progress into
// the job file and complete normally.
func TestPipeline_TORCHExtraction_WithBatchProgress(t *testing.T) {
	tempDir := t.TempDir()
	jobsDir := filepath.Join(tempDir, "jobs")
	require.NoError(t, os.MkdirAll(jobsDir, 0755))
	crtdlPath := writeMinimalCRTDL(t, tempDir)

	mock := mocktorch.New(mocktorch.Config{
		CohortSize:    120,
		BatchSize:     50,
		PollsPerBatch: 2,
	})
	server := httptest.NewServer(mock.Handler())
	defer server.Close()

	config := models.ProjectConfig{
		Services: models.ServiceConfig{
			TORCH: models.TORCHConfig{
				BaseURL:            server.URL,
				ExtractionTimeout:  1 * time.Minute,
				PollingInterval:    20 * time.Millisecond,
				MaxPollingInterval: 20 * time.Millisecond,
			},
		},
		Pipeline: models.PipelineConfig{EnabledSteps: []models.StepName{models.StepTorchImport}},
		Retry:    models.RetryConfig{MaxAttempts: 3, InitialBackoffMs: 100, MaxBackoffMs: 1000},
		JobsDir:  jobsDir,
	}

	logger := lib.NewLogger(lib.LogLevelError)
	job, err := pipeline.CreateJob(models.GenerateJobID(), "", crtdlPath, config, logger)
	require.NoError(t, err)

	httpClient := services.NewHTTPClient(2*time.Second, config.Retry, models.TLSConfig{}, logger)
	updatedJob, err := runImportStep(job, logger, httpClient, false)

	require.NoError(t, err)

	step, found := models.GetStepByName(*updatedJob, models.StepTorchImport)
	require.True(t, found)
	assert.Equal(t, models.StepStatusCompleted, step.Status)
	assert.Greater(t, updatedJob.TotalFiles, 0)

	// The mock advances progress on each status poll, so the persisted
	// progress must exist and report the batch layout of the mock.
	require.NotNil(t, step.Progress, "torch step must persist progress from the Task API")
	assert.Equal(t, 3, step.Progress.Total)
	assert.NotEmpty(t, step.Progress.Message)

	// The job file on disk carries the progress too.
	reloaded, err := pipeline.LoadJob(jobsDir, job.JobID)
	require.NoError(t, err)
	diskStep, found := models.GetStepByName(*reloaded, models.StepTorchImport)
	require.True(t, found)
	require.NotNil(t, diskStep.Progress)
}

// E2E fallback: a TORCH without the Task API route must behave as before —
// no progress, no error.
func TestPipeline_TORCHExtraction_WithoutTaskAPI_NoProgress(t *testing.T) {
	tempDir := t.TempDir()
	jobsDir := filepath.Join(tempDir, "jobs")
	require.NoError(t, os.MkdirAll(jobsDir, 0755))
	crtdlPath := writeMinimalCRTDL(t, tempDir)

	mock := mocktorch.New(mocktorch.Config{
		CohortSize:     120,
		BatchSize:      50,
		PollsPerBatch:  2,
		DisableTaskAPI: true,
	})
	server := httptest.NewServer(mock.Handler())
	defer server.Close()

	config := models.ProjectConfig{
		Services: models.ServiceConfig{
			TORCH: models.TORCHConfig{
				BaseURL:            server.URL,
				ExtractionTimeout:  1 * time.Minute,
				PollingInterval:    20 * time.Millisecond,
				MaxPollingInterval: 20 * time.Millisecond,
			},
		},
		Pipeline: models.PipelineConfig{EnabledSteps: []models.StepName{models.StepTorchImport}},
		Retry:    models.RetryConfig{MaxAttempts: 3, InitialBackoffMs: 100, MaxBackoffMs: 1000},
		JobsDir:  jobsDir,
	}

	logger := lib.NewLogger(lib.LogLevelError)
	job, err := pipeline.CreateJob(models.GenerateJobID(), "", crtdlPath, config, logger)
	require.NoError(t, err)

	httpClient := services.NewHTTPClient(2*time.Second, config.Retry, models.TLSConfig{}, logger)
	updatedJob, err := runImportStep(job, logger, httpClient, false)

	require.NoError(t, err)

	step, found := models.GetStepByName(*updatedJob, models.StepTorchImport)
	require.True(t, found)
	assert.Equal(t, models.StepStatusCompleted, step.Status)
	assert.Nil(t, step.Progress, "no Task API -> no progress, but no failure either")
}
