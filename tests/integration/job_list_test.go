package integration

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/pipeline"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

// TestJobList_MultipleJobs tests listing all jobs in the jobs directory
// Integration test for job list with multiple jobs
func TestJobList_MultipleJobs(t *testing.T) {
	tempDir := t.TempDir()
	jobsDir := filepath.Join(tempDir, "jobs")

	// Create 5 jobs with different states
	job1 := createTestJobWithState(t, jobsDir, models.JobStatusCompleted, models.StepLocalImport)
	time.Sleep(10 * time.Millisecond) // Ensure different timestamps

	job2 := createTestJobWithState(t, jobsDir, models.JobStatusInProgress, models.StepDIMP)
	time.Sleep(10 * time.Millisecond)

	job3 := createTestJobWithState(t, jobsDir, models.JobStatusFailed, models.StepCSVConversion)
	time.Sleep(10 * time.Millisecond)

	job4 := createTestJobWithState(t, jobsDir, models.JobStatusPending, models.StepLocalImport)
	time.Sleep(10 * time.Millisecond)

	job5 := createTestJobWithState(t, jobsDir, models.JobStatusInProgress, models.StepLocalImport)

	// List all jobs
	jobIDs, err := services.ListAllJobs(jobsDir)
	require.NoError(t, err, "ListAllJobs should succeed")

	// Verify: All 5 jobs are listed
	assert.Len(t, jobIDs, 5, "Should list all 5 jobs")

	// Verify: All created job IDs are in the list
	expectedIDs := []string{job1.JobID, job2.JobID, job3.JobID, job4.JobID, job5.JobID}
	for _, expectedID := range expectedIDs {
		assert.Contains(t, jobIDs, expectedID, "Job ID %s should be in the list", expectedID)
	}
}

// TestJobList_EmptyDirectory tests listing when no jobs exist
func TestJobList_EmptyDirectory(t *testing.T) {
	tempDir := t.TempDir()
	jobsDir := filepath.Join(tempDir, "jobs")

	// Don't create jobs directory - should handle gracefully
	jobIDs, err := services.ListAllJobs(jobsDir)
	require.NoError(t, err, "ListAllJobs should handle missing directory")
	assert.Empty(t, jobIDs, "Should return empty list for non-existent directory")
}

// TestJobList_WithInvalidJobs tests that invalid job directories are skipped
func TestJobList_WithInvalidJobs(t *testing.T) {
	tempDir := t.TempDir()
	jobsDir := filepath.Join(tempDir, "jobs")
	require.NoError(t, os.MkdirAll(jobsDir, 0755))

	// Create valid job
	validJob := createTestJobWithState(t, jobsDir, models.JobStatusCompleted, models.StepLocalImport)

	// Create directory without state.json (invalid job)
	invalidJobDir := filepath.Join(jobsDir, uuid.New().String())
	require.NoError(t, os.MkdirAll(invalidJobDir, 0755))

	// Create a file (not a directory) in jobs dir
	invalidFile := filepath.Join(jobsDir, "not-a-job.txt")
	require.NoError(t, os.WriteFile(invalidFile, []byte("test"), 0644))

	// List jobs
	jobIDs, err := services.ListAllJobs(jobsDir)
	require.NoError(t, err)

	// Verify: Only valid job is listed
	assert.Len(t, jobIDs, 1, "Should only list valid jobs")
	assert.Contains(t, jobIDs, validJob.JobID)
}

// TestJobList_LoadMultipleJobDetails tests loading detailed info for multiple jobs
func TestJobList_LoadMultipleJobDetails(t *testing.T) {
	tempDir := t.TempDir()
	jobsDir := filepath.Join(tempDir, "jobs")

	// Create jobs with different characteristics
	jobs := []*models.PipelineJob{
		createJobWithDetails(t, jobsDir, models.JobStatusCompleted, 100, 5000000),
		createJobWithDetails(t, jobsDir, models.JobStatusInProgress, 50, 2500000),
		createJobWithDetails(t, jobsDir, models.JobStatusFailed, 10, 500000),
	}

	// List job IDs
	jobIDs, err := services.ListAllJobs(jobsDir)
	require.NoError(t, err)
	assert.Len(t, jobIDs, 3)

	// Load and verify each job
	for _, originalJob := range jobs {
		loadedJob, err := pipeline.LoadJob(jobsDir, originalJob.JobID)
		require.NoError(t, err, "Should load job %s", originalJob.JobID)

		assert.Equal(t, originalJob.Status, loadedJob.Status)
		assert.Equal(t, originalJob.TotalFiles, loadedJob.TotalFiles)
		assert.Equal(t, originalJob.TotalBytes, loadedJob.TotalBytes)
	}
}

