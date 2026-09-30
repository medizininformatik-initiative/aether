package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
)

// TestVerifyFlatteningLookup_RejectsBadFileWhenFlatteningEnabled covers the
// pipeline-start gate: an enabled flattening step with a defective lookup
// file stops the start.
func TestVerifyFlatteningLookup_RejectsBadFileWhenFlatteningEnabled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "flatten-lookup.json")
	require.NoError(t, os.WriteFile(path, []byte(`not json`), 0o644))

	config := models.DefaultConfig()
	config.Pipeline.EnabledSteps = []models.StepName{models.StepFlattening}
	config.Services.Flattening.LookupPath = path

	err := verifyFlatteningLookup(&config, lib.DefaultLogger)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "lookup")
}

// TestVerifyFlatteningLookup_SkipsCheckWhenFlatteningDisabled covers the gate
// bypass: without the flattening step, the lookup path is not read.
func TestVerifyFlatteningLookup_SkipsCheckWhenFlatteningDisabled(t *testing.T) {
	config := models.DefaultConfig()
	config.Pipeline.EnabledSteps = []models.StepName{models.StepLocalImport}
	config.Services.Flattening.LookupPath = filepath.Join(t.TempDir(), "absent.json")

	assert.NoError(t, verifyFlatteningLookup(&config, lib.DefaultLogger))
}

const testViewDefinition = `{"select": [{"column": [{"name": "family", "path": "name.family", "type": "string"}]}]}`

// writeLookup writes one lookup table per elements object, each with the
// same profile URL.
func writeLookup(t *testing.T, elements ...string) string {
	t.Helper()
	tables := make([]string, 0, len(elements))
	for _, e := range elements {
		tables = append(tables, `{"url": "https://example.com/StructureDefinition/TestProfile", "resourceType": "Patient", "elements": `+e+`}`)
	}
	path := filepath.Join(t.TempDir(), "flatten-lookup.json")
	require.NoError(t, os.WriteFile(path, []byte("["+strings.Join(tables, ",")+"]"), 0o644))
	return path
}

// writeParentMismatchLookup writes a valid lookup file with two
// parent-not-prefix findings: "Patient.other" and "Patient.extra" do not
// extend "Patient.name".
func writeParentMismatchLookup(t *testing.T) string {
	t.Helper()
	return writeLookup(t, `{
		"Patient.name": {"viewDefinition": `+testViewDefinition+`, "children": ["Patient.other", "Patient.extra"]},
		"Patient.other": {"viewDefinition": `+testViewDefinition+`},
		"Patient.extra": {"viewDefinition": `+testViewDefinition+`}
	}`)
}

func flatteningConfigWithLookup(path string) models.ProjectConfig {
	config := models.DefaultConfig()
	config.Pipeline.EnabledSteps = []models.StepName{models.StepFlattening}
	config.Services.Flattening.LookupPath = path
	return config
}

// TestVerifyFlatteningLookup_SummarizesWarningsAndContinues covers the warning
// path: a valid file with warning findings does not stop the start, and the
// log shows one short WARN line with the count per code, without the
// locations.
func TestVerifyFlatteningLookup_SummarizesWarningsAndContinues(t *testing.T) {
	config := flatteningConfigWithLookup(writeParentMismatchLookup(t))

	var logOutput bytes.Buffer
	logger := lib.NewLoggerWithWriter(lib.LogLevelInfo, &logOutput)

	require.NoError(t, verifyFlatteningLookup(&config, logger))
	output := logOutput.String()
	assert.Equal(t, 1, strings.Count(output, "\n"))
	assert.Contains(t, output, "[WARN]")
	assert.Contains(t, output, "parent-not-prefix 2")
	assert.NotContains(t, output, "Patient.other")
}

