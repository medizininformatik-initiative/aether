package integration

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

// TestCRTDLEnrichmentAppliedForLocalImportPath is the regression test for
// bug #323: when the import step is local_import (not torch) but
// CRTDLPreprocessing is enabled, every downstream step (e.g. flattening)
// must read the enriched CRTDL — not the original input.
//
// We don't run the full flattening pipeline here (that needs an external
// fhir-flattener service); instead we mimic exactly what flattening does
// today: it parses job.InputSource via services.ParseCRTDL and consumes the
// resulting attribute groups. If enrichment was correctly applied at job
// creation, the parsed groups must contain the added attribute.
func TestCRTDLEnrichmentAppliedForLocalImportPath(t *testing.T) {
	tempDir := t.TempDir()
	jobsDir := filepath.Join(tempDir, "jobs")
	require.NoError(t, os.MkdirAll(jobsDir, 0755))

	// Original CRTDL — Patient group has no identifier attribute. This is
	// the exact misconfiguration described in issue #323.
	crtdlPath := filepath.Join(tempDir, "query.crtdl")
	crtdl := map[string]any{
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
					},
				},
			},
		},
	}
	bytes, err := json.Marshal(crtdl)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(crtdlPath, bytes, 0644))

	// Pipeline uses local_import (not torch). Without the bug 323 fix,
	// enrichment would be skipped here.
	config := models.ProjectConfig{
		JobsDir: jobsDir,
		Pipeline: models.PipelineConfig{
			EnabledSteps: []models.StepName{models.StepLocalImport, models.StepFlattening},
		},
		Services: models.ServiceConfig{
			LocalImport: models.LocalImportConfig{Dir: tempDir},
			CRTDLPreprocessing: models.CRTDLPreprocessingConfig{
				Enabled: true,
				Enrichments: []models.GroupEnrichment{
					{
						GroupReference: "https://www.medizininformatik-initiative.de/fhir/core/modul-person/StructureDefinition/PatientPseudonymisiert",
						AttributesToAdd: []models.EnrichmentAttribute{
							{AttributeRef: "Patient.identifier", MustHave: false},
						},
					},
				},
			},
		},
		Retry: models.RetryConfig{MaxAttempts: 1, InitialBackoffMs: 1, MaxBackoffMs: 10},
	}

	logger := lib.NewLogger(lib.LogLevelDebug)
	job, err := pipeline.CreateJob(crtdlPath, config, logger)
	require.NoError(t, err)

	// CreateJob must repoint InputSource at the enriched CRTDL inside jobDir.
	expectedPath := filepath.Join(jobsDir, job.JobID, "enriched-crtdl.json")
	require.Equal(t, expectedPath, job.InputSource,
		"InputSource must point at the enriched CRTDL after CreateJob")

	// Mimic what ExecuteFlatteningStep does: ParseCRTDL on job.InputSource.
	parsed, err := services.ParseCRTDL(job.InputSource)
	require.NoError(t, err)

	groups := services.GetAttributeGroups(parsed)
	require.Len(t, groups, 1)

	refs := make([]string, 0, len(groups[0].Attributes))
	for _, a := range groups[0].Attributes {
		refs = append(refs, a.AttributeRef)
	}
	assert.Contains(t, refs, "Patient.identifier",
		"flattening (and every other downstream step) must see Patient.identifier added by enrichment")
	assert.Contains(t, refs, "Patient.id",
		"original Patient.id must be preserved")

	// Reload from disk — the enriched InputSource must be persisted so
	// `aether pipeline continue` resumes with the correct CRTDL.
	reloaded, err := pipeline.LoadJob(jobsDir, job.JobID)
	require.NoError(t, err)
	assert.Equal(t, expectedPath, reloaded.InputSource,
		"persisted state must reference the enriched CRTDL for resume")
}
