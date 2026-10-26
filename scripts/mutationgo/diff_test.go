package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChangedLinesListsTheLinesThatDiffer(t *testing.T) {
	original := []byte("a\nb\nc\nd\n")
	mutated := []byte("a\nB\nc\nD\n")

	lines, ok := changedLines(original, mutated)

	assert.True(t, ok)
	assert.Equal(t, []int{2, 4}, lines)
}

func TestChangedLinesFindsNothingInEqualContent(t *testing.T) {
	lines, ok := changedLines([]byte("a\nb\n"), []byte("a\nb\n"))

	assert.True(t, ok)
	assert.Empty(t, lines)
}

func TestChangedLinesGivesUpWhenTheLineCountDiffers(t *testing.T) {
	_, ok := changedLines([]byte("a\nb\n"), []byte("a\nb\nc\n"))

	assert.False(t, ok)
}

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()

	for name, content := range files {
		path := filepath.Join(root, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	}
}

func TestDifferingSourceFilesFindsTheMutatedFile(t *testing.T) {
	source, worker := t.TempDir(), t.TempDir()
	writeTree(t, source, map[string]string{
		"lib/a.go": "package lib\n", "lib/b.go": "package lib\n", "go.mod": "module x\n",
	})
	writeTree(t, worker, map[string]string{
		"lib/a.go": "package lib\n", "lib/b.go": "package lib // m\n", "go.mod": "module x\n",
	})

	files, err := differingSourceFiles(source, worker)

	require.NoError(t, err)
	assert.Equal(t, []string{"lib/b.go"}, files)
}

func TestDifferingSourceFilesIgnoresTestsAndOtherFiles(t *testing.T) {
	source, worker := t.TempDir(), t.TempDir()
	writeTree(t, source, map[string]string{
		"lib/a.go": "package lib\n", "lib/a_test.go": "package lib\n", "README.md": "one\n",
	})
	writeTree(t, worker, map[string]string{
		"lib/a.go": "package lib\n", "lib/a_test.go": "package lib // m\n", "README.md": "two\n",
		"lib/new.txt": "x\n",
	})

	files, err := differingSourceFiles(source, worker)

	require.NoError(t, err)
	assert.Empty(t, files)
}

func TestDifferingSourceFilesReportsNewGoFiles(t *testing.T) {
	source, worker := t.TempDir(), t.TempDir()
	writeTree(t, source, map[string]string{"lib/a.go": "package lib\n"})
	writeTree(t, worker, map[string]string{"lib/a.go": "package lib\n", "lib/b.go": "package lib\n"})

	files, err := differingSourceFiles(source, worker)

	require.NoError(t, err)
	assert.Equal(t, []string{"lib/b.go"}, files)
}
