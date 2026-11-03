//go:build unix

package unit

import (
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/send"
)

func TestFindCSVFiles_NonRegularCSVFileIsError(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, syscall.Mkfifo(filepath.Join(dir, "pipe.csv"), 0o644))

	_, err := send.FindCSVFiles(dir)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "pipe.csv is not a regular file")
}
