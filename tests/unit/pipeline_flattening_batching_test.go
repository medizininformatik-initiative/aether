package unit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
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
	run, _, err := runFlatteningWithOptions(t, inputs, lib.LogLevelInfo, nil)
	require.NoError(t, err)
	return run
}

// runFlatteningWithOptions is runFlatteningWithFake with a log level and an
// optional error that the fake flattener returns on every call. It returns the
// captured log text and the step error instead of requiring success.
func runFlatteningWithOptions(t *testing.T, inputs map[string][]map[string]any, level lib.LogLevel, flattenErr error) (flatteningRun, string, error) {
	t.Helper()
	var run flatteningRun
	fake := &servicestest.MockFlattener{
		FlattenFunc: func(_ models.ViewDefinition, resources []json.RawMessage) ([][]string, error) {
			run.batches = append(run.batches, len(resources))
			if flattenErr != nil {
				return nil, flattenErr
			}
			rows := make([][]string, len(resources))
			for i, r := range resources {
				var res struct {
					ID string `json:"id"`
				}
				require.NoError(t, json.Unmarshal(r, &res))
				rows[i] = []string{res.ID}
			}
			return rows, nil
		},
	}
	installFakeFlattener(t, fake)

	job, jobDir, inputDir := newBatchingJob(t)
	for name, resources := range inputs {
		writeTestNDJSON(t, filepath.Join(inputDir, name), resources)
	}

	stopCapture := captureStdoutForTest(t)
	var logBuf strings.Builder
	err := runPipelineStep(models.StepFlattening, job, jobDir, lib.NewLoggerWithWriter(level, &logBuf))
	run.stdout = stopCapture()

	step, found := models.GetStepByName(*job, models.StepFlattening)
	require.True(t, found)
	run.step = step
	return run, logBuf.String(), err
}

// newBatchingJob prepares a flattening job for one Patient attribute group with
// a batch budget of 1 MiB. It returns the job, its directory and its import
// directory.
func newBatchingJob(t *testing.T) (*models.PipelineJob, string, string) {
	t.Helper()
	tempDir := t.TempDir()
	jobDir := filepath.Join(tempDir, "jobs", "flatten-batching-job")
	inputDir := filepath.Join(jobDir, "import")
	require.NoError(t, os.MkdirAll(inputDir, 0755))

	crtdlPath := filepath.Join(tempDir, "crtdl.json")
	writeTestCRTDL(t, crtdlPath, batchingGroupID, "Patient", batchingProfileURL)
	lookupPath := filepath.Join(tempDir, "lookup.json")
	writeTestLookupTable(t, lookupPath, batchingProfileURL, "Patient")

	job := createFlatteningTestJob("http://flattener.invalid", lookupPath, crtdlPath)
	job.Config.Services.Flattening.BatchSizeMB = 1
	return job, jobDir, inputDir
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

// linkedBundle puts each Patient in one Bundle, together with a Provenance
// that links it to the test group.
func linkedBundle(patients ...map[string]any) map[string]any {
	entries := make([]map[string]any, 0, 2*len(patients))
	for _, p := range patients {
		id := p["id"].(string)
		entries = append(entries, p, makeProvenance("prov-"+id, "Patient/"+id, batchingGroupID))
	}
	return makeBundle("linked", entries...)
}

func TestFlatteningStep_CountsFilesWritten(t *testing.T) {
	bundle := makeBundle("b1",
		batchingPatient("1"), makeProvenance("prov-1", "Patient/1", batchingGroupID),
		batchingPatient("2"), makeProvenance("prov-2", "Patient/2", batchingGroupID),
	)

	run := runFlatteningWithFake(t, map[string][]map[string]any{"bundle.ndjson": {bundle}})

	assert.Equal(t, 1, run.step.FilesProcessed)
}

func TestFlatteningStep_SkipsResourcesOutsideBundles(t *testing.T) {
	bundle := makeBundle("b1",
		batchingPatient("1"), makeProvenance("prov-1", "Patient/1", batchingGroupID),
		makeProvenance("prov-2", "Patient/2", batchingGroupID),
	)

	run := runFlatteningWithFake(t, map[string][]map[string]any{"input.ndjson": {bundle, batchingPatient("2")}})

	assert.Contains(t, run.stdout, "✓ Patient (1 resources →")
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

func TestFlatteningStep_SkipsMalformedBundleEntriesAndKeepsTheOthers(t *testing.T) {
	bundle := makeBundle("b1",
		batchingPatient("1"), makeProvenance("prov-1", "Patient/1", batchingGroupID),
		batchingPatient("2"), makeProvenance("prov-2", "Patient/2", batchingGroupID),
	)
	bundle["entry"] = append([]any{"not an entry", map[string]any{"resource": "not a resource"}},
		bundle["entry"].([]any)...)

	run := runFlatteningWithFake(t, map[string][]map[string]any{"bundle.ndjson": {bundle}})

	assert.Equal(t, []int{2}, run.batches)
}

func TestFlatteningStep_SkipsBundleEntryWithNonStringID(t *testing.T) {
	bundle := linkedBundle(batchingPatient("1"))
	bundle["entry"] = append([]any{map[string]any{"resource": map[string]any{"resourceType": "Patient", "id": 123}}},
		bundle["entry"].([]any)...)

	run := runFlatteningWithFake(t, map[string][]map[string]any{"bundle.ndjson": {bundle}})

	assert.Equal(t, []int{1}, run.batches)
}

func TestFlatteningStep_FailsOnTopLevelValueThatIsNotAnObject(t *testing.T) {
	installFakeFlattener(t, &servicestest.MockFlattener{})
	job, jobDir, inputDir := newBatchingJob(t)
	require.NoError(t, os.WriteFile(filepath.Join(inputDir, "input.ndjson"), []byte(`"not a resource"`+"\n"), 0o644))

	stopCapture := captureStdoutForTest(t)
	err := runPipelineStep(models.StepFlattening, job, jobDir, lib.NewLogger(lib.LogLevelError))
	stopCapture()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load input.ndjson")
}

func TestFlatteningStep_SendsBundleEntryResourcesWithoutProvenance(t *testing.T) {
	var sent []json.RawMessage
	installFakeFlattener(t, &servicestest.MockFlattener{
		FlattenFunc: func(_ models.ViewDefinition, resources []json.RawMessage) ([][]string, error) {
			sent = append(sent, resources...)
			return nil, nil
		},
	})
	job, jobDir, inputDir := newBatchingJob(t)
	patient := batchingPatient("1")
	writeTestNDJSON(t, filepath.Join(inputDir, "bundle.ndjson"), []map[string]any{
		makeBundle("b1", patient, makeProvenance("prov-1", "Patient/1", batchingGroupID)),
	})

	stopCapture := captureStdoutForTest(t)
	err := runPipelineStep(models.StepFlattening, job, jobDir, lib.NewLogger(lib.LogLevelError))
	stopCapture()
	require.NoError(t, err)

	want, err := json.Marshal(patient)
	require.NoError(t, err)
	require.Len(t, sent, 1)
	assert.JSONEq(t, string(want), string(sent[0]))
}

func TestFlatteningStep_BundleEntriesBelowBudgetStayInOneBatch(t *testing.T) {
	run := runFlatteningWithFake(t, map[string][]map[string]any{
		"bundle.ndjson": {linkedBundle(
			paddedPatient(t, "1", oneMiB/2),
			paddedPatient(t, "2", oneMiB/2-1),
			batchingPatient("3"),
		)},
	})

	assert.Equal(t, []int{3}, run.batches)
}
