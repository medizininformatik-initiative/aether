package pipeline

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

// A stop and the run loop write one state file from two goroutines.
// stopMu makes each read-modify-write of the job status atomic, and stoppedJobs
// holds the jobs that reached their final state.
//
// After a stop writes `stopped`, the process is about to end. A step that
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

// TORCHCancelTimeout limits the $cancel request to TORCH. A stop must end soon,
// also if the TORCH server does not answer.
const TORCHCancelTimeout = 5 * time.Second

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
	stopMu.Lock()
	defer stopMu.Unlock()

	job, err := LoadJob(jobsDir, jobID)
	if err != nil {
		return fmt.Errorf("failed to load job to stop it: %w", err)
	}
	if job.Status != models.JobStatusInProgress {
		return nil
	}
	_, err = recordStop(jobsDir, job)
	return err
}

// markRunningJobStopped records the stop of a job that a process runs. Unlike
// MarkJobStopped, it stops a job in any state that is not ended, because a
// process that runs a step of such a job (for example `job run --step`) is about
// to end. It returns the job as it was before the stop, and the channel of a
// TORCH submit in progress, or nil.
func markRunningJobStopped(jobsDir string, jobID string) (*models.PipelineJob, <-chan string, error) {
	stopMu.Lock()
	defer stopMu.Unlock()

	job, err := LoadJob(jobsDir, jobID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load job to stop it: %w", err)
	}
	if job.Status.IsEnded() {
		return nil, nil, &JobEndedError{JobID: jobID, Status: job.Status}
	}
	submitted, err := recordStop(jobsDir, job)
	if err != nil {
		return nil, nil, err
	}
	return job, submitted, nil
}

// recordStop writes the status `stopped`, and drops every later write to the
// job. The caller must hold stopMu. It returns the channel of a TORCH submit in
// progress, or nil.
func recordStop(jobsDir string, job *models.PipelineJob) (<-chan string, error) {
	if err := saveStopped(jobsDir, job); err != nil {
		return nil, err
	}
	key := stopKey(jobsDir, job.JobID)
	stoppedJobs[key] = true
	return submits[key], nil
}

