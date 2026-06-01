package unit

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

// Console respects the configured level, but the attached job log file always
// captures full DEBUG output regardless of the console level (issue #409).
func TestLoggerDualSink_ConsoleRespectsLevelFileAlwaysDebug(t *testing.T) {
	var console bytes.Buffer
	logger := lib.NewLoggerWithWriter(lib.LogLevelInfo, &console)

	logPath := filepath.Join(t.TempDir(), "job.log")
	require.NoError(t, logger.AttachJobLogFile(logPath))

	logger.Debug("debug-line")
	logger.Info("info-line")
	require.NoError(t, logger.Close())

	fileContent, err := os.ReadFile(logPath)
	require.NoError(t, err)
	file := string(fileContent)

	// Console: INFO threshold suppresses DEBUG.
	assert.Contains(t, console.String(), "info-line")
	assert.NotContains(t, console.String(), "debug-line")

	// File: always captures everything, including DEBUG.
	assert.Contains(t, file, "info-line")
	assert.Contains(t, file, "debug-line")
}

// `pipeline continue` reattaches the same job.log; runs must accumulate rather
// than overwrite, giving one chronological record per job (issue #409).
func TestAttachJobLogFile_AppendsAcrossRuns(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "job.log")

	first := lib.NewLoggerWithWriter(lib.LogLevelInfo, &bytes.Buffer{})
	require.NoError(t, first.AttachJobLogFile(logPath))
	first.Info("first-run")
	require.NoError(t, first.Close())

	second := lib.NewLoggerWithWriter(lib.LogLevelInfo, &bytes.Buffer{})
	require.NoError(t, second.AttachJobLogFile(logPath))
	second.Info("second-run")
	require.NoError(t, second.Close())

	content, err := os.ReadFile(logPath)
	require.NoError(t, err)
	got := string(content)
	assert.Contains(t, got, "first-run")
	assert.Contains(t, got, "second-run")
	assert.Less(t, strings.Index(got, "first-run"), strings.Index(got, "second-run"))
}

// Close on a logger without an attached file must be a safe no-op.
func TestLoggerClose_NoFileAttached(t *testing.T) {
	logger := lib.NewLoggerWithWriter(lib.LogLevelInfo, &bytes.Buffer{})
	assert.NoError(t, logger.Close())
}

// Re-attaching on the same logger must release the previous handle and route
// subsequent lines to the new file, so a leaked handle never accumulates.
func TestAttachJobLogFile_ReattachReleasesPreviousFile(t *testing.T) {
	dir := t.TempDir()
	firstPath := filepath.Join(dir, "first.log")
	secondPath := filepath.Join(dir, "second.log")

	logger := lib.NewLoggerWithWriter(lib.LogLevelInfo, &bytes.Buffer{})
	require.NoError(t, logger.AttachJobLogFile(firstPath))
	logger.Info("to-first")

	// Re-attach on the SAME logger: exercises the close-previous branch.
	require.NoError(t, logger.AttachJobLogFile(secondPath))
	logger.Info("to-second")
	require.NoError(t, logger.Close())

	firstContent, err := os.ReadFile(firstPath)
	require.NoError(t, err)
	secondContent, err := os.ReadFile(secondPath)
	require.NoError(t, err)

	// First file froze at re-attach; second file received the later line only.
	assert.Contains(t, string(firstContent), "to-first")
	assert.NotContains(t, string(firstContent), "to-second")
	assert.Contains(t, string(secondContent), "to-second")
	assert.NotContains(t, string(secondContent), "to-first")
}

// A path whose parent directory does not exist cannot be opened (O_CREATE does
// not create intermediate dirs); the error must be wrapped with the path.
func TestAttachJobLogFile_OpenError(t *testing.T) {
	badPath := filepath.Join(t.TempDir(), "missing-dir", "job.log")

	logger := lib.NewLoggerWithWriter(lib.LogLevelInfo, &bytes.Buffer{})
	err := logger.AttachJobLogFile(badPath)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "opening job log file")
	assert.Contains(t, err.Error(), badPath)
}

func TestGetJobLogFilePath(t *testing.T) {
	got := services.GetJobLogFilePath("/data/jobs", "20260101_1200_abc")
	assert.Equal(t, filepath.Join("/data/jobs", "20260101_1200_abc", "job.log"), got)
}
