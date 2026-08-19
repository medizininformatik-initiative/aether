package unit

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

// seamFakeStep is a pipeline.Step whose behavior is fully controlled by the test.
// It lets the orchestration loop and runStep lifecycle be exercised through the
// public Step seam, with no real services.
type seamFakeStep struct {
	name   models.StepName
	result pipeline.StepResult
	err    error
	calls  int
}

func (f *seamFakeStep) Name() models.StepName { return f.name }

func (f *seamFakeStep) Run(_ *pipeline.StepContext) (pipeline.StepResult, error) {
	f.calls++
	return f.result, f.err
}

// seamClassifyingStep additionally implements the optional ClassifyError hook so
// runStep's classifier override branch is exercised.
type seamClassifyingStep struct {
	seamFakeStep
	errType models.ErrorType
}

func (c *seamClassifyingStep) ClassifyError(error) models.ErrorType { return c.errType }

// seamLoopJob builds a persisted [local_import, dimp] job positioned at its first
// step and registers the given fake steps for the duration of the test.
func seamLoopJob(t *testing.T, fakes map[models.StepName]pipeline.Step) *models.PipelineJob {
	t.Helper()
	return seamJobWithSteps(t, []models.StepName{models.StepLocalImport, models.StepDIMP}, fakes)
}

// seamJobWithSteps builds a persisted job over the given enabled steps, positioned
// at the first, and registers the given fake steps for the duration of the test.
func seamJobWithSteps(t *testing.T, steps []models.StepName, fakes map[models.StepName]pipeline.Step) *models.PipelineJob {
	t.Helper()
	jobsDir := t.TempDir()
	jobID := "550e8400-e29b-41d4-a716-446655440000"
	require.NoError(t, os.MkdirAll(filepath.Join(jobsDir, jobID), 0755))

	job := &models.PipelineJob{
		JobID:       jobID,
		InputSource: "/tmp/x",
		InputType:   models.InputTypeLocal,
		Status:      models.JobStatusInProgress,
		CurrentStep: string(steps[0]),
		Steps:       models.InitializeSteps(steps),
		Config: models.ProjectConfig{
			JobsDir:  jobsDir,
			Pipeline: models.PipelineConfig{EnabledSteps: steps},
		},
	}

	pipeline.SetStepRegistryForTesting(func(n models.StepName) (pipeline.Step, bool) {
		s, ok := fakes[n]
		return s, ok
	})
	t.Cleanup(pipeline.ResetStepRegistry)
	return job
}

func seamLogger() *lib.Logger { return lib.NewLogger(lib.LogLevelError) }

func TestRunLoop_CompletesAllSteps(t *testing.T) {
	imp := &seamFakeStep{name: models.StepLocalImport, result: pipeline.StepResult{FilesProcessed: 1}}
	dimp := &seamFakeStep{name: models.StepDIMP, result: pipeline.StepResult{FilesProcessed: 2}}
	job := seamLoopJob(t, map[models.StepName]pipeline.Step{
		models.StepLocalImport: imp,
		models.StepDIMP:        dimp,
	})

	final, err := pipeline.RunLoop(job, seamLogger(), pipeline.RunOptions{})

	require.NoError(t, err)
	assert.Equal(t, models.JobStatusCompleted, final.Status)
	assert.Equal(t, "", final.CurrentStep)
	assert.Equal(t, 1, imp.calls)
	assert.Equal(t, 1, dimp.calls)
}

func TestRunLoop_StepFailureFailsJob(t *testing.T) {
	imp := &seamFakeStep{name: models.StepLocalImport, err: errors.New("import boom")}
	dimp := &seamFakeStep{name: models.StepDIMP}
	job := seamLoopJob(t, map[models.StepName]pipeline.Step{
		models.StepLocalImport: imp,
		models.StepDIMP:        dimp,
	})

	final, err := pipeline.RunLoop(job, seamLogger(), pipeline.RunOptions{})

	require.Error(t, err)
	assert.Equal(t, models.JobStatusFailed, final.Status)
	assert.Equal(t, 0, dimp.calls, "dimp must not run after import fails")
}

func TestRunLoop_PauseStopsWithoutFailing(t *testing.T) {
	imp := &seamFakeStep{name: models.StepLocalImport, err: pipeline.ErrPaused}
	dimp := &seamFakeStep{name: models.StepDIMP}
	job := seamLoopJob(t, map[models.StepName]pipeline.Step{
		models.StepLocalImport: imp,
		models.StepDIMP:        dimp,
	})

	final, err := pipeline.RunLoop(job, seamLogger(), pipeline.RunOptions{})

	require.ErrorIs(t, err, pipeline.ErrPaused)
	assert.Equal(t, models.JobStatusWaiting, final.Status, "pause must not fail the job")
	s, _ := models.GetStepByName(*final, models.StepLocalImport)
	assert.Equal(t, models.StepStatusWaiting, s.Status)
	assert.Equal(t, 0, dimp.calls)

	// A paused job has no live process, so the persisted status must say waiting.
	reloaded, loadErr := pipeline.LoadJob(job.Config.JobsDir, job.JobID)
	require.NoError(t, loadErr)
	assert.Equal(t, models.JobStatusWaiting, reloaded.Status)
}

