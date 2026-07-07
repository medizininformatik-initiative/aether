package pipeline

import (
	"errors"
	"fmt"
	"time"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

// Step is the seam every pipeline step implements. Run performs only the
// step-specific work; the shared lifecycle (enabled-check, status transition,
// timing, error classification) lives once in runStep.
type Step interface {
	Name() models.StepName
	Run(ctx *StepContext) (StepResult, error)
}

// StepContext is the parameter block passed to a Step's Run. The Execute*Step
// wrappers and the orchestration loop build it — resolving the JobLayout once so
// no Run re-derives the job's directory layout — and Run never constructs it. Not
// every field applies to every step (HTTPClient and ShowProgress are import-only).
type StepContext struct {
	Job    *models.PipelineJob
	Layout services.JobLayout // resolved once by the caller; the single source for a step's directories
	Logger *lib.Logger

	// HTTPClient is populated only by the import wrapper (whose signature takes
	// one); other steps build their own client internally and ignore this.
	HTTPClient   *services.HTTPClient
	ShowProgress bool
}

// StepResult reports what a step produced, for the completion transition.
type StepResult struct {
	FilesProcessed int
	BytesProcessed int64
}

// ErrPaused is returned by a Step.Run (currently only the wait step) to signal
// that the pipeline should pause at StepStatusWaiting instead of completing or
// failing the step.
var ErrPaused = errors.New("pipeline paused at wait step")

// stopError signals that a step finished its work successfully but the pipeline
// must halt (e.g. validation found data-quality issues with fail_on_error). The
// step is marked completed; the wrapped error propagates to stop the loop.
type stopError struct {
	result StepResult
	err    error
}

func (e *stopError) Error() string { return e.err.Error() }
func (e *stopError) Unwrap() error { return e.err }

// stopAfterCompletion wraps err so runStep completes the step (recording result)
// yet still returns err to halt the pipeline.
func stopAfterCompletion(result StepResult, err error) error {
	return &stopError{result: result, err: err}
}

// errorClassifier is the optional hook a Step implements when it needs a
// classification rule other than the shared classifyServiceError (the import
// step preserves its step-name-gated rule this way).
type errorClassifier interface {
	ClassifyError(error) models.ErrorType
}

// RunStep resolves the step registered for name and drives it through the shared
// lifecycle. It is the single exported entrypoint for running one pipeline step,
// used by the manual `job run` path and the black-box tests; the orchestration
// loop resolves via stepLookup and calls runStep directly.
func RunStep(name models.StepName, ctx *StepContext) error {
	step, ok := stepLookup(name)
	if !ok {
		return fmt.Errorf("unknown step: %s", name)
	}
	return runStep(step, ctx)
}

// runStep is the single step lifecycle. It resolves nothing about the step's
// work — that is Run's job — but owns every status transition, gating each one
// through models.CanTransitionTo so an illegal transition fails loudly.
func runStep(step Step, ctx *StepContext) error {
	name := step.Name()

	if !isStepEnabled(ctx.Job.Config, name) {
		ctx.Logger.Info(string(name)+" step not enabled, skipping", "job_id", ctx.Job.JobID)
		return nil
	}

	current := getOrCreateStep(ctx.Job, name)
	if err := transitionStep(current, models.StepStatusInProgress); err != nil {
		return err
	}
	// Preserve the original start time across re-entries (a resumed wait step, or
	// each `pipeline continue` poll, would otherwise lose its first-arrival time).
	firstStarted := current.StartedAt
	*current = models.StartStep(*current)
	if firstStarted != nil {
		current.StartedAt = firstStarted
	}
	lib.LogStepStart(ctx.Logger, string(name), ctx.Job.JobID)

	startTime := time.Now()
	result, err := step.Run(ctx)

	if errors.Is(err, ErrPaused) {
		if terr := transitionStep(current, models.StepStatusWaiting); terr != nil {
			return terr
		}
		current.Status = models.StepStatusWaiting
		return ErrPaused
	}

	var stop *stopError
	if errors.As(err, &stop) {
		if terr := completeStep(current, name, ctx, stop.result, startTime); terr != nil {
			return terr
		}
		return stop.err
	}

	if err != nil {
		errType := classifyServiceError(err)
		if c, ok := step.(errorClassifier); ok {
			errType = c.ClassifyError(err)
		}
		lib.LogStepFailed(ctx.Logger, string(name), ctx.Job.JobID, err, errType == models.ErrorTypeTransient)
		if terr := transitionStep(current, models.StepStatusFailed); terr != nil {
			return terr
		}
		*current = models.FailStep(*current, errType, err.Error(), 0)
		return err
	}

	return completeStep(current, name, ctx, result, startTime)
}

// completeStep applies the shared completion transition: gate to completed, record
// the result, and log completion. Both the normal path and the stop-after-completion
// path funnel through it.
func completeStep(current *models.PipelineStep, name models.StepName, ctx *StepContext, result StepResult, startTime time.Time) error {
	if terr := transitionStep(current, models.StepStatusCompleted); terr != nil {
		return terr
	}
	*current = models.CompleteStep(*current, result.FilesProcessed, result.BytesProcessed)
	lib.LogStepComplete(ctx.Logger, string(name), ctx.Job.JobID, result.FilesProcessed, time.Since(startTime))
	return nil
}

// transitionStep rejects an illegal status change before it is applied.
func transitionStep(step *models.PipelineStep, to models.StepStatus) error {
	if !models.CanTransitionTo(step.Status, to) {
		return fmt.Errorf("illegal transition for step %s: %s -> %s", step.Name, step.Status, to)
	}
	return nil
}
