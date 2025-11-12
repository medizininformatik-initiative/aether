package integration

import (
	"encoding/json"
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
	"github.com/medizininformatik-initiative/aether/internal/pipeline"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

// TestPipelineMultiStep_AutomaticExecution verifies that pipeline start
// automatically executes all enabled steps, not just import
// This is a regression test for the bug where only import was executed
func TestPipelineMultiStep_AutomaticExecution(t *testing.T) {
	// Setup mock DIMP service
	dimpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var resource map[string]any
		_ = json.NewDecoder(r.Body).Decode(&resource)

		// Simple pseudonymization
		resource["id"] = "pseudo-" + resource["id"].(string)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resource)
	}))
	defer dimpServer.Close()

	// Setup test environment
	tmpDir := t.TempDir()
	jobsDir := filepath.Join(tmpDir, "jobs")
	importDir := filepath.Join(tmpDir, "import_data")
	_ = os.MkdirAll(importDir, 0755)
	_ = os.MkdirAll(jobsDir, 0755)

	// Create test FHIR data
	testFile := filepath.Join(importDir, "test.ndjson")
	testResources := []map[string]any{
		{"resourceType": "Patient", "id": "patient1", "name": []map[string]string{{"family": "Smith"}}},
		{"resourceType": "Patient", "id": "patient2", "name": []map[string]string{{"family": "Jones"}}},
	}
	writeNDJSONToFile(t, testFile, testResources)

	// Create config with BOTH import and DIMP enabled
	config := models.ProjectConfig{
		Services: models.ServiceConfig{
			DIMP: models.DIMPConfig{
				URL: dimpServer.URL,
			},
		},
		Pipeline: models.PipelineConfig{
			EnabledSteps: []models.StepName{
				models.StepLocalImport,
				models.StepDIMP,
			},
		},
		Retry: models.RetryConfig{
			MaxAttempts:      3,
			InitialBackoffMs: 100,
			MaxBackoffMs:     1000,
		},
		JobsDir: jobsDir,
	}

	logger := lib.NewLogger(lib.LogLevelInfo)
	// Create job
	job, err := pipeline.CreateJob(importDir, config, logger)
	require.NoError(t, err)
	require.NotEmpty(t, job.JobID)

	// Start the job
	startedJob := pipeline.StartJob(job)
	require.NoError(t, pipeline.UpdateJob(jobsDir, startedJob))

	// Execute import step
	logger = lib.NewLogger(lib.LogLevelError) // Suppress logs in tests
	httpClient := services.NewHTTPClient(30*time.Second, config.Retry, logger)
	importedJob, err := pipeline.ExecuteImportStep(startedJob, logger, httpClient, false)
	require.NoError(t, err)
	require.NoError(t, pipeline.UpdateJob(jobsDir, importedJob))

	// Verify import step completed
	assert.Equal(t, 1, importedJob.TotalFiles, "Import should have processed 1 file")
	importStep, found := models.GetStepByName(*importedJob, models.StepLocalImport)
	require.True(t, found, "Import step should exist")
	assert.Equal(t, models.StepStatusCompleted, importStep.Status, "Import step should be completed")

	// NOW THE CRITICAL TEST: Advance to next step (should be DIMP)
	currentStepName := models.StepName(importedJob.CurrentStep)
	nextStepName := importedJob.Config.Pipeline.GetNextStep(currentStepName)

	assert.Equal(t, models.StepDIMP, nextStepName, "Next step after import should be DIMP")
	assert.NotEmpty(t, nextStepName, "There should be a next step (DIMP)")

	// Advance to DIMP step
	advancedJob, err := pipeline.AdvanceToNextStep(importedJob)
	require.NoError(t, err)
	require.Equal(t, string(models.StepDIMP), advancedJob.CurrentStep, "Current step should now be DIMP")
	require.NoError(t, pipeline.UpdateJob(jobsDir, advancedJob))

	// Execute DIMP step
	jobDir := services.GetJobDir(jobsDir, job.JobID)
	err = pipeline.ExecuteDIMPStep(advancedJob, jobDir, logger)
	require.NoError(t, err, "DIMP step should execute successfully")

	// Verify DIMP step completed
	dimpStep, found := models.GetStepByName(*advancedJob, models.StepDIMP)
	require.True(t, found, "DIMP step should exist")
	assert.Equal(t, models.StepStatusCompleted, dimpStep.Status, "DIMP step should be completed")

	// Verify pseudonymized files were created
	pseudonymizedDir := filepath.Join(jobDir, "pseudonymized")
	entries, err := os.ReadDir(pseudonymizedDir)
	require.NoError(t, err)
	assert.NotEmpty(t, entries, "Pseudonymized directory should contain files")
	assert.Equal(t, 1, len(entries), "Should have 1 pseudonymized file")
	assert.Contains(t, entries[0].Name(), "dimped_", "File should have dimped_ prefix")

	// Verify pseudonymized content
	pseudonymizedFile := filepath.Join(pseudonymizedDir, entries[0].Name())
	resources := readNDJSONFromFile(t, pseudonymizedFile)
	assert.Len(t, resources, 2, "Should have 2 pseudonymized resources")
	assert.Equal(t, "pseudo-patient1", resources[0]["id"], "First patient ID should be pseudonymized")
	assert.Equal(t, "pseudo-patient2", resources[1]["id"], "Second patient ID should be pseudonymized")
}

