package pipeline

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
)

// fakeStep is a Step whose behavior is fully controlled by the test.
type fakeStep struct {
	name   models.StepName
	result StepResult
	err    error
	calls  int
}

func (f *fakeStep) Name() models.StepName { return f.name }

func (f *fakeStep) Run(_ *StepContext) (StepResult, error) {
	f.calls++
	return f.result, f.err
}

// classifyingFakeStep additionally implements the optional ClassifyError hook.
type classifyingFakeStep struct {
	fakeStep
	errType models.ErrorType
}

func (c *classifyingFakeStep) ClassifyError(error) models.ErrorType { return c.errType }

func runStepTestJob(name models.StepName) *models.PipelineJob {
	return &models.PipelineJob{
		JobID:  "runstep-test",
		Status: models.JobStatusInProgress,
		Config: models.ProjectConfig{
			Pipeline: models.PipelineConfig{EnabledSteps: []models.StepName{name}},
		},
		Steps: models.InitializeSteps([]models.StepName{name}),
	}
}

func runStepTestContext(job *models.PipelineJob) *StepContext {
	return &StepContext{Job: job, Logger: lib.NewLogger(lib.LogLevelError)}
}

func TestRunStep_HappyPathCompletes(t *testing.T) {
	step := &fakeStep{name: models.StepDIMP, result: StepResult{FilesProcessed: 3, BytesProcessed: 42}}
	job := runStepTestJob(models.StepDIMP)

	err := runStep(step, runStepTestContext(job))

	require.NoError(t, err)
	assert.Equal(t, 1, step.calls)
	s, ok := models.GetStepByName(*job, models.StepDIMP)
	require.True(t, ok)
	assert.Equal(t, models.StepStatusCompleted, s.Status)
	assert.Equal(t, 3, s.FilesProcessed)
	assert.Equal(t, int64(42), s.BytesProcessed)
	require.NotNil(t, s.StartedAt)
	require.NotNil(t, s.CompletedAt)
}

func TestRunStep_ErrorFails(t *testing.T) {
	step := &fakeStep{name: models.StepDIMP, err: errors.New("boom")}
	job := runStepTestJob(models.StepDIMP)

	err := runStep(step, runStepTestContext(job))

	require.Error(t, err)
	s, _ := models.GetStepByName(*job, models.StepDIMP)
	assert.Equal(t, models.StepStatusFailed, s.Status)
	require.NotNil(t, s.LastError)
	assert.Contains(t, s.LastError.Message, "boom")
}

func TestRunStep_DisabledStepSkips(t *testing.T) {
	step := &fakeStep{name: models.StepValidation}
	job := runStepTestJob(models.StepDIMP) // validation is not enabled

	err := runStep(step, runStepTestContext(job))

	require.NoError(t, err)
	assert.Equal(t, 0, step.calls)
}

func TestRunStep_IllegalTransitionRejected(t *testing.T) {
	step := &fakeStep{name: models.StepDIMP}
	job := runStepTestJob(models.StepDIMP)
	// Pre-complete the step: completed is terminal, so entering in_progress is illegal.
	completed, _ := models.GetStepByName(*job, models.StepDIMP)
	*job = models.ReplaceStep(*job, models.CompleteStep(completed, 1, 1))

	err := runStep(step, runStepTestContext(job))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "transition")
	assert.Equal(t, 0, step.calls) // Run never invoked
}

func TestRunStep_PausePropagatesAsWaiting(t *testing.T) {
	step := &fakeStep{name: models.StepWait, err: ErrPaused}
	job := runStepTestJob(models.StepWait)

	err := runStep(step, runStepTestContext(job))

	require.ErrorIs(t, err, ErrPaused)
	s, _ := models.GetStepByName(*job, models.StepWait)
	assert.Equal(t, models.StepStatusWaiting, s.Status)
}

func TestRunStep_StopAfterCompletionCompletesButHalts(t *testing.T) {
	sentinel := errors.New("data quality stop")
	step := &fakeStep{
		name: models.StepValidation,
		err:  stopAfterCompletion(StepResult{FilesProcessed: 2}, sentinel),
	}
	job := runStepTestJob(models.StepValidation)

	err := runStep(step, runStepTestContext(job))

	require.Error(t, err)
	assert.ErrorIs(t, err, sentinel)
	s, _ := models.GetStepByName(*job, models.StepValidation)
	assert.Equal(t, models.StepStatusCompleted, s.Status)
	assert.Equal(t, 2, s.FilesProcessed)
	require.NotNil(t, s.CompletedAt)
}

func TestRunStep_PreservesStartedAtOnReEntry(t *testing.T) {
	// A wait step that paused once and is resumed (e.g. each `pipeline continue`
	// poll) must keep its original StartedAt, not reset it to now on re-entry.
	step := &fakeStep{name: models.StepWait, err: ErrPaused}
	job := runStepTestJob(models.StepWait)
	original := time.Now().Add(-time.Hour)
	w, _ := models.GetStepByName(*job, models.StepWait)
	w.Status = models.StepStatusWaiting
	w.StartedAt = &original
	*job = models.ReplaceStep(*job, w)

	err := runStep(step, runStepTestContext(job))

	require.ErrorIs(t, err, ErrPaused)
	s, _ := models.GetStepByName(*job, models.StepWait)
	require.NotNil(t, s.StartedAt)
	assert.Equal(t, original.Unix(), s.StartedAt.Unix(), "original pause time must be preserved")
}

func TestRunStep_LogsStepStart(t *testing.T) {
	var buf bytes.Buffer
	logger := lib.NewLoggerWithWriter(lib.LogLevelInfo, &buf)
	step := &fakeStep{name: models.StepDIMP}
	job := runStepTestJob(models.StepDIMP)

	err := runStep(step, &StepContext{Job: job, Logger: logger})

	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Step started")
	assert.Contains(t, buf.String(), string(models.StepDIMP))
}

func TestRunStep_UsesClassifyErrorHook(t *testing.T) {
	step := &classifyingFakeStep{
		fakeStep: fakeStep{name: models.StepDIMP, err: errors.New("plain")},
		errType:  models.ErrorTypeTransient,
	}
	job := runStepTestJob(models.StepDIMP)

	err := runStep(step, runStepTestContext(job))

	require.Error(t, err)
	s, _ := models.GetStepByName(*job, models.StepDIMP)
	require.NotNil(t, s.LastError)
	// The default classifier would call a plain error non_transient; the hook overrides it.
	assert.Equal(t, models.ErrorTypeTransient, s.LastError.Type)
}
