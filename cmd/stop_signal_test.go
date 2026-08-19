package cmd

import (
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
