package main

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var mutantArgs = []string{"test", "-timeout", "12s", "-failfast", "./..."}

// mutantSetup creates a source module, a worker copy with a mutated line 2 of
// lib/a.go, and a test map in which TestA covers lines 1 to 3. It changes to
// the worker directory.
type mutantSetup struct {
	config   config
	argsFile string
	source   string
	worker   string
}

func newMutantSetup(t *testing.T) mutantSetup {
	t.Helper()

	script, argsFile := fakeGo(t, "1")
	source, worker := t.TempDir(), t.TempDir()
	writeTree(t, source, map[string]string{"lib/a.go": "package lib\nvar x = 1\nvar y = 2\n"})
	writeTree(t, worker, map[string]string{"lib/a.go": "package lib\nvar x = 2\nvar y = 2\n"})

	mapPath := filepath.Join(t.TempDir(), "map.json")
	require.NoError(t, writeTestMap(mapPath, testMap{
		Tests: []testID{{Package: "example.com/mod/lib", Name: "TestA"}},
		Files: map[string][]mapBlock{"lib/a.go": {{StartLine: 1, EndLine: 3, Tests: []int{0}}}},
	}))

	t.Chdir(worker)

	return mutantSetup{
		config:   config{RealGo: script, SourceDir: source, MapPath: mapPath},
		argsFile: argsFile,
		source:   source,
		worker:   worker,
	}
}

func (s mutantSetup) run(t *testing.T) (code int, stdout string) {
	t.Helper()

	var out, errOut bytes.Buffer
	code = run(s.config, mutantArgs, &out, &errOut)

	return code, out.String()
}

func TestRunLimitsAMutantCallToTheTestsThatCoverTheMutatedLine(t *testing.T) {
	setup := newMutantSetup(t)

	code, stdout := setup.run(t)

	assert.Equal(t, 1, code)
	assert.Equal(t, "from-stdout\n", stdout)
	assert.Equal(t, []string{
		"test", "-timeout", "12s", "-failfast", "-run", "^(TestA)$", "example.com/mod/lib",
	}, recordedArgs(t, setup.argsFile))
}

func TestRunKeepsTheFullMutantCallWhenTheMapIsMissing(t *testing.T) {
	setup := newMutantSetup(t)
	setup.config.MapPath = filepath.Join(t.TempDir(), "missing.json")

	setup.run(t)

	assert.Equal(t, mutantArgs, recordedArgs(t, setup.argsFile))
}

func TestRunKeepsTheFullMutantCallWhenNoSourceFileDiffers(t *testing.T) {
	setup := newMutantSetup(t)
	writeTree(t, setup.worker, map[string]string{"lib/a.go": "package lib\nvar x = 1\nvar y = 2\n"})

	setup.run(t)

	assert.Equal(t, mutantArgs, recordedArgs(t, setup.argsFile))
}

func TestRunKeepsTheFullMutantCallWhenSeveralSourceFilesDiffer(t *testing.T) {
	setup := newMutantSetup(t)
	writeTree(t, setup.worker, map[string]string{"lib/b.go": "package lib\n"})

	setup.run(t)

	assert.Equal(t, mutantArgs, recordedArgs(t, setup.argsFile))
}

func TestRunKeepsTheFullMutantCallWhenTheLineCountChanged(t *testing.T) {
	setup := newMutantSetup(t)
	writeTree(t, setup.worker, map[string]string{"lib/a.go": "package lib\nvar x = 2\nvar y = 2\nvar z = 3\n"})

	setup.run(t)

	assert.Equal(t, mutantArgs, recordedArgs(t, setup.argsFile))
}

func TestRunKeepsTheFullMutantCallWhenNoTestCoversTheMutatedLine(t *testing.T) {
	setup := newMutantSetup(t)
	require.NoError(t, writeTestMap(setup.config.MapPath, testMap{
		Tests: []testID{{Package: "example.com/mod/lib", Name: "TestA"}},
		Files: map[string][]mapBlock{"lib/a.go": {{StartLine: 3, EndLine: 3, Tests: []int{0}}}},
	}))

	setup.run(t)

	assert.Equal(t, mutantArgs, recordedArgs(t, setup.argsFile))
}
