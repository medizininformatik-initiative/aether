package unit

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/pipeline"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

// TestImportStepAcceptsInputType pins the step-centric parity mapping: the
// import method is chosen by step name, and each method only handles a subset of
// input types. InputTypeLocal is deliberately accepted by both torch (its
// CRTDL-submission default) and local_import.
func TestImportStepAcceptsInputType(t *testing.T) {
	cases := []struct {
		step   models.StepName
		input  models.InputType
		accept bool
	}{
		{models.StepTorchImport, models.InputTypeLocal, true},
		{models.StepTorchImport, models.InputTypeCRTDL, true},
		{models.StepTorchImport, models.InputTypeTORCHURL, true},
		{models.StepTorchImport, models.InputTypeHTTP, false},

		{models.StepLocalImport, models.InputTypeLocal, true},
		{models.StepLocalImport, models.InputTypeHTTP, false},
		{models.StepLocalImport, models.InputTypeCRTDL, false},
		{models.StepLocalImport, models.InputTypeTORCHURL, false},

		{models.StepHttpImport, models.InputTypeHTTP, true},
		{models.StepHttpImport, models.InputTypeLocal, false},
		{models.StepHttpImport, models.InputTypeCRTDL, false},

		{models.StepDIMP, models.InputTypeLocal, false},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.accept, models.ImportStepAcceptsInputType(tc.step, tc.input),
			"step %s accepts input type %s", tc.step, tc.input)
	}
}

// TestImportStep_RejectsInputTypeStepDoesNotAccept guards the resume/RunLoop
// path: an http_import step running against a Local job (the mismatch nothing
// else cross-checks) must fail up front with a clear parity error, not the
// confusing downstream "unsupported protocol" error from attempting to download
// a local path.
func TestImportStep_RejectsInputTypeStepDoesNotAccept(t *testing.T) {
	logger := lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 3, InitialBackoffMs: 500, MaxBackoffMs: 5000}, models.TLSConfig{}, logger)

	job := &models.PipelineJob{
		JobID:       "test-job-123",
		InputSource: "/local/path",
		InputType:   models.InputTypeLocal,
		CurrentStep: string(models.StepHttpImport),
		Status:      models.JobStatusPending,
		Steps:       models.InitializeSteps([]models.StepName{models.StepHttpImport}),
		Config: models.ProjectConfig{
			JobsDir:  t.TempDir(),
			Pipeline: models.PipelineConfig{EnabledSteps: []models.StepName{models.StepHttpImport}},
			Retry:    models.RetryConfig{MaxAttempts: 3, InitialBackoffMs: 500, MaxBackoffMs: 5000},
		},
	}

	_, err := runImportStep(job, logger, httpClient, false)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not accept input type")
}

// TestCreateJob_RejectsSourceStepCannotAccept guards creation: a provided source
// whose detected input type the enabled import step cannot handle (here a local
// directory with http_import enabled) is rejected up front, so a mismatched job
// is never created.
func TestCreateJob_RejectsSourceStepCannotAccept(t *testing.T) {
	jobsDir := t.TempDir()
	srcDir := t.TempDir() // a real directory → detected as InputTypeLocal

	config := models.ProjectConfig{
		JobsDir:  jobsDir,
		Pipeline: models.PipelineConfig{EnabledSteps: []models.StepName{models.StepHttpImport, models.StepDIMP}},
		Retry:    models.RetryConfig{MaxAttempts: 3, InitialBackoffMs: 10, MaxBackoffMs: 100},
	}

	_, err := pipeline.CreateJob(models.GenerateJobID(), srcDir, "", config, lib.NewLogger(lib.LogLevelError))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not accept")
}

// TestAcceptedInputTypes reports nil for a non-import step so callers can detect
// an unsupported import step name.
func TestAcceptedInputTypes(t *testing.T) {
	assert.Equal(t,
		[]models.InputType{models.InputTypeLocal, models.InputTypeCRTDL, models.InputTypeTORCHURL},
		models.AcceptedInputTypes(models.StepTorchImport))
	assert.Equal(t, []models.InputType{models.InputTypeLocal}, models.AcceptedInputTypes(models.StepLocalImport))
	assert.Equal(t, []models.InputType{models.InputTypeHTTP}, models.AcceptedInputTypes(models.StepHttpImport))
	assert.Nil(t, models.AcceptedInputTypes(models.StepDIMP))
}
