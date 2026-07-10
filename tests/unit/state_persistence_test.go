package unit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

// TestGetJobOutputDir tests the output directory mapping for different steps
func TestGetJobOutputDir(t *testing.T) {
	baseDir := "/tmp/jobs"
	jobID := "test-job-123"

	tests := []struct {
		step     models.StepName
		expected string
	}{
		{models.StepLocalImport, "/tmp/jobs/test-job-123/import"},
		{models.StepTorchImport, "/tmp/jobs/test-job-123/import"},
		{models.StepHttpImport, "/tmp/jobs/test-job-123/import"},
		{models.StepDIMP, "/tmp/jobs/test-job-123/dimp"},
		{models.StepFlattening, "/tmp/jobs/test-job-123/csv"},
		{models.StepSend, "/tmp/jobs/test-job-123/send"},
		{models.StepWait, "/tmp/jobs/test-job-123"},
		{models.StepValidation, "/tmp/jobs/test-job-123/validation"},
	}

	for _, tt := range tests {
		t.Run(string(tt.step), func(t *testing.T) {
			result := services.GetJobOutputDir(baseDir, jobID, tt.step)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestStatePersistence_SaveAndLoad tests the complete save/load cycle
// Unit test for state persistence (save/load cycle)
func TestStatePersistence_SaveAndLoad(t *testing.T) {
	// Setup: Create temporary jobs directory
	tempDir := t.TempDir()
	jobID := uuid.New().String()

	// Create a test job
	now := time.Now()
	originalJob := &models.PipelineJob{
		JobID:       jobID,
		CreatedAt:   now,
		UpdatedAt:   now,
		InputSource: "/path/to/test/data",
		InputType:   models.InputTypeLocal,
		CurrentStep: string(models.StepLocalImport),
		Status:      models.JobStatusInProgress,
		Steps: []models.PipelineStep{
			{
				Name:           models.StepLocalImport,
				Status:         models.StepStatusInProgress,
				StartedAt:      &now,
				FilesProcessed: 0,
				BytesProcessed: 0,
			},
		},
		Config: models.ProjectConfig{
			Pipeline: models.PipelineConfig{
				EnabledSteps: []models.StepName{models.StepLocalImport},
			},
			Retry: models.RetryConfig{
				MaxAttempts:      5,
				InitialBackoffMs: 1000,
				MaxBackoffMs:     30000,
			},
			JobsDir: tempDir,
		},
		TotalFiles: 0,
		TotalBytes: 0,
	}

	// Test: Save the job
	err := services.SaveJobState(tempDir, originalJob)
	require.NoError(t, err, "SaveJobState should succeed")

	// Verify: State file exists
	statePath := services.GetStateFilePath(tempDir, jobID)
	_, err = os.Stat(statePath)
	require.NoError(t, err, "State file should exist after save")

	// Test: Load the job
	loadedJob, err := services.LoadJobState(tempDir, jobID)
	require.NoError(t, err, "LoadJobState should succeed")
	require.NotNil(t, loadedJob, "Loaded job should not be nil")

	// Verify: All fields match
	assert.Equal(t, originalJob.JobID, loadedJob.JobID, "JobID should match")
	assert.Equal(t, originalJob.InputSource, loadedJob.InputSource, "InputSource should match")
	assert.Equal(t, originalJob.InputType, loadedJob.InputType, "InputType should match")
	assert.Equal(t, originalJob.CurrentStep, loadedJob.CurrentStep, "CurrentStep should match")
	assert.Equal(t, originalJob.Status, loadedJob.Status, "Status should match")
	assert.Equal(t, originalJob.TotalFiles, loadedJob.TotalFiles, "TotalFiles should match")
	assert.Equal(t, originalJob.TotalBytes, loadedJob.TotalBytes, "TotalBytes should match")
	assert.Len(t, loadedJob.Steps, len(originalJob.Steps), "Steps count should match")

	// Verify: Step details match
	assert.Equal(t, originalJob.Steps[0].Name, loadedJob.Steps[0].Name, "Step name should match")
	assert.Equal(t, originalJob.Steps[0].Status, loadedJob.Steps[0].Status, "Step status should match")
}

// TestStatePersistence_AtomicWrite verifies atomic write behavior
func TestStatePersistence_AtomicWrite(t *testing.T) {
	tempDir := t.TempDir()
	jobID := uuid.New().String()

	// Create initial job
	job := createTestJob(jobID, tempDir)

	// Save job (first write)
	err := services.SaveJobState(tempDir, job)
	require.NoError(t, err, "First save should succeed")

	// Modify job
	job.Status = models.JobStatusCompleted
	job.CurrentStep = string(models.StepLocalImport)
	job.TotalFiles = 100

	// Save again (atomic overwrite)
	err = services.SaveJobState(tempDir, job)
	require.NoError(t, err, "Second save should succeed")

	// Verify: No temporary files left behind
	jobDir := services.GetJobDir(tempDir, jobID)
	entries, err := os.ReadDir(jobDir)
	require.NoError(t, err)

	tempFileCount := 0
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) == ".tmp" || entry.Name() == ".state.tmp" {
			tempFileCount++
		}
	}
	assert.Equal(t, 0, tempFileCount, "No temporary files should remain after atomic write")

	// Verify: Latest state is persisted
	loadedJob, err := services.LoadJobState(tempDir, jobID)
	require.NoError(t, err)
	assert.Equal(t, models.JobStatusCompleted, loadedJob.Status, "Latest status should be persisted")
	assert.Equal(t, 100, loadedJob.TotalFiles, "Latest file count should be persisted")
}

// TestStatePersistence_LoadNonexistent tests loading a job that doesn't exist
func TestStatePersistence_LoadNonexistent(t *testing.T) {
	tempDir := t.TempDir()
	nonexistentJobID := uuid.New().String()

	// Attempt to load nonexistent job
	job, err := services.LoadJobState(tempDir, nonexistentJobID)

	// Verify: Error is returned
	assert.Error(t, err, "Loading nonexistent job should return error")
	assert.Nil(t, job, "Job should be nil for nonexistent ID")
	assert.Contains(t, err.Error(), "job not found", "Error message should indicate job not found")
}

// TestStatePersistence_SaveInvalidJob tests that invalid jobs cannot be saved
func TestStatePersistence_SaveInvalidJob(t *testing.T) {
	tempDir := t.TempDir()

	// Create invalid job (missing required JobID)
	invalidJob := &models.PipelineJob{
		JobID:       "", // Invalid: empty JobID
		InputSource: "/test",
		InputType:   models.InputTypeLocal,
		Status:      models.JobStatusPending,
	}

	// Attempt to save invalid job
	err := services.SaveJobState(tempDir, invalidJob)

	// Verify: Error is returned
	assert.Error(t, err, "Saving invalid job should return error")
	assert.Contains(t, err.Error(), "invalid", "Error message should indicate validation failure")
}

// TestStatePersistence_MultipleSteps tests persistence of jobs with multiple steps
func TestStatePersistence_MultipleSteps(t *testing.T) {
	tempDir := t.TempDir()
	jobID := uuid.New().String()

	now := time.Now()
	completedTime := now.Add(5 * time.Minute)

	// Create job with multiple steps
	job := createTestJob(jobID, tempDir)
	job.Config.Pipeline.EnabledSteps = []models.StepName{
		models.StepLocalImport,
		models.StepDIMP,
		models.StepFlattening,
	}

	job.Steps = []models.PipelineStep{
		{
			Name:           models.StepLocalImport,
			Status:         models.StepStatusCompleted,
			StartedAt:      &now,
			CompletedAt:    &completedTime,
			FilesProcessed: 100,
			BytesProcessed: 1024000,
		},
		{
			Name:           models.StepDIMP,
			Status:         models.StepStatusInProgress,
			StartedAt:      &completedTime,
			FilesProcessed: 50,
			BytesProcessed: 512000,
		},
		{
			Name:           models.StepFlattening,
			Status:         models.StepStatusPending,
			FilesProcessed: 0,
			BytesProcessed: 0,
		},
	}

	// Save and reload
	err := services.SaveJobState(tempDir, job)
	require.NoError(t, err)

	loadedJob, err := services.LoadJobState(tempDir, jobID)
	require.NoError(t, err)

	// Verify: All steps are preserved
	require.Len(t, loadedJob.Steps, 3, "All steps should be preserved")

	assert.Equal(t, models.StepStatusCompleted, loadedJob.Steps[0].Status)
	assert.Equal(t, 100, loadedJob.Steps[0].FilesProcessed)
	assert.NotNil(t, loadedJob.Steps[0].CompletedAt)

	assert.Equal(t, models.StepStatusInProgress, loadedJob.Steps[1].Status)
	assert.Equal(t, 50, loadedJob.Steps[1].FilesProcessed)

	assert.Equal(t, models.StepStatusPending, loadedJob.Steps[2].Status)
	assert.Equal(t, 0, loadedJob.Steps[2].FilesProcessed)
}

// TestStatePersistence_ConcurrentAccess tests that state file remains consistent
// even with rapid successive writes (simulating concurrent scenarios)
func TestStatePersistence_ConcurrentAccess(t *testing.T) {
	tempDir := t.TempDir()
	jobID := uuid.New().String()

	job := createTestJob(jobID, tempDir)

	// Perform rapid successive writes
	for i := 0; i < 10; i++ {
		job.TotalFiles = i * 10
		job.UpdatedAt = time.Now()

		err := services.SaveJobState(tempDir, job)
		require.NoError(t, err, "Rapid save %d should succeed", i)
	}

	// Verify: Final state is consistent
	loadedJob, err := services.LoadJobState(tempDir, jobID)
	require.NoError(t, err)
	assert.Equal(t, 90, loadedJob.TotalFiles, "Final state should have last written value")
}

// TestStatePersistence_ConcurrentWrites runs many goroutines writing distinct
// states to the same job while others load it. The atomic temp+rename write must
// guarantee the final on-disk state is valid JSON equal to exactly one written
// state, and concurrent loads must never observe a torn file. Run under -race.
func TestStatePersistence_ConcurrentWrites(t *testing.T) {
	tempDir := t.TempDir()
	jobID := uuid.New().String()

	// Seed an initial state so concurrent readers always find a file.
	require.NoError(t, services.SaveJobState(tempDir, createTestJob(jobID, tempDir)))

	const workers = 64
	writtenValues := make(map[int]struct{}, workers)
	for i := 0; i < workers; i++ {
		writtenValues[i*7] = struct{}{}
	}

	var wg sync.WaitGroup
	errCh := make(chan error, workers*2)

	for i := 0; i < workers; i++ {
		wg.Add(2)
		value := i * 7
		go func() {
			defer wg.Done()
			job := createTestJob(jobID, tempDir)
			job.TotalFiles = value
			if err := services.SaveJobState(tempDir, job); err != nil {
				errCh <- fmt.Errorf("save: %w", err)
			}
		}()
		go func() {
			defer wg.Done()
			// A load racing an atomic rename must never see a partial file.
			if _, err := services.LoadJobState(tempDir, jobID); err != nil {
				errCh <- fmt.Errorf("load: %w", err)
			}
		}()
	}

	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Errorf("concurrent operation failed: %v", err)
	}

	raw, err := os.ReadFile(services.GetStateFilePath(tempDir, jobID))
	require.NoError(t, err)
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(raw, &decoded), "final state file must be valid JSON")

	loaded, err := services.LoadJobState(tempDir, jobID)
	require.NoError(t, err)
	_, ok := writtenValues[loaded.TotalFiles]
	assert.True(t, ok, "final TotalFiles %d must equal one concurrently written state", loaded.TotalFiles)
}

