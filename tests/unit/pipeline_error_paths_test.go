package unit

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/pipeline"
)

// TestExecuteValidationStep_OutputDirCreationError covers the mkdir failure
// branch: a regular file sits where the validation output directory must go.
func TestExecuteValidationStep_OutputDirCreationError(t *testing.T) {
	tempDir := t.TempDir()
	jobDir := filepath.Join(tempDir, "jobs", "test-validation-job")
	require.NoError(t, os.MkdirAll(jobDir, 0755))

	// Block output-dir creation with a file where the directory should be.
	require.NoError(t, os.WriteFile(filepath.Join(jobDir, "validation"), []byte("x"), 0644))

	job := createValidationTestJob("http://localhost:9999")
	err := pipeline.ExecuteValidationStep(job, jobDir, lib.NewLogger(lib.LogLevelError))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create output directory")
}

// TestExecuteValidationStep_ListInputFilesError covers the findFHIRFiles error
// branch: an unbalanced '[' in the path makes the glob pattern invalid.
func TestExecuteValidationStep_ListInputFilesError(t *testing.T) {
	baseDir := t.TempDir()
	jobDir := filepath.Join(baseDir, "test[invalid")
	require.NoError(t, os.MkdirAll(jobDir, 0755))

	job := createValidationTestJob("http://localhost:9999")
	err := pipeline.ExecuteValidationStep(job, jobDir, lib.NewLogger(lib.LogLevelError))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to list input files")
}

// TestExecuteFlatteningStep_ListInputFilesError covers the findFHIRFiles error
// branch: config, CRTDL and lookup tables all validate, but an unbalanced '[' in
// the job path makes the input-file glob pattern invalid.
func TestExecuteFlatteningStep_ListInputFilesError(t *testing.T) {
	baseDir := t.TempDir()
	jobDir := filepath.Join(baseDir, "test[invalid")
	require.NoError(t, os.MkdirAll(jobDir, 0755))

	lookupPath := filepath.Join(baseDir, "lookup.json")
	writeTestLookupTable(t, lookupPath, "https://example.com/Patient", "Patient")
	crtdlPath := filepath.Join(baseDir, "crtdl.json")
	writeTestCRTDL(t, crtdlPath, "group-1", "Patient", "https://example.com/Patient")

	job := createFlatteningTestJob("http://localhost:9999", lookupPath, crtdlPath)
	err := pipeline.ExecuteFlatteningStep(job, jobDir, createFlatteningTestLogger())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to list input files")
}
