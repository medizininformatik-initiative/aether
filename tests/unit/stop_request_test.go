package unit

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/services"
)

func newStopRequestJobDir(t *testing.T) (jobsDir, jobID string) {
	t.Helper()
	jobsDir = t.TempDir()
	jobID = "job-1"
	require.NoError(t, os.MkdirAll(services.GetJobDir(jobsDir, jobID), 0o755))
	return jobsDir, jobID
}

func TestStopRequest_ClearRemovesMarkerAndToleratesAbsence(t *testing.T) {
	jobsDir, jobID := newStopRequestJobDir(t)
	require.NoError(t, services.RequestStop(jobsDir, jobID))

	require.NoError(t, services.ClearStopRequest(jobsDir, jobID))
	assert.NoFileExists(t, filepath.Join(services.GetJobDir(jobsDir, jobID), ".stop"))

	assert.NoError(t, services.ClearStopRequest(jobsDir, jobID))
}

func TestStopRequest_RequestFailsWhenJobDirIsMissing(t *testing.T) {
	err := services.RequestStop(t.TempDir(), "missing-job")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to write stop request")
}

func TestStopRequest_ClearFailsWhenMarkerCannotBeRemoved(t *testing.T) {
	jobsDir, jobID := newStopRequestJobDir(t)
	marker := filepath.Join(services.GetJobDir(jobsDir, jobID), ".stop")
	require.NoError(t, os.MkdirAll(filepath.Join(marker, "child"), 0o755))

	err := services.ClearStopRequest(jobsDir, jobID)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to remove stop request")
}

func TestStopRequestCheck_MarkerThatExistsAtStartIsNotARequest(t *testing.T) {
	jobsDir, jobID := newStopRequestJobDir(t)
	require.NoError(t, services.RequestStop(jobsDir, jobID))

	requested := services.NewStopRequestCheck(jobsDir, jobID)

	assert.False(t, requested())
}

func TestStopRequestCheck_NoMarkerIsNotARequest(t *testing.T) {
	jobsDir, jobID := newStopRequestJobDir(t)

	assert.False(t, services.NewStopRequestCheck(jobsDir, jobID)())
}

func TestStopRequestCheck_RequestAfterStartIsARequest(t *testing.T) {
	for name, staleMarker := range map[string]bool{"no marker": false, "stale marker": true} {
		t.Run(name, func(t *testing.T) {
			jobsDir, jobID := newStopRequestJobDir(t)
			if staleMarker {
				require.NoError(t, services.RequestStop(jobsDir, jobID))
			}
			requested := services.NewStopRequestCheck(jobsDir, jobID)

			require.NoError(t, services.RequestStop(jobsDir, jobID))

			assert.True(t, requested())
		})
	}
}
