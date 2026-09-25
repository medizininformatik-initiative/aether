package lib

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAttachJobLogFile_ReattachClosesPreviousHandle(t *testing.T) {
	dir := t.TempDir()
	logger := NewLoggerWithWriter(LogLevelInfo, io.Discard)

	require.NoError(t, logger.AttachJobLogFile(filepath.Join(dir, "first.log")))
	previous := logger.file

	require.NoError(t, logger.AttachJobLogFile(filepath.Join(dir, "second.log")))
	t.Cleanup(func() { _ = logger.Close() })

	_, err := previous.Write([]byte("x"))
	assert.ErrorIs(t, err, os.ErrClosed)
}
