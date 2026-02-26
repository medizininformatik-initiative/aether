package unit

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
)

// TestWaitStepCannotBeFirst verifies that wait step cannot be the first step
func TestWaitStepCannotBeFirst(t *testing.T) {
	steps := []models.StepName{models.StepWait, models.StepDIMP}
	err := lib.ValidateWaitStepPlacement(steps)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cannot be first")
}

// TestConsecutiveWaitsRejected verifies that consecutive wait steps are rejected
func TestConsecutiveWaitsRejected(t *testing.T) {
	steps := []models.StepName{models.StepTorchImport, models.StepWait, models.StepWait, models.StepDIMP}
	err := lib.ValidateWaitStepPlacement(steps)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "consecutive wait steps")
}

// TestValidWaitStepPlacement verifies that valid wait step configurations pass
func TestValidWaitStepPlacement(t *testing.T) {
	testCases := []struct {
		name  string
		steps []models.StepName
	}{
		{
			name:  "Single wait after import",
			steps: []models.StepName{models.StepTorchImport, models.StepWait, models.StepDIMP},
		},
		{
			name:  "Wait after DIMP",
			steps: []models.StepName{models.StepTorchImport, models.StepDIMP, models.StepWait},
		},
		{
			name:  "Multiple non-consecutive waits",
			steps: []models.StepName{models.StepTorchImport, models.StepWait, models.StepDIMP, models.StepWait},
		},
		{
			name:  "No wait steps",
			steps: []models.StepName{models.StepTorchImport, models.StepDIMP},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := lib.ValidateWaitStepPlacement(tc.steps)
			assert.NoError(t, err)
		})
	}
}

// TestIsValidStepName_Wait verifies that StepWait is recognized as valid
func TestIsValidStepName_Wait(t *testing.T) {
	assert.True(t, models.IsValidStepName(models.StepWait))
}

// TestIsValidStepStatus_Waiting verifies that waiting status is recognized
func TestIsValidStepStatus_Waiting(t *testing.T) {
	assert.True(t, models.IsValidStepStatus(models.StepStatusWaiting))
}

// TestGetWaitStepDir verifies correct wait directory naming
func TestGetWaitStepDir(t *testing.T) {
	jobsBaseDir := "/tmp/jobs"
	jobID := "test-job-123"

	testCases := []struct {
		name        string
		prevStep    models.StepName
		expectedDir string
	}{
		{
			name:        "After torch import",
			prevStep:    models.StepTorchImport,
			expectedDir: "/tmp/jobs/test-job-123/import_wait",
		},
		{
			name:        "After local import",
			prevStep:    models.StepLocalImport,
			expectedDir: "/tmp/jobs/test-job-123/import_wait",
		},
		{
			name:        "After DIMP",
			prevStep:    models.StepDIMP,
			expectedDir: "/tmp/jobs/test-job-123/pseudonymized_wait",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			waitDir := services.GetWaitStepDir(jobsBaseDir, jobID, tc.prevStep)
			assert.Equal(t, tc.expectedDir, waitDir)
		})
	}
}

// TestGetPreviousStepForWait verifies finding the previous non-wait step
func TestGetPreviousStepForWait(t *testing.T) {
	testCases := []struct {
		name         string
		enabledSteps []models.StepName
		waitIndex    int
		expectedStep models.StepName
		expectError  bool
	}{
		{
			name:         "Simple case - wait after torch",
			enabledSteps: []models.StepName{models.StepTorchImport, models.StepWait, models.StepDIMP},
			waitIndex:    1,
			expectedStep: models.StepTorchImport,
			expectError:  false,
		},
		{
			name:         "Wait after DIMP",
			enabledSteps: []models.StepName{models.StepTorchImport, models.StepDIMP, models.StepWait},
			waitIndex:    2,
			expectedStep: models.StepDIMP,
			expectError:  false,
		},
		{
			name:         "Wait as first step - error",
			enabledSteps: []models.StepName{models.StepWait, models.StepDIMP},
			waitIndex:    0,
			expectError:  true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			step, err := pipeline.GetPreviousStepForWait(tc.enabledSteps, tc.waitIndex)
			if tc.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tc.expectedStep, step)
			}
		})
	}
}

