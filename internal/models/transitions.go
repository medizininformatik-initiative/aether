package models

import "time"

// UpdateJobStatus creates a new PipelineJob with updated status
// Pure function - returns new instance, does not mutate original
// Deep copies slice fields to ensure full immutability
func UpdateJobStatus(job PipelineJob, status JobStatus) PipelineJob {
	// Deep copy steps slice to prevent shared references
	newSteps := make([]PipelineStep, len(job.Steps))
	copy(newSteps, job.Steps)

	job.Steps = newSteps
	job.Status = status
	job.UpdatedAt = time.Now()
	return job
}

// UpdateCurrentStep creates a new PipelineJob with updated current step
// Pure function - returns new instance
// Deep copies slice fields to ensure full immutability
func UpdateCurrentStep(job PipelineJob, step StepName) PipelineJob {
	// Deep copy steps slice to prevent shared references
	newSteps := make([]PipelineStep, len(job.Steps))
	copy(newSteps, job.Steps)

	job.Steps = newSteps
	job.CurrentStep = string(step)
	job.UpdatedAt = time.Now()
	return job
}

// AddError creates a new PipelineJob with error message
// Pure function - returns new instance
// Deep copies slice fields to ensure full immutability
func AddError(job PipelineJob, errorMsg string) PipelineJob {
	// Deep copy steps slice to prevent shared references
	newSteps := make([]PipelineStep, len(job.Steps))
	copy(newSteps, job.Steps)

	job.Steps = newSteps
	job.ErrorMessage = errorMsg
	job.Status = JobStatusFailed
	job.UpdatedAt = time.Now()
	return job
}

// UpdateJobMetrics creates a new PipelineJob with updated file/byte counts
// Pure function - returns new instance
// Deep copies slice fields to ensure full immutability
func UpdateJobMetrics(job PipelineJob, files int, bytes int64) PipelineJob {
	// Deep copy steps slice to prevent shared references
	newSteps := make([]PipelineStep, len(job.Steps))
	copy(newSteps, job.Steps)

	job.Steps = newSteps
	job.TotalFiles = files
	job.TotalBytes = bytes
	job.UpdatedAt = time.Now()
	return job
}

// CanTransitionTo reports whether a step may legally move from one status to
// another. It is the single gate consulted before every step-status mutation.
// in_progress -> in_progress is allowed so a step left in_progress by a killed
// process can be re-entered on resume.
func CanTransitionTo(from, to StepStatus) bool {
	switch from {
	case StepStatusPending:
		return to == StepStatusInProgress
	case StepStatusInProgress:
		return to == StepStatusInProgress || to == StepStatusCompleted ||
			to == StepStatusFailed || to == StepStatusWaiting
	case StepStatusFailed:
		return to == StepStatusInProgress
	case StepStatusWaiting:
		return to == StepStatusInProgress || to == StepStatusCompleted
	default: // StepStatusCompleted is terminal
		return false
	}
}

// StartStep creates a new PipelineStep with in_progress status
// Pure function - returns new instance
func StartStep(step PipelineStep) PipelineStep {
	now := time.Now()
	step.Status = StepStatusInProgress
	step.StartedAt = &now
	return step
}

// CompleteStep creates a new PipelineStep with completed status
// Pure function - returns new instance
func CompleteStep(step PipelineStep, filesProcessed int, bytesProcessed int64) PipelineStep {
	now := time.Now()
	step.Status = StepStatusCompleted
	step.CompletedAt = &now
	step.FilesProcessed = filesProcessed
	step.BytesProcessed = bytesProcessed
	return step
}

// FailStep creates a new PipelineStep with failed status and error details
// Pure function - returns new instance
func FailStep(step PipelineStep, errorType ErrorType, errorMsg string, httpStatus int) PipelineStep {
	step.Status = StepStatusFailed
	step.LastError = &StepError{
		Type:       errorType,
		Message:    errorMsg,
		HTTPStatus: httpStatus,
		Timestamp:  time.Now(),
	}
	return step
}

// ResetStep returns a copy of the step reset to pending, clearing every run
// artifact (timestamps, error, and processed counts) so a re-run starts clean.
// Pure function - returns new instance.
func ResetStep(step PipelineStep) PipelineStep {
	step.Status = StepStatusPending
	step.StartedAt = nil
	step.CompletedAt = nil
	step.FilesProcessed = 0
	step.BytesProcessed = 0
	step.LastError = nil
	return step
}

