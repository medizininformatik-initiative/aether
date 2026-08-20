package cmd

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/pipeline"
)

// installStopHandler records the job as stopped when the user interrupts the
// process, and then stops the process.
//
// No context.Context reaches the steps, so a step that polls or sleeps cannot be
// told to stop. The handler therefore writes the state file and exits at once,
// which keeps the immediate exit the user expects from Ctrl+C. The kernel
// releases the job lock when the process dies.
//
// The caller must call the returned function to remove the handler.
func installStopHandler(jobsDir string, jobID string, logger *lib.Logger) func() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)

	go func() {
		sig, ok := <-ch
		if !ok {
			return
		}
		fmt.Printf("\n%s received. Stopping job %s\n", sig, jobID)
		if err := pipeline.MarkJobStopped(jobsDir, jobID); err != nil {
			logger.Error("Failed to record stopped job state", "job_id", jobID, "error", err)
		}
		fmt.Printf("Continue it with: aether pipeline continue <config> %s\n", jobID)
		_ = logger.Close()
		os.Exit(exitCodeForSignal(sig))
	}()

	return func() { signal.Stop(ch); close(ch) }
}

// exitCodeForSignal gives the shell convention 128+signal, thus 130 for SIGINT.
func exitCodeForSignal(sig os.Signal) int {
	if s, ok := sig.(syscall.Signal); ok {
		return 128 + int(s)
	}
	return 1
}
