package models

import (
	"testing"

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
