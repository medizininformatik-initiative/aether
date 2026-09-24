package unit

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/pipeline"
	"github.com/medizininformatik-initiative/aether/internal/services"
	"github.com/medizininformatik-initiative/aether/internal/services/servicestest"
)

const (
	batchingProfileURL = "https://example.com/Patient"
	batchingGroupID    = "group-patient"
	oneMiB             = 1024 * 1024
)

// flatteningRun is the observable result of one flattening step run.
type flatteningRun struct {
	batches []int // resource count of each flattener call, in call order
	step    models.PipelineStep
	stdout  string
}

// runFlatteningWithFake runs the flattening step for one Patient attribute
// group with a batch budget of 1 MiB. inputs maps an NDJSON file name to its
// resources. The fake flattener returns one row per resource.
func runFlatteningWithFake(t *testing.T, inputs map[string][]map[string]any) flatteningRun {
	t.Helper()
	var run flatteningRun
	fake := &servicestest.MockFlattener{
		FlattenFunc: func(_ models.ViewDefinition, resources []map[string]any) ([][]string, error) {
			run.batches = append(run.batches, len(resources))
			rows := make([][]string, len(resources))
			for i, r := range resources {
				rows[i] = []string{fmt.Sprint(r["id"])}
			}
			return rows, nil
		},
	}
	pipeline.SetFlattenerFactoryForTesting(
		func(models.FlatteningConfig, models.RetryConfig, *http.Transport, *lib.Logger) services.Flattener {
			return fake
		},
	)
	t.Cleanup(pipeline.ResetFlattenerFactory)

	tempDir := t.TempDir()
	jobDir := filepath.Join(tempDir, "jobs", "flatten-batching-job")
	inputDir := filepath.Join(jobDir, "import")
	require.NoError(t, os.MkdirAll(inputDir, 0755))
	for name, resources := range inputs {
		writeTestNDJSON(t, filepath.Join(inputDir, name), resources)
	}

	crtdlPath := filepath.Join(tempDir, "crtdl.json")
	writeTestCRTDL(t, crtdlPath, batchingGroupID, "Patient", batchingProfileURL)
	lookupPath := filepath.Join(tempDir, "lookup.json")
	writeTestLookupTable(t, lookupPath, batchingProfileURL, "Patient")

	job := createFlatteningTestJob("http://flattener.invalid", lookupPath, crtdlPath)
	job.Config.Services.Flattening.BatchSizeMB = 1

	stopCapture := captureStdoutForTest(t)
	err := runPipelineStep(models.StepFlattening, job, jobDir, createFlatteningTestLogger())
	run.stdout = stopCapture()
	require.NoError(t, err)

	step, found := models.GetStepByName(*job, models.StepFlattening)
	require.True(t, found)
	run.step = step
	return run
}

func batchingPatient(id string) map[string]any {
	return map[string]any{
		"resourceType": "Patient",
		"id":           id,
		"meta":         map[string]any{"profile": []any{batchingProfileURL}},
	}
}

// paddedPatient returns a Patient whose JSON encoding is exactly size bytes.
func paddedPatient(t *testing.T, id string, size int) map[string]any {
	t.Helper()
	p := batchingPatient(id)
	p["padding"] = ""
	pad := size - len(mustMarshalJSON(p))
	require.GreaterOrEqual(t, pad, 0)
	p["padding"] = strings.Repeat("x", pad)
	require.Len(t, mustMarshalJSON(p), size)
	return p
}

// provenanceOnlyBundle links Patient/<id> to the test group for each id. The
// Patients themselves come as single resources in another file.
func provenanceOnlyBundle(ids ...string) map[string]any {
	provenances := make([]map[string]any, len(ids))
	for i, id := range ids {
		provenances[i] = makeProvenance("prov-"+id, "Patient/"+id, batchingGroupID)
	}
	return makeBundle("provenances", provenances...)
}

func TestFlatteningStep_CountsFilesWritten(t *testing.T) {
	bundle := makeBundle("b1",
		batchingPatient("1"), makeProvenance("prov-1", "Patient/1", batchingGroupID),
		batchingPatient("2"), makeProvenance("prov-2", "Patient/2", batchingGroupID),
	)

	run := runFlatteningWithFake(t, map[string][]map[string]any{"bundle.ndjson": {bundle}})

	assert.Equal(t, 1, run.step.FilesProcessed)
}

func TestFlatteningStep_ReportsResourceCountOfBundleAndSingleResources(t *testing.T) {
	bundle := makeBundle("b1",
		batchingPatient("1"), makeProvenance("prov-1", "Patient/1", batchingGroupID),
		makeProvenance("prov-2", "Patient/2", batchingGroupID),
	)

	run := runFlatteningWithFake(t, map[string][]map[string]any{"input.ndjson": {bundle, batchingPatient("2")}})

	assert.Contains(t, run.stdout, "✓ Patient (2 resources →")
}

func TestFlatteningStep_BundleEntriesFlushWhenBatchReachesBudget(t *testing.T) {
	bundle := makeBundle("b1",
		paddedPatient(t, "1", oneMiB/2), makeProvenance("prov-1", "Patient/1", batchingGroupID),
		paddedPatient(t, "2", oneMiB/2), makeProvenance("prov-2", "Patient/2", batchingGroupID),
		batchingPatient("3"), makeProvenance("prov-3", "Patient/3", batchingGroupID),
	)

	run := runFlatteningWithFake(t, map[string][]map[string]any{"bundle.ndjson": {bundle}})

	assert.Equal(t, []int{2, 1}, run.batches)
}

// The size of a single resource is its line length. From the second line on,
// the size includes the newline that comes before the line.
func TestFlatteningStep_SingleResourcesFlushWhenBatchReachesBudget(t *testing.T) {
	run := runFlatteningWithFake(t, map[string][]map[string]any{
		"provenance.ndjson": {provenanceOnlyBundle("1", "2", "3")},
		"patients.ndjson": {
			paddedPatient(t, "1", oneMiB/2),
			paddedPatient(t, "2", oneMiB/2-1),
			batchingPatient("3"),
		},
	})

	assert.Equal(t, []int{2, 1}, run.batches)
}

func TestFlatteningStep_SingleResourcesUnderBudgetGoInOneBatch(t *testing.T) {
	run := runFlatteningWithFake(t, map[string][]map[string]any{
		"provenance.ndjson": {provenanceOnlyBundle("1", "2", "3")},
		"patients.ndjson": {
			paddedPatient(t, "1", oneMiB/2),
			batchingPatient("2"),
			batchingPatient("3"),
		},
	})

	assert.Equal(t, []int{3}, run.batches)
}
