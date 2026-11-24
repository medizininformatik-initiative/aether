package cmd

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

// saveTORCHWaitingJob persists a stopped job whose torch step waits for the TORCH
// job torchJobID. An empty torchJobID gives a job without a handle.
func saveTORCHWaitingJob(t *testing.T, jobsDir, jobID, torchJobID string) {
	t.Helper()
	steps := []models.StepName{models.StepTorchImport, models.StepDIMP}
	job := &models.PipelineJob{
		JobID:       jobID,
		InputSource: "crtdl.json",
		InputType:   models.InputTypeCRTDL,
		Status:      models.JobStatusStopped,
		CurrentStep: string(models.StepTorchImport),
		Steps:       models.InitializeSteps(steps),
		TORCHJobID:  torchJobID,
		Config:      models.ProjectConfig{JobsDir: jobsDir},
	}
	job.Steps[0] = models.StartStep(job.Steps[0])
	require.NoError(t, services.SaveJobState(jobsDir, job))
}

func TestReportStop_NamesRunningTORCHJob(t *testing.T) {
	jobsDir := t.TempDir()
	saveTORCHWaitingJob(t, jobsDir, watcherTestJobID, "torch-job-7")
	var out bytes.Buffer

	reportStop(&out, jobsDir, watcherTestJobID)

	assert.Contains(t, out.String(), "Continue it with: aether pipeline continue <config> "+watcherTestJobID+"\n")
	assert.Contains(t, out.String(),
		"TORCH job torch-job-7 still runs on the TORCH server. Cancel it with: aether job stop <config> "+watcherTestJobID+"\n")
}

func TestReportStop_NoTORCHLineWithoutRunningTORCHJob(t *testing.T) {
	jobsDir := t.TempDir()
	saveTORCHWaitingJob(t, jobsDir, watcherTestJobID, "")
	var out bytes.Buffer

	reportStop(&out, jobsDir, watcherTestJobID)

	assert.Equal(t, "Continue it with: aether pipeline continue <config> "+watcherTestJobID+"\n", out.String())
}
