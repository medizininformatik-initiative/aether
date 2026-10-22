package unit

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/medizininformatik-initiative/aether/internal/models"
)

func durationJob() models.PipelineJob {
	created := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	return models.PipelineJob{
		Status:    models.JobStatusInProgress,
		CreatedAt: created,
		UpdatedAt: created.Add(30 * time.Minute),
	}
}

var durationNow = time.Date(2026, 1, 1, 12, 5, 0, 0, time.UTC)

func TestJobDuration_CompletedJobEndsAtLastWrite(t *testing.T) {
	job := durationJob()

	got := models.JobDuration(job, models.JobStatusCompleted, durationNow)

	assert.Equal(t, 30*time.Minute, got)
}

func TestJobDuration_InProgressJobGrowsWithNow(t *testing.T) {
	job := durationJob()

	got := models.JobDuration(job, models.JobStatusInProgress, durationNow)

	assert.Equal(t, 2*time.Hour+5*time.Minute, got)
}

func TestJobDuration_PendingJobGrowsWithNow(t *testing.T) {
	job := durationJob()

	got := models.JobDuration(job, models.JobStatusPending, durationNow)

	assert.Equal(t, 2*time.Hour+5*time.Minute, got)
}

func TestJobDuration_NotRunningStatusEndsAtLastWrite(t *testing.T) {
	for _, status := range []models.JobStatus{
		models.JobStatusFailed, models.JobStatusStopped, models.JobStatusWaiting,
	} {
		t.Run(string(status), func(t *testing.T) {
			got := models.JobDuration(durationJob(), status, durationNow)

			assert.Equal(t, 30*time.Minute, got)
		})
	}
}

func TestJobDuration_UsesGivenStatusNotPersistedStatus(t *testing.T) {
	job := durationJob()
	job.Status = models.JobStatusInProgress

	got := models.JobDuration(job, models.JobStatusStopped, durationNow)

	assert.Equal(t, 30*time.Minute, got)
}
