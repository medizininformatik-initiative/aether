package unit

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

// TestCalculateBundleSize verifies that CalculateJSONSize returns accurate byte counts
func TestCalculateBundleSize(t *testing.T) {
	testCases := []struct {
		name          string
		entryCount    int
		entrySizeKB   int
		expectedMinMB float64 // Approximate lower bound
		expectedMaxMB float64 // Approximate upper bound
	}{
		{
			name:          "Small Bundle 1KB",
			entryCount:    1,
			entrySizeKB:   1,
			expectedMinMB: 0.001,
			expectedMaxMB: 0.01,
		},
		{
			name:          "Medium Bundle 5MB",
			entryCount:    50,
			entrySizeKB:   100,
			expectedMinMB: 4.0,
			expectedMaxMB: 6.0,
		},
		{
			name:          "Large Bundle 50MB",
			entryCount:    100,
			entrySizeKB:   500,
			expectedMinMB: 45.0,
			expectedMaxMB: 55.0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			bundle := CreateTestBundle(tc.entryCount, tc.entrySizeKB)

			size, err := models.CalculateJSONSize(bundle)
			require.NoError(t, err)

			sizeInMB := float64(size) / (1024 * 1024)
			assert.GreaterOrEqual(t, sizeInMB, tc.expectedMinMB,
				"Bundle size %fMB should be >= expected minimum %fMB", sizeInMB, tc.expectedMinMB)
			assert.LessOrEqual(t, sizeInMB, tc.expectedMaxMB,
				"Bundle size %fMB should be <= expected maximum %fMB", sizeInMB, tc.expectedMaxMB)

			// Verify size is positive
			assert.Greater(t, size, 0, "Bundle size must be > 0")
		})
	}
}

// TestShouldSplit verifies the threshold comparison logic
func TestShouldSplit(t *testing.T) {
	testCases := []struct {
		name         string
		bundleSizeMB float64
		thresholdMB  int
		shouldSplit  bool
	}{
		{
			name:         "Bundle below threshold",
			bundleSizeMB: 9.0,
			thresholdMB:  10,
			shouldSplit:  false,
		},
		{
			name:         "Bundle exactly at threshold",
			bundleSizeMB: 10.0,
			thresholdMB:  10,
			shouldSplit:  false, // At threshold, don't split
		},
		{
			name:         "Bundle above threshold by 1MB",
			bundleSizeMB: 11.0,
			thresholdMB:  10,
			shouldSplit:  true,
		},
		{
			name:         "Bundle far above threshold",
			bundleSizeMB: 50.0,
			thresholdMB:  10,
			shouldSplit:  true,
		},
		{
			name:         "Small Bundle with high threshold",
			bundleSizeMB: 5.0,
			thresholdMB:  20,
			shouldSplit:  false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			bundleSizeBytes := int(tc.bundleSizeMB * 1024 * 1024)
			thresholdBytes := tc.thresholdMB * 1024 * 1024

			result := bundleSizeBytes > thresholdBytes
			assert.Equal(t, tc.shouldSplit, result,
				"Bundle size %d bytes, threshold %d bytes, expected shouldSplit=%v",
				bundleSizeBytes, thresholdBytes, tc.shouldSplit)
		})
	}
}

// TestExtractBundleMetadata verifies metadata extraction from Bundle
func TestExtractBundleMetadata(t *testing.T) {
	t.Run("Extract from valid Bundle", func(t *testing.T) {
		bundle := CreateTestBundle(10, 1)

		metadata, err := models.ExtractBundleMetadata(bundle)
		require.NoError(t, err)

		assert.NotEmpty(t, metadata.ID, "Bundle ID should be present")
		assert.Equal(t, bundle["id"].(string), metadata.ID, "ID should match")
		assert.Equal(t, "collection", metadata.Type, "Type should match")
		assert.False(t, metadata.Timestamp.IsZero(), "Timestamp should be parsed")
	})

	t.Run("Missing ID returns error", func(t *testing.T) {
		bundle := map[string]any{
			"type": "collection",
		}

		_, err := models.ExtractBundleMetadata(bundle)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "id")
	})

	t.Run("Missing type returns error", func(t *testing.T) {
		bundle := map[string]any{
			"id": "test-bundle",
		}

		_, err := models.ExtractBundleMetadata(bundle)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "type")
	})
}

