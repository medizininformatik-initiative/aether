package models

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/medizininformatik-initiative/aether/internal/models"
)

func TestIsValidJobStatus(t *testing.T) {
	tests := []struct {
		name   string
		status models.JobStatus
		want   bool
	}{
		{"pending", models.JobStatusPending, true},
		{"in_progress", models.JobStatusInProgress, true},
		{"completed", models.JobStatusCompleted, true},
		{"failed", models.JobStatusFailed, true},
		{"waiting", models.JobStatusWaiting, true},
		{"unknown value", models.JobStatus("bogus"), false},
		{"empty value", models.JobStatus(""), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, models.IsValidJobStatus(tt.status))
		})
	}
}