func TestRunLoop_UnknownStepErrors(t *testing.T) {
	// Registry resolves nothing, so the current step is unknown.
	job := seamLoopJob(t, map[models.StepName]pipeline.Step{})

	final, err := pipeline.RunLoop(job, seamLogger(), pipeline.RunOptions{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown step")
	assert.Equal(t, models.JobStatusInProgress, final.Status)
}

func TestRunLoop_UsesClassifyErrorHook(t *testing.T) {
	imp := &seamClassifyingStep{
		seamFakeStep: seamFakeStep{name: models.StepLocalImport, err: errors.New("plain")},
		errType:      models.ErrorTypeTransient,
	}
	dimp := &seamFakeStep{name: models.StepDIMP}
	job := seamLoopJob(t, map[models.StepName]pipeline.Step{
		models.StepLocalImport: imp,
		models.StepDIMP:        dimp,
	})

	final, err := pipeline.RunLoop(job, seamLogger(), pipeline.RunOptions{})

	require.Error(t, err)
	assert.Equal(t, models.JobStatusFailed, final.Status)
	s, _ := models.GetStepByName(*final, models.StepLocalImport)
	require.NotNil(t, s.LastError)
	// The default classifier calls a plain error non-transient; the hook overrides it.
	assert.Equal(t, models.ErrorTypeTransient, s.LastError.Type)
}

func TestRunLoop_IllegalTransitionSurfacesError(t *testing.T) {
	imp := &seamFakeStep{name: models.StepLocalImport}
	dimp := &seamFakeStep{name: models.StepDIMP}
	job := seamLoopJob(t, map[models.StepName]pipeline.Step{
		models.StepLocalImport: imp,
		models.StepDIMP:        dimp,
	})
	// Pre-complete the current step: completed is terminal, so entering
	// in_progress is illegal and runStep must reject it before invoking Run.
	cur, _ := models.GetStepByName(*job, models.StepLocalImport)
	*job = models.ReplaceStep(*job, models.CompleteStep(cur, 1, 1))

	final, err := pipeline.RunLoop(job, seamLogger(), pipeline.RunOptions{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "transition")
	assert.Equal(t, models.JobStatusFailed, final.Status)
	assert.Equal(t, 0, imp.calls, "Run must not be invoked when the transition is illegal")
}

// failSaveOnNth injects a StateFileOpsProvider that lets the first n-1 job saves
// succeed and fails the nth, so a specific UpdateJob call in RunLoop can be driven
// to error. The provider is restored on test cleanup.
func failSaveOnNth(t *testing.T, n int) {
	t.Helper()
	original := services.StateFileOpsProvider
	t.Cleanup(func() { services.StateFileOpsProvider = original })
	services.StateFileOpsProvider = &countingStateFileOps{
		failOnNth: n,
		failErr:   errors.New("simulated write failure"),
	}
}

func TestRunLoop_FailedJobSaveErrorIsLogged(t *testing.T) {
	imp := &seamFakeStep{name: models.StepLocalImport, err: errors.New("boom")}
	dimp := &seamFakeStep{name: models.StepDIMP}
	job := seamLoopJob(t, map[models.StepName]pipeline.Step{
		models.StepLocalImport: imp,
		models.StepDIMP:        dimp,
	})
	failSaveOnNth(t, 1) // the failed-job save is the first save attempt

	final, err := pipeline.RunLoop(job, seamLogger(), pipeline.RunOptions{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom", "the original step error still propagates")
	assert.Equal(t, models.JobStatusFailed, final.Status, "job is failed even if saving that state fails")
}

func TestRunLoop_PostStepSaveErrorSurfaces(t *testing.T) {
	imp := &seamFakeStep{name: models.StepLocalImport}
	dimp := &seamFakeStep{name: models.StepDIMP}
	job := seamLoopJob(t, map[models.StepName]pipeline.Step{
		models.StepLocalImport: imp,
		models.StepDIMP:        dimp,
	})
	failSaveOnNth(t, 1) // the post-step save is the first save attempt

	_, err := pipeline.RunLoop(job, seamLogger(), pipeline.RunOptions{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to save job state")
}

func TestRunLoop_CompletionSaveErrorSurfaces(t *testing.T) {
	dimp := &seamFakeStep{name: models.StepDIMP}
	job := seamJobWithSteps(t, []models.StepName{models.StepDIMP},
		map[models.StepName]pipeline.Step{models.StepDIMP: dimp})
	failSaveOnNth(t, 2) // post-step save succeeds; the completion save (2nd) fails

	_, err := pipeline.RunLoop(job, seamLogger(), pipeline.RunOptions{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to save job state")
}

func TestRunLoop_PostAdvanceSaveErrorSurfaces(t *testing.T) {
	imp := &seamFakeStep{name: models.StepLocalImport}
	dimp := &seamFakeStep{name: models.StepDIMP}
	job := seamLoopJob(t, map[models.StepName]pipeline.Step{
		models.StepLocalImport: imp,
		models.StepDIMP:        dimp,
	})
	failSaveOnNth(t, 2) // post-step save succeeds; the post-advance save (2nd) fails

	_, err := pipeline.RunLoop(job, seamLogger(), pipeline.RunOptions{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to save job state")
}

func TestRunLoop_AdvanceErrorSurfaces(t *testing.T) {
	// A pipeline with no import step: the first step completes, but advancing to
	// send fails its "import" prerequisite, and RunLoop surfaces that error.
	first := &seamFakeStep{name: models.StepDIMP}
	send := &seamFakeStep{name: models.StepSend}
	job := seamJobWithSteps(t, []models.StepName{models.StepDIMP, models.StepSend},
		map[models.StepName]pipeline.Step{models.StepDIMP: first, models.StepSend: send})

	_, err := pipeline.RunLoop(job, seamLogger(), pipeline.RunOptions{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to advance to next step")
	assert.Equal(t, 0, send.calls, "send must not run when the advance fails")
}

func TestRunStep_RunsRegisteredStepThroughLifecycle(t *testing.T) {
	dimp := &seamFakeStep{name: models.StepDIMP, result: pipeline.StepResult{FilesProcessed: 3}}
	job := seamJobWithSteps(t, []models.StepName{models.StepDIMP},
		map[models.StepName]pipeline.Step{models.StepDIMP: dimp})

	err := pipeline.RunStep(models.StepDIMP, &pipeline.StepContext{
		Job: job, Layout: services.NewJobLayoutForDir(t.TempDir(), nil), Logger: seamLogger(),
	})

	require.NoError(t, err)
	assert.Equal(t, 1, dimp.calls, "the registered step must run once")
	s, ok := models.GetStepByName(*job, models.StepDIMP)
	require.True(t, ok)
	assert.Equal(t, models.StepStatusCompleted, s.Status, "RunStep drives the full lifecycle to completion")
}

func TestRunStep_UnknownStepErrors(t *testing.T) {
	job := seamJobWithSteps(t, []models.StepName{models.StepDIMP},
		map[models.StepName]pipeline.Step{models.StepDIMP: &seamFakeStep{name: models.StepDIMP}})

	err := pipeline.RunStep(models.StepName("bogus"), &pipeline.StepContext{
		Job: job, Layout: services.NewJobLayoutForDir(t.TempDir(), nil), Logger: seamLogger(),
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown step")
}

func TestStepFor_ResolvesEveryDefaultStep(t *testing.T) {
	// Override then reset to exercise both SetStepRegistryForTesting and ResetStepRegistry.
	pipeline.SetStepRegistryForTesting(func(models.StepName) (pipeline.Step, bool) { return nil, false })
	pipeline.ResetStepRegistry()

	names := []models.StepName{
		models.StepTorchImport, models.StepLocalImport, models.StepHttpImport,
		models.StepDIMP, models.StepValidation, models.StepFlattening,
		models.StepSend, models.StepWait,
	}
	for _, n := range names {
		s, ok := pipeline.StepFor(n)
		require.True(t, ok, "expected registry entry for %s", n)
		assert.Equal(t, n, s.Name(), "registered step should report its own name")
	}
}

func TestStepFor_UnknownStep(t *testing.T) {
	pipeline.ResetStepRegistry()
	_, ok := pipeline.StepFor(models.StepName("bogus"))
	assert.False(t, ok)
}

func TestStepFor_OverrideForTesting(t *testing.T) {
	fake := &seamFakeStep{name: models.StepDIMP}
	pipeline.SetStepRegistryForTesting(func(models.StepName) (pipeline.Step, bool) { return fake, true })
	defer pipeline.ResetStepRegistry()

	s, ok := pipeline.StepFor(models.StepDIMP)
	require.True(t, ok)
	assert.Same(t, fake, s)
}
