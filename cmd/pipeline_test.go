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

// The anonymization YAML is a config file of the run, like the CRTDL, so the
// CLI supplies it. It overrides services.dimp.anonymization_config.
func TestPipelineStart_AcceptsAnonymizationConfigFlag(t *testing.T) {
	flag := pipelineStartCmd.Flags().Lookup("anonymization-config")
	require.NotNil(t, flag, "start command must register --anonymization-config")

	t.Cleanup(func() { anonymizationConfig = "" })
	require.NoError(t, pipelineStartCmd.Flags().Set("anonymization-config", "/etc/aether/anonymization.yaml"))

	assert.Equal(t, "/etc/aether/anonymization.yaml", anonymizationConfig,
		"--anonymization-config must bind to the anonymizationConfig variable")
}
