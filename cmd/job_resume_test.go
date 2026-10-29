package cmd

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/pipeline"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

const (
	localImportOnlyConfig = "pipeline:\n  enabled_steps:\n    - local_import\n"
	localImportWaitConfig = "pipeline:\n  enabled_steps:\n    - local_import\n    - wait\n"
)

// startJobFor runs `pipeline start` for the pipeline and returns the config
// path, the jobs directory and the ID of the job.
func startJobFor(t *testing.T, pipelineYAML string) (string, string, string) {
	t.Helper()
	startFlags(t, "", "", false)
	configPath, jobsDir := writeStartConfig(t, pipelineYAML)
	require.NoError(t, runPipelineStart(pipelineStartCmd, []string{configPath, startCRTDLFixture, writeImportDir(t)}))
	return configPath, jobsDir, loadOnlyJob(t, jobsDir).JobID
}

func rewriteJob(t *testing.T, jobsDir, jobID string, change func(*models.PipelineJob)) {
	t.Helper()
	job, err := pipeline.LoadJob(jobsDir, jobID)
	require.NoError(t, err)
	change(job)
	require.NoError(t, pipeline.UpdateJob(jobsDir, job))
}

func setStepStatus(job *models.PipelineJob, name models.StepName, status models.StepStatus) {
	for i := range job.Steps {
		if job.Steps[i].Name == name {
			job.Steps[i].Status = status
		}
	}
}

func reopenJob(job *models.PipelineJob) {
	job.Status = models.JobStatusInProgress
	job.CurrentStep = string(models.StepLocalImport)
}

func lockJob(t *testing.T, jobsDir, jobID string) {
	t.Helper()
	lock, err := services.AcquireJobLock(jobsDir, jobID, lib.NewLogger(lib.LogLevelError))
	require.NoError(t, err)
	t.Cleanup(func() { _ = lock.Release() })
}

