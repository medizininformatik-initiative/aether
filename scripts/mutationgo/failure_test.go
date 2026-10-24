package main

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSameContentComparesSizeAndBytes(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"a": "abc", "same": "abc", "other": "abd", "longer": "abcd"})

	tests := []struct {
		other string
		want  bool
	}{
		{"same", true},
		{"other", false},
		{"longer", false},
		{"missing", false},
	}

	for _, tt := range tests {
		got, err := sameContent(filepath.Join(dir, "a"), filepath.Join(dir, tt.other))

		require.NoError(t, err)
		assert.Equal(t, tt.want, got, tt.other)
	}
}

func TestSameContentFailsForAMissingFirstFile(t *testing.T) {
	dir := t.TempDir()

	_, err := sameContent(filepath.Join(dir, "missing"), filepath.Join(dir, "other"))

	assert.Error(t, err)
}

func TestMeasureCoverageFailsWithoutAProfileFile(t *testing.T) {
	err := measureCoverage(config{}, coverageOptions{}, &bytes.Buffer{})

	assert.ErrorContains(t, err, "no profile file")
}

func TestMeasureCoverageFailsOutsideAModule(t *testing.T) {
	t.Chdir(t.TempDir())
	cfg := config{RealGo: realGo(t), MapPath: filepath.Join(t.TempDir(), "map.json")}

	err := measureCoverage(cfg, coverageOptions{Profile: filepath.Join(t.TempDir(), "coverage")}, &bytes.Buffer{})

	assert.ErrorContains(t, err, "go list")
}

func TestMeasureCoverageFailsWhenTheTestMapCannotBeWritten(t *testing.T) {
	newTinyModule(t, tinyTests)
	cfg := config{RealGo: realGo(t), MapPath: filepath.Join(t.TempDir(), "missing", "map.json")}

	err := measureCoverage(cfg, coverageOptions{Profile: filepath.Join(t.TempDir(), "coverage")}, &bytes.Buffer{})

	assert.ErrorContains(t, err, "write test map")
}

func TestMeasureCoverageFailsWhenTheProfileCannotBeWritten(t *testing.T) {
	newTinyModule(t, tinyTests)
	cfg := config{RealGo: realGo(t), MapPath: filepath.Join(t.TempDir(), "map.json")}

	err := measureCoverage(cfg, coverageOptions{Profile: filepath.Join(t.TempDir(), "missing", "coverage")}, &bytes.Buffer{})

	assert.Error(t, err)
}

func TestConfigFromEnvReadsTheSettingsOfTheScript(t *testing.T) {
	env := map[string]string{
		"MUTATION_REAL_GO":    "/usr/bin/go",
		"MUTATION_SOURCE_DIR": "/src",
		"MUTATION_TEST_MAP":   "/map.json",
	}

	got := configFromEnv(func(key string) string { return env[key] })

	assert.Equal(t, config{RealGo: "/usr/bin/go", SourceDir: "/src", MapPath: "/map.json"}, got)
}

func TestRunFailsWithoutTheRealGo(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := run(config{}, []string{"version"}, &stdout, &stderr)

	assert.Equal(t, 1, code)
	assert.Contains(t, stderr.String(), "MUTATION_REAL_GO is not set")
}

func TestExecGoReportsAGoCommandThatCannotStart(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := execGo(filepath.Join(t.TempDir(), "missing"), []string{"version"}, &stdout, &stderr)

	assert.Equal(t, 1, code)
	assert.Contains(t, stderr.String(), "mutationgo:")
}