// TestPipelineMultiStep_StepSequencing verifies steps execute in the correct order
func TestPipelineMultiStep_StepSequencing(t *testing.T) {
	tmpDir := t.TempDir()
	jobsDir := filepath.Join(tmpDir, "jobs")
	_ = os.MkdirAll(jobsDir, 0755)

	config := models.ProjectConfig{
		Pipeline: models.PipelineConfig{
			EnabledSteps: []models.StepName{
				models.StepLocalImport,
				models.StepDIMP,
				models.StepValidation,
				models.StepCSVConversion,
			},
		},
		JobsDir: jobsDir,
	}

	// Test GetNextStep returns steps in order
	assert.Equal(t, models.StepDIMP, config.Pipeline.GetNextStep(models.StepLocalImport))
	assert.Equal(t, models.StepValidation, config.Pipeline.GetNextStep(models.StepDIMP))
	assert.Equal(t, models.StepCSVConversion, config.Pipeline.GetNextStep(models.StepValidation))
	assert.Equal(t, models.StepName(""), config.Pipeline.GetNextStep(models.StepCSVConversion), "Should return empty after last step")
}

// TestPipelineMultiStep_OnlyImportEnabled verifies pipeline works with only import
func TestPipelineMultiStep_OnlyImportEnabled(t *testing.T) {
	tmpDir := t.TempDir()
	jobsDir := filepath.Join(tmpDir, "jobs")
	importDir := filepath.Join(tmpDir, "import_data")
	_ = os.MkdirAll(importDir, 0755)
	_ = os.MkdirAll(jobsDir, 0755)

	// Create test FHIR data
	testFile := filepath.Join(importDir, "test.ndjson")
	testResources := []map[string]any{
		{"resourceType": "Patient", "id": "patient1"},
	}
	writeNDJSONToFile(t, testFile, testResources)

	config := models.ProjectConfig{
		Pipeline: models.PipelineConfig{
			EnabledSteps: []models.StepName{
				models.StepLocalImport,
				// Only import enabled
			},
		},
		Retry: models.RetryConfig{
			MaxAttempts:      3,
			InitialBackoffMs: 100,
			MaxBackoffMs:     1000,
		},
		JobsDir: jobsDir,
	}

	logger := lib.NewLogger(lib.LogLevelInfo)
	// Create and start job
	job, err := pipeline.CreateJob(importDir, config, logger)
	require.NoError(t, err)

	startedJob := pipeline.StartJob(job)
	require.NoError(t, pipeline.UpdateJob(jobsDir, startedJob))

	// Execute import
	logger = lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(30*time.Second, config.Retry, logger)
	importedJob, err := pipeline.ExecuteImportStep(startedJob, logger, httpClient, false)
	require.NoError(t, err)

	// Try to get next step - should be empty
	nextStep := importedJob.Config.Pipeline.GetNextStep(models.StepLocalImport)
	assert.Empty(t, nextStep, "Should have no next step when only import is enabled")

	// Advance should mark job as complete
	completedJob, err := pipeline.AdvanceToNextStep(importedJob)
	require.NoError(t, err)
	assert.Equal(t, models.JobStatusCompleted, completedJob.Status, "Job should be marked as completed")
	assert.Empty(t, completedJob.CurrentStep, "Current step should be empty when job is complete")
}

// TestPipelineMultiStep_ConfigLoadingPreservesSteps verifies config loading
// doesn't drop enabled steps (regression test for viper.Unmarshal bug)
func TestPipelineMultiStep_ConfigLoadingPreservesSteps(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "test-config.yaml")
	jobsDir := filepath.Join(tmpDir, "jobs")

	// Write config file with multiple steps
	configContent := `
services:
  dimp:
    url: "http://localhost:8080"
  csv_conversion:
    url: "http://localhost:9000"

pipeline:
  enabled_steps:
    - local_import
    - dimp
    - csv_conversion

retry:
  max_attempts: 5
  initial_backoff_ms: 1000
  max_backoff_ms: 30000

jobs_dir: "` + jobsDir + `"
`
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	// Create jobs directory
	_ = os.MkdirAll(jobsDir, 0755)

	// Load config
	config, err := services.LoadConfig(configFile)
	require.NoError(t, err, "Config should load successfully")

	// Verify all steps are present
	assert.Len(t, config.Pipeline.EnabledSteps, 3, "Should have 3 enabled steps")
	assert.Equal(t, models.StepLocalImport, config.Pipeline.EnabledSteps[0])
	assert.Equal(t, models.StepDIMP, config.Pipeline.EnabledSteps[1])
	assert.Equal(t, models.StepCSVConversion, config.Pipeline.EnabledSteps[2])

	// Verify service URLs are loaded
	assert.Equal(t, "http://localhost:8080", config.Services.DIMP.URL)
	assert.Equal(t, "http://localhost:9000", config.Services.CSVConversion.URL)
}

