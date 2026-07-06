package services

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/medizininformatik-initiative/aether/internal/models"
)

// waitDirSuffix is appended to a step's output directory to form the wait
// override directory where a paused pipeline reads manually-staged data.
const waitDirSuffix = "_wait"

// stepDirNames maps each data-producing step to its subdirectory name under the
// job directory — the one place these names are defined. Steps absent from the
// map (e.g. wait) own no output directory and resolve to the job root.
var stepDirNames = map[models.StepName]string{
	models.StepTorchImport: "import",
	models.StepLocalImport: "import",
	models.StepHttpImport:  "import",
	models.StepDIMP:        "dimp",
	models.StepValidation:  "validation",
	models.StepFlattening:  "csv",
	models.StepSend:        "send",
}

// JobLayout resolves the on-disk directories for a job's pipeline steps. It is
// the single source of truth for the job directory layout; every method is pure
// (no filesystem access) so the layout is unit-testable without a real disk.
type JobLayout struct {
	jobsBaseDir  string
	jobID        string
	enabledSteps []models.StepName
}

// NewJobLayout builds a resolver for a job. enabledSteps is the ordered pipeline
// (job.Config.Pipeline.EnabledSteps) and may be nil when only OutputDir is needed.
func NewJobLayout(jobsBaseDir, jobID string, enabledSteps []models.StepName) JobLayout {
	return JobLayout{jobsBaseDir: jobsBaseDir, jobID: jobID, enabledSteps: enabledSteps}
}

// NewJobLayoutForDir builds a resolver from a job's root directory, splitting it
// into its base directory and job ID. Use when the caller holds the job root path
// rather than the base dir and ID separately.
func NewJobLayoutForDir(jobDir string, enabledSteps []models.StepName) JobLayout {
	return NewJobLayout(filepath.Dir(jobDir), filepath.Base(jobDir), enabledSteps)
}

// JobDir returns the root directory for the job.
func (l JobLayout) JobDir() string {
	return GetJobDir(l.jobsBaseDir, l.jobID)
}

// OutputDir returns the directory a step writes its output to. Steps without a
// dedicated output directory resolve to the job root.
func (l JobLayout) OutputDir(step models.StepName) string {
	if name, ok := stepDirNames[step]; ok {
		return filepath.Join(l.JobDir(), name)
	}
	return l.JobDir()
}

// ViewDefinitionsDir returns the directory the flattening step writes its
// generated SQL-on-FHIR ViewDefinitions to.
func (l JobLayout) ViewDefinitionsDir() string {
	return filepath.Join(l.JobDir(), "viewdefinitions")
}

// WaitDir returns the override directory for the wait step at waitStepIndex.
// The directory is named after the step the wait immediately follows, matching
// the path the paused pipeline tells the user to populate. It errors when the
// wait has no data-producing predecessor (e.g. wait as the first step).
func (l JobLayout) WaitDir(waitStepIndex int) (string, error) {
	prev, err := WaitSourceStep(l.enabledSteps, waitStepIndex)
	if err != nil {
		return "", err
	}
	return l.OutputDir(prev) + waitDirSuffix, nil
}

// WaitSourceStep returns the non-wait step immediately preceding the wait step
// at waitStepIndex — the step whose output directory the wait override shadows.
func WaitSourceStep(enabledSteps []models.StepName, waitStepIndex int) (models.StepName, error) {
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

// isTransparentStep reports steps that don't produce FHIR data output, so the
// next step must look further back to find the actual data-producing step.
//   - Wait steps: pause the pipeline for user inspection, no data transformation.
//   - Validation steps: produce only OperationOutcome reports, not FHIR data.
func isTransparentStep(step models.StepName) bool {
	return step == models.StepWait || step == models.StepValidation
}

// InputDir returns the directory a step reads its input from, resolving
// transparent steps and wait overrides. It locates the step within the
// configured pipeline; an unknown step resolves to the job root.
func (l JobLayout) InputDir(step models.StepName) string {
	for i, s := range l.enabledSteps {
		if s == step {
			return l.inputDirAt(i)
		}
	}
	return l.JobDir()
}

// inputDirAt resolves the input directory for the step at currentStepIndex by
// walking backwards to the effective data source. A wait override is named after
// the step it immediately follows — the same directory the paused pipeline tells
// the user to populate — so transparent steps are not skipped across a wait.
func (l JobLayout) inputDirAt(currentStepIndex int) string {
	if currentStepIndex <= 0 {
		return l.JobDir()
	}

	for i := currentStepIndex - 1; i >= 0; i-- {
		step := l.enabledSteps[i]

		if step == models.StepWait {
			if dir, err := l.WaitDir(i); err == nil {
				return dir
			}
			return l.JobDir()
		}

		if !isTransparentStep(step) {
			return l.OutputDir(step)
		}
	}

	return l.JobDir()
}

// EnsureJobDirs creates the standard output directory for every data-producing
// step and returns each step's output path.
func EnsureJobDirs(jobsBaseDir, jobID string) (map[models.StepName]string, error) {
	layout := NewJobLayout(jobsBaseDir, jobID, nil)
	dirs := make(map[models.StepName]string, len(stepDirNames))
	for step := range stepDirNames {
		dir := layout.OutputDir(step)
		dirs[step] = dir
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}
	return dirs, nil
}

// GetJobOutputDir returns the output directory for a specific step.
func GetJobOutputDir(jobsBaseDir, jobID string, step models.StepName) string {
	return NewJobLayout(jobsBaseDir, jobID, nil).OutputDir(step)
}

// GetWaitStepDir returns the wait override directory derived from a producing
// step's output (e.g. import -> import_wait).
func GetWaitStepDir(jobsBaseDir, jobID string, previousStep models.StepName) string {
	return NewJobLayout(jobsBaseDir, jobID, nil).OutputDir(previousStep) + waitDirSuffix
}

// GetStepInputDir returns the input directory for the step at currentStepIndex,
// resolving transparent steps and wait overrides.
func GetStepInputDir(jobsBaseDir, jobID string, steps []models.StepName, currentStepIndex int) string {
	return NewJobLayout(jobsBaseDir, jobID, steps).inputDirAt(currentStepIndex)
}
