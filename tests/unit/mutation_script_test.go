package unit

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// mutationScript returns the absolute path of scripts/mutation.sh.
func mutationScript(t *testing.T) string {
	t.Helper()

	path, err := filepath.Abs(filepath.Join("..", "..", "scripts", "mutation.sh"))
	require.NoError(t, err)

	return path
}

// mutationEnv builds a closed environment for the script and for git. It keeps
// the ambient environment out, so the result does not change with the settings
// of the machine that runs the test.
func mutationEnv(t *testing.T, binDir string, extra ...string) []string {
	t.Helper()

	git, err := exec.LookPath("git")
	require.NoError(t, err)

	path := filepath.Dir(git)
	if binDir != "" {
		path = binDir + ":" + path
	}

	env := []string{
		"PATH=" + path,
		"HOME=" + t.TempDir(),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
	}

	return append(env, extra...)
}

// newDiffRepo creates a git repository with a base branch and a feature branch.
// The base branch has an internal directory. The feature branch adds the given
// files.
func newDiffRepo(t *testing.T, files map[string]string) string {
	t.Helper()

	dir := t.TempDir()
	env := mutationEnv(t, "")
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
	}

	run("init", "--initial-branch=base")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "internal", "doc.go"), []byte("package internal\n"), 0o600))
	run("add", "internal/doc.go")
	run("commit", "-m", "base")
	run("checkout", "-b", "feature")

	for name, content := range files {
		path := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
		run("add", name)
	}

	run("commit", "-m", "feature")

	return dir
}

// gremlinsStub is a fake gremlins on the PATH. It records what the script gives
// it.
type gremlinsStub struct {
	binDir   string
	argsFile string
	tempFile string
}

// stubGremlins puts a fake gremlins in a new directory and returns it. The fake
// writes a report to the file of the --output flag.
func stubGremlins(t *testing.T) gremlinsStub {
	t.Helper()

	return writeGremlinsStub(t,
		"out=''; prev=''\n"+
			"for arg in \"$@\"; do [ \"$prev\" = --output ] && out=\"$arg\"; prev=\"$arg\"; done\n"+
			"echo '{\"mutants_total\":1}' > \"$out\"\n")
}

// stubGremlinsWithoutMutants puts a fake gremlins in a new directory. Like the
// real gremlins, the fake writes no report when it finds no mutants.
func stubGremlinsWithoutMutants(t *testing.T) gremlinsStub {
	t.Helper()

	return writeGremlinsStub(t, "echo 'No results to report.'\n")
}

func writeGremlinsStub(t *testing.T, body string) gremlinsStub {
	t.Helper()

	stub := gremlinsStub{binDir: t.TempDir()}
	stub.argsFile = filepath.Join(stub.binDir, "args.txt")
	stub.tempFile = filepath.Join(stub.binDir, "tmpdir.txt")

	script := "#!/bin/bash\n" +
		"printf '%s\\n' \"$@\" > " + stub.argsFile + "\n" +
		"printf '%s' \"$TMPDIR\" > " + stub.tempFile + "\n" +
		body
	require.NoError(t, os.WriteFile(filepath.Join(stub.binDir, "gremlins"), []byte(script), 0o700))

	return stub
}

// read returns the content of one of the files the stub wrote.
func (s gremlinsStub) read(t *testing.T, file string) string {
	t.Helper()

	content, err := os.ReadFile(file)
	require.NoError(t, err)

	return string(content)
}

func TestMutationDiffSkipsWhenNoGoFileChanged(t *testing.T) {
	repo := newDiffRepo(t, map[string]string{"docs/guide.md": "text\n"})

	cmd := exec.Command(mutationScript(t), "diff")
	cmd.Dir = repo
	cmd.Env = mutationEnv(t, "", "MUTATION_REF=base")

	out, err := cmd.CombinedOutput()

	require.NoError(t, err, string(out))
	require.Contains(t, string(out), "No Go file in ./internal differs from base")
}

