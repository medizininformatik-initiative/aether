package pipeline

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

// CreateJob initializes a new pipeline job
// Returns the created job with generated UUID and initialized steps
func CreateJob(inputSource string, config models.ProjectConfig, logger *lib.Logger) (*models.PipelineJob, error) {
	// Generate unique job ID
	jobID := uuid.New().String()

	// Detect input type using enhanced detection
	inputType, err := lib.DetectInputType(inputSource)
	if err != nil {
		return nil, fmt.Errorf("failed to detect input type: %w", err)
	}

	logger.Info("Detected input type", "type", inputType, "source", inputSource)

	// Validate CRTDL syntax if input is CRTDL file
	if inputType == models.InputTypeCRTDL {
		if err := lib.ValidateCRTDLSyntax(inputSource); err != nil {
			return nil, fmt.Errorf("CRTDL validation failed: %w", err)
		}
		logger.Info("CRTDL syntax validation passed")
	}

	// Determine initial step based on input type
	var initialStep models.StepName
	switch inputType {
	case models.InputTypeCRTDL, models.InputTypeTORCHURL:
		initialStep = models.StepTorchImport
	case models.InputTypeLocal:
		initialStep = models.StepLocalImport
	case models.InputTypeHTTP:
		initialStep = models.StepHttpImport
	default:
		return nil, fmt.Errorf("unknown input type: %s", inputType)
	}

	// Initialize steps from config
	steps := models.InitializeSteps(config.Pipeline.EnabledSteps)

	// Create job
	job := &models.PipelineJob{
		JobID:              jobID,
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
		InputSource:        inputSource,
		InputType:          inputType,
		TORCHExtractionURL: "",                  // Will be set during TORCH extraction if applicable
		CurrentStep:        string(initialStep), // Set based on input type
		Status:             models.JobStatusPending,
		Steps:              steps,
		Config:             config,
		TotalFiles:         0,
		TotalBytes:         0,
		ErrorMessage:       "",
	}

	// Validate the job
	if err := job.Validate(); err != nil {
		return nil, fmt.Errorf("failed to create valid job: %w", err)
	}

	// Create job directory structure
	if _, err := services.EnsureJobDirs(config.JobsDir, jobID); err != nil {
		return nil, fmt.Errorf("failed to create job directories: %w", err)
	}

	// Save initial job state
	if err := services.SaveJobState(config.JobsDir, job); err != nil {
		return nil, fmt.Errorf("failed to save initial job state: %w", err)
	}

	return job, nil
}

// LoadJob loads an existing job from disk
func LoadJob(jobsDir string, jobID string) (*models.PipelineJob, error) {
	return services.LoadJobState(jobsDir, jobID)
}

// UpdateJob updates job state on disk
// Uses pure functions to create new job instance before saving
func UpdateJob(jobsDir string, job *models.PipelineJob) error {
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
	canRun, prerequisite := lib.CanRunStep(*job, nextStepName)
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

// GetJobSummary returns a human-readable summary of the job
func GetJobSummary(job *models.PipelineJob) string {
	duration := time.Since(job.CreatedAt)

	summary := fmt.Sprintf("Job %s\n", job.JobID)
	summary += fmt.Sprintf("Status: %s\n", job.Status)
	summary += fmt.Sprintf("Current Step: %s\n", job.CurrentStep)
	summary += fmt.Sprintf("Files: %d\n", job.TotalFiles)
	summary += fmt.Sprintf("Duration: %v\n", duration.Round(time.Second))

	if job.ErrorMessage != "" {
		summary += fmt.Sprintf("Error: %s\n", job.ErrorMessage)
	}

	return summary
}
