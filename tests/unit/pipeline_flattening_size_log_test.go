package unit

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/lib"
)

const oversizedWarning = "Resource is larger than the batch budget of its group"

// logEntry returns the one log line that contains message, without its timestamp.
func logEntry(t *testing.T, logs, message string) string {
	t.Helper()
	line := logLine(t, bytes.NewBufferString(logs), message)
	return line[strings.Index(line, "["):]
}

func TestFlatteningStep_LogsGroupSummary(t *testing.T) {
	bundle := makeBundle("b1",
		paddedPatient(t, "1", 1500), makeProvenance("prov-1", "Patient/1", batchingGroupID),
		makeProvenance("prov-2", "Patient/2", batchingGroupID),
	)

	_, logs, err := runFlatteningWithOptions(t, map[string][]map[string]any{
		"a_bundle.ndjson":   {bundle},
		"b_patients.ndjson": {paddedPatient(t, "2", 3000)},
	}, lib.LogLevelInfo, nil)
	require.NoError(t, err)

	assert.Equal(t, "[INFO] Flattening group summary | [group_name Patient resource_count 2 total_bytes 4500"+
		" batch_count 1 largest_batch_bytes 4500 largest_resource Patient/2 largest_resource_bytes 3000]",
		logEntry(t, logs, "Flattening group summary"))
}

func TestFlatteningStep_LogsLargestBatchWhenItIsNotTheLast(t *testing.T) {
	_, logs, err := runFlatteningWithOptions(t, map[string][]map[string]any{
		"provenance.ndjson": {provenanceOnlyBundle("1", "2", "3")},
		// Each line after the first also counts the newline before it.
		"patients.ndjson": {
			paddedPatient(t, "1", 600_000),
			paddedPatient(t, "2", 600_000),
			paddedPatient(t, "3", 100_000),
		},
	}, lib.LogLevelInfo, nil)
	require.NoError(t, err)

	assert.Equal(t, "[INFO] Flattening group summary | [group_name Patient resource_count 3 total_bytes 1300002"+
		" batch_count 2 largest_batch_bytes 1200001 largest_resource Patient/2 largest_resource_bytes 600001]",
		logEntry(t, logs, "Flattening group summary"))
}

func TestFlatteningStep_WarnsAboutResourceLargerThanBudget(t *testing.T) {
	_, logs, err := runFlatteningWithOptions(t, map[string][]map[string]any{
		"provenance.ndjson": {provenanceOnlyBundle("big")},
		"patients.ndjson":   {paddedPatient(t, "big", oneMiB+1)},
	}, lib.LogLevelInfo, nil)
	require.NoError(t, err)

	assert.Equal(t, "[WARN] "+oversizedWarning+
		" | [group_name Patient resource Patient/big resource_bytes 1048577 budget_bytes 1048576]",
		logEntry(t, logs, oversizedWarning))
}

func TestFlatteningStep_WarnsAboutBundleEntryLargerThanBudget(t *testing.T) {
	bundle := makeBundle("b1",
		paddedPatient(t, "big", oneMiB+1), makeProvenance("prov-big", "Patient/big", batchingGroupID),
	)

	_, logs, err := runFlatteningWithOptions(t, map[string][]map[string]any{"bundle.ndjson": {bundle}}, lib.LogLevelInfo, nil)
	require.NoError(t, err)

	assert.Equal(t, "[WARN] "+oversizedWarning+
		" | [group_name Patient resource Patient/big resource_bytes 1048577 budget_bytes 1048576]",
		logEntry(t, logs, oversizedWarning))
}

func TestFlatteningStep_LogsGroupSummaryWhenFlattenerFails(t *testing.T) {
	_, logs, err := runFlatteningWithOptions(t, map[string][]map[string]any{
		"provenance.ndjson": {provenanceOnlyBundle("1")},
		"patients.ndjson":   {paddedPatient(t, "1", 2000)},
	}, lib.LogLevelInfo, errors.New("out of memory"))

	require.Error(t, err)
	assert.Equal(t, "[INFO] Flattening group summary | [group_name Patient resource_count 1 total_bytes 2000"+
		" batch_count 1 largest_batch_bytes 2000 largest_resource Patient/1 largest_resource_bytes 2000]",
		logEntry(t, logs, "Flattening group summary"))
}