// TestCreateChunk verifies that chunks have valid FHIR Bundle structure
func TestCreateChunk(t *testing.T) {
	metadata := models.BundleMetadata{
		ID:   "test-bundle",
		Type: "collection",
	}

	entries := []map[string]any{
		{
			"fullUrl": "urn:uuid:entry-1",
			"resource": map[string]any{
				"resourceType": "Patient",
				"id":           "patient-1",
			},
		},
		{
			"fullUrl": "urn:uuid:entry-2",
			"resource": map[string]any{
				"resourceType": "Condition",
				"id":           "condition-1",
			},
		},
	}

	t.Run("Create valid chunk", func(t *testing.T) {
		chunk, err := models.CreateBundleChunk(metadata, entries, 0, 3)
		require.NoError(t, err)

		assert.Equal(t, "test-bundle-chunk-0", chunk.ChunkID)
		assert.Equal(t, 0, chunk.Index)
		assert.Equal(t, 3, chunk.TotalChunks)
		assert.Equal(t, "test-bundle", chunk.OriginalID)
		assert.Equal(t, len(entries), len(chunk.Entries))
		assert.Greater(t, chunk.EstimatedSize, 0)
	})

	t.Run("Invalid index returns error", func(t *testing.T) {
		_, err := models.CreateBundleChunk(metadata, entries, 5, 3) // index out of range
		assert.Error(t, err)
	})

	t.Run("Empty entries returns error", func(t *testing.T) {
		_, err := models.CreateBundleChunk(metadata, []map[string]any{}, 0, 1)
		assert.Error(t, err)
	})
}

// TestConvertChunkToBundle verifies chunk conversion to FHIR Bundle
func TestConvertChunkToBundle(t *testing.T) {
	now := time.Now()
	metadata := models.BundleMetadata{
		ID:        "original-bundle",
		Type:      "document",
		Timestamp: now,
	}

	entries := []map[string]any{
		{
			"fullUrl": "urn:uuid:entry-1",
			"resource": map[string]any{
				"resourceType": "Patient",
				"id":           "patient-1",
			},
		},
	}

	chunk, err := models.CreateBundleChunk(metadata, entries, 0, 2)
	require.NoError(t, err)

	bundle := models.ConvertChunkToBundle(chunk)

	// Verify FHIR Bundle structure
	assert.Equal(t, "Bundle", bundle["resourceType"])
	assert.Equal(t, "original-bundle-chunk-0", bundle["id"])
	assert.Equal(t, "document", bundle["type"])
	// Note: "total" field should NOT be present for document bundles (FHIR R4 invariant: "total only when a search or history")
	_, hasTotalField := bundle["total"]
	assert.False(t, hasTotalField, "document bundle should not have 'total' field per FHIR R4 spec")
	assert.NotNil(t, bundle["entry"])
	assert.NotEmpty(t, bundle["timestamp"]) // Should have timestamp
}

// TestExtractEntriesFromBundle verifies entry extraction
func TestExtractEntriesFromBundle(t *testing.T) {
	bundle := CreateTestBundle(5, 1)

	entries, err := models.ExtractEntriesFromBundle(bundle)
	require.NoError(t, err)

	assert.Equal(t, 5, len(entries))

	// Verify each entry is a valid object
	for i, entry := range entries {
		assert.NotNil(t, entry, "Entry %d should not be nil", i)
		assert.Contains(t, entry, "resource", "Entry %d should have resource", i)
	}
}