// gremlins mutates no test file and nothing outside PKG. Without a changed
// source file in PKG, it finds no mutants, and the run fails.
func TestMutationDiffSkipsWhenOnlyTestsOrOtherDirectoriesChanged(t *testing.T) {
	repo := newDiffRepo(t, map[string]string{
		"internal/lib/add_test.go": "package lib\n",
		"tests/unit/add_test.go":   "package unit\n",
		"cmd/aether/main.go":       "package main\n",
	})
	stub := stubGremlins(t)

	cmd := exec.Command(mutationScript(t), "diff")
	cmd.Dir = repo
	cmd.Env = mutationEnv(t, stub.binDir, "MUTATION_REF=base", "MUTATION_TMPDIR="+t.TempDir())

	out, err := cmd.CombinedOutput()

	require.NoError(t, err, string(out))
	require.Contains(t, string(out), "No Go file in ./internal differs from base")
	require.NoFileExists(t, stub.argsFile)
}

func TestMutationDiffStopsWhenTheReferenceIsUnknown(t *testing.T) {
	repo := newDiffRepo(t, map[string]string{"internal/lib/add.go": "package lib\n"})
	stub := stubGremlins(t)

	cmd := exec.Command(mutationScript(t), "diff")
	cmd.Dir = repo
	cmd.Env = mutationEnv(t, stub.binDir, "MUTATION_REF=origin/main", "MUTATION_TMPDIR="+t.TempDir())

	out, err := cmd.CombinedOutput()

	require.Error(t, err, string(out))
	require.NotContains(t, string(out), "No Go file differs")
	require.NoFileExists(t, stub.argsFile)
}

func TestMutationDiffRunsGremlinsAgainstTheReference(t *testing.T) {
	repo := newDiffRepo(t, map[string]string{"internal/lib/add.go": "package lib\n"})
	stub := stubGremlins(t)

	cmd := exec.Command(mutationScript(t), "diff")
	cmd.Dir = repo
	cmd.Env = mutationEnv(t, stub.binDir, "MUTATION_REF=base", "MUTATION_TMPDIR="+t.TempDir())

	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))

	args := stub.read(t, stub.argsFile)
	require.Contains(t, args, "unleash")
	require.Contains(t, args, "--diff\nbase\n")
	require.Contains(t, args, "\n./internal\n")
}

// gremlins takes a directory, not a Go package pattern. With "./internal/..."
// it finds no mutants.
func TestMutationTestsTheInternalDirectoryByDefault(t *testing.T) {
	repo := newDiffRepo(t, map[string]string{"internal/lib/add.go": "package lib\n"})
	stub := stubGremlins(t)

	cmd := exec.Command(mutationScript(t))
	cmd.Dir = repo
	cmd.Env = mutationEnv(t, stub.binDir, "MUTATION_TMPDIR="+t.TempDir())

	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))

	require.Contains(t, stub.read(t, stub.argsFile), "\n./internal\n")
}

func TestMutationWritesTheReportToTheGivenFile(t *testing.T) {
	repo := newDiffRepo(t, map[string]string{"internal/lib/add.go": "package lib\n"})
	stub := stubGremlins(t)

	cmd := exec.Command(mutationScript(t))
	cmd.Dir = repo
	cmd.Env = mutationEnv(t, stub.binDir,
		"MUTATION_OUTPUT=custom.json", "MUTATION_TMPDIR="+t.TempDir())

	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))

	require.Contains(t, stub.read(t, stub.argsFile), "--output\ncustom.json\n")
}

