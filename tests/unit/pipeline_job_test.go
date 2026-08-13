package unit

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/pipeline"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

// countingStateFileOps wraps the default file ops and fails the Nth WriteFile call.
type countingStateFileOps struct {
	writeCount int
	failOnNth  int
	failErr    error
}

func (c *countingStateFileOps) WriteFile(name string, data []byte, perm os.FileMode) error {
	c.writeCount++
	if c.writeCount == c.failOnNth {
		return c.failErr
	}
	return os.WriteFile(name, data, perm)
}
func (c *countingStateFileOps) Rename(oldpath, newpath string) error {
	return os.Rename(oldpath, newpath)
}
func (c *countingStateFileOps) Remove(name string) error { return os.Remove(name) }

// Helper to create a test job with given steps
func createJobForPipelineTests(steps []models.StepName) models.PipelineJob {
	now := time.Now()
	return models.PipelineJob{
		JobID:       "test-job-123",
		CreatedAt:   now,
		UpdatedAt:   now,
		InputSource: "/test/input",
		InputType:   models.InputTypeLocal,
		CurrentStep: string(steps[0]),
		Status:      models.JobStatusInProgress,
		Steps:       models.InitializeSteps(steps),
		Config: models.ProjectConfig{
			Pipeline: models.PipelineConfig{
				EnabledSteps: steps,
			},
		},
	}
}

// TestFailJob tests marking a job as failed
func TestFailJob(t *testing.T) {
	job := createJobForPipelineTests([]models.StepName{models.StepLocalImport, models.StepDIMP})

	errorMsg := "test error message"
	updatedJob := pipeline.FailJob(&job, errorMsg)

	assert.NotNil(t, updatedJob)
	assert.Equal(t, errorMsg, updatedJob.ErrorMessage)
	assert.Equal(t, models.JobStatusFailed, updatedJob.Status)
}

// TestGetCurrentStep tests retrieving the current step
func TestGetCurrentStep(t *testing.T) {
	tests := []struct {
		name      string
		jobSteps  []models.StepName
		wantFound bool
		wantStep  models.StepName
	}{
		{
			name:      "Valid current step",
			jobSteps:  []models.StepName{models.StepLocalImport, models.StepDIMP},
			wantFound: true,
			wantStep:  models.StepLocalImport,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := createJobForPipelineTests(tt.jobSteps)

			step, found := pipeline.GetCurrentStep(&job)

			assert.Equal(t, tt.wantFound, found)
			if found {
				assert.Equal(t, tt.wantStep, step.Name)
			}
		})
	}
}

// TestGetCurrentStep_NoCurrentStep tests when job has no current step
func TestGetCurrentStep_NoCurrentStep(t *testing.T) {
	job := createJobForPipelineTests([]models.StepName{models.StepLocalImport})
	job.CurrentStep = "" // Clear current step

	step, found := pipeline.GetCurrentStep(&job)

	assert.False(t, found)
	assert.Equal(t, models.PipelineStep{}, step)
}

// TestUpdateJobProgress tests updating job metrics
func TestUpdateJobProgress(t *testing.T) {
	job := createJobForPipelineTests([]models.StepName{models.StepLocalImport})

	files := 5
	bytes := int64(1024)
	updatedJob := pipeline.UpdateJobProgress(&job, files, bytes)

	assert.NotNil(t, updatedJob)
	assert.Equal(t, files, updatedJob.TotalFiles)
	assert.Equal(t, bytes, updatedJob.TotalBytes)
}

// TestIsJobComplete tests job completion check
func TestIsJobComplete(t *testing.T) {
	tests := []struct {
		name         string
		setupJob     func() models.PipelineJob
		wantComplete bool
	}{
		{
			name: "Incomplete job",
			setupJob: func() models.PipelineJob {
				return createJobForPipelineTests([]models.StepName{models.StepLocalImport, models.StepDIMP})
			},
			wantComplete: false,
		},
		{
			name: "Completed job",
			setupJob: func() models.PipelineJob {
				job := createJobForPipelineTests([]models.StepName{models.StepLocalImport})
				// Complete the import step
				step, _ := models.GetStepByName(job, models.StepLocalImport)
				step = models.CompleteStep(step, 10, 1000)
				job = models.ReplaceStep(job, step)
				return job
			},
			wantComplete: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := tt.setupJob()
			complete := pipeline.IsJobComplete(&job)
			assert.Equal(t, tt.wantComplete, complete)
		})
	}
}

