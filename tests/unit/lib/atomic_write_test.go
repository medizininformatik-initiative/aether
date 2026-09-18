package lib_test

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/lib"
)

func TestAtomicWriteFileWritesContentWithPermissions(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "report.ndjson")

	require.NoError(t, lib.AtomicWriteFile(dest, []byte("payload"), 0600))

	content, err := os.ReadFile(dest)
	require.NoError(t, err)
	assert.Equal(t, "payload", string(content))

	info, err := os.Stat(dest)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
}

func TestAtomicWriteFileReplacesAnExistingDestination(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "state.json")
	require.NoError(t, os.WriteFile(dest, []byte("the old state"), 0644))

	require.NoError(t, lib.AtomicWriteFile(dest, []byte("the new state"), 0644))

	content, err := os.ReadFile(dest)
	require.NoError(t, err)
	assert.Equal(t, "the new state", string(content))

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Len(t, entries, 1, "no temporary file must stay next to the destination")
}

func TestAtomicWriteStreamLeavesTheDestinationUntouchedWhenTheRenameFails(t *testing.T) {
	dir := t.TempDir()

	// A directory cannot be replaced by a file, thus the rename fails.
	dest := filepath.Join(dir, "report.ndjson")
	require.NoError(t, os.Mkdir(dest, 0755))

	err := lib.AtomicWriteStream(dest, 0644, func(w io.Writer) error {
		_, writeErr := io.WriteString(w, "a complete report")
		return writeErr
	})

	require.Error(t, err)

	info, statErr := os.Stat(dest)
	require.NoError(t, statErr)
	assert.True(t, info.IsDir(), "the destination must stay untouched")

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Len(t, entries, 1, "the temporary file must be removed")
}

func TestAtomicWriteStreamWritesEverythingTheCallbackProduces(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "report.ndjson")

	err := lib.AtomicWriteStream(dest, 0644, func(w io.Writer) error {
		for _, line := range []string{"first\n", "second\n"} {
			if _, err := io.WriteString(w, line); err != nil {
				return err
			}
		}
		return nil
	})
	require.NoError(t, err)

	content, err := os.ReadFile(dest)
	require.NoError(t, err)
	assert.Equal(t, "first\nsecond\n", string(content))
}

func TestAtomicWriteStreamLeavesNoFileWhenTheCallbackFails(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "report.ndjson")
	failure := errors.New("chunk 3 is not valid")

	err := lib.AtomicWriteStream(dest, 0644, func(w io.Writer) error {
		_, _ = io.WriteString(w, "half a report")
		return failure
	})

	require.ErrorIs(t, err, failure)
	assert.NoFileExists(t, dest, "an incomplete report must not appear under its final name")

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, entries, "the temporary file must be removed")
}

func TestAtomicWriteStreamLeavesNoFileWhenTheCallbackPanics(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "report.ndjson")

	assert.Panics(t, func() {
		_ = lib.AtomicWriteStream(dest, 0644, func(w io.Writer) error {
			_, _ = io.WriteString(w, "half a report")
			panic("marshal failed")
		})
	})

	assert.NoFileExists(t, dest, "an incomplete report must not appear under its final name")

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, entries, "the temporary file must be removed")
}

// recordingFileOps records the calls and fails Rename when renameErr is set.
type recordingFileOps struct {
	written     string
	renamedFrom string
	renamedTo   string
	removed     string
	renameErr   error
}

func (r *recordingFileOps) WriteFile(name string, _ []byte, _ os.FileMode) error {
	r.written = name
	return nil
}

func (r *recordingFileOps) Rename(oldpath, newpath string) error {
	r.renamedFrom = oldpath
	r.renamedTo = newpath
	return r.renameErr
}

func (r *recordingFileOps) Remove(name string) error {
	r.removed = name
	return nil
}

func TestAtomicWriteFileWithUsesTheInjectedFileOps(t *testing.T) {
	ops := &recordingFileOps{}
	dest := filepath.Join(t.TempDir(), "state.json")

	require.NoError(t, lib.AtomicWriteFileWith(ops, dest, []byte("{}"), 0644))

	assert.NotEqual(t, dest, ops.written, "the content must go to a temporary name first")
	assert.Equal(t, ops.written, ops.renamedFrom, "the rename must take the file that got the content")
	assert.Equal(t, dest, ops.renamedTo)
	assert.NoFileExists(t, dest, "the injected ops must replace the real file operations")
}

func TestAtomicWriteFileWithRemovesTheTemporaryFileWhenRenameFails(t *testing.T) {
	ops := &recordingFileOps{renameErr: errors.New("disk is full")}
	dest := filepath.Join(t.TempDir(), "state.json")

	err := lib.AtomicWriteFileWith(ops, dest, []byte("{}"), 0644)

	require.Error(t, err)
	assert.Equal(t, ops.written, ops.removed, "the temporary file must be removed")
}

func TestRemoveStaleTempFilesRemovesOnlyTemporaryFiles(t *testing.T) {
	dir := t.TempDir()

	// A killed run leaves a temporary file behind under the documented name.
	stale := filepath.Join(dir, ".report.ndjson.tmp.3f2a1b8c-5d4e-4a7b-9c1d-2e3f4a5b6c7d")
	require.NoError(t, os.WriteFile(stale, []byte("half a report"), 0644))

	report := filepath.Join(dir, "report.ndjson")
	require.NoError(t, os.WriteFile(report, []byte("a complete report"), 0644))

	// A dotfile of another writer that is not a temporary file of this module.
	foreign := filepath.Join(dir, ".notes.tmp.txt")
	require.NoError(t, os.WriteFile(foreign, []byte("keep me"), 0644))

	require.NoError(t, lib.RemoveStaleTempFiles(dir))

	assert.NoFileExists(t, stale)
	assert.FileExists(t, report, "a complete file must survive")
	assert.FileExists(t, foreign, "a file of another writer must survive")
}

func TestAtomicWriteStreamFailsWhenTheTemporaryFileCannotBeCreated(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "missing-dir", "report.ndjson")
	called := false

	err := lib.AtomicWriteStream(dest, 0644, func(io.Writer) error {
		called = true
		return nil
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create temporary file")
	assert.False(t, called, "the callback must not run when no temporary file exists")
	assert.NoFileExists(t, dest)
}