func TestMutationDefaultsTheTempDirUnderHome(t *testing.T) {
	repo := newDiffRepo(t, map[string]string{"internal/lib/add.go": "package lib\n"})
	stub := stubGremlins(t)
	home := t.TempDir()

	cmd := exec.Command(mutationScript(t))
	cmd.Dir = repo
	cmd.Env = mutationEnv(t, stub.binDir, "HOME="+home)

	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))

	want := filepath.Join(home, ".cache", "mutants-tmp")
	require.Equal(t, want, stub.read(t, stub.tempFile))
	require.DirExists(t, want)
}

func TestMutationGivesGremlinsTheConfiguredTempDir(t *testing.T) {
	repo := newDiffRepo(t, map[string]string{"internal/lib/add.go": "package lib\n"})
	stub := stubGremlins(t)
	tempDir := filepath.Join(t.TempDir(), "mutants-tmp")

	cmd := exec.Command(mutationScript(t))
	cmd.Dir = repo
	cmd.Env = mutationEnv(t, stub.binDir, "MUTATION_TMPDIR="+tempDir)

	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))

	require.Equal(t, tempDir, stub.read(t, stub.tempFile))
	require.DirExists(t, tempDir)
}

func TestMutationTestsOnlyTheSelectedPackage(t *testing.T) {
	repo := newDiffRepo(t, map[string]string{"internal/ui/view.go": "package ui\n"})
	stub := stubGremlins(t)

	cmd := exec.Command(mutationScript(t))
	cmd.Dir = repo
	cmd.Env = mutationEnv(t, stub.binDir, "PKG=./internal/ui", "MUTATION_TMPDIR="+t.TempDir())

	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))

	args := stub.read(t, stub.argsFile)
	require.Contains(t, args, "./internal/ui\n")
	require.Contains(t, args, "--output\nmutation-report.json\n")
	require.NotContains(t, args, "--diff")
}

// A Go package pattern is not a directory. The script stops before gremlins
// runs the test suite for coverage.
func TestMutationRejectsAPackagePattern(t *testing.T) {
	repo := newDiffRepo(t, map[string]string{"internal/lib/add.go": "package lib\n"})
	stub := stubGremlins(t)

	cmd := exec.Command(mutationScript(t))
	cmd.Dir = repo
	cmd.Env = mutationEnv(t, stub.binDir, "PKG=./internal/...", "MUTATION_TMPDIR="+t.TempDir())

	out, err := cmd.CombinedOutput()

	require.Error(t, err, string(out))
	require.Contains(t, string(out), "PKG must be a directory, for example ./internal")
	require.NoFileExists(t, stub.argsFile)
}

// If gremlins finds no mutants, it writes no report and stops with success.
func TestMutationFailsWhenGremlinsFindsNoMutants(t *testing.T) {
	repo := newDiffRepo(t, map[string]string{"internal/lib/add.go": "package lib\n"})
	stub := stubGremlinsWithoutMutants(t)
	report := filepath.Join(repo, "mutation-report.json")
	require.NoError(t, os.WriteFile(report, []byte("{}\n"), 0o600))

	cmd := exec.Command(mutationScript(t))
	cmd.Dir = repo
	cmd.Env = mutationEnv(t, stub.binDir, "MUTATION_TMPDIR="+t.TempDir())

	out, err := cmd.CombinedOutput()

	require.Error(t, err, string(out))
	require.Contains(t, string(out), "gremlins found no mutants in ./internal")
	require.NoFileExists(t, report)
}

// The PATH holds only the directory of git, where gremlins is not. Go installs
// gremlins in the bin directory of GOPATH.
func TestMutationReportsHowToInstallGremlins(t *testing.T) {
	repo := newDiffRepo(t, map[string]string{"internal/lib/add.go": "package lib\n"})

	cmd := exec.Command(mutationScript(t))
	cmd.Dir = repo
	cmd.Env = mutationEnv(t, "", "MUTATION_TMPDIR="+t.TempDir())

	out, err := cmd.CombinedOutput()

	require.Error(t, err)
	require.Contains(t, string(out), "go install github.com/go-gremlins/gremlins/cmd/gremlins@v0.6.0")
}
