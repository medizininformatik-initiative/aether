package unit

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/pipeline"
)

// Helper to create a test job with given steps
func createJobForPipelineTests(steps []models.StepName) models.PipelineJob {
	now := time.Now()
	return models.PipelineJob{
		JobID:       "test-job-123",
		CreatedAt:   now,
		UpdatedAt:   now,
		InputSource: "/test/input",
		InputType:   models.InputTypeLocal,
		CurrentStep: string(steps[0]),
		Status:      models.JobStatusInProgress,
		Steps:       models.InitializeSteps(steps),
		Config: models.ProjectConfig{
			Pipeline: models.PipelineConfig{
				EnabledSteps: steps,
			},
		},
	}
}

// TestFailJob tests marking a job as failed
func TestFailJob(t *testing.T) {
	job := createJobForPipelineTests([]models.StepName{models.StepLocalImport, models.StepDIMP})

	errorMsg := "test error message"
	updatedJob := pipeline.FailJob(&job, errorMsg)

	assert.NotNil(t, updatedJob)
	assert.Equal(t, errorMsg, updatedJob.ErrorMessage)
	assert.Equal(t, models.JobStatusFailed, updatedJob.Status)
}

// TestGetCurrentStep tests retrieving the current step
func TestGetCurrentStep(t *testing.T) {
	tests := []struct {
		name      string
		jobSteps  []models.StepName
		wantFound bool
		wantStep  models.StepName
	}{
		{
			name:      "Valid current step",
			jobSteps:  []models.StepName{models.StepLocalImport, models.StepDIMP},
			wantFound: true,
			wantStep:  models.StepLocalImport,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := createJobForPipelineTests(tt.jobSteps)

			step, found := pipeline.GetCurrentStep(&job)

			assert.Equal(t, tt.wantFound, found)
			if found {
				assert.Equal(t, tt.wantStep, step.Name)
			}
		})
	}
}

// TestGetCurrentStep_NoCurrentStep tests when job has no current step
func TestGetCurrentStep_NoCurrentStep(t *testing.T) {
	job := createJobForPipelineTests([]models.StepName{models.StepLocalImport})
	job.CurrentStep = "" // Clear current step

	step, found := pipeline.GetCurrentStep(&job)

	assert.False(t, found)
	assert.Equal(t, models.PipelineStep{}, step)
}

// TestUpdateJobProgress tests updating job metrics
func TestUpdateJobProgress(t *testing.T) {
	job := createJobForPipelineTests([]models.StepName{models.StepLocalImport})

	files := 5
	bytes := int64(1024)
	updatedJob := pipeline.UpdateJobProgress(&job, files, bytes)

	assert.NotNil(t, updatedJob)
	assert.Equal(t, files, updatedJob.TotalFiles)
	assert.Equal(t, bytes, updatedJob.TotalBytes)
}

// TestIsJobComplete tests job completion check
func TestIsJobComplete(t *testing.T) {
	tests := []struct {
		name         string
		setupJob     func() models.PipelineJob
		wantComplete bool
	}{
		{
			name: "Incomplete job",
			setupJob: func() models.PipelineJob {
				return createJobForPipelineTests([]models.StepName{models.StepLocalImport, models.StepDIMP})
			},
			wantComplete: false,
		},
		{
			name: "Completed job",
			setupJob: func() models.PipelineJob {
				job := createJobForPipelineTests([]models.StepName{models.StepLocalImport})
				// Complete the import step
				step, _ := models.GetStepByName(job, models.StepLocalImport)
				step = models.CompleteStep(step, 10, 1000)
				job = models.ReplaceStep(job, step)
				return job
			},
			wantComplete: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := tt.setupJob()
			complete := pipeline.IsJobComplete(&job)
			assert.Equal(t, tt.wantComplete, complete)
		})
	}
}