// TestGetJobSummary tests generating a job summary
func TestGetJobSummary(t *testing.T) {
	job := createJobForPipelineTests([]models.StepName{models.StepLocalImport, models.StepDIMP})
	job.TotalFiles = 42
	job.TotalBytes = 12345

	summary := pipeline.GetJobSummary(&job)

	assert.Contains(t, summary, job.JobID)
	assert.Contains(t, summary, string(job.Status))
	assert.Contains(t, summary, job.CurrentStep)
	assert.Contains(t, summary, "42") // TotalFiles
}

// TestGetJobSummary_WithError tests summary with error message
func TestGetJobSummary_WithError(t *testing.T) {
	job := createJobForPipelineTests([]models.StepName{models.StepLocalImport})
	errorMsg := "test error occurred"
	job = *pipeline.FailJob(&job, errorMsg)

	summary := pipeline.GetJobSummary(&job)

	assert.Contains(t, summary, errorMsg)
	assert.Contains(t, summary, string(models.JobStatusFailed))
}

// TestAdvanceToNextStep_PrerequisiteNotMet tests advancing when prerequisite is not met
func TestAdvanceToNextStep_PrerequisiteNotMet(t *testing.T) {
	// Create a job with import -> dimp, but import not completed
	job := createJobForPipelineTests([]models.StepName{models.StepLocalImport, models.StepDIMP})

	// Try to advance directly to DIMP without completing import
	job.CurrentStep = string(models.StepLocalImport)
	// Mark import step as started but not completed
	step, _ := models.GetStepByName(job, models.StepLocalImport)
	now := time.Now()
	step.Status = models.StepStatusInProgress
	step.StartedAt = &now
	job = models.ReplaceStep(job, step)

	// Now try to advance to the next step (DIMP)
	// First, manually mark import as not started to simulate the prerequisite check
	step.Status = models.StepStatusPending
	step.StartedAt = nil
	job = models.ReplaceStep(job, step)

	_, err := pipeline.AdvanceToNextStep(&job)

	// This should fail because DIMP requires import to be completed
	require.Error(t, err)
	assert.Contains(t, err.Error(), "prerequisite")
}

// TestAdvanceToNextStep_NoMoreSteps tests completing the last step
func TestAdvanceToNextStep_NoMoreSteps(t *testing.T) {
	// Create a job with only one step
	job := createJobForPipelineTests([]models.StepName{models.StepLocalImport})
	job.CurrentStep = string(models.StepLocalImport)

	// Complete the import step
	step, _ := models.GetStepByName(job, models.StepLocalImport)
	step = models.CompleteStep(step, 10, 1000)
	job = models.ReplaceStep(job, step)

	// Advance to next step (should complete the job)
	updatedJob, err := pipeline.AdvanceToNextStep(&job)

	require.NoError(t, err, "Should advance without error")
	assert.NotNil(t, updatedJob)
	assert.Equal(t, models.JobStatusCompleted, updatedJob.Status, "Job should be completed")
	assert.Equal(t, "", updatedJob.CurrentStep, "Current step should be empty")
}

// TestAdvanceToNextStep_MultipleWaitSteps guards against advancing toward a
// second, non-consecutive wait step. Steps are tracked by name, so the lookup
// resolves the first (already completed) wait step; advancing must still not
// hard-fail for this documented-supported configuration.
func TestAdvanceToNextStep_MultipleWaitSteps(t *testing.T) {
	steps := []models.StepName{models.StepLocalImport, models.StepWait, models.StepDIMP, models.StepWait}
	job := createJobForPipelineTests(steps)
	job.CurrentStep = string(models.StepDIMP)

	for _, name := range []models.StepName{models.StepLocalImport, models.StepDIMP} {
		s, _ := models.GetStepByName(job, name)
		job = models.ReplaceStep(job, models.CompleteStep(s, 1, 1))
	}
	firstWait, _ := models.GetStepByName(job, models.StepWait)
	job = models.ReplaceStep(job, models.CompleteStep(firstWait, 1, 1))

	_, err := pipeline.AdvanceToNextStep(&job)
	require.NoError(t, err)
}

