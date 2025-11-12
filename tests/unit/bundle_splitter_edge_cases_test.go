package unit

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

// TestPartitionEntries_EmptyArray tests error handling for empty entry array
func TestPartitionEntries_EmptyArray(t *testing.T) {
	entries := []map[string]any{}
	thresholdBytes := 10 * 1024 * 1024

	_, err := services.PartitionEntries(entries, thresholdBytes)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty entry array")
}

// TestPartitionEntries_OversizedEntry tests handling of single entry exceeding threshold
func TestPartitionEntries_OversizedEntry(t *testing.T) {
	// Create an entry that's larger than threshold
	largeBundle := CreateTestBundle(50, 500) // ~25MB
	largeEntry := map[string]any{
		"fullUrl": "urn:uuid:large-obs",
		"resource": map[string]any{
			"resourceType": "Observation",
			"id":           "obs-large",
			"note":         largeBundle, // Embed large data
		},
	}

	entries := []map[string]any{largeEntry}
	thresholdBytes := 10 * 1024 * 1024 // 10MB

	_, err := services.PartitionEntries(entries, thresholdBytes)
	require.Error(t, err)

	// Should be an OversizedResourceError
	oversizedErr, ok := err.(*models.OversizedResourceError)
	require.True(t, ok, "Error should be OversizedResourceError")
	assert.Equal(t, "Observation", oversizedErr.ResourceType)
	assert.Greater(t, oversizedErr.Size, thresholdBytes)
	assert.NotEmpty(t, oversizedErr.Guidance)
}

// TestPartitionEntries_MultipleOversizedEntries tests multiple oversized entries
func TestPartitionEntries_MultipleOversizedEntries(t *testing.T) {
	// Create bundle with first entry being oversized
	largeBundle := CreateTestBundle(50, 500) // ~25MB
	entries := []map[string]any{
		{
			"fullUrl": "urn:uuid:large-1",
			"resource": map[string]any{
				"resourceType": "Observation",
				"id":           "obs-1",
				"note":         largeBundle,
			},
		},
		{
			"fullUrl": "urn:uuid:normal",
			"resource": map[string]any{
				"resourceType": "Patient",
				"id":           "pat-1",
			},
		},
	}

	thresholdBytes := 10 * 1024 * 1024

	_, err := services.PartitionEntries(entries, thresholdBytes)
	require.Error(t, err)

	// Should fail on the first oversized entry
	oversizedErr, ok := err.(*models.OversizedResourceError)
	require.True(t, ok)
	assert.Equal(t, "Observation", oversizedErr.ResourceType)
}

// TestPartitionEntries_OversizedEntryWithoutResourceInfo tests error when resource info is missing
func TestPartitionEntries_OversizedEntryWithoutResourceInfo(t *testing.T) {
	// Create an oversized entry with no resource type or ID
	largeData := make([]byte, 15*1024*1024) // 15MB
	for i := range largeData {
		largeData[i] = 'x'
	}

	entries := []map[string]any{
		{
			"fullUrl": "urn:uuid:malformed",
			"resource": map[string]any{
				"data": string(largeData),
			},
		},
	}

	thresholdBytes := 10 * 1024 * 1024

	_, err := services.PartitionEntries(entries, thresholdBytes)
	require.Error(t, err)

	oversizedErr, ok := err.(*models.OversizedResourceError)
	require.True(t, ok)
	assert.Equal(t, "Unknown", oversizedErr.ResourceType)
	assert.Equal(t, "unknown", oversizedErr.ResourceID)
}

// TestSplitBundle_InvalidBundle tests error handling for invalid bundle structure
func TestSplitBundle_InvalidBundle(t *testing.T) {
	testCases := []struct {
		name          string
		bundle        map[string]any
		errorContains string
	}{
		{
			name: "Missing ID",
			bundle: map[string]any{
				"resourceType": "Bundle",
				"type":         "collection",
				"entry":        []any{},
			},
			errorContains: "id",
		},
		{
			name: "Missing type",
			bundle: map[string]any{
				"resourceType": "Bundle",
				"id":           "test-bundle",
				"entry":        []any{},
			},
			errorContains: "type",
		},
		{
			name:          "Empty bundle",
			bundle:        map[string]any{},
			errorContains: "invalid Bundle",
		},
	}

	thresholdBytes := 10 * 1024 * 1024

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := services.SplitBundle(tc.bundle, thresholdBytes)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.errorContains)
		})
	}
}