// TestStatusTransition_InProgressToWaiting verifies status transition to waiting
func TestStatusTransition_InProgressToWaiting(t *testing.T) {
	assert.True(t, models.StepStatusInProgress.CanTransitionTo(models.StepStatusWaiting))
}

// TestStatusTransition_WaitingToCompleted verifies waiting to completed transition
func TestStatusTransition_WaitingToCompleted(t *testing.T) {
	assert.True(t, models.StepStatusWaiting.CanTransitionTo(models.StepStatusCompleted))
}

// TestStatusTransition_WaitingToOther verifies waiting cannot transition to other states
func TestStatusTransition_WaitingToOther(t *testing.T) {
	assert.False(t, models.StepStatusWaiting.CanTransitionTo(models.StepStatusInProgress))
	assert.False(t, models.StepStatusWaiting.CanTransitionTo(models.StepStatusFailed))
	assert.False(t, models.StepStatusWaiting.CanTransitionTo(models.StepStatusPending))
}

func TestCreateWaitDirectory_CreatesEmptyDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	sourceDir := filepath.Join(tmpDir, "import")
	waitDir := filepath.Join(tmpDir, "import_wait")

	require.NoError(t, os.MkdirAll(sourceDir, 0755))

	// Create some files in source - they should NOT be copied
	testFiles := map[string]string{
		"Patient.ndjson":     `{"resourceType":"Patient","id":"1"}`,
		"Observation.ndjson": `{"resourceType":"Observation","id":"2"}`,
	}
	for name, content := range testFiles {
		require.NoError(t, os.WriteFile(filepath.Join(sourceDir, name), []byte(content), 0644))
	}

	require.NoError(t, pipeline.CreateWaitDirectory(sourceDir, waitDir))

	// Verify wait directory exists but is empty
	entries, err := os.ReadDir(waitDir)
	require.NoError(t, err)
	assert.Empty(t, entries, "Wait directory should be empty - files should NOT be copied")
}

func TestCreateWaitDirectory_EmptySource(t *testing.T) {
	tmpDir := t.TempDir()
	sourceDir := filepath.Join(tmpDir, "import")
	waitDir := filepath.Join(tmpDir, "import_wait")

	require.NoError(t, os.MkdirAll(sourceDir, 0755))
	require.NoError(t, pipeline.CreateWaitDirectory(sourceDir, waitDir))

	_, err := os.Stat(waitDir)
	assert.NoError(t, err)

	entries, err := os.ReadDir(waitDir)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestCreateWaitDirectory_NonExistentSource(t *testing.T) {
	tmpDir := t.TempDir()
	sourceDir := filepath.Join(tmpDir, "nonexistent")
	waitDir := filepath.Join(tmpDir, "import_wait")

	require.NoError(t, pipeline.CreateWaitDirectory(sourceDir, waitDir))

	_, err := os.Stat(waitDir)
	assert.NoError(t, err)
}

// TestGetStepInputDir_AfterWait verifies correct input directory after wait step
func TestGetStepInputDir_AfterWait(t *testing.T) {
	jobsBaseDir := "/tmp/jobs"
	jobID := "test-job"

	testCases := []struct {
		name        string
		steps       []models.StepName
		stepIndex   int
		expectedDir string
	}{
		{
			name:        "DIMP after wait (wait after torch)",
			steps:       []models.StepName{models.StepTorchImport, models.StepWait, models.StepDIMP},
			stepIndex:   2, // DIMP is at index 2
			expectedDir: "/tmp/jobs/test-job/import_wait",
		},
		{
			name:        "DIMP directly after torch (no wait)",
			steps:       []models.StepName{models.StepTorchImport, models.StepDIMP},
			stepIndex:   1, // DIMP is at index 1
			expectedDir: "/tmp/jobs/test-job/import",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			inputDir := services.GetStepInputDir(jobsBaseDir, jobID, tc.steps, tc.stepIndex)
			assert.Equal(t, tc.expectedDir, inputDir)
		})
	}
}

func TestCountFilesInDir(t *testing.T) {
	tmpDir := t.TempDir()

	for i := 0; i < 3; i++ {
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, filepath.Base(t.Name())+string(rune('a'+i))+".txt"), []byte("test"), 0644))
	}

	count, err := pipeline.CountFilesInDir(tmpDir)
	require.NoError(t, err)
	assert.Equal(t, 3, count)
}

