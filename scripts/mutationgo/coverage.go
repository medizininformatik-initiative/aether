package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"golang.org/x/sync/errgroup"
	"golang.org/x/tools/cover"
)

// coverageOptions are the parts of the coverage call that the wrapper needs.
type coverageOptions struct {
	Tags     string
	CoverPkg string
	// Profile is the file that gremlins reads after the call.
	Profile string
}

func parseCoverageArgs(args []string) coverageOptions {
	return coverageOptions{
		Tags:     flagValue(args, "-tags"),
		CoverPkg: flagValue(args, "-coverpkg"),
		Profile:  coverProfilePath(args),
	}
}

// flagValue returns the argument after the flag, or an empty string.
func flagValue(args []string, flag string) string {
	i := slices.Index(args, flag)
	if i < 0 || i+1 >= len(args) {
		return ""
	}

	return args[i+1]
}

// parseTestList reads the output of -test.list. Benchmarks do not run in a
// normal test run, so they are not in the result. Fuzz targets and examples
// do run, with their seed corpus and their output check.
func parseTestList(output string) []string {
	var names []string

	for name := range strings.SplitSeq(output, "\n") {
		if strings.HasPrefix(name, "Test") || strings.HasPrefix(name, "Fuzz") || strings.HasPrefix(name, "Example") {
			names = append(names, name)
		}
	}

	return names
}

// testPackage is a package with test files.
type testPackage struct {
	ImportPath string
	Dir        string
}

// testJob is one test of one package, with the test binary of the package.
type testJob struct {
	pkg    testPackage
	binary string
	name   string
}

// runCoverage replaces the coverage call of gremlins. It runs every test
// alone, so that it knows which tests cover a line, writes the test map, and
// writes the union of the coverage as the profile that gremlins asked for.
// If the measurement fails, it runs the original call and writes no test map,
// so that every mutant runs all tests.
func runCoverage(cfg config, args []string, stdout, stderr io.Writer) int {
	// A test map of an earlier run must not limit the mutants of this run.
	_ = os.Remove(cfg.MapPath)

	err := measureCoverage(cfg, parseCoverageArgs(args), stdout)
	if err == nil {
		return 0
	}

	_ = os.Remove(cfg.MapPath)
	_, _ = fmt.Fprintf(stderr, "mutationgo: %v\nmutationgo: every mutant runs all tests\n", err)

	return execGo(cfg.RealGo, args, stdout, stderr)
}

func measureCoverage(cfg config, opts coverageOptions, stdout io.Writer) error {
	if opts.Profile == "" {
		return errors.New("the coverage call has no profile file")
	}

	moduleDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("find the module directory: %w", err)
	}

	scratch, err := os.MkdirTemp("", "mutationgo-")
	if err != nil {
		return fmt.Errorf("create a scratch directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(scratch) }()

	modulePath, results, err := measureTests(goRunner{realGo: cfg.RealGo, dir: moduleDir, tags: opts.Tags}, opts.CoverPkg, scratch)
	if err != nil {
		return err
	}

	if err := writeTestMap(cfg.MapPath, buildTestMap(modulePath, results)); err != nil {
		return err
	}

	return writeProfileFile(opts.Profile, results)
}

// measureTests runs every test of the module alone. It returns the module path
// and the coverage of each test.
func measureTests(goCmd goRunner, coverPkg, scratch string) (modulePath string, results []testResult, err error) {
	modulePath, err = goCmd.output("list", "-m")
	if err != nil {
		return "", nil, err
	}

	packages, err := goCmd.testPackages()
	if err != nil {
		return "", nil, err
	}

	jobs, err := goCmd.buildTestJobs(packages, coverPkg, scratch)
	if err != nil {
		return "", nil, err
	}

	results, err = runTestsAlone(jobs, scratch)
	if err != nil {
		return "", nil, err
	}

	return strings.TrimSpace(modulePath), results, nil
}

func writeProfileFile(path string, results []testResult) error {
	profile, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create the cover profile: %w", err)
	}
	if err := writeMergedProfile(profile, results); err != nil {
		_ = profile.Close()

		return fmt.Errorf("write the cover profile: %w", err)
	}

	if err := profile.Close(); err != nil {
		return fmt.Errorf("write the cover profile: %w", err)
	}

	return nil
}

