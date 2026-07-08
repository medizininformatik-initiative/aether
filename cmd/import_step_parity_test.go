package cmd

import (
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

// TestValidateImportStepMatchesInput_TorchAcceptsLocalDefault verifies that a
// standard torch-via-CRTDL job (InputType=Local, no external source) is accepted
// by `aether job run --step torch`, not rejected as a mismatch.
func TestValidateImportStepMatchesInput_TorchAcceptsLocalDefault(t *testing.T) {
	job := &models.PipelineJob{InputType: models.InputTypeLocal}
	assert.NoError(t, validateImportStepMatchesInput(job, models.StepTorchImport))
}

// TestValidateImportStepMatchesInput_RejectsUnacceptedType covers the manual
// --step guard for a step that cannot handle the job's input type.
func TestValidateImportStepMatchesInput_RejectsUnacceptedType(t *testing.T) {
	job := &models.PipelineJob{InputType: models.InputTypeLocal}
	err := validateImportStepMatchesInput(job, models.StepHttpImport)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not accept input type")
}

// TestExecuteStepManually_RejectsMismatchedImportStep proves the manual
// `job run --step http_import` path rejects an input type the step cannot handle
// before any service call, returning the shared parity error.
func TestExecuteStepManually_RejectsMismatchedImportStep(t *testing.T) {
	jobsDir := t.TempDir()
	steps := []models.StepName{models.StepHttpImport, models.StepDIMP}
	config := &models.ProjectConfig{
		JobsDir:  jobsDir,
		Pipeline: models.PipelineConfig{EnabledSteps: steps},
		Retry:    models.RetryConfig{MaxAttempts: 3, InitialBackoffMs: 10, MaxBackoffMs: 100},
	}
	job := &models.PipelineJob{
		JobID:       "550e8400-e29b-41d4-a716-446655440000",
		Status:      models.JobStatusInProgress,
		InputSource: "/local/path",
		InputType:   models.InputTypeLocal,
		CurrentStep: string(models.StepHttpImport),
		Steps:       models.InitializeSteps(steps),
		Config:      *config,
	}

	err := executeStepManually(job, models.StepHttpImport, config, lib.NewLogger(lib.LogLevelError), false)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not accept input type")
}

// TestExecuteStepManually_TorchStepRunsOnLocalDefaultJob drives the manual path:
// `aether job run --step torch` on a standard torch job (InputType=Local, CRTDL
// attached, no external source) clears the parity guard and runs the TORCH
// import through to completion.
func TestExecuteStepManually_TorchStepRunsOnLocalDefaultJob(t *testing.T) {
	jobsDir := t.TempDir()
	jobID := "550e8400-e29b-41d4-a716-446655440000"
	require.NoError(t, os.MkdirAll(filepath.Join(jobsDir, jobID), 0o755))

	crtdlPath := filepath.Join(jobsDir, jobID, "crtdl.json")
	require.NoError(t, os.WriteFile(crtdlPath, []byte(`{"resourceType":"Parameters"}`), 0o644))

	fake := &servicestest.MockExtractor{
		SubmitFunc: func(string) (string, error) { return "http://torch/fhir/__status/job-1", nil },
		PollFunc:   func(string, bool) ([]string, error) { return []string{"http://torch/out-1.ndjson"}, nil },
		DownloadFunc: func([]string, string, bool, bool, string) ([]models.FHIRDataFile, error) {
			return []models.FHIRDataFile{{FileName: "out-1.ndjson", FileSize: 42}}, nil
		},
	}
	pipeline.SetExtractorFactoryForTesting(func(models.TORCHConfig, *services.HTTPClient, *lib.Logger) services.Extractor {
		return fake
	})
	defer pipeline.ResetExtractorFactory()

	steps := []models.StepName{models.StepTorchImport}
	config := &models.ProjectConfig{
		JobsDir:  jobsDir,
		Pipeline: models.PipelineConfig{EnabledSteps: steps},
		Retry:    models.RetryConfig{MaxAttempts: 3, InitialBackoffMs: 10, MaxBackoffMs: 100},
	}
	job := &models.PipelineJob{
		JobID:       jobID,
		Status:      models.JobStatusInProgress,
		InputType:   models.InputTypeLocal,
		CRTDLPath:   crtdlPath,
		CurrentStep: string(models.StepTorchImport),
		Steps:       models.InitializeSteps(steps),
		Config:      *config,
	}

	err := executeStepManually(job, models.StepTorchImport, config, lib.NewLogger(lib.LogLevelError), false)

	require.NoError(t, err)
	s, ok := models.GetStepByName(*job, models.StepTorchImport)
	require.True(t, ok)
	assert.Equal(t, models.StepStatusCompleted, s.Status)
}