// TestStartJob_EmptySteps tests StartJob when there are no steps
func TestStartJob_EmptySteps(t *testing.T) {
	job := models.PipelineJob{
		JobID:       "test-job-123",
		Status:      models.JobStatusPending,
		Steps:       []models.PipelineStep{}, // Empty steps
		CurrentStep: "",
	}

	updatedJob := pipeline.StartJob(&job)

	// Job should be updated to in_progress even with empty steps
	assert.Equal(t, models.JobStatusInProgress, updatedJob.Status)
	// No steps to start, so Steps should remain empty
	assert.Len(t, updatedJob.Steps, 0)
}

// TestCompleteJob tests marking a job as completed
func TestCompleteJob(t *testing.T) {
	job := createJobForPipelineTests([]models.StepName{models.StepLocalImport})
	job.CurrentStep = string(models.StepLocalImport)

	updatedJob := pipeline.CompleteJob(&job)

	assert.Equal(t, models.JobStatusCompleted, updatedJob.Status)
	assert.Equal(t, "", updatedJob.CurrentStep, "Current step should be cleared")
}

// TestCreateJob_EmptyInputSource tests CreateJob with empty input source (local_import from config)
// Covers job.go lines 28-32
func TestCreateJob_EmptyInputSource(t *testing.T) {
	tmpDir := t.TempDir()
	jobsDir := filepath.Join(tmpDir, "jobs")
	localImportDir := filepath.Join(tmpDir, "local_import")

	config := models.ProjectConfig{
		JobsDir: jobsDir,
		Pipeline: models.PipelineConfig{
			EnabledSteps: []models.StepName{models.StepLocalImport},
		},
		Services: models.ServiceConfig{
			LocalImport: models.LocalImportConfig{
				Dir: localImportDir,
			},
		},
	}

	logger := lib.NewLogger(lib.LogLevelDebug)

	// Create job with empty input source
	job, err := pipeline.CreateJob(models.GenerateJobID(), "", "", config, logger)

	require.NoError(t, err, "CreateJob should succeed with empty input source")
	assert.NotNil(t, job)
	assert.Equal(t, models.InputTypeLocal, job.InputType, "Input type should be local")
	assert.Equal(t, "", job.InputSource, "Input source should be empty")
	assert.Equal(t, string(models.StepLocalImport), job.CurrentStep, "Current step should be local_import")
}

// TestCreateJob_DoesNotAttachJobLog pins the contract that CreateJob keeps the
// logger side effect out: attaching per-job logging is the caller's job, so
// CreateJob must not create job.log on its own.
func TestCreateJob_DoesNotAttachJobLog(t *testing.T) {
	tmpDir := t.TempDir()
	jobsDir := filepath.Join(tmpDir, "jobs")

	config := models.ProjectConfig{
		JobsDir: jobsDir,
		Pipeline: models.PipelineConfig{
			EnabledSteps: []models.StepName{models.StepLocalImport},
		},
		Services: models.ServiceConfig{
			LocalImport: models.LocalImportConfig{Dir: filepath.Join(tmpDir, "local_import")},
		},
	}

	jobID := models.GenerateJobID()
	_, err := pipeline.CreateJob(jobID, "", "", config, lib.NewLogger(lib.LogLevelDebug))
	require.NoError(t, err)

	_, statErr := os.Stat(services.GetJobLogFilePath(jobsDir, jobID))
	assert.True(t, os.IsNotExist(statErr),
		"CreateJob must not create job.log; the caller owns log attachment")
}

