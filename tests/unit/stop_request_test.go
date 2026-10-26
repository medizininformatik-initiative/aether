package unit

import (
	"os"
	"path/filepath"
	"testing"
	"time"

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

func TestStopRequest_AbsentMarkerIsNotARequest(t *testing.T) {
	jobsDir, jobID := newStopRequestJobDir(t)

	assert.False(t, services.StopRequestedSince(jobsDir, jobID, time.Now().Add(-time.Hour)))
}

func TestStopRequest_MarkerWrittenAfterSinceIsARequest(t *testing.T) {
	jobsDir, jobID := newStopRequestJobDir(t)
	since := time.Now().Add(-time.Second)

	require.NoError(t, services.RequestStop(jobsDir, jobID))

	assert.True(t, services.StopRequestedSince(jobsDir, jobID, since))
	assert.FileExists(t, filepath.Join(services.GetJobDir(jobsDir, jobID), ".stop"))
}

func TestStopRequest_MarkerOlderThanSinceIsIgnored(t *testing.T) {
	jobsDir, jobID := newStopRequestJobDir(t)
	require.NoError(t, services.RequestStop(jobsDir, jobID))
	marker := filepath.Join(services.GetJobDir(jobsDir, jobID), ".stop")
	old := time.Now().Add(-time.Hour)
	require.NoError(t, os.Chtimes(marker, old, old))

	assert.False(t, services.StopRequestedSince(jobsDir, jobID, time.Now().Add(-time.Minute)))
}

func TestStopRequest_MarkerWithWholeSecondTimeInSameSecondIsARequest(t *testing.T) {
	jobsDir, jobID := newStopRequestJobDir(t)
	require.NoError(t, services.RequestStop(jobsDir, jobID))
	marker := filepath.Join(services.GetJobDir(jobsDir, jobID), ".stop")
	second := time.Now().Truncate(time.Second)
	require.NoError(t, os.Chtimes(marker, second, second))

	assert.True(t, services.StopRequestedSince(jobsDir, jobID, second.Add(900*time.Millisecond)))
}

func TestStopRequest_ClearRemovesMarkerAndToleratesAbsence(t *testing.T) {
	jobsDir, jobID := newStopRequestJobDir(t)
	require.NoError(t, services.RequestStop(jobsDir, jobID))

	require.NoError(t, services.ClearStopRequest(jobsDir, jobID))
	assert.False(t, services.StopRequestedSince(jobsDir, jobID, time.Now().Add(-time.Hour)))

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
