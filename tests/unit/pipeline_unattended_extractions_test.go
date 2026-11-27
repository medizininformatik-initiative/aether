package unit

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/pipeline"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

// saveJobAs writes a copy of the job under another job ID into its jobs directory.
func saveJobAs(t *testing.T, job *models.PipelineJob, jobID string) *models.PipelineJob {
	t.Helper()
	clone := *job
	clone.JobID = jobID
	require.NoError(t, services.SaveJobState(job.Config.JobsDir, &clone))
	return &clone
}

func TestUnattendedExtractions_ReturnsStoppedJobWithHandle(t *testing.T) {
	job := persistTORCHExtractionJob(t, "http://torch.invalid")
	job.Status = models.JobStatusStopped
	require.NoError(t, pipeline.UpdateJob(job.Config.JobsDir, job))

	got, err := pipeline.UnattendedExtractions(job.Config.JobsDir)

	require.NoError(t, err)
	assert.Equal(t, []pipeline.UnattendedExtraction{{JobID: job.JobID, TORCHJobID: cancelTestTORCHJobID}}, got)
}

func TestUnattendedExtractions_SkipsJobsWithoutRunningTORCHJob(t *testing.T) {
	tests := []struct {
		name   string
		modify func(job *models.PipelineJob)
	}{
		{"no handle", func(job *models.PipelineJob) { job.TORCHJobID = "" }},
		{"current step is not torch", func(job *models.PipelineJob) { job.CurrentStep = string(models.StepDIMP) }},
		{"torch step completed", func(job *models.PipelineJob) { job.Steps[0] = models.CompleteStep(job.Steps[0], 1, 1) }},
		{"completed job", func(job *models.PipelineJob) { job.Status = models.JobStatusCompleted }},
		{"failed job", func(job *models.PipelineJob) { job.Status = models.JobStatusFailed }},
		{"waiting job", func(job *models.PipelineJob) { job.Status = models.JobStatusWaiting }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := persistTORCHExtractionJob(t, "http://torch.invalid")
			job.Status = models.JobStatusStopped
			tt.modify(job)
			require.NoError(t, services.SaveJobState(job.Config.JobsDir, job))

			got, err := pipeline.UnattendedExtractions(job.Config.JobsDir)

			require.NoError(t, err)
			assert.Empty(t, got)
		})
	}
}

func TestUnattendedExtractions_InProgressJobDependsOnItsLock(t *testing.T) {
	job := persistTORCHExtractionJob(t, "http://torch.invalid")
	jobsDir := job.Config.JobsDir

	t.Run("live process holds the lock", func(t *testing.T) {
		lock, err := services.AcquireJobLock(jobsDir, job.JobID, seamLogger())
		require.NoError(t, err)
		defer func() { _ = lock.Release() }()

		got, err := pipeline.UnattendedExtractions(jobsDir)

		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("process died and the lock is free", func(t *testing.T) {
		got, err := pipeline.UnattendedExtractions(jobsDir)

		require.NoError(t, err)
		assert.Equal(t, []pipeline.UnattendedExtraction{{JobID: job.JobID, TORCHJobID: cancelTestTORCHJobID}}, got)
	})
}

func TestUnattendedExtractions_KeepsListingOrderAndSkipsUnreadableState(t *testing.T) {
	job := persistTORCHExtractionJob(t, "http://torch.invalid")
	job.Status = models.JobStatusStopped
	require.NoError(t, services.SaveJobState(job.Config.JobsDir, job))
	other := saveJobAs(t, job, "aaaaaaaa-0000-0000-0000-000000000000")
	broken := "bbbbbbbb-0000-0000-0000-000000000000"
	require.NoError(t, os.MkdirAll(filepath.Join(job.Config.JobsDir, broken), 0755))
	require.NoError(t, os.WriteFile(services.GetStateFilePath(job.Config.JobsDir, broken), []byte("{"), 0644))

	got, err := pipeline.UnattendedExtractions(job.Config.JobsDir)

	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, job.JobID, got[0].JobID)
	assert.Equal(t, other.JobID, got[1].JobID)
}

func TestUnattendedExtractions_MissingJobsDirGivesEmptyList(t *testing.T) {
	got, err := pipeline.UnattendedExtractions(filepath.Join(t.TempDir(), "missing"))

	require.NoError(t, err)
	assert.Empty(t, got)
}
