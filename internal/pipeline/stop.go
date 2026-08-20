package pipeline

import (
	"fmt"
	"sync"

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
var (
	stopMu      sync.Mutex
	stoppedJobs = map[string]bool{}
)

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

	update := models.UpdateJobStatus(*job, models.JobStatusStopped)
	if err := saveJob(jobsDir, &update); err != nil {
		return fmt.Errorf("failed to save stopped job state: %w", err)
	}
	stoppedJobs[stopKey(jobsDir, jobID)] = true
	return nil
}

// stopKey identifies the state file, because one job ID can name different jobs
// in different job directories.
func stopKey(jobsDir string, jobID string) string {
	return services.GetStateFilePath(jobsDir, jobID)
}
