package services

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/uuid"

	"github.com/medizininformatik-initiative/aether/internal/models"
)

const (
	StateFileName = "state.json"
	// JobLogFileName is the per-job log file stored in the job directory.
	JobLogFileName = "job.log"
)

// StateFileOps interface for file operations in state management, allowing mocking in tests
type StateFileOps interface {
	WriteFile(name string, data []byte, perm os.FileMode) error
	Rename(oldpath, newpath string) error
	Remove(name string) error
}

// defaultStateFileOps implements StateFileOps using the standard library
type defaultStateFileOps struct{}

func (d defaultStateFileOps) WriteFile(name string, data []byte, perm os.FileMode) error {
	return os.WriteFile(name, data, perm)
}
func (d defaultStateFileOps) Rename(oldpath, newpath string) error {
	return os.Rename(oldpath, newpath)
}
func (d defaultStateFileOps) Remove(name string) error { return os.Remove(name) }

// StateFileOpsProvider allows tests to inject mock file operations
var StateFileOpsProvider StateFileOps = defaultStateFileOps{}

// MarshalFunc is the function used to marshal job state to JSON (mockable for tests)
var MarshalFunc = func(v any, prefix, indent string) ([]byte, error) {
	return json.MarshalIndent(v, prefix, indent)
}

// GetJobDir returns the directory path for a specific job
func GetJobDir(jobsBaseDir string, jobID string) string {
	return filepath.Join(jobsBaseDir, jobID)
}

// GetStateFilePath returns the full path to a job's state file
func GetStateFilePath(jobsBaseDir string, jobID string) string {
	return filepath.Join(GetJobDir(jobsBaseDir, jobID), StateFileName)
}

// GetJobLogFilePath returns the full path to a job's log file.
func GetJobLogFilePath(jobsBaseDir string, jobID string) string {
	return filepath.Join(GetJobDir(jobsBaseDir, jobID), JobLogFileName)
}

// LoadJobState reads a job's state from disk
// Returns error if file doesn't exist or can't be parsed
func LoadJobState(jobsBaseDir string, jobID string) (*models.PipelineJob, error) {
	statePath := GetStateFilePath(jobsBaseDir, jobID)

	data, err := os.ReadFile(statePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("job not found: %s", jobID)
		}
		return nil, fmt.Errorf("failed to read job state: %w", err)
	}

	var job models.PipelineJob
	if err := json.Unmarshal(data, &job); err != nil {
		return nil, fmt.Errorf("failed to parse job state: %w", err)
	}

	// Backfill CRTDLPath for jobs saved before issue #286: when the positional
	// input was a CRTDL, InputSource held the CRTDL path. Preserve that linkage
	// so flattening still works on pre-existing jobs.
	if job.CRTDLPath == "" && job.InputType == models.InputTypeCRTDL {
		job.CRTDLPath = job.InputSource
	}

	if err := job.Validate(); err != nil {
		return nil, fmt.Errorf("invalid job state loaded from disk: %w", err)
	}

	return &job, nil
}

// SaveJobState writes a job's state to disk with atomic write
// Uses temp file + rename for atomicity (prevents corruption if process dies mid-write)
func SaveJobState(jobsBaseDir string, job *models.PipelineJob) error {
	if err := job.Validate(); err != nil {
		return fmt.Errorf("cannot save invalid job: %w", err)
	}

	jobDir := GetJobDir(jobsBaseDir, job.JobID)
	if err := os.MkdirAll(jobDir, 0755); err != nil {
		return fmt.Errorf("failed to create job directory: %w", err)
	}

	data, err := MarshalFunc(job, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal job state: %w", err)
	}

	tempFile := filepath.Join(jobDir, fmt.Sprintf(".state.tmp.%s", uuid.New().String()))
	if err := StateFileOpsProvider.WriteFile(tempFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write temp state file: %w", err)
	}

	statePath := GetStateFilePath(jobsBaseDir, job.JobID)
	if err := StateFileOpsProvider.Rename(tempFile, statePath); err != nil {
		_ = StateFileOpsProvider.Remove(tempFile)
		return fmt.Errorf("failed to save job state: %w", err)
	}

	return nil
}

// ListAllJobs scans the jobs directory and returns all job IDs
func ListAllJobs(jobsBaseDir string) ([]string, error) {
	entries, err := os.ReadDir(jobsBaseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("failed to read jobs directory: %w", err)
	}

	var jobIDs []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		jobID := entry.Name()
		statePath := GetStateFilePath(jobsBaseDir, jobID)
		if _, err := os.Stat(statePath); err == nil {
			jobIDs = append(jobIDs, jobID)
		}
	}

	return jobIDs, nil
}

