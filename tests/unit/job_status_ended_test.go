package unit

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/medizininformatik-initiative/aether/internal/models"
)

func TestJobStatus_IsEnded(t *testing.T) {
	tests := map[models.JobStatus]bool{
		models.JobStatusCompleted:  true,
		models.JobStatusFailed:     true,
		models.JobStatusPending:    false,
		models.JobStatusInProgress: false,
		models.JobStatusWaiting:    false,
		models.JobStatusStopped:    false,
	}
	for status, want := range tests {
		t.Run(string(status), func(t *testing.T) {
			assert.Equal(t, want, status.IsEnded())
		})
	}
}