// TestStatePersistence_ConcurrentWritesWithLock demonstrates that wrapping a
// read-modify-write in WithJobLock serializes access: every increment survives.
// Without the lock, concurrent load/increment/save would lose updates.
func TestStatePersistence_ConcurrentWritesWithLock(t *testing.T) {
	tempDir := t.TempDir()
	jobID := uuid.New().String()
	logger := lib.NewLogger(lib.LogLevelError)

	require.NoError(t, services.SaveJobState(tempDir, createTestJob(jobID, tempDir)))

	const workers = 32
	var wg sync.WaitGroup
	errCh := make(chan error, workers)

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// WithJobLock is non-blocking, so retry until the exclusive lock is held.
			for {
				err := services.WithJobLock(tempDir, jobID, logger, func() error {
					job, err := services.LoadJobState(tempDir, jobID)
					if err != nil {
						return err
					}
					job.TotalFiles++
					return services.SaveJobState(tempDir, job)
				})
				if err == nil {
					return
				}
				if !strings.Contains(err.Error(), "locked by another process") {
					errCh <- err
					return
				}
				time.Sleep(time.Millisecond)
			}
		}()
	}

	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Errorf("locked operation failed: %v", err)
	}

	loaded, err := services.LoadJobState(tempDir, jobID)
	require.NoError(t, err)
	assert.Equal(t, workers, loaded.TotalFiles,
		"serialized read-modify-write under WithJobLock must preserve every increment")
}

