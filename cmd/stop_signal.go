package cmd

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/pipeline"
)

// installStopHandler records the job as stopped when the user confirms an
// interrupt or the process gets SIGTERM, and then stops the process.
//
// No context.Context reaches the steps, so a step that polls or sleeps cannot be
// told to stop. The handler therefore writes the state file and exits at once,
// which keeps the immediate exit the user expects from a confirmed Ctrl+C. The
// stop does not cancel work on the external services. The kernel
// releases the job lock when the process dies.
//
// The caller must call the returned function to remove the handler.
func installStopHandler(jobsDir string, jobID string, logger *lib.Logger) func() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)

	go handleStopSignals(ch, os.Stdout, func(sig os.Signal) {
		fmt.Printf("\n%s received. Stopping job %s\n", sig, jobID)
		if err := pipeline.MarkJobStopped(jobsDir, jobID); err != nil {
			logger.Error("Failed to record stopped job state", "job_id", jobID, "error", err)
		}
		fmt.Printf("Continue it with: aether pipeline continue <config> %s\n", jobID)
		_ = logger.Close()
		os.Exit(exitCodeForSignal(sig))
	})

	return func() { signal.Stop(ch); close(ch) }
}

// handleStopSignals asks for a confirmation at the first interrupt and calls
// stop at the second. A SIGTERM calls stop at once, because a service manager
// or a shutdown of the host sends it.
//
// handleStopSignals reads signals until it calls stop or the channel closes.
func handleStopSignals(ch <-chan os.Signal, out io.Writer, stop func(sig os.Signal)) {
	confirmed := false
	for sig := range ch {
		if sig == os.Interrupt && !confirmed {
			confirmed = true
			_, _ = fmt.Fprintln(out, "\nPress Ctrl+C again to quit")
			continue
		}
		stop(sig)
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
