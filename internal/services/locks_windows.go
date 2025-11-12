//go:build windows

package services

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"

	"github.com/medizininformatik-initiative/aether/internal/lib"
)

var (
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	procLockFileEx   = kernel32.NewProc("LockFileEx")
	procUnlockFileEx = kernel32.NewProc("UnlockFileEx")
)

const (
	LOCKFILE_FAIL_IMMEDIATELY = 0x00000001
	LOCKFILE_EXCLUSIVE_LOCK   = 0x00000002
	ERROR_LOCK_VIOLATION      = syscall.Errno(33) // File is locked by another process
)

// AcquireJobLock attempts to acquire an exclusive lock for a job (Windows implementation)
// Returns a JobLock if successful, error if lock is already held by another process
func AcquireJobLock(jobsDir string, jobID string, logger *lib.Logger) (*JobLock, error) {
	jobDir := GetJobDir(jobsDir, jobID)
	lockPath := filepath.Join(jobDir, ".lock")

	// Ensure job directory exists
	if err := os.MkdirAll(jobDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create job directory: %w", err)
	}

	// Open/create lock file
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open lock file: %w", err)
	}

	// Try to acquire exclusive lock (non-blocking)
	handle := syscall.Handle(lockFile.Fd())
	overlapped := syscall.Overlapped{}

	// LockFileEx with FAIL_IMMEDIATELY flag for non-blocking behavior
	r1, _, err := procLockFileEx.Call(
		uintptr(handle),
		uintptr(LOCKFILE_EXCLUSIVE_LOCK|LOCKFILE_FAIL_IMMEDIATELY),
		0,
		uintptr(1),
		0,
		uintptr(unsafe.Pointer(&overlapped)),
	)

	if r1 == 0 {
		_ = lockFile.Close()
		// On Windows, if the lock fails due to the file already being locked, err will be ERROR_LOCK_VIOLATION
		if err == ERROR_LOCK_VIOLATION {
			return nil, fmt.Errorf("job %s is locked by another process", jobID)
		}
		return nil, fmt.Errorf("failed to acquire lock: %w", err)
	}

	lock := &JobLock{
		jobID:    jobID,
		lockFile: lockFile,
		lockPath: lockPath,
		logger:   logger,
	}

	// Write lock info
	if err := lock.writeLockInfo(); err != nil {
		logger.Warn("Failed to write lock info", "job_id", jobID, "error", err)
	}

	logger.Debug("Acquired job lock", "job_id", jobID, "pid", os.Getpid())

	return lock, nil
}

// Release releases the job lock (Windows implementation)
// Should be called when job operations are complete
func (jl *JobLock) Release() error {
	if jl.lockFile == nil {
		return nil
	}

	// Release lock
	handle := syscall.Handle(jl.lockFile.Fd())
	overlapped := syscall.Overlapped{}

	_, _, err := procUnlockFileEx.Call(
		uintptr(handle),
		0,
		uintptr(1),
		0,
		uintptr(unsafe.Pointer(&overlapped)),
	)

	if err != syscall.Errno(0) {
		jl.logger.Warn("Failed to release lock", "job_id", jl.jobID, "error", err)
	}

	// Close lock file
	if err := jl.lockFile.Close(); err != nil {
		jl.logger.Warn("Failed to close lock file", "job_id", jl.jobID, "error", err)
		return err
	}

	jl.logger.Debug("Released job lock", "job_id", jl.jobID, "pid", os.Getpid())
	jl.lockFile = nil

	return nil
}

// IsJobLocked checks if a job is currently locked by any process (Windows implementation)
// This is a non-destructive check that doesn't acquire the lock
func IsJobLocked(jobsDir string, jobID string) bool {
	jobDir := GetJobDir(jobsDir, jobID)
	lockPath := filepath.Join(jobDir, ".lock")

	// If lock file doesn't exist, job is not locked
	if _, err := os.Stat(lockPath); os.IsNotExist(err) {
		return false
	}

	// Try to open lock file
	lockFile, err := os.Open(lockPath)
	if err != nil {
		// Can't open lock file - assume not locked
		return false
	}
	defer func() {
		_ = lockFile.Close()
	}()

	// Try to acquire lock (non-blocking)
	handle := syscall.Handle(lockFile.Fd())
	overlapped := syscall.Overlapped{}

	r1, _, err := procLockFileEx.Call(
		uintptr(handle),
		uintptr(LOCKFILE_EXCLUSIVE_LOCK|LOCKFILE_FAIL_IMMEDIATELY),
		0,
		uintptr(1),
		0,
		uintptr(unsafe.Pointer(&overlapped)),
	)

	if r1 == 0 {
		// Lock is held or can't acquire
		if err == ERROR_LOCK_VIOLATION {
			return true
		}
		// Can't determine lock status, assume not locked
		return false
	}

	// We acquired the lock - release it immediately
	procUnlockFileEx.Call(
		uintptr(handle),
		0,
		uintptr(1),
		0,
		uintptr(unsafe.Pointer(&overlapped)),
	)
	return false
}
