package unit

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func TestImportCSVFiles_CopiesTopLevelFiles(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	writeTestFile(t, filepath.Join(src, "patient.csv"), "id\n1\n")
	writeTestFile(t, filepath.Join(src, "observation.csv"), "id,code\n1,a\n")

	files, bytes, err := services.ImportCSVFiles(src, dst, lib.NewLogger(lib.LogLevelInfo))

	require.NoError(t, err)
	assert.Equal(t, 2, files)
	assert.Equal(t, int64(len("id\n1\n")+len("id,code\n1,a\n")), bytes)
	content, err := os.ReadFile(filepath.Join(dst, "patient.csv"))
	require.NoError(t, err)
	assert.Equal(t, "id\n1\n", string(content))
	assert.FileExists(t, filepath.Join(dst, "observation.csv"))
}

func TestImportCSVFiles_KeepsRelativePathsOfSubdirectories(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	writeTestFile(t, filepath.Join(src, "a", "x.csv"), "x\n")
	writeTestFile(t, filepath.Join(src, "a", "b", "y.csv"), "y\n")
	writeTestFile(t, filepath.Join(src, "top.csv"), "t\n")

	files, bytes, err := services.ImportCSVFiles(src, dst, lib.NewLogger(lib.LogLevelInfo))

	require.NoError(t, err)
	assert.Equal(t, 3, files)
	assert.Equal(t, int64(6), bytes)
	assert.FileExists(t, filepath.Join(dst, "a", "x.csv"))
	assert.FileExists(t, filepath.Join(dst, "a", "b", "y.csv"))
	assert.FileExists(t, filepath.Join(dst, "top.csv"))
}

func TestImportCSVFiles_IgnoresOtherFilesAndMatchesExtensionCaseInsensitive(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	writeTestFile(t, filepath.Join(src, "Patient.ndjson"), "{}\n")
	writeTestFile(t, filepath.Join(src, "notes.txt"), "hello")
	writeTestFile(t, filepath.Join(src, "UPPER.CSV"), "id\n")
	writeTestFile(t, filepath.Join(src, "lower.csv"), "id\n")

	files, _, err := services.ImportCSVFiles(src, dst, lib.NewLogger(lib.LogLevelInfo))

	require.NoError(t, err)
	assert.Equal(t, 2, files)
	assert.FileExists(t, filepath.Join(dst, "UPPER.CSV"))
	assert.FileExists(t, filepath.Join(dst, "lower.csv"))
	assert.NoFileExists(t, filepath.Join(dst, "Patient.ndjson"))
	assert.NoFileExists(t, filepath.Join(dst, "notes.txt"))
}

func TestImportCSVFiles_FailsWithoutCSVFiles(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	writeTestFile(t, filepath.Join(src, "Patient.ndjson"), "{}\n")

	_, _, err := services.ImportCSVFiles(src, filepath.Join(root, "dst"), lib.NewLogger(lib.LogLevelInfo))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no CSV files found")
	assert.Contains(t, err.Error(), src)
}

func TestImportCSVFiles_FailsForMissingSourceOrFile(t *testing.T) {
	root := t.TempDir()
	logger := lib.NewLogger(lib.LogLevelInfo)

	_, _, err := services.ImportCSVFiles(filepath.Join(root, "missing"), filepath.Join(root, "dst"), logger)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "directory does not exist")

	file := filepath.Join(root, "a.csv")
	writeTestFile(t, file, "id\n")
	_, _, err = services.ImportCSVFiles(file, filepath.Join(root, "dst"), logger)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected directory but got file")
}

func TestImportCSVFiles_FailsWhenDestinationCannotBeCreated(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	parent := filepath.Join(root, "readonly")
	writeTestFile(t, filepath.Join(src, "a.csv"), "id\n")
	require.NoError(t, os.MkdirAll(parent, 0o755))
	require.NoError(t, os.Chmod(parent, 0o555))
	t.Cleanup(func() { _ = os.Chmod(parent, 0o755) })

	_, _, err := services.ImportCSVFiles(src, filepath.Join(parent, "dst"), lib.NewLogger(lib.LogLevelInfo))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to copy a.csv")
}