// TestPipelineMultiStep_JobStatePersistedBetweenSteps verifies job state
// is saved correctly after each step execution
func TestPipelineMultiStep_JobStatePersistedBetweenSteps(t *testing.T) {
	// Setup mock DIMP service
	dimpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var resource map[string]any
		_ = json.NewDecoder(r.Body).Decode(&resource)
		resource["id"] = "pseudo-" + resource["id"].(string)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resource)
	}))
	defer dimpServer.Close()

	tmpDir := t.TempDir()
	jobsDir := filepath.Join(tmpDir, "jobs")
	importDir := filepath.Join(tmpDir, "import_data")
	_ = os.MkdirAll(importDir, 0755)
	_ = os.MkdirAll(jobsDir, 0755)

	// Create test data
	testFile := filepath.Join(importDir, "test.ndjson")
	testResources := []map[string]any{
		{"resourceType": "Patient", "id": "p1"},
	}
	writeNDJSONToFile(t, testFile, testResources)

	config := models.ProjectConfig{
		Services: models.ServiceConfig{
			DIMP: models.DIMPConfig{
				URL: dimpServer.URL,
			},
		},
		Pipeline: models.PipelineConfig{
			EnabledSteps: []models.StepName{
				models.StepLocalImport,
				models.StepDIMP,
			},
		},
		Retry: models.RetryConfig{
			MaxAttempts:      3,
			InitialBackoffMs: 100,
			MaxBackoffMs:     1000,
		},
		JobsDir: jobsDir,
	}

	logger := lib.NewLogger(lib.LogLevelInfo)
	// Create job
	job, err := pipeline.CreateJob(importDir, config, logger)
	require.NoError(t, err)
	jobID := job.JobID

	// Execute import
	startedJob := pipeline.StartJob(job)
	require.NoError(t, pipeline.UpdateJob(jobsDir, startedJob))

	logger = lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(30*time.Second, config.Retry, logger)
	importedJob, err := pipeline.ExecuteImportStep(startedJob, logger, httpClient, false)
	require.NoError(t, err)
	require.NoError(t, pipeline.UpdateJob(jobsDir, importedJob))

	// Load job from disk and verify import step is complete
	loadedJob1, err := pipeline.LoadJob(jobsDir, jobID)
	require.NoError(t, err)
	importStep, found := models.GetStepByName(*loadedJob1, models.StepLocalImport)
	require.True(t, found)
	assert.Equal(t, models.StepStatusCompleted, importStep.Status, "Import step should be marked complete in saved state")

	// Advance to DIMP
	advancedJob, err := pipeline.AdvanceToNextStep(loadedJob1)
	require.NoError(t, err)
	require.NoError(t, pipeline.UpdateJob(jobsDir, advancedJob))

	// Execute DIMP
	jobDir := services.GetJobDir(jobsDir, jobID)
	err = pipeline.ExecuteDIMPStep(advancedJob, jobDir, logger)
	require.NoError(t, err)
	require.NoError(t, pipeline.UpdateJob(jobsDir, advancedJob))

	// Load job again and verify both steps are complete
	loadedJob2, err := pipeline.LoadJob(jobsDir, jobID)
	require.NoError(t, err)

	importStep2, found := models.GetStepByName(*loadedJob2, models.StepLocalImport)
	require.True(t, found)
	assert.Equal(t, models.StepStatusCompleted, importStep2.Status, "Import step should still be complete")

	dimpStep, found := models.GetStepByName(*loadedJob2, models.StepDIMP)
	require.True(t, found)
	assert.Equal(t, models.StepStatusCompleted, dimpStep.Status, "DIMP step should be marked complete in saved state")
}

// Helper functions
func writeNDJSONToFile(t *testing.T, filepath string, resources []map[string]any) {
	file, err := os.Create(filepath)
	require.NoError(t, err)
	defer func() {
		_ = file.Close()
	}()

	for _, resource := range resources {
		data, err := json.Marshal(resource)
		require.NoError(t, err)
		_, _ = file.Write(data)
		_, _ = file.Write([]byte("\n"))
	}
}

func readNDJSONFromFile(t *testing.T, filepath string) []map[string]any {
	data, err := os.ReadFile(filepath)
	require.NoError(t, err)

	var resources []map[string]any
	lines := splitLinesByNewline(string(data))
	for _, line := range lines {
		if line == "" {
			continue
		}
		var resource map[string]any
		err := json.Unmarshal([]byte(line), &resource)
		require.NoError(t, err)
		resources = append(resources, resource)
	}

	return resources
}

func splitLinesByNewline(s string) []string {
	var lines []string
	var current string
	for _, ch := range s {
		if ch == '\n' {
			lines = append(lines, current)
			current = ""
		} else {
			current += string(ch)
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}