// TestCreateJob_FailsWhenSaveStateAfterCRTDLPreparationFails covers job.go
// lines 107-109: the second SaveJobState call (after PrepareCRTDL has rewritten
// job.InputSource) errors. We force the failure by injecting a
// StateFileOpsProvider that succeeds for the first SaveJobState write and
// fails for the second.
func TestCreateJob_FailsWhenSaveStateAfterCRTDLPreparationFails(t *testing.T) {
	tmpDir := t.TempDir()
	jobsDir := filepath.Join(tmpDir, "jobs")

	// Write a valid CRTDL file so DetectInputType returns CRTDL and
	// ParseCRTDL succeeds.
	crtdlPath := filepath.Join(tmpDir, "input.json")
	crtdlContent := map[string]any{
		"cohortDefinition": map[string]any{
			"version":           "1.0.0",
			"inclusionCriteria": []any{},
		},
		"dataExtraction": map[string]any{
			"attributeGroups": []map[string]any{
				{
					"id":             "g1",
					"name":           "PatientPseudonymisiert",
					"groupReference": "https://example.org/Patient",
					"attributes": []map[string]any{
						{"attributeRef": "Patient.id", "mustHave": true},
					},
				},
			},
		},
	}
	bytes, err := json.Marshal(crtdlContent)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(crtdlPath, bytes, 0644))

	config := models.ProjectConfig{
		JobsDir: jobsDir,
		Pipeline: models.PipelineConfig{
			EnabledSteps: []models.StepName{models.StepTorchImport, models.StepFlattening},
		},
		Services: models.ServiceConfig{
			CRTDLPreprocessing: models.CRTDLPreprocessingConfig{Enabled: false},
		},
	}

	original := services.StateFileOpsProvider
	t.Cleanup(func() { services.StateFileOpsProvider = original })

	ops := &countingStateFileOps{
		failOnNth: 2,
		failErr:   errors.New("simulated write failure"),
	}
	services.StateFileOpsProvider = ops

	job, err := pipeline.CreateJob(models.GenerateJobID(), crtdlPath, "", config, lib.NewLogger(lib.LogLevelDebug))

	require.Error(t, err, "CreateJob must surface the second SaveJobState failure")
	assert.Nil(t, job)
	assert.Contains(t, err.Error(), "save job state after CRTDL preparation",
		"Error must come from the post-PrepareCRTDL SaveJobState branch (job.go:107-109)")
	assert.GreaterOrEqual(t, ops.writeCount, 2, "Second SaveJobState must have been attempted")
}

// TestCreateJob_NoImportStepEnabled tests CreateJob when no import step is enabled
// Covers job.go lines 62-63
func TestCreateJob_NoImportStepEnabled(t *testing.T) {
	tmpDir := t.TempDir()
	jobsDir := filepath.Join(tmpDir, "jobs")

	config := models.ProjectConfig{
		JobsDir: jobsDir,
		Pipeline: models.PipelineConfig{
			EnabledSteps: []models.StepName{models.StepDIMP, models.StepFlattening}, // No import step
		},
	}

	logger := lib.NewLogger(lib.LogLevelDebug)

	// Create job - should fail because no import step is enabled
	job, err := pipeline.CreateJob(models.GenerateJobID(), "/some/path", "", config, logger)

	require.Error(t, err, "CreateJob should fail when no import step is enabled")
	assert.Nil(t, job)
	assert.Contains(t, err.Error(), "no import step enabled", "Error should mention no import step")
}

// writeValidCRTDL writes a minimal structurally-valid CRTDL file for testing.
func writeValidCRTDL(t *testing.T, path string) {
	t.Helper()
	content := `{"cohortDefinition":{"inclusionCriteria":[]},"dataExtraction":{"attributeGroups":[]}}`
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
}

// TestCreateJob_CRTDLFlagWithHTTPInput verifies issue #286: an HTTP URL can
// be combined with a CRTDL attached via the explicit crtdlPath arg. The job
// retains InputType=HTTP (so http_import runs) and carries CRTDLPath for
// flattening. CreateJob runs PrepareCRTDL so CRTDLPath ends up pointing at
// the canonical copy inside the job directory.
func TestCreateJob_CRTDLFlagWithHTTPInput(t *testing.T) {
	tmpDir := t.TempDir()
	crtdlPath := filepath.Join(tmpDir, "crtdl.json")
	writeValidCRTDL(t, crtdlPath)

	jobsDir := filepath.Join(tmpDir, "jobs")
	config := models.ProjectConfig{
		JobsDir: jobsDir,
		Pipeline: models.PipelineConfig{
			EnabledSteps: []models.StepName{models.StepHttpImport, models.StepFlattening},
		},
	}

	job, err := pipeline.CreateJob(models.GenerateJobID(), "https://example.com/data.ndjson", crtdlPath, config, lib.NewLogger(lib.LogLevelDebug))

	require.NoError(t, err)
	require.NotNil(t, job)
	assert.Equal(t, models.InputTypeHTTP, job.InputType, "http URL should still be http_url")
	assert.Equal(t, "https://example.com/data.ndjson", job.InputSource)
	assert.Equal(t, filepath.Join(jobsDir, job.JobID, "crtdl.json"), job.CRTDLPath,
		"CRTDLPath must point at the prepared CRTDL inside jobDir")
	assert.Equal(t, string(models.StepHttpImport), job.CurrentStep)
}

