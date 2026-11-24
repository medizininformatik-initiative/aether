package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/lib"
)

func TestWarnUnattendedExtractions_PrintsThreeLinesPerJob(t *testing.T) {
	jobsDir := t.TempDir()
	saveTORCHWaitingJob(t, jobsDir, watcherTestJobID, "torch-job-7")
	var out bytes.Buffer

	warnUnattendedExtractions(&out, jobsDir, "my/config.yaml", lib.NewLogger(lib.LogLevelError))

	assert.Equal(t, "Warning: job "+watcherTestJobID+" is stopped, but its TORCH job torch-job-7 can still run on the TORCH server.\n"+
		"  Continue it with: aether pipeline continue my/config.yaml "+watcherTestJobID+"\n"+
		"  Cancel it with:   aether job stop my/config.yaml "+watcherTestJobID+"\n", out.String())
}

func TestWarnUnattendedExtractions_PrintsNothingWithoutUnattendedExtraction(t *testing.T) {
	jobsDir := t.TempDir()
	saveTORCHWaitingJob(t, jobsDir, watcherTestJobID, "")
	var out bytes.Buffer

	warnUnattendedExtractions(&out, jobsDir, "my/config.yaml", lib.NewLogger(lib.LogLevelError))

	assert.Empty(t, out.String())
}

func TestWarnUnattendedExtractions_UnreadableJobsDirDoesNotFail(t *testing.T) {
	jobsDir := filepath.Join(t.TempDir(), "jobs")
	require.NoError(t, os.WriteFile(jobsDir, []byte("not a directory"), 0o600))
	var out bytes.Buffer

	warnUnattendedExtractions(&out, jobsDir, "my/config.yaml", lib.NewLogger(lib.LogLevelError))

	assert.Empty(t, out.String())
}
