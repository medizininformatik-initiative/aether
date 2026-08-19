package services

import (
	"github.com/medizininformatik-initiative/aether/internal/models"
)

// EffectiveJobStatus reports the status to show the user, which can differ from
// the persisted status. It does not write.
//
// A process that dies from SIGKILL, a crash, or a power loss cannot write a
// terminal status, so its job keeps in_progress on disk. The job lock gives the
// missing fact: the kernel releases it when the owner process dies. Only
// in_progress is reinterpreted; a waiting job has no live process by design.
func EffectiveJobStatus(jobsDir string, job models.PipelineJob) models.JobStatus {
	if job.Status != models.JobStatusInProgress {
		return job.Status
	}
	if IsJobLocked(jobsDir, job.JobID) {
		return models.JobStatusInProgress
	}
	return models.JobStatusStopped
}

// EffectiveJobStatusText reports the status as text for display. A stop that
// EffectiveJobStatus derives from a free lock names its cause, so the user can
// tell a died process from a deliberate stop.
func EffectiveJobStatusText(jobsDir string, job models.PipelineJob) string {
	status := EffectiveJobStatus(jobsDir, job)
	if status == models.JobStatusStopped && job.Status == models.JobStatusInProgress {
		return "stopped (process died)"
	}
	return string(status)
}
