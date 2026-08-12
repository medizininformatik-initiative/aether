package cmd

import (
	"bytes"
	"os"
	"path/filepath"
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

// TestVerifyFlatteningLookup_LogsWarningsAndContinues covers the warning path:
// a valid file with a warning finding does not stop the start, and the warning
// text lands in the log.
func TestVerifyFlatteningLookup_LogsWarningsAndContinues(t *testing.T) {
	// "Patient.other" does not extend "Patient.name", so the parent-not-prefix
	// warning fires; the file stays valid.
	path := filepath.Join(t.TempDir(), "flatten-lookup.json")
	lookupJSON := `[
		{
			"url": "https://example.com/StructureDefinition/TestProfile",
			"resourceType": "Patient",
			"elements": {
				"Patient.name": {"viewDefinition": {"select": [{"column": [{"name": "family", "path": "name.family", "type": "string"}]}]}, "children": ["Patient.other"]},
				"Patient.other": {"viewDefinition": {"select": [{"column": [{"name": "family", "path": "name.family", "type": "string"}]}]}}
			}
		}
	]`
	require.NoError(t, os.WriteFile(path, []byte(lookupJSON), 0o644))

	config := models.DefaultConfig()
	config.Pipeline.EnabledSteps = []models.StepName{models.StepFlattening}
	config.Services.Flattening.LookupPath = path

	var logOutput bytes.Buffer
	logger := lib.NewLoggerWithWriter(lib.LogLevelInfo, &logOutput)

	require.NoError(t, verifyFlatteningLookup(&config, logger))
	assert.Contains(t, logOutput.String(), "parent-not-prefix")
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
