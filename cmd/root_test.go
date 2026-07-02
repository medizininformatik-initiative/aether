package cmd

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A subcommand failure must surface a single clean error without Cobra dumping
// the usage/flags block or printing the error itself. Execute() owns the one
// user-facing `Error:` line, so Cobra must stay silent on both (issue #467).
func TestRootCommand_FailureSuppressesUsageAndCobraError(t *testing.T) {
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	rootCmd.SetArgs([]string{"pipeline", "start", "only-one-arg"})
	t.Cleanup(func() {
		rootCmd.SetArgs(nil)
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
	})

	err := rootCmd.Execute()

	require.Error(t, err)
	assert.NotContains(t, out.String(), "Usage:")
	assert.NotContains(t, out.String(), "Error:")
}