// TestVerifyFlatteningLookup_PutsAllCodesInOneWarnLine covers a file with
// two warning codes: one WARN line holds the count of each code.
func TestVerifyFlatteningLookup_PutsAllCodesInOneWarnLine(t *testing.T) {
	config := flatteningConfigWithLookup(writeLookup(t, `{
		"Patient.name": {"viewDefinition": `+testViewDefinition+`, "children": ["Patient.name.missing", "Patient.other", "Patient.extra"]},
		"Patient.other": {"viewDefinition": `+testViewDefinition+`},
		"Patient.extra": {"viewDefinition": `+testViewDefinition+`}
	}`))

	var logOutput bytes.Buffer
	logger := lib.NewLoggerWithWriter(lib.LogLevelInfo, &logOutput)

	require.NoError(t, verifyFlatteningLookup(&config, logger))
	output := logOutput.String()
	assert.Equal(t, 1, strings.Count(output, "\n"))
	assert.Contains(t, output, "unresolved-child 1")
	assert.Contains(t, output, "parent-not-prefix 2")
}

// TestVerifyFlatteningLookup_LogsNothingForCleanFile covers a file without
// findings: no line goes to the log, also at DEBUG level.
func TestVerifyFlatteningLookup_LogsNothingForCleanFile(t *testing.T) {
	config := flatteningConfigWithLookup(writeLookup(t, `{
		"Patient.name": {"viewDefinition": `+testViewDefinition+`, "children": ["Patient.name.family"]},
		"Patient.name.family": {"viewDefinition": `+testViewDefinition+`}
	}`))

	var logOutput bytes.Buffer
	logger := lib.NewLoggerWithWriter(lib.LogLevelDebug, &logOutput)

	require.NoError(t, verifyFlatteningLookup(&config, logger))
	assert.Empty(t, logOutput.String())
}

// TestVerifyFlatteningLookup_LogsWarningsBeforeError covers a file with
// errors and warnings: the start stops, and the WARN summary is in the log.
func TestVerifyFlatteningLookup_LogsWarningsBeforeError(t *testing.T) {
	config := flatteningConfigWithLookup(writeLookup(t,
		`{"Patient.name": {"viewDefinition": `+testViewDefinition+`, "children": ["Patient.name.missing"]}}`,
		`{"Patient.name": {"viewDefinition": `+testViewDefinition+`}}`,
	))

	var logOutput bytes.Buffer
	logger := lib.NewLoggerWithWriter(lib.LogLevelInfo, &logOutput)

	err := verifyFlatteningLookup(&config, logger)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate profile URL")
	assert.Contains(t, logOutput.String(), "unresolved-child 1")
}

// TestCheckFlatteningLookup_ReturnsWarningsForLaterLog covers the check that
// pipeline start uses before job.log exists: it gives the warnings to the
// caller, so the caller can log them after it attaches job.log.
func TestCheckFlatteningLookup_ReturnsWarningsForLaterLog(t *testing.T) {
	config := flatteningConfigWithLookup(writeParentMismatchLookup(t))

	warnings, err := checkFlatteningLookup(&config)

	require.NoError(t, err)
	require.Len(t, warnings, 1)
	assert.Equal(t, "parent-not-prefix", warnings[0].Code)
	assert.Equal(t, 2, warnings[0].Count)
}

// TestCheckFlatteningLookup_ReturnsWarningsWithError covers a file with
// errors and warnings: the caller gets both, so it can log the warnings
// before the start stops.
func TestCheckFlatteningLookup_ReturnsWarningsWithError(t *testing.T) {
	config := flatteningConfigWithLookup(writeLookup(t,
		`{"Patient.name": {"viewDefinition": `+testViewDefinition+`, "children": ["Patient.name.missing"]}}`,
		`{"Patient.name": {"viewDefinition": `+testViewDefinition+`}}`,
	))

	warnings, err := checkFlatteningLookup(&config)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "lookup file check failed")
	require.Len(t, warnings, 1)
	assert.Equal(t, "unresolved-child", warnings[0].Code)
}

// TestLogLookupWarnings_WritesLocationsToJobLogWithoutVerbose covers the log
// after job.log is attached: the console gets only the WARN summary, and
// job.log also gets the locations at INFO level.
func TestLogLookupWarnings_WritesLocationsToJobLogWithoutVerbose(t *testing.T) {
	config := flatteningConfigWithLookup(writeParentMismatchLookup(t))
	warnings, err := checkFlatteningLookup(&config)
	require.NoError(t, err)

	var console bytes.Buffer
	logger := lib.NewLoggerWithWriter(lib.LogLevelInfo, &console)
	jobLog := filepath.Join(t.TempDir(), "job.log")
	require.NoError(t, logger.AttachJobLogFile(jobLog))
	t.Cleanup(func() { _ = logger.Close() })

	logLookupWarnings(warnings, logger)

	assert.NotContains(t, console.String(), "Patient.other")
	fileContent, err := os.ReadFile(jobLog)
	require.NoError(t, err)
	assert.Contains(t, string(fileContent), "parent-not-prefix 2")
	assert.Contains(t, string(fileContent), "Patient.other")
}

