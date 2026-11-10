package unit

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/send"
)

func TestFindCSVFiles_FiltersCaseInsensitiveRecursiveAndSorted(t *testing.T) {
	dir := t.TempDir()
	writeSendFile(t, filepath.Join(dir, "b.csv"), "x")
	writeSendFile(t, filepath.Join(dir, "A.CSV"), "x")
	writeSendFile(t, filepath.Join(dir, "sub", "deep", "c.Csv"), "x")
	writeSendFile(t, filepath.Join(dir, "notes.txt"), "x")
	writeSendFile(t, filepath.Join(dir, "data.ndjson"), "x")
	writeSendFile(t, filepath.Join(dir, "csv"), "x")

	files, err := send.FindCSVFiles(dir)
	require.NoError(t, err)

	names := make([]string, len(files))
	for i, f := range files {
		names[i] = f.Name
		assert.Equal(t, filepath.Join(dir, filepath.FromSlash(f.Name)), f.Path)
		assert.True(t, filepath.IsAbs(f.Path))
	}
	assert.Equal(t, []string{"A.CSV", "b.csv", "sub/deep/c.Csv"}, names)
}

func TestFindCSVFiles_FollowsSymlinkedCSVFile(t *testing.T) {
	dir := t.TempDir()
	writeSendFile(t, filepath.Join(dir, "real.csv"), "x")
	require.NoError(t, os.Symlink(filepath.Join(dir, "real.csv"), filepath.Join(dir, "link.csv")))

	files, err := send.FindCSVFiles(dir)

	require.NoError(t, err)
	require.Len(t, files, 2)
	assert.Equal(t, "link.csv", files[0].Name)
	assert.Equal(t, filepath.Join(dir, "link.csv"), files[0].Path)
}

func TestFindCSVFiles_SymlinkLoopIsError(t *testing.T) {
	dir := t.TempDir()
	writeSendFile(t, filepath.Join(dir, "sub", "a.csv"), "x")
	require.NoError(t, os.Symlink(dir, filepath.Join(dir, "sub", "back")))

	_, err := send.FindCSVFiles(dir)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "sub/back")
}

func TestFindCSVFiles_DanglingSymlinkIsError(t *testing.T) {
	dir := t.TempDir()
	writeSendFile(t, filepath.Join(dir, "a.csv"), "x")
	require.NoError(t, os.Symlink(filepath.Join(dir, "missing"), filepath.Join(dir, "gone")))

	_, err := send.FindCSVFiles(dir)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "gone")
}

func TestFindCSVFiles_NoCSVFilesIsError(t *testing.T) {
	dir := t.TempDir()
	writeSendFile(t, filepath.Join(dir, "notes.txt"), "x")

	_, err := send.FindCSVFiles(dir)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no CSV files")
}

func TestFindCSVFiles_MissingOrNonDirectoryIsError(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a.csv")
	writeSendFile(t, file, "x")

	_, err := send.FindCSVFiles(filepath.Join(dir, "absent"))
	require.Error(t, err)

	_, err = send.FindCSVFiles(file)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a directory")
}

func snapshotOf(t *testing.T, dir string) send.Source {
	t.Helper()
	files, err := send.FindCSVFiles(dir)
	require.NoError(t, err)
	src, err := send.SnapshotSource(dir, files)
	require.NoError(t, err)
	return src
}

func TestSnapshotSource_RecordsSizeAndSHA256(t *testing.T) {
	dir := t.TempDir()
	writeSendFile(t, filepath.Join(dir, "a.csv"), "abc")

	src := snapshotOf(t, dir)

	assert.Equal(t, dir, src.Dir)
	assert.False(t, src.Completed)
	require.Len(t, src.Files, 1)
	assert.Equal(t, send.SourceFile{
		Name:   "a.csv",
		Size:   3,
		SHA256: "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
	}, src.Files[0])
}

