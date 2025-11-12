package services

import (
	"fmt"
	"os"
	"time"

	"github.com/medizininformatik-initiative/aether/internal/lib"
)

// JobLock represents a file lock for a specific job
// Prevents concurrent modification of job state by multiple processes
type JobLock struct {
	jobID    string
	lockFile *os.File
	lockPath string
	logger   *lib.Logger
}

// WithJobLock executes a function while holding a job lock
// Automatically acquires the lock, executes the function, and releases the lock
// Returns error if lock cannot be acquired or if the function returns an error
func WithJobLock(jobsDir string, jobID string, logger *lib.Logger, fn func() error) error {
	// Acquire lock
	lock, err := AcquireJobLock(jobsDir, jobID, logger)
	if err != nil {
		return err
	}
	defer func() {
		if err := lock.Release(); err != nil {
			logger.Error("Failed to release job lock", "error", err)
		}
	}()

	// Execute function with lock held
	return fn()
}

// writeLockInfo writes debug information to the lock file
func (jl *JobLock) writeLockInfo() error {
	lockInfo := fmt.Sprintf("pid=%d\ntime=%s\n", os.Getpid(), time.Now().Format(time.RFC3339))
	_ = jl.lockFile.Truncate(0)
	_, _ = jl.lockFile.Seek(0, 0)
	_, _ = jl.lockFile.WriteString(lockInfo)
	return jl.lockFile.Sync()
}
