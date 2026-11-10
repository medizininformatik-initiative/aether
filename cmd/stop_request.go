package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/pipeline"
)

const stopRequestPollInterval = 500 * time.Millisecond

// exitCodeStopRequested is the code of a process that an outside party ended.
var exitCodeStopRequested = exitCodeForSignal(syscall.SIGTERM)

// installStopTriggers makes the process stop the job on a signal and on a
// request of `aether job stop`. requested tells if `aether job stop` asked for
// the stop. The caller must call the returned function to remove both.
func installStopTriggers(jobsDir string, jobID string, requested func() bool, logger *lib.Logger) func() {
	removeHandler := installStopHandler(jobsDir, jobID, logger)
	removeWatcher := installStopRequestWatcher(jobsDir, jobID, requested, logger)
	return func() {
		removeWatcher()
		removeHandler()
	}
}

// installStopRequestWatcher stops the job when `aether job stop` asks for it.
// That command runs in another process, and it cannot send a signal on every
// platform. It writes a marker file in the job directory instead.
//
// The caller must call the returned function to end the watcher.
func installStopRequestWatcher(jobsDir string, jobID string, requested func() bool, logger *lib.Logger) func() {
	return watchStopRequests(jobsDir, jobID, requested, logger, stopRequestPollInterval, os.Exit)
}

func watchStopRequests(
	jobsDir string,
	jobID string,
	requested func() bool,
	logger *lib.Logger,
	interval time.Duration,
	exit func(int),
) func() {
	done := make(chan struct{})
	ended := make(chan struct{})

	go func() {
		defer close(ended)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				// select picks a ready case at random, thus a tick can win
				// against done.
				select {
				case <-done:
					return
				default:
				}
				if !requested() {
					continue
				}
				if !stopOnRequest(jobsDir, jobID, logger) {
					return
				}
				exit(exitCodeStopRequested)
				return
			}
		}
	}()

	// A stop that started ends the process itself, with its exit code and after
	// the TORCH cancel. Thus the remove waits for it.
	return func() {
		close(done)
		<-ended
	}
}

// stopOnRequest records the stop and cancels the TORCH job. It returns false
// if the job was completed or failed already: the run then ends with its own
// exit code, and the process must not exit.
func stopOnRequest(jobsDir string, jobID string, logger *lib.Logger) bool {
	fmt.Printf("\nStop requested. Stopping job %s\n", jobID)

	err := pipeline.StopJobAndCancelExtraction(context.Background(), jobsDir, jobID, logger)
	var ended *pipeline.JobEndedError
	if errors.As(err, &ended) {
		logger.Info("Stop request ignored", "job_id", jobID, "status", ended.Status)
		return false
	}
	if err != nil {
		logger.Warn("Failed to stop the job cleanly", "job_id", jobID, "error", err)
	}

	fmt.Printf("Continue it with: aether pipeline continue <config> %s\n", jobID)
	_ = logger.Close()
	return true
}