// TestLoadJobState_BackfillCRTDLPath verifies the legacy-state backfill branch
// in state.go (lines 75-77): jobs saved before issue #286 carried the CRTDL
// path in InputSource (with InputType=crtdl_file) and no CRTDLPath field.
// On load, CRTDLPath must be populated from InputSource so flattening still
// resolves the CRTDL for pre-existing jobs.
func TestLoadJobState_BackfillCRTDLPath(t *testing.T) {
	tempDir := t.TempDir()
	jobID := uuid.New().String()
	jobDir := services.GetJobDir(tempDir, jobID)
	require.NoError(t, os.MkdirAll(jobDir, 0755))

	crtdlInSource := filepath.Join(tempDir, "legacy.json")
	now := time.Now()

	legacy := map[string]any{
		"job_id":       jobID,
		"created_at":   now.Format(time.RFC3339Nano),
		"updated_at":   now.Format(time.RFC3339Nano),
		"input_source": crtdlInSource,
		"input_type":   string(models.InputTypeCRTDL),
		// CRTDLPath intentionally omitted to simulate pre-#286 state.
		"current_step": string(models.StepTorchImport),
		"status":       string(models.JobStatusPending),
		"steps": []map[string]any{
			{
				"name":            string(models.StepTorchImport),
				"status":          string(models.StepStatusPending),
				"files_processed": 0,
				"bytes_processed": 0,
			},
		},
		"config": map[string]any{
			"pipeline": map[string]any{
				"enabled_steps": []string{string(models.StepTorchImport)},
			},
			"services": map[string]any{
				"torch": map[string]any{
					"base_url": "http://torch.example",
				},
			},
			"retry": map[string]any{
				"max_attempts":       5,
				"initial_backoff_ms": 1000,
				"max_backoff_ms":     30000,
			},
			"jobs_dir": tempDir,
		},
		"total_files": 0,
		"total_bytes": 0,
	}

	data, err := json.MarshalIndent(legacy, "", "  ")
	require.NoError(t, err)
	statePath := services.GetStateFilePath(tempDir, jobID)
	require.NoError(t, os.WriteFile(statePath, data, 0644))

	loaded, err := services.LoadJobState(tempDir, jobID)
	require.NoError(t, err, "legacy state without CRTDLPath must load via backfill")
	require.NotNil(t, loaded)
	assert.Equal(t, crtdlInSource, loaded.CRTDLPath, "CRTDLPath must be backfilled from InputSource")
	assert.Equal(t, crtdlInSource, loaded.InputSource, "InputSource must remain unchanged")
	assert.Equal(t, models.InputTypeCRTDL, loaded.InputType)
}