// ResetStepAndDownstream resets the named step and every step ordered after it to
// pending: a re-run invalidates the output each later step consumed, so those results
// are stale until re-run (we do not diff outputs — conservative). job.Steps mirrors
// EnabledSteps order. Unknown step returns the job unchanged. Pure function.
func ResetStepAndDownstream(job PipelineJob, name StepName) PipelineJob {
	idx := -1
	for i, step := range job.Steps {
		if step.Name == name {
			idx = i
			break
		}
	}
	if idx == -1 {
		return job
	}

	newSteps := make([]PipelineStep, len(job.Steps))
	copy(newSteps, job.Steps)
	for i := idx; i < len(newSteps); i++ {
		newSteps[i] = ResetStep(newSteps[i])
	}

	job.Steps = newSteps
	job.UpdatedAt = time.Now()
	return job
}

// UpdateStepProgress creates a new PipelineStep with updated progress metrics
// Pure function - returns new instance
func UpdateStepProgress(step PipelineStep, filesProcessed int, bytesProcessed int64) PipelineStep {
	step.FilesProcessed = filesProcessed
	step.BytesProcessed = bytesProcessed
	return step
}

// ReplaceStep replaces a step in the job's step list
// Pure function - returns new job instance with updated steps
func ReplaceStep(job PipelineJob, updatedStep PipelineStep) PipelineJob {
	newSteps := make([]PipelineStep, len(job.Steps))
	copy(newSteps, job.Steps)

	for i, step := range newSteps {
		if step.Name == updatedStep.Name {
			newSteps[i] = updatedStep
			break
		}
	}

	job.Steps = newSteps
	job.UpdatedAt = time.Now()
	return job
}

// InitializeSteps creates initial step list from enabled steps config
// Pure function - creates new step instances
func InitializeSteps(enabledSteps []StepName) []PipelineStep {
	steps := make([]PipelineStep, len(enabledSteps))
	for i, stepName := range enabledSteps {
		steps[i] = PipelineStep{
			Name:           stepName,
			Status:         StepStatusPending,
			StartedAt:      nil,
			CompletedAt:    nil,
			FilesProcessed: 0,
			BytesProcessed: 0,
			LastError:      nil,
		}
	}
	return steps
}

// GetStepByName finds a step by name in the job's step list
// Pure function - returns copy of step if found
func GetStepByName(job PipelineJob, name StepName) (PipelineStep, bool) {
	for _, step := range job.Steps {
		if step.Name == name {
			return step, true
		}
	}
	return PipelineStep{}, false
}

// IsJobComplete checks if all steps are completed
// Pure function - no mutations
func IsJobComplete(job PipelineJob) bool {
	if len(job.Steps) == 0 {
		return false
	}

	for _, step := range job.Steps {
		if step.Status != StepStatusCompleted {
			return false
		}
	}
	return true
}

// DeriveJobStatus computes job status purely from its steps, so status is derived
// rather than left stale after out-of-band mutations (e.g. `job run --step X [--force]`).
// Precedence:
//   - any failed           -> failed
//   - all completed        -> completed
//   - any underway/partial -> in_progress
//   - otherwise (none, or all pending) -> pending
//
// Pure function - no mutations.
func DeriveJobStatus(job PipelineJob) JobStatus {
	anyFailed, anyUnderway, anyCompleted, anyPending := false, false, false, false
	for _, step := range job.Steps {
		switch step.Status {
		case StepStatusFailed:
			anyFailed = true
		case StepStatusInProgress, StepStatusWaiting:
			anyUnderway = true
		case StepStatusCompleted:
			anyCompleted = true
		default: // StepStatusPending
			anyPending = true
		}
	}

	switch {
	case anyFailed:
		return JobStatusFailed
	case anyCompleted && !anyPending && !anyUnderway:
		return JobStatusCompleted
	case anyUnderway || anyCompleted:
		return JobStatusInProgress
	default:
		return JobStatusPending
	}
}

// GetNextPendingStep finds the first pending step in the job
// Pure function - returns copy of step if found
func GetNextPendingStep(job PipelineJob) (PipelineStep, bool) {
	for _, step := range job.Steps {
		if step.Status == StepStatusPending {
			return step, true
		}
	}
	return PipelineStep{}, false
}
