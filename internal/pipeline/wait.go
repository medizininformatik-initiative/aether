package pipeline

import (
	"fmt"
	"os"

	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

// FileOps interface for file operations, allowing mocking in tests
type FileOps interface {
	ReadDir(name string) ([]os.DirEntry, error)
	MkdirAll(path string, perm os.FileMode) error
}

// defaultFileOps implements FileOps using the standard library
type defaultFileOps struct{}

func (d defaultFileOps) ReadDir(name string) ([]os.DirEntry, error) { return os.ReadDir(name) }
func (d defaultFileOps) MkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

// FileOpsProvider allows tests to inject mock file operations
var FileOpsProvider FileOps = defaultFileOps{}

// GetPreviousStepForWait finds the non-wait step that precedes the current wait step
func GetPreviousStepForWait(enabledSteps []models.StepName, waitStepIndex int) (models.StepName, error) {
	if waitStepIndex <= 0 {
		return "", fmt.Errorf("wait step cannot be first in pipeline")
	}

	for i := waitStepIndex - 1; i >= 0; i-- {
		if enabledSteps[i] != models.StepWait {
			return enabledSteps[i], nil
		}
	}

	return "", fmt.Errorf("no non-wait step found before wait step at index %d", waitStepIndex)
}

// FindWaitStepIndex finds the index of a wait step in the job's enabled steps
// waitStepPosition is the 0-based position among all wait steps (e.g., 0 for first wait, 1 for second)
func FindWaitStepIndex(job *models.PipelineJob, waitStepPosition int) (int, error) {
	waitCount := 0
	for i, step := range job.Config.Pipeline.EnabledSteps {
		if step == models.StepWait {
			if waitCount == waitStepPosition {
				return i, nil
			}
			waitCount++
		}
	}
	return 0, fmt.Errorf("wait step at position %d not found (only %d wait steps exist)", waitStepPosition, waitCount)
}

// GetCurrentWaitStepIndex finds the index of the currently executing wait step
func GetCurrentWaitStepIndex(job *models.PipelineJob) (int, error) {
	for i, step := range job.Steps {
		if step.Name == models.StepWait && step.Status == models.StepStatusInProgress {
			return i, nil
		}
	}
	return 0, fmt.Errorf("no wait step currently in progress")
}

// ExecuteWaitStep creates an empty wait directory for manual data inspection
// Returns nil on success, the job should then be saved with status "waiting"
func ExecuteWaitStep(job *models.PipelineJob, jobsBaseDir string, waitStepIndex int) error {
	enabledSteps := job.Config.Pipeline.EnabledSteps

	prevStep, err := GetPreviousStepForWait(enabledSteps, waitStepIndex)
	if err != nil {
		return fmt.Errorf("failed to find previous step: %w", err)
	}

	sourceDir := services.GetJobOutputDir(jobsBaseDir, job.JobID, prevStep)
	waitDir := services.GetWaitStepDir(jobsBaseDir, job.JobID, prevStep)

	if err := CreateWaitDirectory(sourceDir, waitDir); err != nil {
		return fmt.Errorf("failed to create wait directory: %w", err)
	}

	return nil
}

// CreateWaitDirectory creates an empty wait directory for manual data inspection
// The directory is intentionally empty - users must copy/place their modified data here
func CreateWaitDirectory(sourceDir, waitDir string) error {
	if err := FileOpsProvider.MkdirAll(waitDir, 0755); err != nil {
		return fmt.Errorf("failed to create wait directory %s: %w", waitDir, err)
	}
	return nil
}

// CountFilesInDir counts the number of files in a directory (non-recursive)
func CountFilesInDir(dir string) (int, error) {
	entries, err := FileOpsProvider.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	count := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			count++
		}
	}
	return count, nil
}

// GetDirSize calculates the total size of files in a directory (non-recursive)
func GetDirSize(dir string) (int64, error) {
	entries, err := FileOpsProvider.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	var size int64
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		size += info.Size()
	}
	return size, nil
}