// TestJobList_SortedByCreationTime tests that jobs can be sorted by creation time
func TestJobList_SortedByCreationTime(t *testing.T) {
	tempDir := t.TempDir()
	jobsDir := filepath.Join(tempDir, "jobs")

	// Create jobs with deliberate time gaps
	var jobsWithTimes []struct {
		job       *models.PipelineJob
		createdAt time.Time
	}

	for i := 0; i < 3; i++ {
		job := createTestJobWithState(t, jobsDir, models.JobStatusCompleted, models.StepLocalImport)
		jobsWithTimes = append(jobsWithTimes, struct {
			job       *models.PipelineJob
			createdAt time.Time
		}{job, job.CreatedAt})
		time.Sleep(20 * time.Millisecond) // Ensure different timestamps
	}

	// List jobs
	jobIDs, err := services.ListAllJobs(jobsDir)
	require.NoError(t, err)
	assert.Len(t, jobIDs, 3)

	// Load jobs and sort by creation time (newest first)
	type jobWithTime struct {
		id        string
		createdAt time.Time
	}
	var loadedJobs []jobWithTime

	for _, id := range jobIDs {
		job, err := pipeline.LoadJob(jobsDir, id)
		require.NoError(t, err)
		loadedJobs = append(loadedJobs, jobWithTime{id, job.CreatedAt})
	}

	sort.Slice(loadedJobs, func(i, j int) bool {
		return loadedJobs[i].createdAt.After(loadedJobs[j].createdAt)
	})

	// Verify: Newest job is first
	assert.Equal(t, jobsWithTimes[2].job.JobID, loadedJobs[0].id, "Newest job should be first when sorted")
	assert.Equal(t, jobsWithTimes[0].job.JobID, loadedJobs[2].id, "Oldest job should be last when sorted")
}

// TestJobList_FilterByStatus tests filtering jobs by status
func TestJobList_FilterByStatus(t *testing.T) {
	tempDir := t.TempDir()
	jobsDir := filepath.Join(tempDir, "jobs")

	// Create jobs with different statuses
	completedJob := createTestJobWithState(t, jobsDir, models.JobStatusCompleted, models.StepLocalImport)
	inProgressJob := createTestJobWithState(t, jobsDir, models.JobStatusInProgress, models.StepDIMP)
	failedJob := createTestJobWithState(t, jobsDir, models.JobStatusFailed, models.StepCSVConversion)
	_ = createTestJobWithState(t, jobsDir, models.JobStatusPending, models.StepLocalImport)

	// List all jobs
	allJobIDs, err := services.ListAllJobs(jobsDir)
	require.NoError(t, err)
	assert.Len(t, allJobIDs, 4)

	// Filter by status (manual filtering for this test)
	var completedJobs, inProgressJobs, failedJobs []string
	for _, id := range allJobIDs {
		job, err := pipeline.LoadJob(jobsDir, id)
		require.NoError(t, err)

		switch job.Status {
		case models.JobStatusCompleted:
			completedJobs = append(completedJobs, id)
		case models.JobStatusInProgress:
			inProgressJobs = append(inProgressJobs, id)
		case models.JobStatusFailed:
			failedJobs = append(failedJobs, id)
		}
	}

	// Verify: Correct jobs in each status
	assert.Len(t, completedJobs, 1)
	assert.Contains(t, completedJobs, completedJob.JobID)

	assert.Len(t, inProgressJobs, 1)
	assert.Contains(t, inProgressJobs, inProgressJob.JobID)

	assert.Len(t, failedJobs, 1)
	assert.Contains(t, failedJobs, failedJob.JobID)
}

// Helper: Create a test job with specific state
func createTestJobWithState(t *testing.T, jobsDir string, status models.JobStatus, currentStep models.StepName) *models.PipelineJob {
	config := models.ProjectConfig{
		Pipeline: models.PipelineConfig{
			EnabledSteps: []models.StepName{models.StepLocalImport, models.StepDIMP, models.StepCSVConversion},
		},
		Retry: models.RetryConfig{
			MaxAttempts:      5,
			InitialBackoffMs: 100,
			MaxBackoffMs:     1000,
		},
		JobsDir: jobsDir,
	}

	// Create temporary source directory
	sourceDir := t.TempDir()
	createTestFHIRFile(t, sourceDir)

	logger := lib.NewLogger(lib.LogLevelInfo)
	job, err := pipeline.CreateJob(sourceDir, config, logger)
	require.NoError(t, err)

	// Update job to desired state
	job.Status = status
	job.CurrentStep = string(currentStep)

	err = pipeline.UpdateJob(jobsDir, job)
	require.NoError(t, err)

	return job
}

// Helper: Create job with specific details
func createJobWithDetails(t *testing.T, jobsDir string, status models.JobStatus, totalFiles int, totalBytes int64) *models.PipelineJob {
	job := createTestJobWithState(t, jobsDir, status, models.StepLocalImport)
	job.TotalFiles = totalFiles
	job.TotalBytes = totalBytes

	err := pipeline.UpdateJob(jobsDir, job)
	require.NoError(t, err)

	return job
}

// Helper: Create a single test FHIR file
func createTestFHIRFile(t *testing.T, dir string) {
	filename := filepath.Join(dir, uuid.New().String()+".ndjson")
	content := `{"resourceType":"Patient","id":"test-123"}`
	err := os.WriteFile(filename, []byte(content), 0644)
	require.NoError(t, err)
}
