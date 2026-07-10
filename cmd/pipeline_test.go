package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// `pipeline continue` reads noProgress into RunOptions, so the flag must be
// registered on that command too — otherwise --no-progress is an unknown flag
// and a continued job can never disable progress indicators.
func TestPipelineContinue_AcceptsNoProgressFlag(t *testing.T) {
	flag := pipelineContinueCmd.Flags().Lookup("no-progress")
	require.NotNil(t, flag, "continue command must register --no-progress")

	t.Cleanup(func() { noProgress = false })
	require.NoError(t, pipelineContinueCmd.Flags().Set("no-progress", "true"))

	assert.True(t, noProgress, "--no-progress must bind to the noProgress variable")
}
