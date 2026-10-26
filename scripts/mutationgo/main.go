// Command mutationgo stands in for the go command while gremlins runs. It
// limits the test run of each mutant to the tests that cover the mutated line.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

// config holds the settings that scripts/mutation.sh gives in the environment.
type config struct {
	// RealGo is the path of the real go command.
	RealGo string
	// SourceDir is the module root that gremlins copies for its workers.
	SourceDir string
	// MapPath is the file that maps covered lines to tests.
	MapPath string
}

func configFromEnv(getenv func(string) string) config {
	return config{
		RealGo:    getenv("MUTATION_REAL_GO"),
		SourceDir: getenv("MUTATION_SOURCE_DIR"),
		MapPath:   getenv("MUTATION_TEST_MAP"),
	}
}

func main() {
	os.Exit(run(configFromEnv(os.Getenv), os.Args[1:], os.Stdout, os.Stderr))
}

func run(cfg config, args []string, stdout, stderr io.Writer) int {
	if cfg.RealGo == "" {
		_, _ = fmt.Fprintln(stderr, "mutationgo: MUTATION_REAL_GO is not set")

		return 1
	}

	switch classify(args) {
	case callCoverage:
		return runCoverage(cfg, args, stdout, stderr)
	case callMutant:
		args = limitMutantCall(cfg, args)
	case callOther:
	}

	return execGo(cfg.RealGo, args, stdout, stderr)
}

// limitMutantCall returns the arguments of the mutant call with the tests that
// cover the mutated line. It returns the given arguments if it cannot find
// these tests with certainty. The full test run is always correct, only slower.
func limitMutantCall(cfg config, args []string) []string {
	tm, err := readTestMap(cfg.MapPath)
	if err != nil {
		return args
	}

	file, lines, ok := findMutation(cfg.SourceDir)
	if !ok {
		return args
	}

	tests := selectTests(tm, file, lines)
	if len(tests) == 0 {
		return args
	}

	return selectedTestArgs(args, tests)
}

// findMutation returns the source file that the mutation changed in the
// current directory, and the lines that it changed.
func findMutation(sourceDir string) (file string, lines []int, ok bool) {
	workerDir, err := os.Getwd()
	if err != nil {
		return "", nil, false
	}

	files, err := differingSourceFiles(sourceDir, workerDir)
	if err != nil || len(files) != 1 {
		return "", nil, false
	}

	original, err := os.ReadFile(filepath.Join(sourceDir, files[0]))
	if err != nil {
		return "", nil, false
	}

	mutated, err := os.ReadFile(filepath.Join(workerDir, files[0]))
	if err != nil {
		return "", nil, false
	}

	lines, ok = changedLines(original, mutated)

	return files[0], lines, ok
}

// execGo runs the real go command and returns its exit code.
func execGo(realGo string, args []string, stdout, stderr io.Writer) int {
	cmd := exec.Command(realGo, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	err := cmd.Run()
	if err == nil {
		return 0
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}

	_, _ = fmt.Fprintf(stderr, "mutationgo: %v\n", err)

	return 1
}
