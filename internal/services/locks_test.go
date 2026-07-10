package services

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/lib"
)

func testLockLogger() *lib.Logger {
	return lib.NewLogger(lib.LogLevelError)
}

func TestAcquireJobLockCreatesLockFileAndReportsLocked(t *testing.T) {
	jobsDir := t.TempDir()
	jobID := "job-acquire"

	assert.False(t, IsJobLocked(jobsDir, jobID), "job should not be locked before acquire")

	lock, err := AcquireJobLock(jobsDir, jobID, testLockLogger())
	require.NoError(t, err)
	require.NotNil(t, lock)

	lockPath := filepath.Join(GetJobDir(jobsDir, jobID), ".lock")
	content, err := os.ReadFile(lockPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "pid=")
	assert.True(t, strings.Contains(string(content), "time="), "lock file should record acquisition time")

	assert.True(t, IsJobLocked(jobsDir, jobID), "job should report locked while held")

	require.NoError(t, lock.Release())
	assert.False(t, IsJobLocked(jobsDir, jobID), "job should report unlocked after release")
}

func TestAcquireJobLockFailsWhenAlreadyHeld(t *testing.T) {
	jobsDir := t.TempDir()
	jobID := "job-contended"

	first, err := AcquireJobLock(jobsDir, jobID, testLockLogger())
	require.NoError(t, err)
	defer func() { _ = first.Release() }()

	second, err := AcquireJobLock(jobsDir, jobID, testLockLogger())
	assert.Nil(t, second)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "locked by another process")
}

func TestWithJobLockPropagatesErrorAndReleases(t *testing.T) {
	jobsDir := t.TempDir()
	jobID := "job-with-lock"

	wantErr := errors.New("callback failed")
	err := WithJobLock(jobsDir, jobID, testLockLogger(), func() error {
		assert.True(t, IsJobLocked(jobsDir, jobID), "lock should be held during callback")
		return wantErr
	})
	require.ErrorIs(t, err, wantErr)

	assert.False(t, IsJobLocked(jobsDir, jobID), "lock should be released after WithJobLock returns")

	// A follow-up acquire must succeed, proving the lock was released.
	lock, err := AcquireJobLock(jobsDir, jobID, testLockLogger())
	require.NoError(t, err)
	require.NoError(t, lock.Release())
}

func TestReleaseIsIdempotent(t *testing.T) {
	jobsDir := t.TempDir()
	jobID := "job-double-release"

	lock, err := AcquireJobLock(jobsDir, jobID, testLockLogger())
	require.NoError(t, err)

	require.NoError(t, lock.Release())
	assert.NotPanics(t, func() {
		require.NoError(t, lock.Release())
	})
}
