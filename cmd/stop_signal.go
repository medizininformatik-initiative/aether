package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/pipeline"
)

// installStopHandler records the job as stopped when the user confirms an
// interrupt or the process gets SIGTERM, and then stops the process.
//
// No context.Context reaches the steps, so a step that polls or sleeps cannot be
// told to stop. The handler therefore writes the state file, cancels the TORCH
// job within a short time limit if the user confirmed an interrupt, and exits.
// The kernel releases the job lock when the process dies.
//
// The caller must call the returned function to remove the handler. If a stop
// is in progress, the function waits until the stop exits the process.
func installStopHandler(jobsDir string, jobID string, logger *lib.Logger) func() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)

	done := make(chan struct{})
	go func() {
		defer close(done)
		handleStopSignals(ch, os.Stdout, func(sig os.Signal, cancelTORCH bool) {
			// A further Ctrl+C during the cancel ends the process at once.
			signal.Reset(os.Interrupt, syscall.SIGTERM)
			fmt.Printf("\n%s received. Stopping job %s\n", sig, jobID)
			stopJob(jobsDir, jobID, cancelTORCH, cancelTimeout, logger)
			fmt.Printf("Continue it with: aether pipeline continue <config> %s\n", jobID)
			_ = logger.Close()
			os.Exit(exitCodeForSignal(sig))
		})
	}()

	return func() {
		signal.Stop(ch)
		close(ch)
		<-done
	}
}

// cancelTimeout limits the wait for TORCH at a stop. The user wants the process
// to end, and a TORCH job that is not cancelled only costs server time.
const cancelTimeout = 5 * time.Second

// stopJob records the job as stopped and, if cancelTORCH is set, cancels the
// TORCH job. It returns after timeout at the latest: a step that does not obey
// the context, such as the fetch of an OAuth token, cannot delay the stop.
func stopJob(jobsDir string, jobID string, cancelTORCH bool, timeout time.Duration, logger *lib.Logger) {
	if !cancelTORCH {
		if err := pipeline.MarkJobStopped(jobsDir, jobID); err != nil {
			logger.Error("Failed to record stopped job state", "job_id", jobID, "error", err)
		}
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- pipeline.StopJobAndCancelExtraction(ctx, jobsDir, jobID, logger) }()

	select {
	case err := <-result:
		if err != nil {
			logger.Warn("Failed to stop the job cleanly", "job_id", jobID, "error", err)
		}
	case <-ctx.Done():
		logger.Warn("TORCH did not confirm the cancel in time", "job_id", jobID, "timeout", timeout)
	}
}

// handleStopSignals asks for a confirmation at the first interrupt and calls
// stop at the second. A SIGTERM calls stop at once. Only a confirmed interrupt
// cancels the TORCH job: a service manager or a shutdown of the host sends
// SIGTERM, and `pipeline continue` can then re-attach to the TORCH job.
//
// handleStopSignals reads signals until it calls stop or the channel closes.
func handleStopSignals(ch <-chan os.Signal, out io.Writer, stop func(sig os.Signal, cancelTORCH bool)) {
	confirmed := false
	for sig := range ch {
		interrupt := sig == os.Interrupt
		if interrupt && !confirmed {
			confirmed = true
			_, _ = fmt.Fprintln(out, "\nPress Ctrl+C again to quit")
			continue
		}
		stop(sig, interrupt)
		return
	}
}

// exitCodeForSignal gives the shell convention 128+signal, thus 130 for SIGINT.
func exitCodeForSignal(sig os.Signal) int {
	if s, ok := sig.(syscall.Signal); ok {
		return 128 + int(s)
	}
	return 1
}
