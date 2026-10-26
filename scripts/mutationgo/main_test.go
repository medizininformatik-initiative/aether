package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeGo writes a shell script that records its arguments, one per line, in
// the returned file, writes to both output streams and stops with exitCode.
func fakeGo(t *testing.T, exitCode string) (script, argsFile string) {
	t.Helper()

	dir := t.TempDir()
	script = filepath.Join(dir, "go")
	argsFile = filepath.Join(dir, "args.txt")
	body := "#!/bin/sh\n" +
		"printf '%s\\n' \"$@\" > " + argsFile + "\n" +
		"echo from-stdout\n" +
		"echo from-stderr >&2\n" +
		"exit " + exitCode + "\n"
	require.NoError(t, os.WriteFile(script, []byte(body), 0o700))

	return script, argsFile
}

func recordedArgs(t *testing.T, argsFile string) []string {
	t.Helper()

	content, err := os.ReadFile(argsFile)
	require.NoError(t, err)

	return strings.Split(strings.TrimSuffix(string(content), "\n"), "\n")
}

func TestRunPassesUnknownCommandsToTheRealGo(t *testing.T) {
	script, argsFile := fakeGo(t, "7")
	var stdout, stderr bytes.Buffer

	code := run(config{RealGo: script}, []string{"mod", "download"}, &stdout, &stderr)

	assert.Equal(t, 7, code)
	assert.Equal(t, "from-stdout\n", stdout.String())
	assert.Equal(t, "from-stderr\n", stderr.String())
	assert.Equal(t, []string{"mod", "download"}, recordedArgs(t, argsFile))
}