// TestLoadJobState_BackfillSkippedForNonCRTDL verifies the backfill does NOT
// run when InputType is something other than crtdl_file: CRTDLPath must stay
// empty and InputSource must not be copied into it.
func TestLoadJobState_BackfillSkippedForNonCRTDL(t *testing.T) {
	tempDir := t.TempDir()
	jobID := uuid.New().String()

	job := createTestJob(jobID, tempDir)
	job.InputType = models.InputTypeLocal
	job.InputSource = filepath.Join(tempDir, "data")
	job.CRTDLPath = ""

	require.NoError(t, services.SaveJobState(tempDir, job))

	loaded, err := services.LoadJobState(tempDir, jobID)
	require.NoError(t, err)
	assert.Empty(t, loaded.CRTDLPath, "non-CRTDL jobs must NOT backfill CRTDLPath")
}

// Helper function to create a test job
func createTestJob(jobID, jobsDir string) *models.PipelineJob {
	now := time.Now()
	return &models.PipelineJob{
		JobID:       jobID,
		CreatedAt:   now,
		UpdatedAt:   now,
		InputSource: "/test/data",
		InputType:   models.InputTypeLocal,
		CurrentStep: string(models.StepLocalImport),
		Status:      models.JobStatusPending,
		Steps: []models.PipelineStep{
			{
				Name:           models.StepLocalImport,
				Status:         models.StepStatusPending,
				FilesProcessed: 0,
				BytesProcessed: 0,
			},
		},
		Config: models.ProjectConfig{
			Pipeline: models.PipelineConfig{
				EnabledSteps: []models.StepName{models.StepLocalImport},
			},
			Retry: models.RetryConfig{
				MaxAttempts:      5,
				InitialBackoffMs: 1000,
				MaxBackoffMs:     30000,
			},
			JobsDir: jobsDir,
		},
		TotalFiles: 0,
		TotalBytes: 0,
	}
}