func TestRunPipelineContinue_ReportsMissingConfig(t *testing.T) {
	err := runPipelineContinue(pipelineContinueCmd, []string{"/nonexistent/aether.yaml", "job"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load configuration")
}

func TestRunPipelineContinue_ReportsUnknownJob(t *testing.T) {
	startFlags(t, "", "", false)
	configPath, _ := writeStartConfig(t, localImportOnlyConfig)

	err := runPipelineContinue(pipelineContinueCmd, []string{configPath, "absent"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load job")
}

func TestRunPipelineContinue_LeavesCompletedJobAlone(t *testing.T) {
	configPath, jobsDir, jobID := startJobFor(t, localImportOnlyConfig)

	require.NoError(t, runPipelineContinue(pipelineContinueCmd, []string{configPath, jobID}))

	job, err := pipeline.LoadJob(jobsDir, jobID)
	require.NoError(t, err)
	assert.Equal(t, models.JobStatusCompleted, job.Status)
}

func TestRunPipelineContinue_RefusesJobThatAnotherProcessHolds(t *testing.T) {
	configPath, jobsDir, jobID := startJobFor(t, localImportWaitConfig)
	lockJob(t, jobsDir, jobID)

	err := runPipelineContinue(pipelineContinueCmd, []string{configPath, jobID})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot continue pipeline")
}

func TestRunPipelineContinue_StaysPausedAtWaitStep(t *testing.T) {
	configPath, jobsDir, jobID := startJobFor(t, localImportWaitConfig)

	require.NoError(t, runPipelineContinue(pipelineContinueCmd, []string{configPath, jobID}))

	job, err := pipeline.LoadJob(jobsDir, jobID)
	require.NoError(t, err)
	assert.Equal(t, string(models.StepWait), job.CurrentStep)
	assert.NotEqual(t, models.JobStatusCompleted, job.Status)
}

func TestRunPipelineContinue_AdvancesPastCompletedStep(t *testing.T) {
	configPath, jobsDir, jobID := startJobFor(t, localImportWaitConfig)
	rewriteJob(t, jobsDir, jobID, func(job *models.PipelineJob) { job.CurrentStep = string(models.StepLocalImport) })

	require.NoError(t, runPipelineContinue(pipelineContinueCmd, []string{configPath, jobID}))

	job, err := pipeline.LoadJob(jobsDir, jobID)
	require.NoError(t, err)
	assert.Equal(t, string(models.StepWait), job.CurrentStep)
}

func TestRunPipelineContinue_CompletesJobWhenNoStepIsLeft(t *testing.T) {
	configPath, jobsDir, jobID := startJobFor(t, localImportOnlyConfig)
	rewriteJob(t, jobsDir, jobID, func(job *models.PipelineJob) {
		reopenJob(job)
	})

	require.NoError(t, runPipelineContinue(pipelineContinueCmd, []string{configPath, jobID}))

	job, err := pipeline.LoadJob(jobsDir, jobID)
	require.NoError(t, err)
	assert.Equal(t, models.JobStatusCompleted, job.Status)
}

func TestRunPipelineContinue_ResumesIncompleteStep(t *testing.T) {
	configPath, jobsDir, jobID := startJobFor(t, localImportOnlyConfig)
	rewriteJob(t, jobsDir, jobID, func(job *models.PipelineJob) {
		reopenJob(job)
		setStepStatus(job, models.StepLocalImport, models.StepStatusPending)
	})

	require.NoError(t, runPipelineContinue(pipelineContinueCmd, []string{configPath, jobID}))

	job, err := pipeline.LoadJob(jobsDir, jobID)
	require.NoError(t, err)
	assert.Equal(t, models.JobStatusCompleted, job.Status)
}

// runStepFlags sets the flag variables of `job run` and sets them back to their
// values after the test.
func runStepFlags(t *testing.T, step string, force bool) {
	t.Helper()
	oldStep, oldForce := stepFlag, forceFlag
	t.Cleanup(func() { stepFlag, forceFlag = oldStep, oldForce })
	stepFlag, forceFlag = step, force
}

func TestRunJobRun_RejectsUnknownStepName(t *testing.T) {
	runStepFlags(t, "bogus", false)

	err := runJobRun(jobRunCmd, []string{"aether.yaml", "job"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid step name")
}

func TestRunJobRun_ReportsMissingConfig(t *testing.T) {
	runStepFlags(t, "local_import", false)

	err := runJobRun(jobRunCmd, []string{"/nonexistent/aether.yaml", "job"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load configuration")
}

func TestRunJobRun_RejectsStepThatIsNotEnabled(t *testing.T) {
	runStepFlags(t, "dimp", false)
	configPath, _, jobID := startJobFor(t, localImportOnlyConfig)

	err := runJobRun(jobRunCmd, []string{configPath, jobID})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "is not enabled in configuration")
}

func TestRunJobRun_ReportsUnknownJob(t *testing.T) {
	runStepFlags(t, "local_import", false)
	configPath, _ := writeStartConfig(t, localImportOnlyConfig)

	err := runJobRun(jobRunCmd, []string{configPath, "absent"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load job")
}

func TestRunJobRun_RequiresCompletedPrerequisite(t *testing.T) {
	dimp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	t.Cleanup(dimp.Close)
	startFlags(t, "", "", false)
	configPath, jobsDir := writeStartConfig(t, "pipeline:\n  enabled_steps:\n    - local_import\n    - dimp\nservices:\n  dimp:\n    url: "+dimp.URL+"\n")
	// The DIMP stub gives no valid answer, so the start fails at the dimp step.
	require.Error(t, runPipelineStart(pipelineStartCmd, []string{configPath, startCRTDLFixture, writeImportDir(t)}))
	jobID := loadOnlyJob(t, jobsDir).JobID
	rewriteJob(t, jobsDir, jobID, func(job *models.PipelineJob) {
		setStepStatus(job, models.StepLocalImport, models.StepStatusPending)
	})
	runStepFlags(t, "dimp", false)

	err := runJobRun(jobRunCmd, []string{configPath, jobID})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "prerequisite step")
}

func TestRunJobRun_RefusesJobThatAnotherProcessHolds(t *testing.T) {
	configPath, jobsDir, jobID := startJobFor(t, localImportWaitConfig)
	lockJob(t, jobsDir, jobID)
	runStepFlags(t, "local_import", true)

	err := runJobRun(jobRunCmd, []string{configPath, jobID})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot execute step")
}

func TestRunJobRun_RefusesCompletedStepWithoutForce(t *testing.T) {
	configPath, _, jobID := startJobFor(t, localImportOnlyConfig)
	runStepFlags(t, "local_import", false)

	err := runJobRun(jobRunCmd, []string{configPath, jobID})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "pass --force to re-run")
}

func TestRunJobRun_RerunsCompletedStepWithForce(t *testing.T) {
	configPath, jobsDir, jobID := startJobFor(t, localImportOnlyConfig)
	runStepFlags(t, "local_import", true)

	require.NoError(t, runJobRun(jobRunCmd, []string{configPath, jobID}))

	job, err := pipeline.LoadJob(jobsDir, jobID)
	require.NoError(t, err)
	assert.Equal(t, models.JobStatusCompleted, job.Status)
}