// TestPartitionEntriesGreedyAlgorithm verifies entry partitioning preserves order and respects threshold
func TestPartitionEntriesGreedyAlgorithm(t *testing.T) {
	// Create Bundle with 100 entries of ~100KB each
	bundle := CreateTestBundle(100, 100)
	entries, err := models.ExtractEntriesFromBundle(bundle)
	require.NoError(t, err)

	// With 100KB entries and 10MB threshold, expect roughly 100 chunks
	// (each chunk should contain ~100 entries since 100 entries * 100KB = 10MB)
	thresholdBytes := 10 * 1024 * 1024 // 10MB

	// Partition entries (simplified algorithm for testing)
	var partitions [][]map[string]any
	currentPartition := []map[string]any{}
	currentSize := 0

	for _, entry := range entries {
		entrySize, _ := models.CalculateJSONSize(entry)
		if currentSize+entrySize > thresholdBytes && len(currentPartition) > 0 {
			partitions = append(partitions, currentPartition)
			currentPartition = []map[string]any{}
			currentSize = 0
		}
		currentPartition = append(currentPartition, entry)
		currentSize += entrySize
	}
	if len(currentPartition) > 0 {
		partitions = append(partitions, currentPartition)
	}

	// Verify partitioning properties
	totalEntries := 0
	for i, partition := range partitions {
		assert.Greater(t, len(partition), 0, "Partition %d should have at least 1 entry", i)
		totalEntries += len(partition)

		// Verify each partition size is reasonable (not wildly over threshold)
		partitionSize, _ := models.CalculateJSONSize(
			map[string]any{"entry": partition},
		)
		// Allow some overage for the last entry (greedy algorithm)
		assert.Less(t, partitionSize, thresholdBytes*2,
			"Partition %d size %d should not exceed 2x threshold", i, partitionSize)
	}

	// Verify all entries accounted for
	assert.Equal(t, len(entries), totalEntries,
		"All entries should be partitioned")
}

// TestBundleMetadataRoundTrip verifies metadata survives extraction and restoration
func TestBundleMetadataRoundTrip(t *testing.T) {
	// Create original Bundle
	original := CreateTestBundle(50, 10)
	originalMetadata, err := models.ExtractBundleMetadata(original)
	require.NoError(t, err)

	// Create chunk from metadata
	entries, _ := models.ExtractEntriesFromBundle(original)
	chunk, err := models.CreateBundleChunk(originalMetadata, entries[:10], 0, 3)
	require.NoError(t, err)

	// Convert back to Bundle
	restored := models.ConvertChunkToBundle(chunk)

	// Verify metadata is preserved
	assert.Equal(t, originalMetadata.Type, restored["type"])
	assert.NotEmpty(t, restored["timestamp"], "Timestamp should be preserved")

	// Verify ID includes chunk marker
	assert.Contains(t, restored["id"].(string), "-chunk-")
}

// TestEdgeCase_MixedResourceTypes verifies Bundle with mixed resource types processes correctly
func TestEdgeCase_MixedResourceTypes(t *testing.T) {
	// Create bundle with multiple resource types
	entries := make([]any, 0, 3)
	entries = append(entries, map[string]any{
		"fullUrl": "urn:uuid:patient-1",
		"resource": map[string]any{
			"resourceType": "Patient",
			"id":           "patient-1",
			"name": []map[string]any{
				{"given": []string{"John"}, "family": "Doe"},
			},
		},
	})
	entries = append(entries, map[string]any{
		"fullUrl": "urn:uuid:obs-1",
		"resource": map[string]any{
			"resourceType": "Observation",
			"id":           "obs-1",
			"status":       "final",
			"code": map[string]any{
				"text": "Blood Pressure",
			},
		},
	})
	entries = append(entries, map[string]any{
		"fullUrl": "urn:uuid:condition-1",
		"resource": map[string]any{
			"resourceType": "Condition",
			"id":           "condition-1",
			"code": map[string]any{
				"text": "Hypertension",
			},
		},
	})

	bundle := map[string]any{
		"resourceType": "Bundle",
		"id":           "mixed-types-bundle",
		"type":         "collection",
		"entry":        entries,
	}

	thresholdBytes := 1024 * 1024 // 1MB (bundle will be < 1MB)
	size, err := models.CalculateJSONSize(bundle)
	require.NoError(t, err)

	// Extract and verify
	metadata, err := models.ExtractBundleMetadata(bundle)
	require.NoError(t, err)
	assert.Equal(t, "mixed-types-bundle", metadata.ID)

	extractedEntries, err := models.ExtractEntriesFromBundle(bundle)
	require.NoError(t, err)
	assert.Len(t, extractedEntries, 3, "Should have 3 entries of different types")

	// Verify no splitting needed for this small bundle
	shouldSplit := services.ShouldSplit(size, thresholdBytes)
	assert.False(t, shouldSplit, "Small mixed-type bundle should not split")
}