// TestSplitBundle_InvalidEntries tests handling of malformed entries
func TestSplitBundle_InvalidEntries(t *testing.T) {
	// Bundle with entries field but wrong type
	bundle := map[string]any{
		"resourceType": "Bundle",
		"id":           "test-bundle",
		"type":         "collection",
		"entry":        "not-an-array", // Wrong type
	}

	thresholdBytes := 10 * 1024 * 1024

	_, err := services.SplitBundle(bundle, thresholdBytes)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "extract entries")
}

// TestSplitBundle_OversizedSingleEntry tests bundle with single oversized entry
func TestSplitBundle_OversizedSingleEntry(t *testing.T) {
	// Create bundle with single oversized entry
	largeData := make([]byte, 15*1024*1024) // 15MB
	for i := range largeData {
		largeData[i] = 'x'
	}

	bundle := map[string]any{
		"resourceType": "Bundle",
		"id":           "oversized-bundle",
		"type":         "collection",
		"entry": []any{
			map[string]any{
				"fullUrl": "urn:uuid:obs-large",
				"resource": map[string]any{
					"resourceType": "Observation",
					"id":           "obs-oversized",
					"note": []map[string]any{
						{"text": string(largeData)},
					},
				},
			},
		},
	}

	thresholdBytes := 10 * 1024 * 1024

	_, err := services.SplitBundle(bundle, thresholdBytes)
	require.Error(t, err)

	// Check if it's an OversizedResourceError (might be wrapped)
	var oversizedErr *models.OversizedResourceError
	if errors.As(err, &oversizedErr) {
		assert.Equal(t, "Observation", oversizedErr.ResourceType)
		assert.Contains(t, err.Error(), "exceeds threshold")
	} else {
		// If not an OversizedResourceError, just verify error mentions the issue
		assert.Contains(t, err.Error(), "exceeds threshold")
	}
}

// TestReassembleBundle_EmptyArray tests error handling for empty chunk array
func TestReassembleBundle_EmptyArray(t *testing.T) {
	metadata := models.BundleMetadata{
		ID:   "test-bundle",
		Type: "collection",
	}

	chunks := []map[string]any{}

	_, err := services.ReassembleBundle(metadata, chunks)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty chunk array")
}

// TestReassembleBundle_InvalidFirstChunk tests error when first chunk is not a Bundle
func TestReassembleBundle_InvalidFirstChunk(t *testing.T) {
	metadata := models.BundleMetadata{
		ID:   "test-bundle",
		Type: "collection",
	}

	chunks := []map[string]any{
		{
			"resourceType": "Patient", // Not a Bundle
			"id":           "pat-1",
		},
	}

	_, err := services.ReassembleBundle(metadata, chunks)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a Bundle")
}

// TestReassembleBundle_InvalidMiddleChunk tests error when middle chunk is invalid
func TestReassembleBundle_InvalidMiddleChunk(t *testing.T) {
	metadata := models.BundleMetadata{
		ID:   "test-bundle",
		Type: "collection",
	}

	chunks := []map[string]any{
		{
			"resourceType": "Bundle",
			"id":           "chunk-0",
			"type":         "collection",
			"entry": []any{
				map[string]any{
					"fullUrl": "urn:uuid:pat-1",
					"resource": map[string]any{
						"resourceType": "Patient",
						"id":           "pat-1",
					},
				},
			},
		},
		{
			"resourceType": "Observation", // Not a Bundle
			"id":           "obs-1",
		},
	}

	_, err := services.ReassembleBundle(metadata, chunks)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "chunk 1")
	assert.Contains(t, err.Error(), "not a Bundle")
}

// TestReassembleBundle_ChunkWithInvalidEntries tests chunk with malformed entries
func TestReassembleBundle_ChunkWithInvalidEntries(t *testing.T) {
	metadata := models.BundleMetadata{
		ID:   "test-bundle",
		Type: "collection",
	}

	chunks := []map[string]any{
		{
			"resourceType": "Bundle",
			"id":           "chunk-0",
			"type":         "collection",
			"entry":        "not-an-array", // Invalid
		},
	}

	_, err := services.ReassembleBundle(metadata, chunks)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "extract entries")
}