// TestGetJobSummary tests generating a job summary
func TestGetJobSummary(t *testing.T) {
	job := createJobForPipelineTests([]models.StepName{models.StepLocalImport, models.StepDIMP})
	job.TotalFiles = 42
	job.TotalBytes = 12345

	summary := pipeline.GetJobSummary(&job)

	assert.Contains(t, summary, job.JobID)
	assert.Contains(t, summary, string(job.Status))
	assert.Contains(t, summary, job.CurrentStep)
	assert.Contains(t, summary, "42") // TotalFiles
}

// TestGetJobSummary_WithError tests summary with error message
func TestGetJobSummary_WithError(t *testing.T) {
	job := createJobForPipelineTests([]models.StepName{models.StepLocalImport})
	errorMsg := "test error occurred"
	job = *pipeline.FailJob(&job, errorMsg)

	summary := pipeline.GetJobSummary(&job)

	assert.Contains(t, summary, errorMsg)
	assert.Contains(t, summary, string(models.JobStatusFailed))
}

// TestAdvanceToNextStep_PrerequisiteNotMet tests advancing when prerequisite is not met
func TestAdvanceToNextStep_PrerequisiteNotMet(t *testing.T) {
	// Create a job with import -> dimp, but import not completed
	job := createJobForPipelineTests([]models.StepName{models.StepLocalImport, models.StepDIMP})

	// Try to advance directly to DIMP without completing import
	job.CurrentStep = string(models.StepLocalImport)
	// Mark import step as started but not completed
	step, _ := models.GetStepByName(job, models.StepLocalImport)
	now := time.Now()
	step.Status = models.StepStatusInProgress
	step.StartedAt = &now
	job = models.ReplaceStep(job, step)

	// Now try to advance to the next step (DIMP)
	// First, manually mark import as not started to simulate the prerequisite check
	step.Status = models.StepStatusPending
	step.StartedAt = nil
	job = models.ReplaceStep(job, step)

	_, err := pipeline.AdvanceToNextStep(&job)

	// This should fail because DIMP requires import to be completed
	require.Error(t, err)
	assert.Contains(t, err.Error(), "prerequisite")
}

// TestAdvanceToNextStep_NoMoreSteps tests completing the last step
func TestAdvanceToNextStep_NoMoreSteps(t *testing.T) {
	// Create a job with only one step
	job := createJobForPipelineTests([]models.StepName{models.StepLocalImport})
	job.CurrentStep = string(models.StepLocalImport)

	// Complete the import step
	step, _ := models.GetStepByName(job, models.StepLocalImport)
	step = models.CompleteStep(step, 10, 1000)
	job = models.ReplaceStep(job, step)

	// Advance to next step (should complete the job)
	updatedJob, err := pipeline.AdvanceToNextStep(&job)

	require.NoError(t, err, "Should advance without error")
	assert.NotNil(t, updatedJob)
	assert.Equal(t, models.JobStatusCompleted, updatedJob.Status, "Job should be completed")
	assert.Equal(t, "", updatedJob.CurrentStep, "Current step should be empty")
}

// TestStartJob_EmptySteps tests StartJob when there are no steps
func TestStartJob_EmptySteps(t *testing.T) {
	job := models.PipelineJob{
		JobID:       "test-job-123",
		Status:      models.JobStatusPending,
		Steps:       []models.PipelineStep{}, // Empty steps
		CurrentStep: "",
	}

	updatedJob := pipeline.StartJob(&job)

	// Job should be updated to in_progress even with empty steps
	assert.Equal(t, models.JobStatusInProgress, updatedJob.Status)
	// No steps to start, so Steps should remain empty
	assert.Len(t, updatedJob.Steps, 0)
}

// TestCompleteJob tests marking a job as completed
func TestCompleteJob(t *testing.T) {
	job := createJobForPipelineTests([]models.StepName{models.StepLocalImport})
	job.CurrentStep = string(models.StepLocalImport)

	updatedJob := pipeline.CompleteJob(&job)

	assert.Equal(t, models.JobStatusCompleted, updatedJob.Status)
	assert.Equal(t, "", updatedJob.CurrentStep, "Current step should be cleared")
}
