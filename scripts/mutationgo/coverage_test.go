package main

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/cover"
)

func TestParseCoverageArgsReadsTagsPackagesAndProfile(t *testing.T) {
	got := parseCoverageArgs([]string{
		"test", "-tags", "integration", "-coverpkg", "./internal/...",
		"-cover", "-coverprofile", "/work/coverage", "./...",
	})

	assert.Equal(t, coverageOptions{Tags: "integration", CoverPkg: "./internal/...", Profile: "/work/coverage"}, got)
}

func TestParseCoverageArgsAllowsMissingTagsAndPackages(t *testing.T) {
	got := parseCoverageArgs([]string{"test", "-cover", "-coverprofile", "/work/coverage", "./..."})

	assert.Equal(t, coverageOptions{Profile: "/work/coverage"}, got)
}

func TestParseTestListKeepsTestsFuzzTargetsAndExamples(t *testing.T) {
	got := parseTestList("TestA\nBenchmarkB\nFuzzC\nExampleD\nTestE\n\n")

	assert.Equal(t, []string{"TestA", "FuzzC", "ExampleD", "TestE"}, got)
}

func realGo(t *testing.T) string {
	t.Helper()

	path, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go is not on the PATH")
	}

	return path
}

const tinyLib = `package lib

func Add(a, b int) int {
	return a + b
}

func Sub(a, b int) int {
	return a - b
}
`

const tinyTests = `package lib

import "testing"

func TestAdd(t *testing.T) {
	if Add(1, 2) != 3 {
		t.Fatal("wrong sum")
	}
}

func TestSub(t *testing.T) {
	if Sub(3, 2) != 1 {
		t.Fatal("wrong difference")
	}
}
`

// newTinyModule creates a module with one package. Each of its two tests covers
// one function. It changes to the module directory.
func newTinyModule(t *testing.T, tests string) {
	t.Helper()

	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"go.mod":          "module example.com/tiny\n\ngo 1.21\n",
		"lib/lib.go":      tinyLib,
		"lib/lib_test.go": tests,
	})
	t.Chdir(dir)
}

func TestRunMeasuresWhichTestCoversWhichLine(t *testing.T) {
	newTinyModule(t, tinyTests)
	mapPath := filepath.Join(t.TempDir(), "map.json")
	profilePath := filepath.Join(t.TempDir(), "coverage")
	cfg := config{RealGo: realGo(t), MapPath: mapPath}
	var stdout, stderr bytes.Buffer

	code := run(cfg, []string{
		"test", "-coverpkg", "./...", "-cover", "-coverprofile", profilePath, "./...",
	}, &stdout, &stderr)

	require.Equal(t, 0, code, stderr.String())

	tm, err := readTestMap(mapPath)
	require.NoError(t, err)
	add := testID{Package: "example.com/tiny/lib", Name: "TestAdd"}
	sub := testID{Package: "example.com/tiny/lib", Name: "TestSub"}
	assert.Equal(t, []testID{add}, selectTests(tm, "lib/lib.go", []int{4}))
	assert.Equal(t, []testID{sub}, selectTests(tm, "lib/lib.go", []int{8}))

	profiles, err := cover.ParseProfiles(profilePath)
	require.NoError(t, err)
	require.Len(t, profiles, 1)
	assert.Equal(t, "set", profiles[0].Mode)
	require.Len(t, profiles[0].Blocks, 2)
	for _, b := range profiles[0].Blocks {
		assert.Equal(t, 1, b.Count)
	}
}

func TestRunMeasuresTestsOfOtherPackagesFuzzTargetsAndExamples(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"go.mod":      "module example.com/tiny\n\ngo 1.21\n",
		"lib/lib.go":  tinyLib,
		"cmd/main.go": "package main\n\nfunc main() {}\n",
		"tests/lib_test.go": `package tests

import (
	"fmt"
	"testing"

	"example.com/tiny/lib"
)

func FuzzAdd(f *testing.F) {
	f.Add(1)
	f.Fuzz(func(t *testing.T, n int) {
		if lib.Add(n, 0) != n {
			t.Fatal("wrong sum")
		}
	})
}

func ExampleSub() {
	fmt.Println(lib.Sub(3, 2))
	// Output: 1
}
`,
	})
	t.Chdir(dir)
	mapPath := filepath.Join(t.TempDir(), "map.json")
	cfg := config{RealGo: realGo(t), MapPath: mapPath}
	var stdout, stderr bytes.Buffer

	code := run(cfg, []string{
		"test", "-coverpkg", "./lib/...", "-cover", "-coverprofile", filepath.Join(t.TempDir(), "coverage"), "./...",
	}, &stdout, &stderr)

	require.Equal(t, 0, code, stderr.String())

	tm, err := readTestMap(mapPath)
	require.NoError(t, err)
	assert.Equal(t, []testID{{Package: "example.com/tiny/tests", Name: "FuzzAdd"}},
		selectTests(tm, "lib/lib.go", []int{4}))
	assert.Equal(t, []testID{{Package: "example.com/tiny/tests", Name: "ExampleSub"}},
		selectTests(tm, "lib/lib.go", []int{8}))
}

// orderDependentTests holds a test that passes after TestAdd and fails alone.
const orderDependentTests = tinyTests + `
var addRan bool

func TestAddRuns(t *testing.T) { addRan = true }

func TestNeedsAddRuns(t *testing.T) {
	if !addRan {
		t.Fatal("TestAddRuns did not run first")
	}
}
`

func TestRunFallsBackToTheOriginalCoverageCallWhenATestFailsAlone(t *testing.T) {
	newTinyModule(t, orderDependentTests)
	mapPath := filepath.Join(t.TempDir(), "map.json")
	require.NoError(t, writeTestMap(mapPath, testMap{}))
	profilePath := filepath.Join(t.TempDir(), "coverage")
	cfg := config{RealGo: realGo(t), MapPath: mapPath}
	var stdout, stderr bytes.Buffer

	code := run(cfg, []string{
		"test", "-cover", "-coverprofile", profilePath, "./...",
	}, &stdout, &stderr)

	assert.Equal(t, 0, code, stderr.String())
	assert.Contains(t, stderr.String(), "TestNeedsAddRuns")
	assert.NoFileExists(t, mapPath)
	assert.FileExists(t, profilePath)
}

func TestRunGivesTheResultOfTheOriginalCoverageCallWhenATestFails(t *testing.T) {
	newTinyModule(t, tinyTests+`
func TestBroken(t *testing.T) { t.Fatal("broken on purpose") }
`)
	mapPath := filepath.Join(t.TempDir(), "map.json")
	cfg := config{RealGo: realGo(t), MapPath: mapPath}
	var stdout, stderr bytes.Buffer

	code := run(cfg, []string{
		"test", "-cover", "-coverprofile", filepath.Join(t.TempDir(), "coverage"), "./...",
	}, &stdout, &stderr)

	assert.NotZero(t, code)
	assert.Contains(t, stdout.String()+stderr.String(), "broken on purpose")
	assert.NoFileExists(t, mapPath)
}