// beginSubmit registers a TORCH submit for the job, or returns ErrJobStopped.
// The caller must call the returned function with the new TORCH job ID, or
// with "" if the submit failed, after it persisted the handle.
func beginSubmit(jobsDir string, jobID string) (func(torchJobID string), error) {
	stopMu.Lock()
	defer stopMu.Unlock()

	if isStopped(jobsDir, jobID) {
		return nil, ErrJobStopped
	}
	key := stopKey(jobsDir, jobID)
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

// persistSubmittedHandle writes the TORCH job handle of a new submit. If a stop
// is recorded, UpdateJob would write nothing. Then this function adds only the
// handle to the stopped state, so that `pipeline continue` finds the TORCH job
// when the cancel fails.
func persistSubmittedHandle(jobsDir string, job *models.PipelineJob) error {
	stopMu.Lock()
	defer stopMu.Unlock()

	if !isStopped(jobsDir, job.JobID) {
		return saveJob(jobsDir, job)
	}
	stored, err := LoadJob(jobsDir, job.JobID)
	if err != nil {
		return fmt.Errorf("failed to load stopped job: %w", err)
	}
	stored.TORCHJobID = job.TORCHJobID
	stored.TORCHExtractionURL = job.TORCHExtractionURL
	return saveJob(jobsDir, stored)
}

// StopJobAndCancelExtraction records the stop of a job that a process runs, and
// then cancels the TORCH job that the torch step waits for. The handle stays in the
// state file: for the cancelled TORCH job, `pipeline continue` gets 410 Gone and
// submits a new extraction.
//
// The stop comes first: the run loop of this process also gets 410 Gone after
// the cancel, and it must not submit a new extraction. If a submit is in
// progress, the stop waits for it and cancels the new TORCH job. Only the
// cancel request has a time limit, TORCHCancelTimeout.
//
// It returns *JobEndedError if the job is completed or failed.
func StopJobAndCancelExtraction(ctx context.Context, jobsDir string, jobID string, logger *lib.Logger) error {
	job, submitted, err := markRunningJobStopped(jobsDir, jobID)
	if err != nil {
		return err
	}

	torchJobID := torchJobToCancel(job, submitted)
	if torchJobID == "" {
		return nil
	}
	return cancelTORCHJob(ctx, job, torchJobID, logger)
}

// torchJobToCancel returns the ID of the TORCH job to cancel, or "" if there is
// none. A submit in progress has priority over the handle in the state file.
//
// The wait for a submit has no deadline of its own: the submit ends by the
// timeout and the retries of its TORCH request. If the process ended earlier,
// the new TORCH job would run without a collector.
func torchJobToCancel(job *models.PipelineJob, submitted <-chan string) string {
	if submitted != nil {
		return <-submitted
	}
	if torchExtractionRunning(job) {
		return job.TORCHJobID
	}
	return ""
}

// JobEndedError tells that a job cannot stop, because it is completed or failed.
type JobEndedError struct {
	JobID  string
	Status models.JobStatus
}

func (e *JobEndedError) Error() string {
	return fmt.Sprintf("job %s is already %s", e.JobID, e.Status)
}

// StopIdleJob stops a job that no process runs. It persists the status
// `stopped` and cancels the TORCH job that the torch step waits for. A job that
// is already stopped stays stopped, and its TORCH job is cancelled again, because
// the first cancel can have failed.
//
// If the cancel fails, the status `stopped` stays in the state file.
func StopIdleJob(ctx context.Context, jobsDir string, jobID string, logger *lib.Logger) error {
	job, err := stopIdleJobState(jobsDir, jobID)
	if err != nil {
		return err
	}
	if !torchExtractionRunning(job) {
		return nil
	}
	return cancelTORCHJob(ctx, job, job.TORCHJobID, logger)
}

func stopIdleJobState(jobsDir string, jobID string) (*models.PipelineJob, error) {
	stopMu.Lock()
	defer stopMu.Unlock()

	job, err := LoadJob(jobsDir, jobID)
	if err != nil {
		return nil, fmt.Errorf("failed to load job to stop it: %w", err)
	}
	if job.Status.IsEnded() {
		return nil, &JobEndedError{JobID: jobID, Status: job.Status}
	}
	if job.Status == models.JobStatusStopped {
		return job, nil
	}

	if err := saveStopped(jobsDir, job); err != nil {
		return nil, err
	}
	return job, nil
}

func saveStopped(jobsDir string, job *models.PipelineJob) error {
	update := models.UpdateJobStatus(*job, models.JobStatusStopped)
	if err := saveJob(jobsDir, &update); err != nil {
		return fmt.Errorf("failed to save stopped job state: %w", err)
	}
	return nil
}

func cancelTORCHJob(ctx context.Context, job *models.PipelineJob, torchJobID string, logger *lib.Logger) error {
	ctx, cancel := context.WithTimeout(ctx, TORCHCancelTimeout)
	defer cancel()
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

// jobStopped tells if a stop is recorded for the job.
func jobStopped(jobsDir string, jobID string) bool {
	stopMu.Lock()
	defer stopMu.Unlock()
	return isStopped(jobsDir, jobID)
}

// isStopped is jobStopped for a caller that holds stopMu.
func isStopped(jobsDir string, jobID string) bool {
	return stoppedJobs[stopKey(jobsDir, jobID)]
}

// ResetStoppedJobsForTesting forgets every recorded stop, as a new process does.
func ResetStoppedJobsForTesting() {
	stopMu.Lock()
	defer stopMu.Unlock()
	stoppedJobs = map[string]bool{}
	submits = map[string]chan string{}
}

// stopKey identifies the state file, because one job ID can name different jobs
// in different job directories.
func stopKey(jobsDir string, jobID string) string {
	return services.GetStateFilePath(jobsDir, jobID)
}
