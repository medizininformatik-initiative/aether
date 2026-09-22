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
// The feature branch adds the given files.
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
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("base\n"), 0o600))
	run("add", "README.md")
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

// stubGremlins puts a fake gremlins in a new directory and returns it.
func stubGremlins(t *testing.T) gremlinsStub {
	t.Helper()

	stub := gremlinsStub{binDir: t.TempDir()}
	stub.argsFile = filepath.Join(stub.binDir, "args.txt")
	stub.tempFile = filepath.Join(stub.binDir, "tmpdir.txt")

	script := "#!/bin/bash\n" +
		"printf '%s\\n' \"$@\" > " + stub.argsFile + "\n" +
		"printf '%s' \"$TMPDIR\" > " + stub.tempFile + "\n"
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
	require.Contains(t, string(out), "No Go file differs from base")
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
	require.Contains(t, args, "./internal/...")
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
	repo := newDiffRepo(t, map[string]string{"internal/lib/add.go": "package lib\n"})
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
