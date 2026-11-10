package pipeline

import (
	"fmt"
	"time"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

// CreateJob initializes a new pipeline job with the given jobID.
//
// Callers generate jobID and attach any per-job logging before calling, so
// CreateJob has no side effects on shared state such as the logger.
//
// crtdlPath is the CRTDL file attached to the job; every job carries a CRTDL
// so it is normally non-empty (the "" case is kept only for tests that
// exercise non-CRTDL paths). CreateJob does not validate the file. The caller
// must run services.VerifyCRTDLFile first, so that an invalid CRTDL stops the
// start before a job directory exists.
//
// inputSource is the import-step input:
//   - HTTP(S) URL (for http_import)
//   - TORCH result URL (auto-detected)
//   - Local directory path (for local_import)
//   - Empty string: torch_import submits the CRTDL to TORCH, or local_import
//     falls back to config.Services.LocalImport.Dir
//
// Returns the created job with initialized steps.
func CreateJob(jobID string, inputSource string, crtdlPath string, config models.ProjectConfig, logger *lib.Logger) (*models.PipelineJob, error) {
	inputType, err := detectJobInputType(inputSource, config.Services.LocalImport.Dir, logger)
	if err != nil {
		return nil, err
	}

	initialStep := firstImportStep(config.Pipeline.EnabledSteps)
	if initialStep == "" {
		return nil, fmt.Errorf("no import step enabled in pipeline configuration")
	}

	logger.Info("Determined initial step from config", "step", initialStep)

	// Reject a provided source whose detected type the enabled import step cannot
	// handle, so a mismatched job is never created. An empty source is the torch/
	// local default placeholder, not a user-chosen type, so it is not cross-checked
	// here; the import step's own validation reports a missing source clearly.
	if inputSource != "" && !models.ImportStepAcceptsInputType(initialStep, inputType) {
		return nil, fmt.Errorf("input %q was detected as %s, which the enabled import step %q does not accept (accepts %v)", inputSource, inputType, initialStep, models.AcceptedInputTypes(initialStep))
	}

	job := &models.PipelineJob{
		JobID:       jobID,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		InputSource: inputSource,
		InputType:   inputType,
		CRTDLPath:   crtdlPath,
		CurrentStep: string(initialStep),
		Status:      models.JobStatusPending,
		Steps:       models.InitializeSteps(config.Pipeline.EnabledSteps),
		Config:      config,
	}

	if err := job.Validate(); err != nil {
		return nil, fmt.Errorf("failed to create valid job: %w", err)
	}

	if err := persistNewJob(job, logger); err != nil {
		return nil, err
	}

	return job, nil
}

// detectJobInputType derives the input type from the provided source. An empty
// source means torch_import submits the CRTDL, or local_import uses
// localImportDir.
func detectJobInputType(inputSource string, localImportDir string, logger *lib.Logger) (models.InputType, error) {
	if inputSource == "" {
		logger.Info("No input source provided, using local import from config", "dir", localImportDir)
		return models.InputTypeLocal, nil
	}
	inputType, err := lib.DetectInputType(inputSource)
	if err != nil {
		return "", fmt.Errorf("failed to detect input type: %w", err)
	}
	logger.Info("Detected input type", "type", inputType, "source", inputSource)
	return inputType, nil
}

// firstImportStep returns the first enabled import step, or "" when none is enabled.
func firstImportStep(steps []models.StepName) models.StepName {
	for _, step := range steps {
		if len(models.AcceptedInputTypes(step)) > 0 {
			return step
		}
	}
	return ""
}

// persistNewJob creates the job directories and saves the job state. It
// prepares the effective CRTDL once so every downstream step reads the same
// enriched/copied file, then saves again so InputSource is up to date.
func persistNewJob(job *models.PipelineJob, logger *lib.Logger) error {
	jobsDir := job.Config.JobsDir
	if _, err := services.EnsureJobDirs(jobsDir, job.JobID); err != nil {
		return fmt.Errorf("failed to create job directories: %w", err)
	}

	if err := services.SaveJobState(jobsDir, job); err != nil {
		return fmt.Errorf("failed to save initial job state: %w", err)
	}

	if err := PrepareCRTDL(job, logger); err != nil {
		return fmt.Errorf("failed to prepare CRTDL: %w", err)
	}
	if err := services.SaveJobState(jobsDir, job); err != nil {
		return fmt.Errorf("failed to save job state after CRTDL preparation: %w", err)
	}
	return nil
}

// LoadJob loads an existing job from disk
func LoadJob(jobsDir string, jobID string) (*models.PipelineJob, error) {
	return services.LoadJobState(jobsDir, jobID)
}

// UpdateJob writes job state to disk and sets UpdatedAt.
//
// It writes nothing and gives no error if MarkJobStopped already recorded a stop
// for this job. The process is ending, thus the stop is the last state it writes.
func UpdateJob(jobsDir string, job *models.PipelineJob) error {
	stopMu.Lock()
	defer stopMu.Unlock()
	if isStopped(jobsDir, job.JobID) {
		return nil
	}
	return saveJob(jobsDir, job)
}

// saveJob writes the state file. The caller must hold stopMu.
func saveJob(jobsDir string, job *models.PipelineJob) error {
	job.UpdatedAt = time.Now()
	return services.SaveJobState(jobsDir, job)
}

// StartJob transitions job to in_progress status and starts first step
func StartJob(job *models.PipelineJob) *models.PipelineJob {
	// Update job status
	updatedJob := models.UpdateJobStatus(*job, models.JobStatusInProgress)

	// Start first step (should be import)
	if len(updatedJob.Steps) > 0 {
		firstStep := updatedJob.Steps[0]
		startedStep := models.StartStep(firstStep)
		updatedJob = models.ReplaceStep(updatedJob, startedStep)
	}

	return &updatedJob
}

// CompleteJob marks job as completed
func CompleteJob(job *models.PipelineJob) *models.PipelineJob {
	updatedJob := models.UpdateJobStatus(*job, models.JobStatusCompleted)
	updatedJob.CurrentStep = "" // No current step when complete
	return &updatedJob
}

// FailJob marks job as failed with error message
func FailJob(job *models.PipelineJob, errorMsg string) *models.PipelineJob {
	updatedJob := models.AddError(*job, errorMsg)
	return &updatedJob
}

// GetCurrentStep returns the current step being executed
func GetCurrentStep(job *models.PipelineJob) (models.PipelineStep, bool) {
	if job.CurrentStep == "" {
		return models.PipelineStep{}, false
	}

	stepName := models.StepName(job.CurrentStep)
	return models.GetStepByName(*job, stepName)
}

// AdvanceToNextStep moves job to the next enabled step
func AdvanceToNextStep(job *models.PipelineJob) (*models.PipelineJob, error) {
	currentStepName := models.StepName(job.CurrentStep)

	// Get next step from config
	nextStepName := job.Config.Pipeline.GetNextStep(currentStepName)

	if nextStepName == "" {
		// No more steps - job is complete
		return CompleteJob(job), nil
	}

	// Validate prerequisites before advancing
	// Note: This is a safety check. Normal execution should already ensure prerequisites.
	// This catches cases where user manually tries to run a step out of order.
	canRun, prerequisite := models.CanRunStep(*job, nextStepName)
	if !canRun {
		return nil, lib.ErrStepPrerequisiteNotMet(nextStepName, prerequisite)
	}

	// Update current step
	updatedJob := models.UpdateCurrentStep(*job, nextStepName)

	// Start the next step
	nextStep, found := models.GetStepByName(updatedJob, nextStepName)
	if !found {
		return nil, fmt.Errorf("next step not found: %s", nextStepName)
	}

	startedStep := models.StartStep(nextStep)
	updatedJob = models.ReplaceStep(updatedJob, startedStep)

	return &updatedJob, nil
}

// UpdateJobProgress updates total files and bytes processed
func UpdateJobProgress(job *models.PipelineJob, files int, bytes int64) *models.PipelineJob {
	updatedJob := models.UpdateJobMetrics(*job, files, bytes)
	return &updatedJob
}

// IsJobComplete checks if all steps are completed
func IsJobComplete(job *models.PipelineJob) bool {
	return models.IsJobComplete(*job)
}

// GetJobSummary returns a human-readable summary of the job. The caller gives
// the effective status and its text, because the shown status can differ from
// the persisted one.
func GetJobSummary(job *models.PipelineJob, status models.JobStatus, statusText string) string {
	duration := models.JobDuration(*job, status, time.Now())

	summary := fmt.Sprintf("Job %s\n", job.JobID)
	summary += fmt.Sprintf("Status: %s\n", statusText)
	summary += fmt.Sprintf("Current Step: %s\n", job.CurrentStep)
	summary += fmt.Sprintf("Files: %d\n", job.TotalFiles)
	summary += fmt.Sprintf("Duration: %v\n", duration.Round(time.Second))

	if job.ErrorMessage != "" {
		summary += fmt.Sprintf("Error: %s\n", job.ErrorMessage)
	}

	return summary
}
