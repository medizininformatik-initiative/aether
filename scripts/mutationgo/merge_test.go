package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/cover"
)

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

func TestWriteMergedProfileReturnsTheWriteError(t *testing.T) {
	err := writeMergedProfile(failingWriter{}, []testResult{{Profiles: profilesOf(t, "mode: set\nexample.com/mod/a.go:3.1,4.2 2 1\n")}})

	require.ErrorContains(t, err, "disk full")
}

func TestWriteMergedProfileCountsABlockIfAnyTestRanIt(t *testing.T) {
	first := profilesOf(t,
		"mode: set\nexample.com/mod/b.go:1.1,2.2 1 0\nexample.com/mod/a.go:3.1,4.2 2 1\nexample.com/mod/a.go:6.1,7.2 1 0\n")
	second := profilesOf(t,
		"mode: set\nexample.com/mod/b.go:1.1,2.2 1 1\nexample.com/mod/a.go:3.1,4.2 2 1\nexample.com/mod/a.go:6.1,7.2 1 0\n")

	var out bytes.Buffer
	require.NoError(t, writeMergedProfile(&out, []testResult{{Profiles: first}, {Profiles: second}}))

	assert.Equal(t, "mode: set\n"+
		"example.com/mod/a.go:3.1,4.2 2 1\n"+
		"example.com/mod/a.go:6.1,7.2 1 0\n"+
		"example.com/mod/b.go:1.1,2.2 1 1\n", out.String())
}

// gremlins reads the profile with golang.org/x/tools/cover.
func TestMergedProfileIsReadableByGremlins(t *testing.T) {
	var out bytes.Buffer
	require.NoError(t, writeMergedProfile(&out, []testResult{{Profiles: profilesOf(t, "mode: set\nexample.com/mod/a.go:3.1,4.2 2 1\n")}}))
	path := filepath.Join(t.TempDir(), "coverage")
	require.NoError(t, os.WriteFile(path, out.Bytes(), 0o600))

	profiles, err := cover.ParseProfiles(path)

	require.NoError(t, err)
	require.Len(t, profiles, 1)
	assert.Equal(t, "example.com/mod/a.go", profiles[0].FileName)
	assert.Equal(t, "set", profiles[0].Mode)
	require.Len(t, profiles[0].Blocks, 1)
	assert.Equal(t, 1, profiles[0].Blocks[0].Count)
}
