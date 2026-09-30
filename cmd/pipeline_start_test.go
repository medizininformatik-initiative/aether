package cmd

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/pipeline"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

const startCRTDLFixture = "../internal/lib/crtdl/testdata/diagnosis_linked_with_encounter_corrected.json"

// startFlags sets the flag variables of `pipeline start` and sets them back
// to their values after the test.
func startFlags(t *testing.T, dir, anonymization string, allowHTTP bool) {
	t.Helper()
	oldDir, oldAnon, oldAllow, oldVerbose, oldNoProgress := localImportDir, anonymizationConfig, allowHTTPCRTDL, verbose, noProgress
	t.Cleanup(func() {
		localImportDir, anonymizationConfig, allowHTTPCRTDL, verbose, noProgress = oldDir, oldAnon, oldAllow, oldVerbose, oldNoProgress
	})
	localImportDir, anonymizationConfig, allowHTTPCRTDL, verbose, noProgress = dir, anonymization, allowHTTP, true, true
}

// writeStartConfig writes an aether.yaml with the given pipeline and services
// sections and returns its path and the jobs directory.
func writeStartConfig(t *testing.T, body string) (string, string) {
	t.Helper()
	tmp := t.TempDir()
	jobsDir := filepath.Join(tmp, "jobs")
	configPath := filepath.Join(tmp, "aether.yaml")
	content := body + "\njobs_dir: \"" + jobsDir + "\"\n"
	require.NoError(t, os.WriteFile(configPath, []byte(content), 0o644))
	return configPath, jobsDir
}

func writeImportDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	patient := `{"resourceType":"Patient","id":"p1"}` + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "patients.ndjson"), []byte(patient), 0o644))
	return dir
}

func loadOnlyJob(t *testing.T, jobsDir string) *models.PipelineJob {
	t.Helper()
	entries, err := os.ReadDir(jobsDir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	job, err := pipeline.LoadJob(jobsDir, entries[0].Name())
	require.NoError(t, err)
	return job
}

func TestRunPipelineStart_RejectsJSONThatIsNotCRTDL(t *testing.T) {
	startFlags(t, "", "", false)
	notCRTDL := filepath.Join(t.TempDir(), "other.json")
	require.NoError(t, os.WriteFile(notCRTDL, []byte(`{"resourceType":"Patient"}`), 0o644))

	err := runPipelineStart(pipelineStartCmd, []string{"aether.yaml", notCRTDL})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid CRTDL argument")
}

func TestRunPipelineStart_RejectsDirectoryAsCRTDL(t *testing.T) {
	startFlags(t, "", "", false)

	err := runPipelineStart(pipelineStartCmd, []string{"aether.yaml", t.TempDir()})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "second argument must be a CRTDL file")
}

func TestRunPipelineStart_ReportsMissingConfig(t *testing.T) {
	startFlags(t, "", "", false)
	missing := filepath.Join(t.TempDir(), "absent.yaml")

	err := runPipelineStart(pipelineStartCmd, []string{missing, startCRTDLFixture})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load configuration")
}

func TestRunPipelineStart_RequiresLocalImportDirectory(t *testing.T) {
	startFlags(t, "", "", false)
	configPath, _ := writeStartConfig(t, "pipeline:\n  enabled_steps:\n    - local_import\n")

	err := runPipelineStart(pipelineStartCmd, []string{configPath, startCRTDLFixture})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "local_import step enabled but no directory specified")
}

func TestRunPipelineStart_RequiresAllowFlagForHTTPImport(t *testing.T) {
	startFlags(t, "", "", false)
	configPath, _ := writeStartConfig(t, "pipeline:\n  enabled_steps:\n    - http_import\n")

	err := runPipelineStart(pipelineStartCmd, []string{configPath, startCRTDLFixture, "http://example.org/data"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "--allow-http-crtdl")
}

func TestRunPipelineStart_CompletesLocalImportFromDirFlag(t *testing.T) {
	startFlags(t, writeImportDir(t), "/etc/aether/anonymization.yaml", false)
	configPath, jobsDir := writeStartConfig(t, "pipeline:\n  enabled_steps:\n    - local_import\n")

	require.NoError(t, runPipelineStart(pipelineStartCmd, []string{configPath, startCRTDLFixture}))

	job := loadOnlyJob(t, jobsDir)
	assert.Equal(t, models.JobStatusCompleted, job.Status)
	assert.Equal(t, localImportDir, job.Config.Services.LocalImport.Dir)
	assert.Equal(t, "/etc/aether/anonymization.yaml", job.Config.Services.DIMP.AnonymizationConfig)
}

func TestRunPipelineStart_PausesAtWaitStep(t *testing.T) {
	startFlags(t, "", "", false)
	configPath, jobsDir := writeStartConfig(t, "pipeline:\n  enabled_steps:\n    - local_import\n    - wait\n")

	require.NoError(t, runPipelineStart(pipelineStartCmd, []string{configPath, startCRTDLFixture, writeImportDir(t)}))

	job := loadOnlyJob(t, jobsDir)
	assert.Equal(t, string(models.StepWait), job.CurrentStep)
}

func TestRunPipelineStart_ReturnsStepFailure(t *testing.T) {
	startFlags(t, "", "", false)
	configPath, jobsDir := writeStartConfig(t, "pipeline:\n  enabled_steps:\n    - local_import\n")
	emptyDir := t.TempDir()

	err := runPipelineStart(pipelineStartCmd, []string{configPath, startCRTDLFixture, emptyDir})

	require.Error(t, err)
	job := loadOnlyJob(t, jobsDir)
	assert.Equal(t, models.JobStatusFailed, job.Status)
}

func TestRunPipelineStart_WritesLookupWarningLocationsToJobLog(t *testing.T) {
	startFlags(t, "", "", false)
	verbose = false
	lookupPath := writeParentMismatchLookup(t)
	configPath, jobsDir := writeStartConfig(t, "pipeline:\n  enabled_steps:\n    - local_import\n    - wait\n    - flattening\n"+
		"services:\n  flattening:\n    lookup_path: \""+lookupPath+"\"\n")

	require.NoError(t, runPipelineStart(pipelineStartCmd, []string{configPath, startCRTDLFixture, writeImportDir(t)}))

	job := loadOnlyJob(t, jobsDir)
	jobLog, err := os.ReadFile(services.GetJobLogFilePath(jobsDir, job.JobID))
	require.NoError(t, err)
	assert.Contains(t, string(jobLog), "parent-not-prefix 2")
	assert.Contains(t, string(jobLog), "Patient.other")
}

func TestRunPipelineStart_StopsBeforeJobWhenTORCHIsWrongServer(t *testing.T) {
	startFlags(t, "", "", false)
	server := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(server.Close)
	configPath, jobsDir := writeStartConfig(t, `services:
  torch:
    base_url: "`+server.URL+`"
    username: "user"
    password: "pass"
pipeline:
  enabled_steps:
    - torch
`)

	err := runPipelineStart(pipelineStartCmd, []string{configPath, startCRTDLFixture})

	require.Error(t, err)
	assert.Contains(t, err.Error(), server.URL+"/fhir/metadata")
	assert.Contains(t, err.Error(), "services.torch.base_url")
	entries, readErr := os.ReadDir(jobsDir)
	require.NoError(t, readErr)
	assert.Empty(t, entries, "no job must exist after a failed preflight check")
}
