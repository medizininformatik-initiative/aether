package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTestMapSurvivesAWriteAndRead(t *testing.T) {
	want := testMap{
		Tests: []testID{{Package: "example.com/mod/lib", Name: "TestA"}},
		Files: map[string][]mapBlock{"lib/a.go": {{StartLine: 3, EndLine: 5, Tests: []int{0}}}},
	}
	path := filepath.Join(t.TempDir(), "map.json")

	require.NoError(t, writeTestMap(path, want))
	got, err := readTestMap(path)

	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestReadTestMapFailsForMissingAndBrokenFiles(t *testing.T) {
	dir := t.TempDir()
	_, err := readTestMap(filepath.Join(dir, "missing.json"))
	assert.Error(t, err)

	broken := filepath.Join(dir, "broken.json")
	require.NoError(t, os.WriteFile(broken, []byte("{"), 0o600))
	_, err = readTestMap(broken)
	assert.Error(t, err)
}