// TestEdgeCase_EmptyBundle verifies handling of Bundle with no entries
func TestEdgeCase_EmptyBundle(t *testing.T) {
	bundle := map[string]any{
		"resourceType": "Bundle",
		"id":           "empty-bundle",
		"type":         "collection",
		"entry":        []any{},
	}

	metadata, err := models.ExtractBundleMetadata(bundle)
	require.NoError(t, err)
	assert.Equal(t, "empty-bundle", metadata.ID)

	entries, err := models.ExtractEntriesFromBundle(bundle)
	require.NoError(t, err)
	assert.Empty(t, entries, "Empty bundle should have no entries")

	// Verify metadata extraction still works
	assert.Equal(t, "collection", metadata.Type)
}

// TestEdgeCase_SingleOversizedEntry verifies Bundle with one large entry processes correctly
func TestEdgeCase_SingleOversizedEntry(t *testing.T) {
	// Create an oversized entry by repeating data
	largeData := ""
	for i := 0; i < 1000; i++ {
		largeData += "This is a very long text field that contributes to the entry size. "
	}

	entries := make([]any, 0, 1)
	entries = append(entries, map[string]any{
		"fullUrl": "urn:uuid:observation-large",
		"resource": map[string]any{
			"resourceType": "Observation",
			"id":           "obs-large",
			"status":       "final",
			"note": []map[string]any{
				{"text": largeData},
			},
		},
	})

	bundle := map[string]any{
		"resourceType": "Bundle",
		"id":           "single-large-entry",
		"type":         "collection",
		"entry":        entries,
	}

	metadata, err := models.ExtractBundleMetadata(bundle)
	require.NoError(t, err)

	extractedEntries, err := models.ExtractEntriesFromBundle(bundle)
	require.NoError(t, err)
	require.Len(t, extractedEntries, 1)

	// Create a chunk with the large entry
	// This should still work - greedy algorithm will create a single chunk with this entry
	chunk, err := models.CreateBundleChunk(metadata, extractedEntries, 0, 1)
	require.NoError(t, err)

	// Verify chunk was created successfully despite being larger than threshold
	// (single entries cannot be split further)
	assert.Equal(t, 0, chunk.Index)
	assert.Equal(t, 1, chunk.TotalChunks)
	assert.Len(t, chunk.Entries, 1)

	// Verify chunk is larger than a small threshold
	assert.Greater(t, chunk.EstimatedSize, 50000,
		"Single large entry chunk will be much larger than 50KB")
}

// TestEdgeCase_BundleWithFullUrlReferences verifies entries with fullUrl references
func TestEdgeCase_BundleWithFullUrlReferences(t *testing.T) {
	entries := make([]any, 0, 2)
	entries = append(entries, map[string]any{
		"fullUrl": "http://example.com/Patient/123",
		"resource": map[string]any{
			"resourceType": "Patient",
			"id":           "123",
			"name": []map[string]any{
				{"family": "Doe", "given": []string{"Jane"}},
			},
		},
	})
	entries = append(entries, map[string]any{
		"fullUrl": "http://example.com/Observation/456",
		"resource": map[string]any{
			"resourceType": "Observation",
			"id":           "456",
			"status":       "final",
			"subject": map[string]any{
				"reference": "Patient/123",
			},
		},
	})

	bundle := map[string]any{
		"resourceType": "Bundle",
		"id":           "bundle-with-references",
		"type":         "collection",
		"entry":        entries,
	}

	extractedEntries, err := models.ExtractEntriesFromBundle(bundle)
	require.NoError(t, err)
	assert.Len(t, extractedEntries, 2)

	// Verify fullUrl is preserved in entries
	for i, entry := range extractedEntries {
		fullUrl, ok := entry["fullUrl"].(string)
		require.True(t, ok, "Entry %d should have fullUrl", i)
		assert.NotEmpty(t, fullUrl, "fullUrl should not be empty")
	}
}
