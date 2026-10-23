package pipeline

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

// The signal handler and the run loop write one state file from two goroutines.
// stopMu makes each read-modify-write of the job status atomic, and stoppedJobs
// holds the jobs that reached their final state.
//
// After the handler writes `stopped`, the process is about to end. A step that
// finishes at that moment must not put the job back to in_progress, and it must
// not overwrite the stop with a snapshot it read before the stop. Thus every
// later write to that job is dropped.
//
// submits holds the TORCH submits in progress. The state file has no handle
// for such a submit yet, thus a stop takes the TORCH job ID from the channel.
var (
	stopMu      sync.Mutex
	stoppedJobs = map[string]bool{}
	submits     = map[string]chan string{}
)

// ErrJobStopped tells that a step did not start work on TORCH, because the user
// stopped the job.
var ErrJobStopped = errors.New("job is stopped")

// MarkJobStopped records that the user stopped the job process.
//
// It re-reads the state file instead of using the in-memory job, because the run
// loop owns that value and keeps writing to it. Only an in_progress job changes:
// a signal that arrives after the loop settled the job must not overwrite the
// real outcome.
func MarkJobStopped(jobsDir string, jobID string) error {
	_, _, err := markJobStopped(jobsDir, jobID)
	return err
}

// markJobStopped returns the job as it was before the stop, or nil if the job
// was not in progress. It also returns the channel of a TORCH submit in
// progress, or nil.
func markJobStopped(jobsDir string, jobID string) (*models.PipelineJob, <-chan string, error) {
	stopMu.Lock()
	defer stopMu.Unlock()

	job, err := LoadJob(jobsDir, jobID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load job to stop it: %w", err)
	}
	if job.Status != models.JobStatusInProgress {
		return nil, nil, nil
	}

	update := models.UpdateJobStatus(*job, models.JobStatusStopped)
	if err := saveJob(jobsDir, &update); err != nil {
		return nil, nil, fmt.Errorf("failed to save stopped job state: %w", err)
	}
	key := stopKey(jobsDir, jobID)
	stoppedJobs[key] = true
	return job, submits[key], nil
}

// beginSubmit registers a TORCH submit for the job, or returns ErrJobStopped.
// The caller must call the returned function with the new TORCH job ID, or
// with "" if the submit failed, after it persisted the handle.
func beginSubmit(jobsDir string, jobID string) (func(torchJobID string), error) {
	stopMu.Lock()
	defer stopMu.Unlock()

	key := stopKey(jobsDir, jobID)
	if isStopped(jobsDir, jobID) {
		return nil, ErrJobStopped
	}
	ch := make(chan string, 1)
	submits[key] = ch
	return func(torchJobID string) {
		stopMu.Lock()
		delete(submits, key)
		stopMu.Unlock()
		ch <- torchJobID
		close(ch)
	}, nil
}

// StopJobAndCancelExtraction records the stop as MarkJobStopped does, and then
// cancels the TORCH job that the torch step waits for. The handle stays in the
// state file: for the cancelled TORCH job, `pipeline continue` gets 410 Gone and
// submits a new extraction.
//
// The stop comes first: the run loop of this process also gets 410 Gone after
// the cancel, and it must not submit a new extraction.
func StopJobAndCancelExtraction(ctx context.Context, jobsDir string, jobID string, logger *lib.Logger) error {
	job, submitted, err := markJobStopped(jobsDir, jobID)
	if err != nil {
		return err
	}
	if job == nil {
		return nil
	}

	torchJobID := ""
	switch {
	case submitted != nil:
		select {
		case torchJobID = <-submitted:
		case <-ctx.Done():
			return fmt.Errorf("TORCH submit did not end: %w", ctx.Err())
		}
	case torchExtractionRunning(job):
		torchJobID = job.TORCHJobID
	}
	if torchJobID == "" {
		return nil
	}

	logger.Info("Cancelling TORCH job", "torch_job_id", torchJobID)
	torchConfig := job.Config.Services.TORCH
	httpClient := services.NewHTTPClient(torchConfig.EffectiveRequestTimeout(), job.Config.Retry, job.Config.TLS, logger)
	if err := services.NewTORCHClient(torchConfig, httpClient, logger).CancelExtraction(ctx, torchJobID); err != nil {
		return fmt.Errorf("failed to cancel TORCH job %s: %w", torchJobID, err)
	}
	return nil
}

// torchExtractionRunning tells if the job waits for a TORCH job that it
// submitted.
func torchExtractionRunning(job *models.PipelineJob) bool {
	step, found := models.GetStepByName(*job, models.StepTorchImport)
	return job.CurrentStep == string(models.StepTorchImport) &&
		found && step.Status == models.StepStatusInProgress &&
		job.TORCHJobID != ""
}

// jobStopped tells if MarkJobStopped recorded a stop for the job.
func jobStopped(jobsDir string, jobID string) bool {
	stopMu.Lock()
	defer stopMu.Unlock()
	return isStopped(jobsDir, jobID)
}

// ResetStoppedJobsForTesting forgets every recorded stop, as a new process does.
func ResetStoppedJobsForTesting() {
	stopMu.Lock()
	defer stopMu.Unlock()
	stoppedJobs = map[string]bool{}
	submits = map[string]chan string{}
}

// isStopped is jobStopped for a caller that holds stopMu.
func isStopped(jobsDir string, jobID string) bool {
	return stoppedJobs[stopKey(jobsDir, jobID)]
}

// stopKey identifies the state file, because one job ID can name different jobs
// in different job directories.
func stopKey(jobsDir string, jobID string) string {
	return services.GetStateFilePath(jobsDir, jobID)
}