func TestGetDirSize(t *testing.T) {
	tmpDir := t.TempDir()

	content := "hello world" // 11 bytes
	for i := 0; i < 3; i++ {
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, filepath.Base(t.Name())+string(rune('a'+i))+".txt"), []byte(content), 0644))
	}

	size, err := pipeline.GetDirSize(tmpDir)
	require.NoError(t, err)
	assert.Equal(t, int64(33), size) // 11 bytes * 3 files
}

func TestGetDirSize_NonExistentDir(t *testing.T) {
	size, err := pipeline.GetDirSize("/nonexistent/path/12345")
	require.NoError(t, err)
	assert.Equal(t, int64(0), size)
}

func TestGetDirSize_WithSubdirectories(t *testing.T) {
	tmpDir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "file.txt"), []byte("hello"), 0644))
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "subdir"), 0755)) // should be ignored

	size, err := pipeline.GetDirSize(tmpDir)
	require.NoError(t, err)
	assert.Equal(t, int64(5), size)
}

func TestCountFilesInDir_NonExistentDir(t *testing.T) {
	count, err := pipeline.CountFilesInDir("/nonexistent/path/12345")
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestCountFilesInDir_WithSubdirectories(t *testing.T) {
	tmpDir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "file1.txt"), []byte("a"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "file2.txt"), []byte("b"), 0644))
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "subdir"), 0755)) // should be ignored

	count, err := pipeline.CountFilesInDir(tmpDir)
	require.NoError(t, err)
	assert.Equal(t, 2, count)
}

func TestFindWaitStepIndex(t *testing.T) {
	job := &models.PipelineJob{
		Config: models.ProjectConfig{
			Pipeline: models.PipelineConfig{
				EnabledSteps: []models.StepName{
					models.StepTorchImport,
					models.StepWait,
					models.StepDIMP,
					models.StepWait,
				},
			},
		},
	}

	idx, err := pipeline.FindWaitStepIndex(job, 0) // first wait step
	assert.NoError(t, err)
	assert.Equal(t, 1, idx)

	idx, err = pipeline.FindWaitStepIndex(job, 1) // second wait step
	assert.NoError(t, err)
	assert.Equal(t, 3, idx)

	_, err = pipeline.FindWaitStepIndex(job, 2) // doesn't exist
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestGetCurrentWaitStepIndex(t *testing.T) {
	t.Run("No wait step in progress", func(t *testing.T) {
		job := &models.PipelineJob{
			Steps: []models.PipelineStep{
				{Name: models.StepTorchImport, Status: models.StepStatusCompleted},
				{Name: models.StepWait, Status: models.StepStatusPending},
			},
		}
		_, err := pipeline.GetCurrentWaitStepIndex(job)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no wait step currently in progress")
	})

	t.Run("Wait step in progress", func(t *testing.T) {
		job := &models.PipelineJob{
			Steps: []models.PipelineStep{
				{Name: models.StepTorchImport, Status: models.StepStatusCompleted},
				{Name: models.StepWait, Status: models.StepStatusInProgress},
				{Name: models.StepDIMP, Status: models.StepStatusPending},
			},
		}
		idx, err := pipeline.GetCurrentWaitStepIndex(job)
		assert.NoError(t, err)
		assert.Equal(t, 1, idx)
	})

	t.Run("Non-wait step in progress", func(t *testing.T) {
		job := &models.PipelineJob{
			Steps: []models.PipelineStep{
				{Name: models.StepTorchImport, Status: models.StepStatusInProgress},
				{Name: models.StepWait, Status: models.StepStatusPending},
			},
		}
		_, err := pipeline.GetCurrentWaitStepIndex(job)
		assert.Error(t, err)
	})
}

// TestExecuteWaitStep_ErrorOnInvalidIndex verifies ExecuteWaitStep fails with invalid index
func TestExecuteWaitStep_ErrorOnInvalidIndex(t *testing.T) {
	tmpDir := t.TempDir()
	job := &models.PipelineJob{
		JobID: "test-job",
		Config: models.ProjectConfig{
			Pipeline: models.PipelineConfig{
				EnabledSteps: []models.StepName{models.StepWait}, // Invalid: wait as first step
			},
		},
	}

	err := pipeline.ExecuteWaitStep(job, tmpDir, 0)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to find previous step")
}

// TestCreateWaitDirectory_IgnoresSourceContent verifies that nothing is copied from source
func TestCreateWaitDirectory_IgnoresSourceContent(t *testing.T) {
	tmpDir := t.TempDir()
	sourceDir := filepath.Join(tmpDir, "source")
	waitDir := filepath.Join(tmpDir, "source_wait")

	// Create source with files and subdirectories - none should be copied
	require.NoError(t, os.MkdirAll(sourceDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "file.ndjson"), []byte("data"), 0644))

	subDir := filepath.Join(sourceDir, "subdir")
	require.NoError(t, os.MkdirAll(subDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(subDir, "nested.txt"), []byte("nested"), 0644))

	err := pipeline.CreateWaitDirectory(sourceDir, waitDir)
	require.NoError(t, err)

	// Wait directory should be empty
	entries, err := os.ReadDir(waitDir)
	require.NoError(t, err)
	assert.Empty(t, entries, "Wait directory should be empty")
}

