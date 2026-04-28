package unit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/pipeline"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

// crtdlWithoutPatientIdentifier matches the bug 323 scenario: a Patient
// attribute group missing the identifier attribute. The enrichment should add
// Patient.identifier so downstream steps (flattening) see the enriched group.
func crtdlWithoutPatientIdentifier() map[string]any {
	return map[string]any{
		"cohortDefinition": map[string]any{
			"version":           "1.0.0",
			"inclusionCriteria": []any{},
		},
		"dataExtraction": map[string]any{
			"attributeGroups": []map[string]any{
				{
					"id":             "patient-group",
					"name":           "PatientPseudonymisiert",
					"groupReference": "https://www.medizininformatik-initiative.de/fhir/core/modul-person/StructureDefinition/PatientPseudonymisiert",
					"attributes": []map[string]any{
						{"attributeRef": "Patient.id", "mustHave": true},
						{"attributeRef": "Patient.gender", "mustHave": false},
					},
				},
			},
		},
	}
}

func writeCRTDLFile(t *testing.T, dir, filename string, content map[string]any) string {
	t.Helper()
	path := filepath.Join(dir, filename)
	bytes, err := json.Marshal(content)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, bytes, 0644))
	return path
}

func newJobForPrep(t *testing.T, jobsDir, jobID, crtdlPath string, preprocessing models.CRTDLPreprocessingConfig) *models.PipelineJob {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(jobsDir, jobID), 0755))
	return &models.PipelineJob{
		JobID:       jobID,
		InputSource: crtdlPath,
		InputType:   models.InputTypeCRTDL,
		CRTDLPath:   crtdlPath,
		Config: models.ProjectConfig{
			JobsDir: jobsDir,
			Services: models.ServiceConfig{
				CRTDLPreprocessing: preprocessing,
			},
		},
	}
}

func TestPrepareCRTDL_NoCRTDLAttached_NoOp(t *testing.T) {
	tempDir := t.TempDir()
	jobsDir := filepath.Join(tempDir, "jobs")

	job := &models.PipelineJob{
		JobID:       "job-no-crtdl",
		InputSource: "/some/local/dir",
		InputType:   models.InputTypeLocal,
		Config:      models.ProjectConfig{JobsDir: jobsDir},
	}

	require.NoError(t, pipeline.PrepareCRTDL(job, lib.NewLogger(lib.LogLevelDebug)))

	assert.Equal(t, "", job.CRTDLPath,
		"CRTDLPath must remain empty when no CRTDL is attached to the job")
}

func TestPrepareCRTDL_PreprocessingDisabled_CopiesOriginalToJobDir(t *testing.T) {
	tempDir := t.TempDir()
	jobsDir := filepath.Join(tempDir, "jobs")
	jobID := "job-disabled"

	crtdlPath := writeCRTDLFile(t, tempDir, "input.json", crtdlWithoutPatientIdentifier())
	job := newJobForPrep(t, jobsDir, jobID, crtdlPath, models.CRTDLPreprocessingConfig{Enabled: false})

	require.NoError(t, pipeline.PrepareCRTDL(job, lib.NewLogger(lib.LogLevelDebug)))

	expectedPath := filepath.Join(jobsDir, jobID, "crtdl.json")
	assert.Equal(t, expectedPath, job.CRTDLPath,
		"CRTDLPath must point at jobDir/crtdl.json after copy")

	originalBytes, err := os.ReadFile(crtdlPath)
	require.NoError(t, err)
	copiedBytes, err := os.ReadFile(expectedPath)
	require.NoError(t, err)
	assert.Equal(t, originalBytes, copiedBytes, "Copied CRTDL must match original byte-for-byte")
}