// goRunner runs the real go command in the module root.
type goRunner struct {
	realGo string
	dir    string
	tags   string
}

func (g goRunner) output(args ...string) (string, error) {
	cmd := exec.Command(g.realGo, args...)
	cmd.Dir = g.dir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("go %s: %w\n%s", strings.Join(args, " "), err, stderr.String())
	}

	return stdout.String(), nil
}

// tagArgs gives the build tag flag for a go subcommand.
func (g goRunner) tagArgs() []string {
	if g.tags == "" {
		return nil
	}

	return []string{"-tags", g.tags}
}

func (g goRunner) testPackages() ([]testPackage, error) {
	args := append([]string{"list"}, g.tagArgs()...)
	args = append(args, "-f",
		"{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}} {{.Dir}}{{end}}", "./...")

	out, err := g.output(args...)
	if err != nil {
		return nil, err
	}

	var packages []testPackage

	for line := range strings.SplitSeq(out, "\n") {
		importPath, dir, found := strings.Cut(line, " ")
		if found {
			packages = append(packages, testPackage{ImportPath: importPath, Dir: dir})
		}
	}

	return packages, nil
}

// buildTestJobs builds the test binary of each package with coverage for the
// coverPkg packages, and lists its tests.
func (g goRunner) buildTestJobs(packages []testPackage, coverPkg, scratch string) ([]testJob, error) {
	perPackage := make([][]testJob, len(packages))

	err := parallel(len(packages), func(i int) error {
		pkg := packages[i]
		binary := filepath.Join(scratch, fmt.Sprintf("pkg%d.test", i))

		args := append([]string{"test"}, g.tagArgs()...)
		args = append(args, "-c", "-cover", "-covermode=set")

		if coverPkg != "" {
			args = append(args, "-coverpkg", coverPkg)
		}

		if _, err := g.output(append(args, "-o", binary, pkg.ImportPath)...); err != nil {
			return err
		}

		list := exec.Command(binary, "-test.list", ".*")
		list.Dir = pkg.Dir

		listing, err := list.Output()
		if err != nil {
			return fmt.Errorf("list the tests of %s: %w", pkg.ImportPath, err)
		}

		for _, name := range parseTestList(string(listing)) {
			perPackage[i] = append(perPackage[i], testJob{pkg: pkg, binary: binary, name: name})
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return slices.Concat(perPackage...), nil
}

// runTestsAlone runs every test in its own process. A test that fails alone
// makes the map wrong, so the first failures stop the measurement.
func runTestsAlone(jobs []testJob, scratch string) ([]testResult, error) {
	results := make([]testResult, len(jobs))

	err := parallel(len(jobs), func(i int) error {
		job := jobs[i]
		profilePath := filepath.Join(scratch, fmt.Sprintf("test%d.out", i))

		cmd := exec.Command(job.binary,
			"-test.run", "^"+job.name+"$", "-test.coverprofile", profilePath)
		cmd.Dir = job.pkg.Dir

		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("test %s of %s fails when it runs alone: %w\n%s",
				job.name, job.pkg.ImportPath, err, out)
		}

		file, err := os.Open(profilePath)
		if err != nil {
			return fmt.Errorf("test %s of %s wrote no cover profile: %w", job.name, job.pkg.ImportPath, err)
		}
		defer func() { _ = file.Close() }()

		profiles, err := cover.ParseProfilesFromReader(file)
		if err != nil {
			return fmt.Errorf("test %s of %s: %w", job.name, job.pkg.ImportPath, err)
		}

		results[i] = testResult{ID: testID{Package: job.pkg.ImportPath, Name: job.name}, Profiles: profiles}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return results, nil
}

// parallel runs fn for the indexes 0 to n-1 in at most GOMAXPROCS goroutines.
// After the first error it starts no more calls and returns that error.
func parallel(n int, fn func(i int) error) error {
	g, ctx := errgroup.WithContext(context.Background())
	g.SetLimit(runtime.GOMAXPROCS(0))

	for i := range n {
		if ctx.Err() != nil {
			break
		}

		g.Go(func() error { return fn(i) })
	}

	return g.Wait()
}
