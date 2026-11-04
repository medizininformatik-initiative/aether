package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/pipeline"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

func TestRunPipelineContinue_ReportsUnwritableJobLog(t *testing.T) {
	configPath, jobsDir, jobID := startJobFor(t, localImportWaitConfig)
	logPath := services.GetJobLogFilePath(jobsDir, jobID)
	require.NoError(t, os.Remove(logPath))
	require.NoError(t, os.Mkdir(logPath, 0o755))

	err := runPipelineContinue(pipelineContinueCmd, []string{configPath, jobID})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to attach job log file")
}

func TestRunPipelineContinue_RejectsBadLookupFileForPendingFlattening(t *testing.T) {
	configPath, jobsDir, jobID := startJobFor(t, localImportWaitConfig)
	lookupPath := filepath.Join(t.TempDir(), "flatten-lookup.json")
	require.NoError(t, os.WriteFile(lookupPath, []byte(`not json`), 0o644))
	rewriteJob(t, jobsDir, jobID, func(job *models.PipelineJob) {
		job.Config.Services.Flattening.LookupPath = lookupPath
		job.Config.Pipeline.EnabledSteps = append(job.Config.Pipeline.EnabledSteps, models.StepFlattening)
		job.Steps = append(job.Steps, models.PipelineStep{Name: models.StepFlattening, Status: models.StepStatusPending})
	})

	err := runPipelineContinue(pipelineContinueCmd, []string{configPath, jobID})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "lookup")
}

func TestRunPipelineContinue_ReportsMissingCRTDLSource(t *testing.T) {
	configPath, jobsDir, jobID := startJobFor(t, localImportWaitConfig)
	rewriteJob(t, jobsDir, jobID, func(job *models.PipelineJob) {
		job.CRTDLPath = filepath.Join(t.TempDir(), "absent-crtdl.json")
	})

	err := runPipelineContinue(pipelineContinueCmd, []string{configPath, jobID})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to prepare CRTDL")
}

func TestRunPipelineContinue_ReportsNextStepThatIsNotInJob(t *testing.T) {
	configPath, jobsDir, jobID := startJobFor(t, localImportWaitConfig)
	rewriteJob(t, jobsDir, jobID, func(job *models.PipelineJob) {
		job.CurrentStep = string(models.StepLocalImport)
		var kept []models.PipelineStep
		for _, step := range job.Steps {
			if step.Name != models.StepWait {
				kept = append(kept, step)
			}
		}
		job.Steps = kept
	})

	err := runPipelineContinue(pipelineContinueCmd, []string{configPath, jobID})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to advance to next step")
}

// failStateWriteOnNth makes the nth write of a job state file fail.
func failStateWriteOnNth(t *testing.T, n int) {
	t.Helper()
	original := services.StateFileOpsProvider
	t.Cleanup(func() { services.StateFileOpsProvider = original })
	services.StateFileOpsProvider = &failingStateFileOps{failOn: n}
}

type failingStateFileOps struct {
	lib.OSFileOps
	writes int
	failOn int
}

func (f *failingStateFileOps) WriteFile(name string, data []byte, perm os.FileMode) error {
	f.writes++
	if f.writes == f.failOn {
		return errors.New("simulated write failure")
	}
	return f.OSFileOps.WriteFile(name, data, perm)
}

func TestRunPipelineContinue_ReportsFailedSaveOfPreparedJob(t *testing.T) {
	configPath, _, jobID := startJobFor(t, localImportWaitConfig)
	failStateWriteOnNth(t, 1)

	err := runPipelineContinue(pipelineContinueCmd, []string{configPath, jobID})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to save job state")
}

func TestRunPipelineContinue_ReportsFailedSaveOfCompletedJob(t *testing.T) {
	configPath, jobsDir, jobID := startJobFor(t, localImportWaitConfig)
	rewriteJob(t, jobsDir, jobID, func(job *models.PipelineJob) {
		job.CurrentStep = string(models.StepWait)
		setStepStatus(job, models.StepWait, models.StepStatusCompleted)
	})
	failStateWriteOnNth(t, 2)

	err := runPipelineContinue(pipelineContinueCmd, []string{configPath, jobID})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to update job")
	job, loadErr := pipeline.LoadJob(jobsDir, jobID)
	require.NoError(t, loadErr)
	assert.NotEqual(t, models.JobStatusCompleted, job.Status)
}

func TestRunPipelineContinue_ReportsFailedSaveOfAdvancedJob(t *testing.T) {
	configPath, jobsDir, jobID := startJobFor(t, localImportWaitConfig)
	rewriteJob(t, jobsDir, jobID, func(job *models.PipelineJob) {
		job.CurrentStep = string(models.StepLocalImport)
	})
	failStateWriteOnNth(t, 2)

	err := runPipelineContinue(pipelineContinueCmd, []string{configPath, jobID})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to save job state")
	job, loadErr := pipeline.LoadJob(jobsDir, jobID)
	require.NoError(t, loadErr)
	assert.Equal(t, string(models.StepLocalImport), job.CurrentStep)
}