func histogramInputs(t *testing.T) map[string][]map[string]any {
	t.Helper()
	return map[string][]map[string]any{
		"provenance.ndjson": {provenanceOnlyBundle("a", "b", "c", "d", "e")},
		"patients.ndjson": {
			paddedPatient(t, "a", 999),
			paddedPatient(t, "b", 1000),
			paddedPatient(t, "c", 9998), // plus the newline before the line: 9999
			paddedPatient(t, "d", 100000),
			paddedPatient(t, "e", 1000000),
		},
	}
}

func TestFlatteningStep_LogsSizeHistogramAtDebugLevel(t *testing.T) {
	_, logs, err := runFlatteningWithOptions(t, histogramInputs(t), lib.LogLevelDebug, nil)
	require.NoError(t, err)

	assert.Contains(t, logs, "[DEBUG] Flattening group resource sizes")
	assert.Contains(t, logs, "group_name Patient lt_1kb 1 lt_10kb 2 lt_100kb 0 lt_1mb 1 ge_1mb 1")
}

func TestFlatteningStep_OmitsSizeHistogramAtInfoLevel(t *testing.T) {
	_, logs, err := runFlatteningWithOptions(t, histogramInputs(t), lib.LogLevelInfo, nil)
	require.NoError(t, err)

	assert.NotContains(t, logs, "Flattening group resource sizes")
}

func TestFlatteningStep_NoWarningForResourceEqualToBudget(t *testing.T) {
	_, logs, err := runFlatteningWithOptions(t, map[string][]map[string]any{
		"provenance.ndjson": {provenanceOnlyBundle("edge")},
		"patients.ndjson":   {paddedPatient(t, "edge", oneMiB)},
	}, lib.LogLevelInfo, nil)
	require.NoError(t, err)

	assert.NotContains(t, logs, oversizedWarning)
}

func TestFlatteningStep_LogsFirstOfEqualLargestResources(t *testing.T) {
	_, logs, err := runFlatteningWithOptions(t, map[string][]map[string]any{
		"provenance.ndjson": {provenanceOnlyBundle("1", "2")},
		"patients.ndjson": {
			paddedPatient(t, "1", 2000),
			paddedPatient(t, "2", 1999), // plus the newline before the line: 2000
		},
	}, lib.LogLevelInfo, nil)
	require.NoError(t, err)

	assert.Contains(t, logs, "largest_resource Patient/1")
	assert.Contains(t, logs, "largest_resource_bytes 2000")
}

func TestFlatteningStep_SizeHistogramPutsBucketLimitInNextBucket(t *testing.T) {
	_, logs, err := runFlatteningWithOptions(t, map[string][]map[string]any{
		"provenance.ndjson": {provenanceOnlyBundle("a", "b", "c", "d")},
		// Each line after the first also counts the newline before it.
		"patients.ndjson": {
			paddedPatient(t, "a", 1000),
			paddedPatient(t, "b", 9999),
			paddedPatient(t, "c", 99999),
			paddedPatient(t, "d", 999999),
		},
	}, lib.LogLevelDebug, nil)
	require.NoError(t, err)

	assert.Contains(t, logs, "group_name Patient lt_1kb 0 lt_10kb 1 lt_100kb 1 lt_1mb 1 ge_1mb 1")
}

func TestFlatteningStep_StopsAtFirstFailedFlushOfFullBatch(t *testing.T) {
	run, _, err := runFlatteningWithOptions(t, map[string][]map[string]any{
		"provenance.ndjson": {provenanceOnlyBundle("1", "2")},
		"patients.ndjson": {
			paddedPatient(t, "1", oneMiB),
			paddedPatient(t, "2", oneMiB),
		},
	}, lib.LogLevelInfo, errors.New("out of memory"))

	require.ErrorContains(t, err, "out of memory")
	assert.Equal(t, []int{1}, run.batches)
}