func TestImportCSVFiles_FailsWhenDestinationCannotBeCleared(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	parent := filepath.Join(root, "readonly")
	dst := filepath.Join(parent, "dst")
	writeTestFile(t, filepath.Join(src, "a.csv"), "id\n")
	writeTestFile(t, filepath.Join(dst, "old.csv"), "o\n")
	require.NoError(t, os.Chmod(dst, 0o555))
	t.Cleanup(func() { _ = os.Chmod(dst, 0o755) })

	_, _, err := services.ImportCSVFiles(src, dst, lib.NewLogger(lib.LogLevelInfo))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to clear destination directory")
}

func TestImportCSVFiles_FailsForUnreadableFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores file permissions")
	}
	root := t.TempDir()
	src := filepath.Join(root, "src")
	writeTestFile(t, filepath.Join(src, "a.csv"), "id\n")
	require.NoError(t, os.Chmod(filepath.Join(src, "a.csv"), 0o000))

	_, _, err := services.ImportCSVFiles(src, filepath.Join(root, "dst"), lib.NewLogger(lib.LogLevelInfo))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to copy a.csv")
}

func TestImportCSVFiles_FailsForUnreadableSubdirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores file permissions")
	}
	root := t.TempDir()
	src := filepath.Join(root, "src")
	writeTestFile(t, filepath.Join(src, "sub", "a.csv"), "id\n")
	require.NoError(t, os.Chmod(filepath.Join(src, "sub"), 0o000))
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(src, "sub"), 0o755) })

	_, _, err := services.ImportCSVFiles(src, filepath.Join(root, "dst"), lib.NewLogger(lib.LogLevelInfo))

	require.Error(t, err)
}

func TestImportCSVFiles_FailsWhenCSVEntryCannotBeRead(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	writeTestFile(t, filepath.Join(src, "other", "keep.txt"), "x")
	require.NoError(t, os.Symlink(filepath.Join(src, "other"), filepath.Join(src, "linked.csv")))

	_, _, err := services.ImportCSVFiles(src, filepath.Join(root, "dst"), lib.NewLogger(lib.LogLevelInfo))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to copy linked.csv")
}

func TestImportCSVFiles_RemovesFilesOfAnEarlierRun(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	writeTestFile(t, filepath.Join(src, "keep.csv"), "k\n")
	writeTestFile(t, filepath.Join(dst, "stale.csv"), "s\n")
	writeTestFile(t, filepath.Join(dst, "old", "stale.csv"), "s\n")

	files, _, err := services.ImportCSVFiles(src, dst, lib.NewLogger(lib.LogLevelInfo))

	require.NoError(t, err)
	assert.Equal(t, 1, files)
	assert.FileExists(t, filepath.Join(dst, "keep.csv"))
	assert.NoFileExists(t, filepath.Join(dst, "stale.csv"))
	assert.NoDirExists(t, filepath.Join(dst, "old"))
}

func TestImportCSVFiles_RefusesSourceInsideDestination(t *testing.T) {
	root := t.TempDir()
	dst := filepath.Join(root, "dst")
	for name, src := range map[string]string{
		"same":   dst,
		"nested": filepath.Join(dst, "sub"),
	} {
		t.Run(name, func(t *testing.T) {
			writeTestFile(t, filepath.Join(src, "data.csv"), "d\n")

			_, _, err := services.ImportCSVFiles(src, dst, lib.NewLogger(lib.LogLevelInfo))

			require.Error(t, err)
			assert.Contains(t, err.Error(), "source directory is inside the destination directory")
			assert.FileExists(t, filepath.Join(src, "data.csv"))
		})
	}
}

func TestImportCSVFiles_RefusesSymlinkToDestination(t *testing.T) {
	root := t.TempDir()
	dst := filepath.Join(root, "dst")
	link := filepath.Join(root, "link")
	writeTestFile(t, filepath.Join(dst, "data.csv"), "d\n")
	require.NoError(t, os.Symlink(dst, link))

	_, _, err := services.ImportCSVFiles(link, dst, lib.NewLogger(lib.LogLevelInfo))

	require.Error(t, err)
	assert.FileExists(t, filepath.Join(dst, "data.csv"))
}