func TestPrepareCRTDL_PreprocessingEnabled_AppliesEnrichmentAndSavesEnrichedFile(t *testing.T) {
	tempDir := t.TempDir()
	jobsDir := filepath.Join(tempDir, "jobs")
	jobID := "job-enriched"

	crtdlPath := writeCRTDLFile(t, tempDir, "input.json", crtdlWithoutPatientIdentifier())
	preprocessing := models.CRTDLPreprocessingConfig{
		Enabled: true,
		Enrichments: []models.GroupEnrichment{
			{
				GroupReference: "https://www.medizininformatik-initiative.de/fhir/core/modul-person/StructureDefinition/PatientPseudonymisiert",
				CreateIfNotExists: &models.CreateGroupConfig{
					GroupName: "PatientPseudonymisiert",
				},
				AttributesToAdd: []models.EnrichmentAttribute{
					{AttributeRef: "Patient.identifier", MustHave: false},
				},
			},
		},
	}
	job := newJobForPrep(t, jobsDir, jobID, crtdlPath, preprocessing)

	require.NoError(t, pipeline.PrepareCRTDL(job, lib.NewLogger(lib.LogLevelDebug)))

	expectedPath := filepath.Join(jobsDir, jobID, "enriched-crtdl.json")
	assert.Equal(t, expectedPath, job.CRTDLPath,
		"CRTDLPath must point at jobDir/enriched-crtdl.json after enrichment")

	enriched, err := services.ParseCRTDL(expectedPath)
	require.NoError(t, err)
	require.Len(t, enriched.DataExtraction.AttributeGroups, 1)

	patientGroup := enriched.DataExtraction.AttributeGroups[0]
	refs := make([]string, 0, len(patientGroup.Attributes))
	for _, a := range patientGroup.Attributes {
		refs = append(refs, a.AttributeRef)
	}
	assert.Contains(t, refs, "Patient.identifier",
		"Enriched CRTDL must contain Patient.identifier added by enrichment rule")
	assert.Contains(t, refs, "Patient.id",
		"Enriched CRTDL must preserve original Patient.id")
}

func TestPrepareCRTDL_PreprocessingEnabledButNoEnrichments_FallsBackToCopy(t *testing.T) {
	tempDir := t.TempDir()
	jobsDir := filepath.Join(tempDir, "jobs")
	jobID := "job-empty-enrich"

	crtdlPath := writeCRTDLFile(t, tempDir, "input.json", crtdlWithoutPatientIdentifier())
	job := newJobForPrep(t, jobsDir, jobID, crtdlPath, models.CRTDLPreprocessingConfig{
		Enabled:     true,
		Enrichments: []models.GroupEnrichment{},
	})

	require.NoError(t, pipeline.PrepareCRTDL(job, lib.NewLogger(lib.LogLevelDebug)))

	expectedPath := filepath.Join(jobsDir, jobID, "crtdl.json")
	assert.Equal(t, expectedPath, job.CRTDLPath,
		"With enabled+empty enrichments, fall back to copying as crtdl.json")

	originalBytes, err := os.ReadFile(crtdlPath)
	require.NoError(t, err)
	copiedBytes, err := os.ReadFile(expectedPath)
	require.NoError(t, err)
	assert.Equal(t, originalBytes, copiedBytes)
}

func TestPrepareCRTDL_Idempotent_SkipsWhenCRTDLPathAlreadyInJobDir(t *testing.T) {
	tempDir := t.TempDir()
	jobsDir := filepath.Join(tempDir, "jobs")
	jobID := "job-idempotent"

	crtdlPath := writeCRTDLFile(t, tempDir, "input.json", crtdlWithoutPatientIdentifier())
	job := newJobForPrep(t, jobsDir, jobID, crtdlPath, models.CRTDLPreprocessingConfig{
		Enabled: true,
		Enrichments: []models.GroupEnrichment{
			{
				GroupReference: "https://www.medizininformatik-initiative.de/fhir/core/modul-person/StructureDefinition/PatientPseudonymisiert",
				AttributesToAdd: []models.EnrichmentAttribute{
					{AttributeRef: "Patient.identifier", MustHave: false},
				},
			},
		},
	})

	logger := lib.NewLogger(lib.LogLevelDebug)
	require.NoError(t, pipeline.PrepareCRTDL(job, logger))
	firstPath := job.CRTDLPath

	// Capture file content after first run, then call again — content must be
	// identical (no double-enrichment) because the second call is a no-op.
	firstBytes, err := os.ReadFile(firstPath)
	require.NoError(t, err)

	require.NoError(t, pipeline.PrepareCRTDL(job, logger))
	assert.Equal(t, firstPath, job.CRTDLPath, "CRTDLPath unchanged on repeat call")

	secondBytes, err := os.ReadFile(firstPath)
	require.NoError(t, err)
	assert.Equal(t, firstBytes, secondBytes, "File content unchanged on repeat call")
}

func TestPrepareCRTDL_EnrichmentEnabled_FailsWhenInputCRTDLInvalid(t *testing.T) {
	tempDir := t.TempDir()
	jobsDir := filepath.Join(tempDir, "jobs")

	crtdlPath := filepath.Join(tempDir, "bad.json")
	require.NoError(t, os.WriteFile(crtdlPath, []byte("{not json"), 0644))

	job := newJobForPrep(t, jobsDir, "job-bad-crtdl", crtdlPath, models.CRTDLPreprocessingConfig{
		Enabled: true,
		Enrichments: []models.GroupEnrichment{
			{
				GroupReference: "https://example.org/Patient",
				AttributesToAdd: []models.EnrichmentAttribute{
					{AttributeRef: "Patient.id"},
				},
			},
		},
	})

	err := pipeline.PrepareCRTDL(job, lib.NewLogger(lib.LogLevelDebug))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse CRTDL")
}

