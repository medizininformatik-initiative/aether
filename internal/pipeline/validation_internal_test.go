package pipeline

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
)

// chunkTestThreshold holds two small patients in one chunk, but not a big one.
const chunkTestThreshold = 400

func smallPatient(id string) map[string]any {
	return map[string]any{"resourceType": "Patient", "id": id}
}

func bigPatient(id string) map[string]any {
	return map[string]any{"resourceType": "Patient", "id": id, "text": map[string]any{"div": strings.Repeat("x", 1000)}}
}

// chunkIDs returns the resource ids of each chunk, in chunk order.
func chunkIDs(t *testing.T, chunks [][]map[string]any) [][]string {
	t.Helper()
	ids := make([][]string, len(chunks))
	for i, chunk := range chunks {
		for _, entry := range chunk {
			resource, ok := lib.EntryResource(entry)
			require.True(t, ok)
			ids[i] = append(ids[i], lib.ResourceID(resource))
		}
	}
	return ids
}

func TestChunkResources_OversizedResourceGetsOwnChunkInInputOrder(t *testing.T) {
	tests := []struct {
		name      string
		resources []map[string]any
		want      [][]string
	}{
		{
			name:      "oversized in the middle",
			resources: []map[string]any{smallPatient("s1"), bigPatient("big"), smallPatient("s2")},
			want:      [][]string{{"s1"}, {"big"}, {"s2"}},
		},
		{
			name:      "oversized first",
			resources: []map[string]any{bigPatient("big"), smallPatient("s1")},
			want:      [][]string{{"big"}, {"s1"}},
		},
		{
			name:      "oversized last",
			resources: []map[string]any{smallPatient("s1"), bigPatient("big")},
			want:      [][]string{{"s1"}, {"big"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chunks, err := chunkResources(tt.resources, chunkTestThreshold, lib.NewLogger(lib.LogLevelError))
			require.NoError(t, err)
			assert.Equal(t, tt.want, chunkIDs(t, chunks))
		})
	}
}

// entrySize returns the serialized size of the Bundle entry that chunkResources builds
// for the resource at the given index.
func entrySize(t *testing.T, resource map[string]any, index int) int {
	t.Helper()
	size, err := models.CalculateJSONSize(map[string]any{
		"fullUrl":  validationEntryFullURL(resource, index),
		"resource": resource,
	})
	require.NoError(t, err)
	return size
}

// The collection Bundle wrapper counts toward the threshold: a resource that fits
// only without the wrapper is oversized, and it gets its own chunk.
func TestChunkResources_ResourceThatFitsOnlyWithoutWrapperIsOversized(t *testing.T) {
	resource := smallPatient("p1")
	threshold := entrySize(t, resource, 0)
	var logs strings.Builder
	logger := lib.NewLoggerWithWriter(lib.LogLevelWarn, &logs)

	chunks, err := chunkResources([]map[string]any{resource}, threshold, logger)

	require.NoError(t, err)
	assert.Equal(t, [][]string{{"p1"}}, chunkIDs(t, chunks))
	assert.Contains(t, logs.String(), "Oversized resource placed in individual chunk")
}

// Two resources that fit in one chunk only without the wrapper go to two chunks.
func TestChunkResources_WrapperSizeSplitsResourcesThatFitTogetherWithoutIt(t *testing.T) {
	p1, p2 := smallPatient("p1"), smallPatient("p2")
	size1, size2 := entrySize(t, p1, 0), entrySize(t, p2, 1)
	// The 1 is the comma between the two entries in the serialized entry array.
	threshold := size1 + 1 + size2
	wrapper := validationWrapperBytes()
	require.Positive(t, wrapper)
	require.LessOrEqual(t, max(size1, size2)+wrapper, threshold, "each resource alone must fit with the wrapper")

	chunks, err := chunkResources([]map[string]any{p1, p2}, threshold, lib.NewLogger(lib.LogLevelError))

	require.NoError(t, err)
	assert.Equal(t, [][]string{{"p1"}, {"p2"}}, chunkIDs(t, chunks))
}

// A resource that fills the threshold exactly is not oversized. Only the resource
// that exceeds the threshold is reported, with its type and size.
func TestChunkResources_WarnsOnlyForResourceAboveThreshold(t *testing.T) {
	fits := smallPatient("fits")
	big := bigPatient("big")
	threshold := entrySize(t, fits, 0) + validationWrapperBytes()
	var logs strings.Builder
	logger := lib.NewLoggerWithWriter(lib.LogLevelWarn, &logs)

	chunks, err := chunkResources([]map[string]any{fits, big}, threshold, logger)
	require.NoError(t, err)
	require.Equal(t, [][]string{{"fits"}, {"big"}}, chunkIDs(t, chunks))

	var warnings []string
	for _, line := range strings.Split(logs.String(), "\n") {
		if strings.Contains(line, "Oversized resource placed in individual chunk") {
			warnings = append(warnings, line)
		}
	}
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], fmt.Sprintf("[resourceType Patient size %d]", entrySize(t, big, 1)))
}