// TestGetStepInputDir_FirstStep verifies GetStepInputDir returns job dir for first step
func TestGetStepInputDir_FirstStep(t *testing.T) {
	jobsBaseDir := "/tmp/jobs"
	jobID := "test-job"
	steps := []models.StepName{models.StepTorchImport, models.StepDIMP}

	// First step (index 0) should return job directory
	inputDir := services.GetStepInputDir(jobsBaseDir, jobID, steps, 0)
	assert.Equal(t, "/tmp/jobs/test-job", inputDir)
}

// TestGetStepInputDir_NegativeIndex verifies GetStepInputDir handles negative index
func TestGetStepInputDir_NegativeIndex(t *testing.T) {
	jobsBaseDir := "/tmp/jobs"
	jobID := "test-job"
	steps := []models.StepName{models.StepTorchImport}

	inputDir := services.GetStepInputDir(jobsBaseDir, jobID, steps, -1)
	assert.Equal(t, "/tmp/jobs/test-job", inputDir)
}

// TestGetPreviousStepForWait_AllWaitsBeforeIndex verifies error when all previous steps are waits
func TestGetPreviousStepForWait_AllWaitsBeforeIndex(t *testing.T) {
	// This is an edge case that shouldn't happen with proper validation,
	// but tests the fallback path
	steps := []models.StepName{models.StepWait, models.StepWait, models.StepDIMP}

	_, err := pipeline.GetPreviousStepForWait(steps, 1)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no non-wait step found")
}

// TestGetStepInputDir_AllTransparentNonWaitFallback verifies the fallback when all previous
// steps are transparent non-wait (e.g., all validation steps).
func TestGetStepInputDir_AllTransparentNonWaitFallback(t *testing.T) {
	jobsBaseDir := "/tmp/jobs"
	jobID := "test-job"
	// All preceding steps are validation (transparent, non-wait) — falls back to job dir
	steps := []models.StepName{models.StepValidation, models.StepDIMP}

	inputDir := services.GetStepInputDir(jobsBaseDir, jobID, steps, 1)
	assert.Equal(t, "/tmp/jobs/test-job", inputDir)
}

// TestGetStepInputDir_AllWaitsFallback verifies the fallback when all previous steps are waits
func TestGetStepInputDir_AllWaitsFallback(t *testing.T) {
	jobsBaseDir := "/tmp/jobs"
	jobID := "test-job"
	// Edge case: wait steps at beginning (shouldn't happen with validation but tests fallback)
	steps := []models.StepName{models.StepWait, models.StepWait, models.StepDIMP}

	// When we're at index 2 and steps 0 and 1 are both waits, fallback to job dir
	inputDir := services.GetStepInputDir(jobsBaseDir, jobID, steps, 2)
	// Since validation prevents this, the function falls back to job dir
	assert.Equal(t, "/tmp/jobs/test-job", inputDir)
}

// TestCreateWaitDirectory_ErrorCreatingDir verifies error when wait directory cannot be created
func TestCreateWaitDirectory_ErrorCreatingDir(t *testing.T) {
	sourceDir := t.TempDir()
	waitDir := "/proc/invalid/path/that/cannot/be/created"

	err := pipeline.CreateWaitDirectory(sourceDir, waitDir)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create wait directory")
}

