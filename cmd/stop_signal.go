package cmd

import (
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
// told to stop. The handler therefore writes the state file and exits at once,
// which keeps the immediate exit the user expects from a confirmed Ctrl+C. The
// stop does not cancel work on the external services. The kernel
// releases the job lock when the process dies.
//
// The caller must call the returned function to remove the handler.
func installStopHandler(jobsDir string, jobID string, logger *lib.Logger) func() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	remove := watchStopSignals(ch, func(sig os.Signal) {
		fmt.Printf("\n%s received. Stopping job %s\n", sig, jobID)
		if err := pipeline.MarkJobStopped(jobsDir, jobID); err != nil {
			logger.Error("Failed to record stopped job state", "job_id", jobID, "error", err)
		}
		fmt.Printf("Continue it with: aether pipeline continue <config> %s\n", jobID)
		_ = logger.Close()
		os.Exit(exitCodeForSignal(sig))
	})
	return func() {
		signal.Stop(ch)
		remove()
	}
}

// watchStopSignals runs handleStopSignals in the background. The caller must
// call the returned function to end it.
func watchStopSignals(ch chan os.Signal, stop func(sig os.Signal)) func() {
	ended := make(chan struct{})
	go func() {
		defer close(ended)
		handleStopSignals(ch, os.Stdout, time.Now, stop)
	}()

	// A stop that started ends the process itself with its exit code. Thus the
	// remove waits for it.
	return func() {
		close(ch)
		<-ended
	}
}

// confirmWindow is the time in which a second interrupt stops the job. A later
// interrupt asks again, because the user can no longer see the first request.
const confirmWindow = 5 * time.Second

// handleStopSignals asks for a confirmation at an interrupt and calls stop at
// a second interrupt in confirmWindow. A SIGTERM calls stop at once, because a
// service manager or a shutdown of the host sends it.
//
// handleStopSignals reads signals until it calls stop or the channel closes.
func handleStopSignals(ch <-chan os.Signal, out io.Writer, now func() time.Time, stop func(sig os.Signal)) {
	var askedAt time.Time
	for sig := range ch {
		t := now()
		if sig == os.Interrupt && (askedAt.IsZero() || t.Sub(askedAt) > confirmWindow) {
			askedAt = t
			_, _ = fmt.Fprintf(out, "\nPress Ctrl+C again within %s to quit\n", confirmWindow)
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
