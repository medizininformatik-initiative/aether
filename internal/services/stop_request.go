package services

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/medizininformatik-initiative/aether/internal/lib"
)

// stopRequestFileName is the marker that `aether job stop` writes into the job
// directory. A file works on every platform, unlike a signal.
const stopRequestFileName = ".stop"

func stopRequestPath(jobsDir string, jobID string) string {
	return filepath.Join(GetJobDir(jobsDir, jobID), stopRequestFileName)
}

// RequestStop asks the process that holds the job lock to stop the job. Each
// request writes a new token, thus a process can tell it from a stale marker.
// The write is atomic, thus a reader never sees a partial token.
func RequestStop(jobsDir string, jobID string) error {
	token := fmt.Sprintf("%d-%d", os.Getpid(), time.Now().UnixNano())
	if err := lib.AtomicWriteFile(stopRequestPath(jobsDir, jobID), []byte(token), 0o600); err != nil {
		return fmt.Errorf("failed to write stop request: %w", err)
	}
	return nil
}

// ClearStopRequest removes the stop request. A missing request is not an error.
func ClearStopRequest(jobsDir string, jobID string) error {
	err := os.Remove(stopRequestPath(jobsDir, jobID))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("failed to remove stop request: %w", err)
	}
	return nil
}

// NewStopRequestCheck returns a function that tells if a stop request arrived
// after this call. A marker that exists already at this call is stale: a
// `job stop` that was killed left it behind for an earlier process.
//
// Call it before the job lock is taken. `job stop` writes a request only while
// another process holds the lock, thus each request after the lock is fresh.
func NewStopRequestCheck(jobsDir string, jobID string) func() bool {
	stale := readStopRequest(jobsDir, jobID)
	return func() bool {
		token := readStopRequest(jobsDir, jobID)
		return token != "" && token != stale
	}
}

func readStopRequest(jobsDir string, jobID string) string {
	content, err := os.ReadFile(stopRequestPath(jobsDir, jobID))
	if err != nil {
		return ""
	}
	return string(content)
}