// TestExecuteWaitStep_Success verifies ExecuteWaitStep with valid configuration
func TestExecuteWaitStep_Success(t *testing.T) {
	tmpDir := t.TempDir()
	jobsDir := filepath.Join(tmpDir, "jobs")
	jobID := "test-job-123"

	// Create import directory with files (these should NOT be copied to wait dir)
	importDir := filepath.Join(jobsDir, jobID, "import")
	require.NoError(t, os.MkdirAll(importDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(importDir, "Patient.ndjson"), []byte(`{"id":"1"}`), 0644))

	job := &models.PipelineJob{
		JobID: jobID,
		Config: models.ProjectConfig{
			Pipeline: models.PipelineConfig{
				EnabledSteps: []models.StepName{models.StepTorchImport, models.StepWait, models.StepDIMP},
			},
		},
	}

	err := pipeline.ExecuteWaitStep(job, jobsDir, 1)
	require.NoError(t, err)

	// Verify wait directory was created but is empty
	waitDir := filepath.Join(jobsDir, jobID, "import_wait")
	info, err := os.Stat(waitDir)
	require.NoError(t, err)
	assert.True(t, info.IsDir(), "Wait directory should be created")

	entries, err := os.ReadDir(waitDir)
	require.NoError(t, err)
	assert.Empty(t, entries, "Wait directory should be empty - files should NOT be copied")
}

// TestExecuteWaitStep_CreateWaitDirectoryError verifies ExecuteWaitStep handles directory creation error
func TestExecuteWaitStep_CreateWaitDirectoryError(t *testing.T) {
	job := &models.PipelineJob{
		JobID: "test-job",
		Config: models.ProjectConfig{
			Pipeline: models.PipelineConfig{
				EnabledSteps: []models.StepName{models.StepTorchImport, models.StepWait},
			},
		},
	}

	// Use a path that will fail to create
	err := pipeline.ExecuteWaitStep(job, "/proc/invalid", 1)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create wait directory")
}

// TestCountFilesInDir_ErrorReadingDir verifies CountFilesInDir error handling
func TestCountFilesInDir_ErrorReadingDir(t *testing.T) {
	tmpDir := t.TempDir()
	restrictedDir := filepath.Join(tmpDir, "restricted")
	require.NoError(t, os.MkdirAll(restrictedDir, 0000))
	defer func() { _ = os.Chmod(restrictedDir, 0755) }()

	count, err := pipeline.CountFilesInDir(restrictedDir)
	assert.Error(t, err)
	assert.Equal(t, 0, count)
}

// TestGetDirSize_ErrorReadingDir verifies GetDirSize error handling
func TestGetDirSize_ErrorReadingDir(t *testing.T) {
	tmpDir := t.TempDir()
	restrictedDir := filepath.Join(tmpDir, "restricted")
	require.NoError(t, os.MkdirAll(restrictedDir, 0000))
	defer func() { _ = os.Chmod(restrictedDir, 0755) }()

	size, err := pipeline.GetDirSize(restrictedDir)
	assert.Error(t, err)
	assert.Equal(t, int64(0), size)
}

// ==================== Mock-based tests for coverage ====================

// mockFileOps provides a mockable implementation of FileOps for testing error paths
type mockFileOps struct {
	readDirFunc func(name string) ([]os.DirEntry, error)
	mkdirFunc   func(path string, perm os.FileMode) error
}

func (m *mockFileOps) ReadDir(name string) ([]os.DirEntry, error) {
	if m.readDirFunc != nil {
		return m.readDirFunc(name)
	}
	return os.ReadDir(name)
}

func (m *mockFileOps) MkdirAll(path string, perm os.FileMode) error {
	if m.mkdirFunc != nil {
		return m.mkdirFunc(path, perm)
	}
	return os.MkdirAll(path, perm)
}

// TestGetDirSize_NonExistError tests GetDirSize with non-IsNotExist error
func TestGetDirSize_NonExistError(t *testing.T) {
	mock := &mockFileOps{
		readDirFunc: func(name string) ([]os.DirEntry, error) {
			return nil, os.ErrPermission
		},
	}

	original := pipeline.FileOpsProvider
	pipeline.FileOpsProvider = mock
	defer func() { pipeline.FileOpsProvider = original }()

	size, err := pipeline.GetDirSize("/some/dir")
	assert.Error(t, err)
	assert.Equal(t, int64(0), size)
}