// TestPrepareCRTDL_PreprocessingDisabled_FailsWhenInputMissing covers the
// ReadFile error branch in copyOriginalCRTDL (crtdl_prep.go lines 78-80).
func TestPrepareCRTDL_PreprocessingDisabled_FailsWhenInputMissing(t *testing.T) {
	tempDir := t.TempDir()
	jobsDir := filepath.Join(tempDir, "jobs")
	jobID := "job-missing-input"

	missingPath := filepath.Join(tempDir, "does-not-exist.json")
	job := newJobForPrep(t, jobsDir, jobID, missingPath, models.CRTDLPreprocessingConfig{Enabled: false})

	err := pipeline.PrepareCRTDL(job, lib.NewLogger(lib.LogLevelDebug))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read CRTDL file",
		"Error must come from copyOriginalCRTDL ReadFile branch")
}

// TestPrepareCRTDL_InputInJobDirWithDifferentName_NotConsideredPrepared covers
// isPreparedCRTDLPath returning false from the switch default (crtdl_prep.go
// line 95): the path lives in jobDir but the filename is neither crtdl.json
// nor enriched-crtdl.json, so preparation must proceed and produce the
// canonical crtdl.json.
func TestPrepareCRTDL_InputInJobDirWithDifferentName_NotConsideredPrepared(t *testing.T) {
	tempDir := t.TempDir()
	jobsDir := filepath.Join(tempDir, "jobs")
	jobID := "job-other-name"

	jobDir := filepath.Join(jobsDir, jobID)
	require.NoError(t, os.MkdirAll(jobDir, 0755))

	// CRTDL lives inside jobDir but under a non-canonical filename.
	otherNamePath := writeCRTDLFile(t, jobDir, "user-supplied.json", crtdlWithoutPatientIdentifier())

	job := &models.PipelineJob{
		JobID:       jobID,
		InputSource: otherNamePath,
		InputType:   models.InputTypeCRTDL,
		CRTDLPath:   otherNamePath,
		Config: models.ProjectConfig{
			JobsDir:  jobsDir,
			Services: models.ServiceConfig{CRTDLPreprocessing: models.CRTDLPreprocessingConfig{Enabled: false}},
		},
	}

	require.NoError(t, pipeline.PrepareCRTDL(job, lib.NewLogger(lib.LogLevelDebug)))

	expectedPath := filepath.Join(jobDir, "crtdl.json")
	assert.Equal(t, expectedPath, job.CRTDLPath,
		"isPreparedCRTDLPath must return false for non-canonical filenames so prep proceeds")

	_, err := os.Stat(expectedPath)
	require.NoError(t, err, "Canonical crtdl.json must be written even though source was inside jobDir")
}

// TestPrepareCRTDL_PreprocessingDisabled_FailsWhenJobDirUnwritable covers the
// WriteFile error branch in saveCRTDL (crtdl_prep.go lines 101-103). Pass a
// jobsDir whose path is occupied by a regular file so jobDir cannot exist as a
// directory and os.WriteFile fails when saveCRTDL tries to write into it.
func TestPrepareCRTDL_PreprocessingDisabled_FailsWhenJobDirUnwritable(t *testing.T) {
	tempDir := t.TempDir()

	// jobsDir is a regular file, not a directory — any path inside it is invalid.
	jobsDir := filepath.Join(tempDir, "jobs-as-file")
	require.NoError(t, os.WriteFile(jobsDir, []byte("not a directory"), 0644))

	crtdlPath := writeCRTDLFile(t, tempDir, "input.json", crtdlWithoutPatientIdentifier())

	job := &models.PipelineJob{
		JobID:       "job-bad-jobdir",
		InputSource: crtdlPath,
		InputType:   models.InputTypeCRTDL,
		CRTDLPath:   crtdlPath,
		Config: models.ProjectConfig{
			JobsDir:  jobsDir,
			Services: models.ServiceConfig{CRTDLPreprocessing: models.CRTDLPreprocessingConfig{Enabled: false}},
		},
	}

	err := pipeline.PrepareCRTDL(job, lib.NewLogger(lib.LogLevelDebug))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to save CRTDL to job directory",
		"Error must come from saveCRTDL WriteFile branch")
}
