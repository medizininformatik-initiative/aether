package cmd

import (
	"bytes"
	"os"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
)

// fakeSignal is an os.Signal that is not a syscall.Signal, thus it exercises the
// fallback of exitCodeForSignal.
type fakeSignal struct{}

func (fakeSignal) String() string { return "fake" }
func (fakeSignal) Signal()        {}

func TestExitCodeForSignal(t *testing.T) {
	tests := []struct {
		name string
		sig  os.Signal
		want int
	}{
		{"SIGINT gives 130", syscall.SIGINT, 130},
		{"SIGTERM gives 143", syscall.SIGTERM, 143},
		{"unknown signal gives 1", fakeSignal{}, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, exitCodeForSignal(tt.sig))
		})
	}
}

// runStopHandler sends the signals to handleStopSignals and returns its output
// and the signals that it gave to its stop function.
func runStopHandler(signals ...os.Signal) (string, []os.Signal) {
	ch := make(chan os.Signal, len(signals))
	for _, sig := range signals {
		ch <- sig
	}
	close(ch)

	var out bytes.Buffer
	var stops []os.Signal
	handleStopSignals(ch, &out, func(sig os.Signal) { stops = append(stops, sig) })
	return out.String(), stops
}

func TestHandleStopSignals_FirstInterruptAsksForConfirmation(t *testing.T) {
	out, stops := runStopHandler(os.Interrupt)

	assert.Contains(t, out, "Press Ctrl+C again to quit")
	assert.Empty(t, stops, "the pipeline must continue")
}

func TestHandleStopSignals_SecondInterruptStops(t *testing.T) {
	_, stops := runStopHandler(os.Interrupt, os.Interrupt)

	assert.Equal(t, []os.Signal{os.Interrupt}, stops)
}

// A service manager sends SIGTERM and expects the process to end.
func TestHandleStopSignals_TerminateStopsWithoutConfirmation(t *testing.T) {
	for _, signals := range [][]os.Signal{
		{syscall.SIGTERM},
		{os.Interrupt, syscall.SIGTERM},
	} {
		_, stops := runStopHandler(signals...)

		assert.Equal(t, []os.Signal{syscall.SIGTERM}, stops)
	}
}