// TestCountFilesInDir_NonExistError tests CountFilesInDir with non-IsNotExist error
func TestCountFilesInDir_NonExistError(t *testing.T) {
	mock := &mockFileOps{
		readDirFunc: func(name string) ([]os.DirEntry, error) {
			return nil, os.ErrPermission
		},
	}

	original := pipeline.FileOpsProvider
	pipeline.FileOpsProvider = mock
	defer func() { pipeline.FileOpsProvider = original }()

	count, err := pipeline.CountFilesInDir("/some/dir")
	assert.Error(t, err)
	assert.Equal(t, 0, count)
}

// mockDirEntry is a mock DirEntry that can fail on Info()
type mockDirEntry struct {
	name  string
	isDir bool
	err   error
}

func (m mockDirEntry) Name() string               { return m.name }
func (m mockDirEntry) IsDir() bool                { return m.isDir }
func (m mockDirEntry) Type() os.FileMode          { return 0 }
func (m mockDirEntry) Info() (os.FileInfo, error) { return nil, m.err }

// TestGetDirSize_EntryInfoError tests GetDirSize when entry.Info() fails
func TestGetDirSize_EntryInfoError(t *testing.T) {
	mock := &mockFileOps{
		readDirFunc: func(name string) ([]os.DirEntry, error) {
			return []os.DirEntry{
				mockDirEntry{name: "file1.txt", isDir: false, err: os.ErrPermission},
			}, nil
		},
	}

	original := pipeline.FileOpsProvider
	pipeline.FileOpsProvider = mock
	defer func() { pipeline.FileOpsProvider = original }()

	size, err := pipeline.GetDirSize("/some/dir")
	assert.NoError(t, err) // Function continues on Info() error
	assert.Equal(t, int64(0), size)
}

// TestGetStepInputDir_AfterValidation verifies that steps after validation
// skip over validation and read from the previous data-producing step
func TestGetStepInputDir_AfterValidation(t *testing.T) {
	jobsBaseDir := "/tmp/jobs"
	jobID := "test-job"

	testCases := []struct {
		name        string
		steps       []models.StepName
		stepIndex   int
		expectedDir string
	}{
		{
			name:        "Step after validation reads from import",
			steps:       []models.StepName{models.StepLocalImport, models.StepValidation, models.StepFlattening},
			stepIndex:   2,
			expectedDir: "/tmp/jobs/test-job/import",
		},
		{
			name:        "Step after validation reads from pseudonymized",
			steps:       []models.StepName{models.StepLocalImport, models.StepDIMP, models.StepValidation, models.StepFlattening},
			stepIndex:   3,
			expectedDir: "/tmp/jobs/test-job/pseudonymized",
		},
		{
			name:        "Validation itself reads from import",
			steps:       []models.StepName{models.StepLocalImport, models.StepValidation},
			stepIndex:   1,
			expectedDir: "/tmp/jobs/test-job/import",
		},
		{
			name:        "Validation reads from pseudonymized after DIMP",
			steps:       []models.StepName{models.StepLocalImport, models.StepDIMP, models.StepValidation},
			stepIndex:   2,
			expectedDir: "/tmp/jobs/test-job/pseudonymized",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			inputDir := services.GetStepInputDir(jobsBaseDir, jobID, tc.steps, tc.stepIndex)
			assert.Equal(t, tc.expectedDir, inputDir)
		})
	}
}

// TestGetStepInputDir_WaitThenValidation verifies correct behavior with both transparent steps
func TestGetStepInputDir_WaitThenValidation(t *testing.T) {
	jobsBaseDir := "/tmp/jobs"
	jobID := "test-job"

	// import -> wait -> validation -> flattening
	steps := []models.StepName{
		models.StepLocalImport, models.StepWait, models.StepValidation, models.StepFlattening,
	}

	// Validation (index 2) should read from wait dir (previous is wait at index 1)
	inputDir := services.GetStepInputDir(jobsBaseDir, jobID, steps, 2)
	assert.Equal(t, "/tmp/jobs/test-job/import_wait", inputDir)

	// Flattening (index 3): prevStep is validation -> walk back -> find wait -> apply wait semantics
	inputDir = services.GetStepInputDir(jobsBaseDir, jobID, steps, 3)
	assert.Equal(t, "/tmp/jobs/test-job/import_wait", inputDir)
}
