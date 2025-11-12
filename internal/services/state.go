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
)

// GetJobDir returns the directory path for a specific job
func GetJobDir(jobsBaseDir string, jobID string) string {
	return filepath.Join(jobsBaseDir, jobID)
}

// GetStateFilePath returns the full path to a job's state file
func GetStateFilePath(jobsBaseDir string, jobID string) string {
	return filepath.Join(GetJobDir(jobsBaseDir, jobID), StateFileName)
}

// LoadJobState reads a job's state from disk
// Returns error if file doesn't exist or can't be parsed
func LoadJobState(jobsBaseDir string, jobID string) (*models.PipelineJob, error) {
	statePath := GetStateFilePath(jobsBaseDir, jobID)

	// Read file
	data, err := os.ReadFile(statePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("job not found: %s", jobID)
		}
		return nil, fmt.Errorf("failed to read job state: %w", err)
	}

	// Parse JSON
	var job models.PipelineJob
	if err := json.Unmarshal(data, &job); err != nil {
		return nil, fmt.Errorf("failed to parse job state: %w", err)
	}

	// Validate loaded job
	if err := job.Validate(); err != nil {
		return nil, fmt.Errorf("invalid job state loaded from disk: %w", err)
	}

	return &job, nil
}

// SaveJobState writes a job's state to disk with atomic write
// Uses temp file + rename for atomicity (prevents corruption if process dies mid-write)
func SaveJobState(jobsBaseDir string, job *models.PipelineJob) error {
	// Validate job before saving
	if err := job.Validate(); err != nil {
		return fmt.Errorf("cannot save invalid job: %w", err)
	}

	// Ensure job directory exists
	jobDir := GetJobDir(jobsBaseDir, job.JobID)
	if err := os.MkdirAll(jobDir, 0755); err != nil {
		return fmt.Errorf("failed to create job directory: %w", err)
	}

	// Marshal to JSON with indentation for human readability
	data, err := json.MarshalIndent(job, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal job state: %w", err)
	}

	// Write to temporary file first (atomic write pattern)
	tempFile := filepath.Join(jobDir, fmt.Sprintf(".state.tmp.%s", uuid.New().String()))
	if err := os.WriteFile(tempFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write temp state file: %w", err)
	}

	// Atomic rename (overwrites existing state.json)
	statePath := GetStateFilePath(jobsBaseDir, job.JobID)
	if err := os.Rename(tempFile, statePath); err != nil {
		// Cleanup temp file on failure
		_ = os.Remove(tempFile)
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

		// Verify this is a valid job (has state.json)
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

	// Verify job exists before deleting
	if _, err := os.Stat(jobDir); os.IsNotExist(err) {
		return fmt.Errorf("job not found: %s", jobID)
	}

	// Remove entire job directory
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
		models.StepTorchImport:       filepath.Join(jobDir, "import"),
		models.StepLocalImport:       filepath.Join(jobDir, "import"),
		models.StepHttpImport:        filepath.Join(jobDir, "import"),
		models.StepDIMP:              filepath.Join(jobDir, "pseudonymized"),
		models.StepCSVConversion:     filepath.Join(jobDir, "csv"),
		models.StepParquetConversion: filepath.Join(jobDir, "parquet"),
	}

	// Create all directories
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
		return filepath.Join(jobDir, "pseudonymized")
	case models.StepCSVConversion:
		return filepath.Join(jobDir, "csv")
	case models.StepParquetConversion:
		return filepath.Join(jobDir, "parquet")
	default:
		return jobDir
	}
}
