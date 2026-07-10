package models

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/medizininformatik-initiative/aether/internal/models"
)

func TestCanTransitionTo(t *testing.T) {
	tests := []struct {
		name string
		from models.StepStatus
		to   models.StepStatus
		want bool
	}{
		// pending may only begin
		{"pending to in_progress", models.StepStatusPending, models.StepStatusInProgress, true},
		{"pending to completed", models.StepStatusPending, models.StepStatusCompleted, false},
		{"pending to failed", models.StepStatusPending, models.StepStatusFailed, false},
		{"pending to waiting", models.StepStatusPending, models.StepStatusWaiting, false},

		// in_progress may complete, fail, pause, or re-enter (crash resume)
		{"in_progress to completed", models.StepStatusInProgress, models.StepStatusCompleted, true},
		{"in_progress to failed", models.StepStatusInProgress, models.StepStatusFailed, true},
		{"in_progress to waiting", models.StepStatusInProgress, models.StepStatusWaiting, true},
		{"in_progress to in_progress", models.StepStatusInProgress, models.StepStatusInProgress, true},
		{"in_progress to pending", models.StepStatusInProgress, models.StepStatusPending, false},

		// failed may only retry
		{"failed to in_progress", models.StepStatusFailed, models.StepStatusInProgress, true},
		{"failed to completed", models.StepStatusFailed, models.StepStatusCompleted, false},
		{"failed to failed", models.StepStatusFailed, models.StepStatusFailed, false},

		// waiting may resume or complete
		{"waiting to in_progress", models.StepStatusWaiting, models.StepStatusInProgress, true},
		{"waiting to completed", models.StepStatusWaiting, models.StepStatusCompleted, true},
		{"waiting to failed", models.StepStatusWaiting, models.StepStatusFailed, false},

		// completed is terminal for a step
		{"completed to in_progress", models.StepStatusCompleted, models.StepStatusInProgress, false},
		{"completed to completed", models.StepStatusCompleted, models.StepStatusCompleted, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, models.CanTransitionTo(tt.from, tt.to))
		})
	}
}

// TestResetStep verifies that resetting a completed step clears every run
// artifact (timestamps, error, and processed counts) so a forced re-run starts
// from a clean pending state, and that the original step is left unmutated.
func TestResetStep(t *testing.T) {
	now := time.Now()
	step := models.PipelineStep{
		Name:           models.StepDIMP,
		Status:         models.StepStatusCompleted,
		StartedAt:      &now,
		CompletedAt:    &now,
		FilesProcessed: 5,
		BytesProcessed: 1024,
		LastError:      &models.StepError{Message: "boom"},
	}

	reset := models.ResetStep(step)

	assert.Equal(t, models.StepStatusPending, reset.Status)
	assert.Nil(t, reset.StartedAt)
	assert.Nil(t, reset.CompletedAt)
	assert.Nil(t, reset.LastError)
	assert.Zero(t, reset.FilesProcessed)
	assert.Zero(t, reset.BytesProcessed)
	assert.Equal(t, models.StepDIMP, reset.Name, "step name must be preserved")

	// Pure function: the original must be untouched.
	assert.Equal(t, models.StepStatusCompleted, step.Status)
	assert.NotNil(t, step.StartedAt)
}

// TestDeriveJobStatus proves the job-level status is a pure function of its
// steps: any failed step wins, otherwise all-completed means completed, any work
// underway means in_progress, and an untouched job stays pending.
func TestDeriveJobStatus(t *testing.T) {
	steps := func(statuses ...models.StepStatus) []models.PipelineStep {
		out := make([]models.PipelineStep, len(statuses))
		for i, s := range statuses {
			out[i] = models.PipelineStep{Name: models.StepName(string(rune('a' + i))), Status: s}
		}
		return out
	}

	tests := []struct {
		name  string
		steps []models.PipelineStep
		want  models.JobStatus
	}{
		{"all completed -> completed", steps(models.StepStatusCompleted, models.StepStatusCompleted), models.JobStatusCompleted},
		{"partial progress -> in_progress", steps(models.StepStatusCompleted, models.StepStatusPending), models.JobStatusInProgress},
		{"a step in progress -> in_progress", steps(models.StepStatusInProgress, models.StepStatusPending), models.JobStatusInProgress},
		{"any failed step -> failed", steps(models.StepStatusFailed, models.StepStatusCompleted), models.JobStatusFailed},
		{"failed wins over in_progress", steps(models.StepStatusInProgress, models.StepStatusFailed), models.JobStatusFailed},
		{"all pending -> pending", steps(models.StepStatusPending, models.StepStatusPending), models.JobStatusPending},
		{"no steps -> pending", nil, models.JobStatusPending},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := models.PipelineJob{Steps: tt.steps}
			assert.Equal(t, tt.want, models.DeriveJobStatus(job))
		})
	}
}