// TestCommandLogLevel_FollowsVerboseFlag covers the level of the command
// logger: --verbose enables DEBUG, so the lookup finding locations show.
func TestCommandLogLevel_FollowsVerboseFlag(t *testing.T) {
	t.Cleanup(func() { verbose = false })

	verbose = false
	assert.Equal(t, lib.LogLevelInfo, commandLogLevel())

	verbose = true
	assert.Equal(t, lib.LogLevelDebug, commandLogLevel())
}

// TestVerifyFlatteningLookup_LogsWarningLocationsAtDebug covers the details:
// with DEBUG logging, the locations of all findings are in the log.
func TestVerifyFlatteningLookup_LogsWarningLocationsAtDebug(t *testing.T) {
	config := flatteningConfigWithLookup(writeParentMismatchLookup(t))

	var logOutput bytes.Buffer
	logger := lib.NewLoggerWithWriter(lib.LogLevelDebug, &logOutput)

	require.NoError(t, verifyFlatteningLookup(&config, logger))
	output := logOutput.String()
	assert.Contains(t, output, "[WARN]")
	assert.Contains(t, output, "[DEBUG]")
	assert.Contains(t, output, "Patient.other")
	assert.Contains(t, output, "Patient.extra")
}

// TestVerifyFlatteningLookupForJob_RejectsBadFileWhenFlatteningPending covers
// the gate on `pipeline continue`: a job whose flattening step has not run yet
// gets the same check as `pipeline start`.
func TestVerifyFlatteningLookupForJob_RejectsBadFileWhenFlatteningPending(t *testing.T) {
	path := filepath.Join(t.TempDir(), "flatten-lookup.json")
	require.NoError(t, os.WriteFile(path, []byte(`not json`), 0o644))

	job := &models.PipelineJob{
		Config: models.DefaultConfig(),
		Steps:  []models.PipelineStep{{Name: models.StepFlattening, Status: models.StepStatusPending}},
	}
	job.Config.Pipeline.EnabledSteps = []models.StepName{models.StepFlattening}
	job.Config.Services.Flattening.LookupPath = path

	err := verifyFlatteningLookupForJob(job, lib.DefaultLogger)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "lookup")
}

// TestVerifyFlatteningLookupForJob_SkipsCompletedFlattening covers the resume
// of a job after flattening: the lookup file is no longer needed, so a missing
// file does not block the continue.
func TestVerifyFlatteningLookupForJob_SkipsCompletedFlattening(t *testing.T) {
	job := &models.PipelineJob{
		Config: models.DefaultConfig(),
		Steps:  []models.PipelineStep{{Name: models.StepFlattening, Status: models.StepStatusCompleted}},
	}
	job.Config.Pipeline.EnabledSteps = []models.StepName{models.StepFlattening}
	job.Config.Services.Flattening.LookupPath = filepath.Join(t.TempDir(), "absent.json")

	assert.NoError(t, verifyFlatteningLookupForJob(job, lib.DefaultLogger))
}

// TestVerifyFlatteningLookupForJob_SkipsJobWithoutFlatteningStep covers the
// continue of a job that never had a flattening step: the lookup path is not
// read.
func TestVerifyFlatteningLookupForJob_SkipsJobWithoutFlatteningStep(t *testing.T) {
	job := &models.PipelineJob{
		Config: models.DefaultConfig(),
		Steps:  []models.PipelineStep{{Name: models.StepDIMP, Status: models.StepStatusCompleted}},
	}
	job.Config.Services.Flattening.LookupPath = filepath.Join(t.TempDir(), "absent.json")

	assert.NoError(t, verifyFlatteningLookupForJob(job, lib.DefaultLogger))
}
