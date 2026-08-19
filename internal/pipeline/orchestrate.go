package pipeline

import (
	"errors"
	"fmt"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

// RunOptions carries orchestration-loop settings.
type RunOptions struct {
	NoProgress bool
}

// RunLoop drives a job from its current step to completion or a wait-step pause.
// It is the single loop shared by pipeline start and continue: it claims the job
// as in_progress, executes the current step through runStep, persists after the
// step, advances to the next, and completes the job when no step remains. On
// ErrPaused it stops cleanly (the job becomes waiting); on any other step error
// it fails the job. UpdateJob drops every write after a recorded stop.
//
// The job must already be positioned at the step to run (CurrentStep set). Fake
// steps injected via SetStepRegistryForTesting make this loop unit-testable
// without real services.
func RunLoop(job *models.PipelineJob, logger *lib.Logger, opts RunOptions) (*models.PipelineJob, error) {
	for {
		stepName := models.StepName(job.CurrentStep)
		step, ok := stepLookup(stepName)
		if !ok {
			return job, fmt.Errorf("unknown step: %s", stepName)
		}

		// A resumed job carries the status that stopped it (stopped or waiting).
		// The loop owns the job while a step runs, so claim it before the step
		// starts: state persisted during the step must not report the old stop.
		if job.Status != models.JobStatusInProgress {
			running := models.UpdateJobStatus(*job, models.JobStatusInProgress)
			job = &running
			if err := UpdateJob(job.Config.JobsDir, job); err != nil {
				return job, fmt.Errorf("failed to save job state: %w", err)
			}
		}

		ctx := &StepContext{
			Job:          job,
			Layout:       services.NewJobLayout(job.Config.JobsDir, job.JobID, job.Config.Pipeline.EnabledSteps),
			Logger:       logger,
			ShowProgress: !opts.NoProgress,
		}

		if err := runStep(step, ctx); err != nil {
			if errors.Is(err, ErrPaused) {
				paused := models.UpdateJobStatus(*job, models.JobStatusWaiting)
				job = &paused
				_ = UpdateJob(job.Config.JobsDir, job)
				return job, ErrPaused
			}
			failed := FailJob(job, err.Error())
			if saveErr := UpdateJob(job.Config.JobsDir, failed); saveErr != nil {
				logger.Error("Failed to save failed job state", "error", saveErr)
			}
			return failed, err
		}

		if err := UpdateJob(job.Config.JobsDir, job); err != nil {
			return job, fmt.Errorf("failed to save job state: %w", err)
		}

		if job.Config.Pipeline.GetNextStep(stepName) == "" {
			completed := CompleteJob(job)
			if err := UpdateJob(job.Config.JobsDir, completed); err != nil {
				return completed, fmt.Errorf("failed to save job state: %w", err)
			}
			return completed, nil
		}

		advanced, err := AdvanceToNextStep(job)
		if err != nil {
			return job, fmt.Errorf("failed to advance to next step: %w", err)
		}
		job = advanced
		if err := UpdateJob(job.Config.JobsDir, job); err != nil {
			return job, fmt.Errorf("failed to save job state: %w", err)
		}
	}
}
