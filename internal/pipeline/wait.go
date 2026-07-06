package pipeline

import (
	"fmt"
	"os"

	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

// waitStep pauses the pipeline for manual data inspection. Its Run is idempotent:
// it ensures the wait directory exists, then pauses while empty and completes
// once the user has placed files there — so first-arrival and resume are the
// same code path.
type waitStep struct{}

func (waitStep) Name() models.StepName { return models.StepWait }

func (waitStep) Run(ctx *StepContext) (StepResult, error) {
	idx, err := GetCurrentWaitStepIndex(ctx.Job)
	if err != nil {
		return StepResult{}, err
	}

	waitDir, err := resolveWaitDir(ctx.Layout, idx)
	if err != nil {
		return StepResult{}, err
	}

	count, err := CountFilesInDir(waitDir)
	if err != nil {
		return StepResult{}, err
	}
	if count == 0 {
		fmt.Printf("\n⏸ Pipeline paused at wait step\n")
		fmt.Printf("  Wait directory: %s\n", waitDir)
		fmt.Printf("  The directory is EMPTY - place your modified data there\n")
		fmt.Printf("\n  To continue: aether pipeline continue <config> %s\n", ctx.Job.JobID)
		return StepResult{}, ErrPaused
	}
	fmt.Printf("  Found %d file(s) in wait directory\n", count)
	return StepResult{FilesProcessed: count}, nil
}

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
	layout := services.NewJobLayout(jobsBaseDir, job.JobID, job.Config.Pipeline.EnabledSteps)
	_, err := resolveWaitDir(layout, waitStepIndex)
	return err
}

// resolveWaitDir resolves the wait override directory for the wait step at
// waitStepIndex and ensures it exists. waitStep.Run and the exported
// ExecuteWaitStep share it so both report the same errors from one place.
func resolveWaitDir(layout services.JobLayout, waitStepIndex int) (string, error) {
	waitDir, err := layout.WaitDir(waitStepIndex)
	if err != nil {
		return "", fmt.Errorf("failed to find previous step: %w", err)
	}
	if err := CreateWaitDirectory(waitDir); err != nil {
		return "", fmt.Errorf("failed to create wait directory: %w", err)
	}
	return waitDir, nil
}

// CreateWaitDirectory creates an empty wait directory for manual data inspection.
// The directory is intentionally empty - users must place their modified data here.
func CreateWaitDirectory(waitDir string) error {
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