// TestReassembleBundle_SingleChunk tests reassembly of single chunk (not split)
func TestReassembleBundle_SingleChunk(t *testing.T) {
	metadata := models.BundleMetadata{
		ID:   "test-bundle",
		Type: "collection",
	}

	chunks := []map[string]any{
		{
			"resourceType": "Bundle",
			"id":           "test-bundle",
			"type":         "collection",
			"entry": []any{
				map[string]any{
					"fullUrl": "urn:uuid:pat-1",
					"resource": map[string]any{
						"resourceType": "Patient",
						"id":           "pat-1",
					},
				},
			},
		},
	}

	result, err := services.ReassembleBundle(metadata, chunks)
	require.NoError(t, err)

	assert.Equal(t, 1, result.EntryCount)
	assert.False(t, result.WasReassembled) // Only 1 chunk means not split
	assert.Equal(t, "test-bundle", result.OriginalID)
}

// TestCalculateChunkStats_EmptyResult tests stats calculation with empty result
func TestCalculateChunkStats_EmptyResult(t *testing.T) {
	result := models.SplitResult{
		Metadata: models.BundleMetadata{
			ID:   "test-bundle",
			Type: "collection",
		},
		Chunks:       []models.BundleChunk{},
		WasSplit:     false,
		OriginalSize: 1000,
		TotalChunks:  0,
	}

	stats := services.CalculateChunkStats(result)

	assert.Equal(t, "test-bundle", stats.BundleID)
	assert.Equal(t, 1000, stats.OriginalSize)
	assert.Equal(t, 0, stats.OriginalEntries)
	assert.Equal(t, 0, stats.ChunksCreated)
	assert.Equal(t, 0, stats.SmallestChunkSize)
	assert.Equal(t, 0, stats.LargestChunkSize)
	assert.Equal(t, 0, stats.AverageChunkSize)
}

// TestCalculateChunkStats_SingleChunk tests stats for non-split bundle
func TestCalculateChunkStats_SingleChunk(t *testing.T) {
	entries := []map[string]any{
		{
			"fullUrl": "urn:uuid:pat-1",
			"resource": map[string]any{
				"resourceType": "Patient",
				"id":           "pat-1",
			},
		},
	}

	metadata := models.BundleMetadata{
		ID:   "single-chunk",
		Type: "collection",
	}

	chunk, _ := models.CreateBundleChunk(metadata, entries, 0, 1)

	result := models.SplitResult{
		Metadata:     metadata,
		Chunks:       []models.BundleChunk{chunk},
		WasSplit:     false,
		OriginalSize: 500,
		TotalChunks:  1,
	}

	stats := services.CalculateChunkStats(result)

	assert.Equal(t, "single-chunk", stats.BundleID)
	assert.Equal(t, 1, stats.OriginalEntries)
	assert.Equal(t, 1, stats.ChunksCreated)
	assert.Equal(t, chunk.EstimatedSize, stats.SmallestChunkSize)
	assert.Equal(t, chunk.EstimatedSize, stats.LargestChunkSize)
	assert.Equal(t, chunk.EstimatedSize, stats.AverageChunkSize)
}

// TestCalculateChunkStats_MultipleChunks tests stats for split bundle
func TestCalculateChunkStats_MultipleChunks(t *testing.T) {
	// Create bundle with multiple chunks of varying sizes
	bundle := CreateTestBundle(100, 100) // ~10MB
	thresholdBytes := 3 * 1024 * 1024    // 3MB

	result, err := services.SplitBundle(bundle, thresholdBytes)
	require.NoError(t, err)
	require.True(t, result.WasSplit)
	require.Greater(t, len(result.Chunks), 1)

	stats := services.CalculateChunkStats(result)

	assert.Equal(t, bundle["id"].(string), stats.BundleID)
	assert.Equal(t, 100, stats.OriginalEntries)
	assert.Equal(t, len(result.Chunks), stats.ChunksCreated)
	assert.Greater(t, stats.SmallestChunkSize, 0)
	assert.Greater(t, stats.LargestChunkSize, 0)
	assert.GreaterOrEqual(t, stats.LargestChunkSize, stats.SmallestChunkSize)
	assert.Greater(t, stats.AverageChunkSize, 0)
}