// DeleteJob removes a job's directory and all its data
// WARNING: This is destructive and cannot be undone
func DeleteJob(jobsBaseDir string, jobID string) error {
	jobDir := GetJobDir(jobsBaseDir, jobID)

	if _, err := os.Stat(jobDir); os.IsNotExist(err) {
		return fmt.Errorf("job not found: %s", jobID)
	}

	if err := os.RemoveAll(jobDir); err != nil {
		return fmt.Errorf("failed to delete job: %w", err)
	}

	return nil
}

// EnsureJobDirs creates the standard directory structure for a job
// Returns the paths to each subdirectory
func EnsureJobDirs(jobsBaseDir string, jobID string) (map[models.StepName]string, error) {
	jobDir := GetJobDir(jobsBaseDir, jobID)

	dirs := map[models.StepName]string{
		models.StepTorchImport: filepath.Join(jobDir, "import"),
		models.StepLocalImport: filepath.Join(jobDir, "import"),
		models.StepHttpImport:  filepath.Join(jobDir, "import"),
		models.StepDIMP:        filepath.Join(jobDir, "dimp"),
		models.StepValidation:  filepath.Join(jobDir, "validation"),
		models.StepFlattening:  filepath.Join(jobDir, "csv"),
		models.StepSend:        filepath.Join(jobDir, "send"),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	return dirs, nil
}

// GetJobOutputDir returns the output directory for a specific step
func GetJobOutputDir(jobsBaseDir string, jobID string, step models.StepName) string {
	jobDir := GetJobDir(jobsBaseDir, jobID)

	switch step {
	case models.StepTorchImport, models.StepLocalImport, models.StepHttpImport:
		return filepath.Join(jobDir, "import")
	case models.StepDIMP:
		return filepath.Join(jobDir, "dimp")
	case models.StepValidation:
		return filepath.Join(jobDir, "validation")
	case models.StepFlattening:
		return filepath.Join(jobDir, "csv")
	case models.StepSend:
		return filepath.Join(jobDir, "send")
	case models.StepWait:
		// Wait step output depends on previous step - use GetWaitStepDir instead
		return jobDir
	default:
		return jobDir
	}
}

// GetWaitStepDir returns the wait directory based on the previous step's output
// For example, if previous step is torch (outputs to import/), returns import_wait/
func GetWaitStepDir(jobsBaseDir, jobID string, previousStep models.StepName) string {
	prevDir := GetJobOutputDir(jobsBaseDir, jobID, previousStep)
	return prevDir + "_wait"
}

// isTransparentStep returns true for steps that don't produce FHIR data output.
// These steps are skipped when determining the input directory for the next step.
// - Wait steps: pause pipeline for user inspection, no data transformation
// - Validation steps: produce only OperationOutcome reports, not FHIR data
func isTransparentStep(step models.StepName) bool {
	return step == models.StepWait || step == models.StepValidation
}

// GetStepInputDir returns the input directory for a step, considering transparent steps.
// Transparent steps (wait, validation) don't produce FHIR data, so subsequent steps
// need to look further back to find the actual data-producing step.
// Wait steps are special: they expose a _wait directory where users can modify data,
// so when encountered during the backwards walk we resolve through GetWaitStepDir.
func GetStepInputDir(jobsBaseDir, jobID string, steps []models.StepName, currentStepIndex int) string {
	if currentStepIndex <= 0 {
		return GetJobDir(jobsBaseDir, jobID)
	}

	// Walk backwards from the previous step to find the effective data source
	for i := currentStepIndex - 1; i >= 0; i-- {
		step := steps[i]

		if step == models.StepWait {
			// Wait step: find the data producer before it for the _wait directory
			for j := i - 1; j >= 0; j-- {
				if !isTransparentStep(steps[j]) {
					return GetWaitStepDir(jobsBaseDir, jobID, steps[j])
				}
			}
			return GetJobDir(jobsBaseDir, jobID)
		}

		if !isTransparentStep(step) {
			return GetJobOutputDir(jobsBaseDir, jobID, step)
		}
		// Transparent non-wait step (e.g. validation): keep walking back
	}

	return GetJobDir(jobsBaseDir, jobID)
}