// TestCreateJob_TorchCRTDLOnly verifies a torch job where the CRTDL is
// attached via crtdlPath and no separate input source is provided (the
// typical torch-extraction shape under the CRTDL-as-positional CLI contract).
// PrepareCRTDL repoints CRTDLPath at the canonical copy inside jobDir.
func TestCreateJob_TorchCRTDLOnly(t *testing.T) {
	tmpDir := t.TempDir()
	crtdlPath := filepath.Join(tmpDir, "crtdl.json")
	writeValidCRTDL(t, crtdlPath)

	jobsDir := filepath.Join(tmpDir, "jobs")
	config := models.ProjectConfig{
		JobsDir: jobsDir,
		Pipeline: models.PipelineConfig{
			EnabledSteps: []models.StepName{models.StepTorchImport},
		},
	}

	job, err := pipeline.CreateJob(models.GenerateJobID(), "", crtdlPath, config, lib.NewLogger(lib.LogLevelDebug))

	require.NoError(t, err)
	require.NotNil(t, job)
	assert.Equal(t, models.InputTypeLocal, job.InputType, "no external input source → default InputTypeLocal")
	assert.Empty(t, job.InputSource)
	assert.Equal(t, filepath.Join(jobsDir, job.JobID, "crtdl.json"), job.CRTDLPath,
		"CRTDLPath must point at the prepared CRTDL inside jobDir")
}

// TestCreateJob_InvalidCRTDL shows that an unreadable CRTDL file stops the job
// creation. CreateJob has no CRTDL check of its own; PrepareCRTDL reads the
// file and reports the failure.
func TestCreateJob_InvalidCRTDL(t *testing.T) {
	tmpDir := t.TempDir()
	missingCRTDL := filepath.Join(tmpDir, "does-not-exist.json")

	config := models.ProjectConfig{
		JobsDir: filepath.Join(tmpDir, "jobs"),
		Pipeline: models.PipelineConfig{
			EnabledSteps: []models.StepName{models.StepHttpImport, models.StepFlattening},
		},
	}

	job, err := pipeline.CreateJob(models.GenerateJobID(), "https://example.com/data.ndjson", missingCRTDL, config, lib.NewLogger(lib.LogLevelDebug))

	require.Error(t, err, "CreateJob must fail when CRTDL file is unreadable")
	assert.Nil(t, job)
	assert.Contains(t, err.Error(), "failed to prepare CRTDL", "error must name the failed step")
}

// TestCreateJob_CRTDLFlagOnlyNoPositional covers the local_import scenario
// where the data dir comes from config and the CRTDL is attached separately.
func TestCreateJob_CRTDLFlagOnlyNoPositional(t *testing.T) {
	tmpDir := t.TempDir()
	crtdlPath := filepath.Join(tmpDir, "crtdl.json")
	writeValidCRTDL(t, crtdlPath)

	jobsDir := filepath.Join(tmpDir, "jobs")
	config := models.ProjectConfig{
		JobsDir: jobsDir,
		Services: models.ServiceConfig{
			LocalImport: models.LocalImportConfig{Dir: tmpDir},
		},
		Pipeline: models.PipelineConfig{
			EnabledSteps: []models.StepName{models.StepLocalImport, models.StepFlattening},
		},
	}

	job, err := pipeline.CreateJob(models.GenerateJobID(), "", crtdlPath, config, lib.NewLogger(lib.LogLevelDebug))

	require.NoError(t, err)
	require.NotNil(t, job)
	assert.Equal(t, models.InputTypeLocal, job.InputType)
	assert.Equal(t, "", job.InputSource)
	assert.Equal(t, filepath.Join(jobsDir, job.JobID, "crtdl.json"), job.CRTDLPath,
		"CRTDLPath must point at the prepared CRTDL inside jobDir")
}