func TestSourceCompare(t *testing.T) {
	dir := t.TempDir()
	writeSendFile(t, filepath.Join(dir, "a.csv"), "abc")
	writeSendFile(t, filepath.Join(dir, "b.csv"), "def")
	stored := snapshotOf(t, dir)

	t.Run("equal", func(t *testing.T) {
		assert.NoError(t, stored.Compare(snapshotOf(t, dir)))
	})

	t.Run("completed flag is ignored", func(t *testing.T) {
		done := stored
		done.Completed = true
		assert.NoError(t, done.Compare(snapshotOf(t, dir)))
	})

	t.Run("hash change", func(t *testing.T) {
		other := t.TempDir()
		writeSendFile(t, filepath.Join(other, "a.csv"), "abX")
		writeSendFile(t, filepath.Join(other, "b.csv"), "def")
		cur := snapshotOf(t, other)
		cur.Dir = stored.Dir
		err := stored.Compare(cur)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "a.csv")
		assert.Contains(t, err.Error(), "changed")
	})

	t.Run("size change", func(t *testing.T) {
		other := t.TempDir()
		writeSendFile(t, filepath.Join(other, "a.csv"), "abcd")
		writeSendFile(t, filepath.Join(other, "b.csv"), "def")
		cur := snapshotOf(t, other)
		cur.Dir = stored.Dir
		err := stored.Compare(cur)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "a.csv")
		assert.Contains(t, err.Error(), "size")
	})

	t.Run("added and removed", func(t *testing.T) {
		other := t.TempDir()
		writeSendFile(t, filepath.Join(other, "a.csv"), "abc")
		writeSendFile(t, filepath.Join(other, "c.csv"), "ghi")
		cur := snapshotOf(t, other)
		cur.Dir = stored.Dir
		err := stored.Compare(cur)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "added: c.csv")
		assert.Contains(t, err.Error(), "removed: b.csv")
	})

	t.Run("different directory", func(t *testing.T) {
		cur := stored
		cur.Dir = filepath.Join(dir, "elsewhere")
		err := stored.Compare(cur)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "directory")
	})
}

func TestSourcePersistence_RoundTripAndAtomic(t *testing.T) {
	dir := t.TempDir()
	writeSendFile(t, filepath.Join(dir, "a.csv"), "abc")
	src := snapshotOf(t, dir)
	src.Completed = true
	path := filepath.Join(t.TempDir(), "source.json")

	require.NoError(t, send.SaveSource(path, src))
	loaded, err := send.LoadSource(path)
	require.NoError(t, err)
	assert.Equal(t, src, loaded)

	entries, err := os.ReadDir(filepath.Dir(path))
	require.NoError(t, err)
	assert.Len(t, entries, 1, "no temp file remains")

	_, err = send.LoadSource(filepath.Join(dir, "absent.json"))
	assert.Error(t, err)
}

func TestSaveSource_MissingDirectoryIsError(t *testing.T) {
	err := send.SaveSource(filepath.Join(t.TempDir(), "absent", "source.json"), send.Source{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "write source file")
}

func TestSaveSource_PathIsDirectoryIsErrorAndLeavesNoTempFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "source.json")
	require.NoError(t, os.Mkdir(path, 0o755))

	err := send.SaveSource(path, send.Source{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "replace source file")
	entries, readErr := os.ReadDir(dir)
	require.NoError(t, readErr)
	assert.Len(t, entries, 1)
}

func TestLoadSource_InvalidJSONIsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source.json")
	writeSendFile(t, path, "not json")

	_, err := send.LoadSource(path)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse source file")
}

func TestSnapshotSource_UnreadableFileIsError(t *testing.T) {
	dir := t.TempDir()

	_, err := send.SnapshotSource(dir, []send.File{{Path: filepath.Join(dir, "absent.csv"), Name: "absent.csv"}})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "hash absent.csv")
}

func TestFindCSVFiles_UnreadableSubdirectoryIsError(t *testing.T) {
	dir := t.TempDir()
	writeSendFile(t, filepath.Join(dir, "a.csv"), "x")
	locked := filepath.Join(dir, "locked")
	require.NoError(t, os.Mkdir(locked, 0o755))
	require.NoError(t, os.Chmod(locked, 0o000))
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	if _, err := os.ReadDir(locked); err == nil {
		t.Skip("directory permissions are not enforced")
	}

	_, err := send.FindCSVFiles(dir)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "list CSV files")
}

func TestFindCSVFiles_FollowsSymlinkedRoot(t *testing.T) {
	target := t.TempDir()
	writeSendFile(t, filepath.Join(target, "a.csv"), "x")
	link := filepath.Join(t.TempDir(), "csv")
	require.NoError(t, os.Symlink(target, link))

	files, err := send.FindCSVFiles(link)

	require.NoError(t, err)
	require.Len(t, files, 1)
	assert.Equal(t, "a.csv", files[0].Name)
}

func TestFindCSVFiles_FollowsSymlinkedSubdirectory(t *testing.T) {
	archive := t.TempDir()
	writeSendFile(t, filepath.Join(archive, "a.csv"), "x")
	dir := t.TempDir()
	writeSendFile(t, filepath.Join(dir, "b.csv"), "x")
	require.NoError(t, os.Symlink(archive, filepath.Join(dir, "2024")))

	files, err := send.FindCSVFiles(dir)

	require.NoError(t, err)
	names := make([]string, len(files))
	for i, f := range files {
		names[i] = f.Name
	}
	assert.Equal(t, []string{"2024/a.csv", "b.csv"}, names)
}

func TestSnapshotSource_ReadErrorIsError(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "data.csv")
	require.NoError(t, os.Mkdir(sub, 0o755))

	_, err := send.SnapshotSource(dir, []send.File{{Path: sub, Name: "data.csv"}})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "hash data.csv")
}