// TestResetStepAndDownstream proves that re-running a step invalidates every
// step ordered after it: the named step and all later steps are reset to
// pending (their prior output is stale), while earlier steps are untouched.
func TestResetStepAndDownstream(t *testing.T) {
	steps := []models.StepName{models.StepLocalImport, models.StepDIMP, models.StepValidation, models.StepSend}
	job := models.PipelineJob{Steps: models.InitializeSteps(steps)}
	// Every step ran to completion.
	for _, name := range steps {
		st, _ := models.GetStepByName(job, name)
		job = models.ReplaceStep(job, models.CompleteStep(st, 3, 99))
	}

	reset := models.ResetStepAndDownstream(job, models.StepDIMP)

	// Upstream stays completed.
	imp, _ := models.GetStepByName(reset, models.StepLocalImport)
	assert.Equal(t, models.StepStatusCompleted, imp.Status, "steps before the re-run must be untouched")

	// The re-run step and everything after it are reset to pending and cleared.
	for _, name := range []models.StepName{models.StepDIMP, models.StepValidation, models.StepSend} {
		s, _ := models.GetStepByName(reset, name)
		assert.Equal(t, models.StepStatusPending, s.Status, "%s must be reset to pending", name)
		assert.Zero(t, s.FilesProcessed, "%s counts must be cleared", name)
		assert.Nil(t, s.CompletedAt, "%s completion must be cleared", name)
	}

	// Pure function: the original job is untouched.
	orig, _ := models.GetStepByName(job, models.StepValidation)
	assert.Equal(t, models.StepStatusCompleted, orig.Status)

	// An unknown step leaves the job unchanged.
	unchanged := models.ResetStepAndDownstream(job, models.StepName("nope"))
	u, _ := models.GetStepByName(unchanged, models.StepSend)
	assert.Equal(t, models.StepStatusCompleted, u.Status)
}

// TestUpdateStepProgress proves progress counters are overwritten while the rest
// of the step is left intact, and the original step is not mutated.
func TestUpdateStepProgress(t *testing.T) {
	tests := []struct {
		name       string
		startFiles int
		startBytes int64
		files      int
		bytes      int64
	}{
		{"from zero", 0, 0, 10, 2048},
		{"overwrites prior progress", 3, 100, 7, 4096},
		{"back to zero", 5, 500, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			step := models.PipelineStep{
				Name:           models.StepDIMP,
				Status:         models.StepStatusInProgress,
				FilesProcessed: tt.startFiles,
				BytesProcessed: tt.startBytes,
			}

			updated := models.UpdateStepProgress(step, tt.files, tt.bytes)

			assert.Equal(t, tt.files, updated.FilesProcessed)
			assert.Equal(t, tt.bytes, updated.BytesProcessed)
			assert.Equal(t, models.StepStatusInProgress, updated.Status, "status is untouched")
			assert.Equal(t, models.StepDIMP, updated.Name, "name is untouched")

			assert.Equal(t, tt.startFiles, step.FilesProcessed, "original step must not be mutated")
			assert.Equal(t, tt.startBytes, step.BytesProcessed, "original step must not be mutated")
		})
	}
}

// TestGetNextPendingStep proves the first pending step (in order) is returned,
// and that a job with no pending steps reports none.
func TestGetNextPendingStep(t *testing.T) {
	tests := []struct {
		name     string
		statuses []models.StepStatus
		wantName models.StepName
		wantOK   bool
	}{
		{
			name:     "first pending is returned",
			statuses: []models.StepStatus{models.StepStatusCompleted, models.StepStatusPending, models.StepStatusPending},
			wantName: models.StepDIMP,
			wantOK:   true,
		},
		{
			name:     "no pending steps",
			statuses: []models.StepStatus{models.StepStatusCompleted, models.StepStatusCompleted},
			wantOK:   false,
		},
		{
			name:     "empty job has no pending step",
			statuses: nil,
			wantOK:   false,
		},
	}
	names := []models.StepName{models.StepLocalImport, models.StepDIMP, models.StepValidation}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			steps := make([]models.PipelineStep, len(tt.statuses))
			for i, s := range tt.statuses {
				steps[i] = models.PipelineStep{Name: names[i], Status: s}
			}
			job := models.PipelineJob{Steps: steps}

			step, ok := models.GetNextPendingStep(job)
			assert.Equal(t, tt.wantOK, ok)
			if tt.wantOK {
				assert.Equal(t, tt.wantName, step.Name)
			}
		})
	}
}