// TestSplitBundle_RealWorldScenario tests realistic large bundle splitting
func TestSplitBundle_RealWorldScenario(t *testing.T) {
	// Simulate a 50MB bundle with 500 entries (~100KB each)
	bundle := CreateTestBundle(500, 100)
	thresholdBytes := 10 * 1024 * 1024 // 10MB

	result, err := services.SplitBundle(bundle, thresholdBytes)
	require.NoError(t, err)

	// Verify splitting occurred
	assert.True(t, result.WasSplit)
	assert.Greater(t, result.TotalChunks, 1)

	// Verify chunk count is reasonable (expect ~5 chunks for 50MB / 10MB)
	assert.GreaterOrEqual(t, result.TotalChunks, 4)
	assert.LessOrEqual(t, result.TotalChunks, 10)

	// Verify all entries are preserved
	totalEntries := 0
	for _, chunk := range result.Chunks {
		totalEntries += len(chunk.Entries)
		assert.Greater(t, len(chunk.Entries), 0)
	}
	assert.Equal(t, 500, totalEntries)

	// Verify chunk sizes are reasonable
	for i, chunk := range result.Chunks {
		// Each chunk should be roughly under threshold (may exceed due to greedy algorithm)
		// But not by more than the largest single entry
		assert.Less(t, chunk.EstimatedSize, thresholdBytes*2,
			"Chunk %d size %d exceeds 2x threshold", i, chunk.EstimatedSize)
	}
}

// TestReassembleBundle_PreservesMetadata tests that reassembly preserves FHIR metadata
func TestReassembleBundle_PreservesMetadata(t *testing.T) {
	// Create bundle large enough to trigger splitting
	bundle := CreateTestBundle(100, 100) // ~10MB total
	thresholdBytes := 3 * 1024 * 1024    // 3MB threshold ensures splitting

	// Split the bundle
	splitResult, err := services.SplitBundle(bundle, thresholdBytes)
	require.NoError(t, err)
	require.True(t, splitResult.WasSplit, "Bundle should be split for this test")
	require.Greater(t, len(splitResult.Chunks), 1, "Should have multiple chunks")

	// Convert chunks to FHIR bundles (simulating DIMP processing)
	pseudonymizedChunks := make([]map[string]any, len(splitResult.Chunks))
	for i, chunk := range splitResult.Chunks {
		chunkBundle := models.ConvertChunkToBundle(chunk)

		// Verify the chunk bundle has proper structure
		require.NotNil(t, chunkBundle["entry"], "Chunk should have entry field")

		// Simulate DIMP adding metadata
		chunkBundle["meta"] = map[string]any{
			"security": []map[string]any{
				{"system": "http://example.com/security", "code": "pseudonymized"},
			},
		}
		pseudonymizedChunks[i] = chunkBundle
	}

	// Reassemble
	reassembled, err := services.ReassembleBundle(splitResult.Metadata, pseudonymizedChunks)
	require.NoError(t, err)

	// Verify metadata preservation
	assert.Equal(t, splitResult.Metadata.Type, reassembled.Bundle["type"])
	assert.NotNil(t, reassembled.Bundle["meta"]) // DIMP metadata preserved
	assert.Equal(t, 100, reassembled.EntryCount)

	// Verify type-specific fields
	if splitResult.Metadata.Type == "collection" || splitResult.Metadata.Type == "document" {
		_, hasTotal := reassembled.Bundle["total"]
		assert.False(t, hasTotal, "collection/document bundles should not have 'total' field")
	}
}

