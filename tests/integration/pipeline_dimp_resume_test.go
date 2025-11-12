package integration

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/pipeline"
)

// TestDIMPResumeAfterInterrupt tests that DIMP can resume after being interrupted mid-processing
// Reproduces bug: when DIMP is interrupted, it re-processes all files instead of skipping completed ones
func TestDIMPResumeAfterInterrupt(t *testing.T) {
	// Check if DIMP service is available
	dimpURL := "http://localhost:32861/fhir"
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(dimpURL)
	if err != nil || (resp != nil && resp.StatusCode >= 500) {
		t.Skip("Skipping test: DIMP service not available at localhost:32861. Run docker-compose up dimp-service to enable this test.")
	}
	if resp != nil {
		_ = resp.Body.Close()
	}

	// Setup
	tempDir := t.TempDir()
	jobsDir := filepath.Join(tempDir, "jobs")
	require.NoError(t, os.MkdirAll(jobsDir, 0755))

	// Create test config
	config := models.ProjectConfig{
		JobsDir: jobsDir,
		Services: models.ServiceConfig{
			DIMP: models.DIMPConfig{
				URL: "http://localhost:32861/fhir", // Assume DIMP service is running
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
	}

	logger := lib.NewLogger(lib.LogLevelDebug)
	// Create a job
	job, err := pipeline.CreateJob("test-input", config, logger)
	require.NoError(t, err)

	// Manually set up import directory with test files
	importDir := filepath.Join(jobsDir, job.JobID, "import")
	require.NoError(t, os.MkdirAll(importDir, 0755))

	// Create 3 test NDJSON files
	testFiles := []string{"patient.ndjson", "observation.ndjson", "condition.ndjson"}
	for _, filename := range testFiles {
		testData := `{"resourceType":"Patient","id":"test-1"}
{"resourceType":"Patient","id":"test-2"}
{"resourceType":"Patient","id":"test-3"}
`
		require.NoError(t, os.WriteFile(filepath.Join(importDir, filename), []byte(testData), 0644))
	}

	// Mark import step as completed
	job = pipeline.StartJob(job)
	importStep, _ := models.GetStepByName(*job, models.StepLocalImport)
	importStep = models.CompleteStep(importStep, len(testFiles), 300)
	*job = models.ReplaceStep(*job, importStep)
	require.NoError(t, pipeline.UpdateJob(jobsDir, job))

	// Advance to DIMP step
	advancedJob, err := pipeline.AdvanceToNextStep(job)
	require.NoError(t, err)
	require.NoError(t, pipeline.UpdateJob(jobsDir, advancedJob))

	// Create output directory (simulating partial processing)
	outputDir := filepath.Join(jobsDir, job.JobID, "pseudonymized")
	require.NoError(t, os.MkdirAll(outputDir, 0755))

	// SIMULATE INTERRUPTION: Process only the first file manually
	// This simulates what happens when user presses Ctrl+C after first file completes
	firstOutputFile := filepath.Join(outputDir, "dimped_patient.ndjson")
	pseudonymizedData := `{"resourceType":"Patient","id":"pseudo-1"}
{"resourceType":"Patient","id":"pseudo-2"}
{"resourceType":"Patient","id":"pseudo-3"}
`
	require.NoError(t, os.WriteFile(firstOutputFile, []byte(pseudonymizedData), 0644))

	// Job state still shows DIMP as in_progress with 0 files (because we interrupted)
	dimpStep, _ := models.GetStepByName(*advancedJob, models.StepDIMP)
	assert.Equal(t, models.StepStatusInProgress, dimpStep.Status)
	assert.Equal(t, 0, dimpStep.FilesProcessed)

	// NOW RESUME: Load job and continue DIMP
	reloadedJob, err := pipeline.LoadJob(jobsDir, job.JobID)
	require.NoError(t, err)

	// Execute DIMP step (this should skip patient.ndjson since it's already processed)
	jobDir := filepath.Join(jobsDir, job.JobID)
	err = pipeline.ExecuteDIMPStep(reloadedJob, jobDir, logger)

	// The bug: ExecuteDIMPStep currently processes ALL files, including patient.ndjson
	// Expected: Should only process observation.ndjson and condition.ndjson
	// Actual: Processes all 3 files, overwriting patient.ndjson

	// Verify DIMP completed
	require.NoError(t, err)

	// Check that all 3 output files exist
	for _, filename := range testFiles {
		outputPath := filepath.Join(outputDir, "dimped_"+filename)
		_, err := os.Stat(outputPath)
		assert.NoError(t, err, "Expected %s to exist after DIMP resume", filename)
	}

	// Reload job and check step status
	finalJob, err := pipeline.LoadJob(jobsDir, job.JobID)
	require.NoError(t, err)

	finalDimpStep, found := models.GetStepByName(*finalJob, models.StepDIMP)
	require.True(t, found)
	assert.Equal(t, models.StepStatusCompleted, finalDimpStep.Status)
	assert.Equal(t, len(testFiles), finalDimpStep.FilesProcessed)

	// TODO: Add assertion to verify that patient.ndjson was NOT re-processed
	// (This requires tracking or logging which files were actually sent to DIMP)
}
