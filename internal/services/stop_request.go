package services

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// stopRequestFileName is the marker that `aether job stop` writes into the job
// directory. A file works on every platform, unlike a signal.
const stopRequestFileName = ".stop"

func stopRequestPath(jobsDir string, jobID string) string {
	return filepath.Join(GetJobDir(jobsDir, jobID), stopRequestFileName)
}

// RequestStop asks the process that holds the job lock to stop the job.
func RequestStop(jobsDir string, jobID string) error {
	if err := os.WriteFile(stopRequestPath(jobsDir, jobID), nil, 0o600); err != nil {
		return fmt.Errorf("failed to write stop request: %w", err)
	}
	return nil
}

// StopRequestedSince tells if a stop request exists that is not older than
// since. A process passes its start time: a marker that a killed stop command
// left behind is older than a later process and does not stop it. The
// comparison is in whole seconds, because some filesystems keep only seconds.
func StopRequestedSince(jobsDir string, jobID string, since time.Time) bool {
	info, err := os.Stat(stopRequestPath(jobsDir, jobID))
	if err != nil {
		return false
	}
	return !info.ModTime().Before(since.Truncate(time.Second))
}

// ClearStopRequest removes the stop request. A missing request is not an error.
func ClearStopRequest(jobsDir string, jobID string) error {
	err := os.Remove(stopRequestPath(jobsDir, jobID))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("failed to remove stop request: %w", err)
	}
	return nil
}