// TestReassembleBundle_TypeSearchset tests correct handling of searchset bundles
func TestReassembleBundle_TypeSearchset(t *testing.T) {
	metadata := models.BundleMetadata{
		ID:   "searchset-bundle",
		Type: "searchset", // Searchset type requires 'total' field
	}

	chunks := []map[string]any{
		{
			"resourceType": "Bundle",
			"id":           "chunk-0",
			"type":         "collection",
			"entry": []any{
				map[string]any{
					"fullUrl": "urn:uuid:pat-1",
					"resource": map[string]any{
						"resourceType": "Patient",
						"id":           "pat-1",
					},
				},
			},
		},
		{
			"resourceType": "Bundle",
			"id":           "chunk-1",
			"type":         "collection",
			"entry": []any{
				map[string]any{
					"fullUrl": "urn:uuid:pat-2",
					"resource": map[string]any{
						"resourceType": "Patient",
						"id":           "pat-2",
					},
				},
			},
		},
	}

	reassembled, err := services.ReassembleBundle(metadata, chunks)
	require.NoError(t, err)

	// Verify searchset has 'total' field
	assert.Equal(t, "searchset", reassembled.Bundle["type"])
	total, hasTotal := reassembled.Bundle["total"]
	assert.True(t, hasTotal, "searchset bundles must have 'total' field")
	assert.Equal(t, 2, total)
}

// TestReassembleBundle_TypeHistory tests correct handling of history bundles
func TestReassembleBundle_TypeHistory(t *testing.T) {
	metadata := models.BundleMetadata{
		ID:   "history-bundle",
		Type: "history", // History type requires 'total' field
	}

	chunks := []map[string]any{
		{
			"resourceType": "Bundle",
			"id":           "chunk-0",
			"type":         "collection",
			"entry": []any{
				map[string]any{
					"fullUrl": "urn:uuid:pat-1",
					"resource": map[string]any{
						"resourceType": "Patient",
						"id":           "pat-1",
					},
				},
			},
		},
	}

	reassembled, err := services.ReassembleBundle(metadata, chunks)
	require.NoError(t, err)

	// Verify history has 'total' field
	assert.Equal(t, "history", reassembled.Bundle["type"])
	total, hasTotal := reassembled.Bundle["total"]
	assert.True(t, hasTotal, "history bundles must have 'total' field")
	assert.Equal(t, 1, total)
}

// TestSplitBundle_SmallBundleNoSplit tests when bundle is below threshold (no splitting needed)
// This covers the no-split path in SplitBundle (lines 196-215)
func TestSplitBundle_SmallBundleNoSplit(t *testing.T) {
	// Create a small bundle (2 entries, ~2KB each = ~4KB total)
	bundle := map[string]any{
		"resourceType": "Bundle",
		"id":           "small-bundle",
		"type":         "collection",
		"entry": []any{
			map[string]any{
				"fullUrl": "urn:uuid:pat-1",
				"resource": map[string]any{
					"resourceType": "Patient",
					"id":           "pat-1",
					"name": []map[string]any{
						{"use": "official", "given": []string{"John"}, "family": "Doe"},
					},
				},
			},
			map[string]any{
				"fullUrl": "urn:uuid:pat-2",
				"resource": map[string]any{
					"resourceType": "Patient",
					"id":           "pat-2",
					"name": []map[string]any{
						{"use": "official", "given": []string{"Jane"}, "family": "Smith"},
					},
				},
			},
		},
	}

	// Large threshold ensures no splitting
	thresholdBytes := 100 * 1024 * 1024 // 100MB

	result, err := services.SplitBundle(bundle, thresholdBytes)

	// Verify no error
	require.NoError(t, err, "Should succeed for small bundle")

	// Verify not split
	assert.False(t, result.WasSplit, "Small bundle should not be split")
	assert.Equal(t, 1, result.TotalChunks, "Should have exactly 1 chunk")
	assert.Len(t, result.Chunks, 1, "Should have 1 chunk in result")

	// Verify chunk contains all entries
	assert.Equal(t, 2, len(result.Chunks[0].Entries), "Chunk should contain all 2 entries")

	// Verify metadata is preserved
	assert.Equal(t, "small-bundle", result.Metadata.ID)
	assert.Equal(t, "collection", result.Metadata.Type)
}

// TestSplitBundle_BundleExactlyAtThreshold tests when bundle size equals threshold (no split)
func TestSplitBundle_BundleExactlyAtThreshold(t *testing.T) {
	bundle := CreateTestBundle(5, 50) // Creates ~250KB bundle

	// Get exact size to set threshold equal to it
	bundleSize, err := models.CalculateJSONSize(bundle)
	require.NoError(t, err)

	// Set threshold exactly equal to bundle size (should not split, uses > comparison)
	thresholdBytes := bundleSize

	result, err := services.SplitBundle(bundle, thresholdBytes)

	require.NoError(t, err)
	assert.False(t, result.WasSplit, "Bundle at threshold should not split (uses > comparison)")
	assert.Equal(t, 1, result.TotalChunks)
}

// TestPartitionEntries_SingleEntry tests error path with single small entry
func TestPartitionEntries_SingleEntry(t *testing.T) {
	entries := []map[string]any{
		{
			"fullUrl": "urn:uuid:pat-1",
			"resource": map[string]any{
				"resourceType": "Patient",
				"id":           "pat-1",
				"name": []map[string]any{
					{"family": "Doe"},
				},
			},
		},
	}

	thresholdBytes := 10 * 1024 * 1024

	partitions, err := services.PartitionEntries(entries, thresholdBytes)

	// Should succeed
	require.NoError(t, err)
	assert.Len(t, partitions, 1, "Should create 1 partition")
	assert.Len(t, partitions[0], 1, "Partition should have 1 entry")
}

// TestPartitionEntries_EntryAtBoundary tests entry that's close to threshold
func TestPartitionEntries_EntryAtBoundary(t *testing.T) {
	// Create a medium-sized entry (~5MB)
	largeEntry := CreateTestBundle(50, 100)

	entries := []map[string]any{
		{
			"fullUrl": "urn:uuid:obs-1",
			"resource": map[string]any{
				"resourceType": "Observation",
				"id":           "obs-1",
				"value":        largeEntry, // Embed large data
			},
		},
		{
			"fullUrl": "urn:uuid:pat-1",
			"resource": map[string]any{
				"resourceType": "Patient",
				"id":           "pat-1",
			},
		},
	}

	thresholdBytes := 10 * 1024 * 1024 // 10MB - should fit first entry with second

	partitions, err := services.PartitionEntries(entries, thresholdBytes)

	require.NoError(t, err)
	assert.Len(t, partitions, 1, "Both entries should fit in single partition under 10MB threshold")
	assert.Len(t, partitions[0], 2, "Should have both entries in partition")
}

// TestPartitionEntries_MultiplePartitions tests greedy algorithm creating multiple partitions
func TestPartitionEntries_MultiplePartitions(t *testing.T) {
	// Create 5 medium entries, each ~2MB
	entries := []map[string]any{}
	for i := 0; i < 5; i++ {
		bundle := CreateTestBundle(20, 100)
		entries = append(entries, map[string]any{
			"fullUrl": fmt.Sprintf("urn:uuid:obs-%d", i),
			"resource": map[string]any{
				"resourceType": "Observation",
				"id":           fmt.Sprintf("obs-%d", i),
				"note":         bundle,
			},
		})
	}

	thresholdBytes := 5 * 1024 * 1024 // 5MB - should split entries into multiple partitions

	partitions, err := services.PartitionEntries(entries, thresholdBytes)

	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(partitions), 2, "Should create at least 2 partitions")

	// Verify all entries are preserved
	totalEntries := 0
	for _, partition := range partitions {
		totalEntries += len(partition)
		assert.Greater(t, len(partition), 0, "Each partition should have at least 1 entry")
	}
	assert.Equal(t, 5, totalEntries, "All entries should be preserved")
}

// TestCalculateChunkStats_ConsistentStats tests stats calculation consistency
func TestCalculateChunkStats_ConsistentStats(t *testing.T) {
	bundle := CreateTestBundle(50, 100) // ~5MB bundle
	thresholdBytes := 2 * 1024 * 1024   // 2MB threshold

	splitResult, err := services.SplitBundle(bundle, thresholdBytes)
	require.NoError(t, err)

	stats := services.CalculateChunkStats(splitResult)

	// Verify stats consistency
	assert.Equal(t, splitResult.Metadata.ID, stats.BundleID)
	assert.Equal(t, splitResult.OriginalSize, stats.OriginalSize)
	assert.Equal(t, len(splitResult.Chunks), stats.ChunksCreated)

	// Verify chunk sizes are logical
	if len(splitResult.Chunks) > 0 {
		totalChunkSize := 0
		for _, chunk := range splitResult.Chunks {
			totalChunkSize += chunk.EstimatedSize
		}
		assert.Greater(t, totalChunkSize, 0, "Total chunk size should be > 0")
	}
}
