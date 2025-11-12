// Package services provides business logic for the aether DIMP pipeline
//
// Bundle Splitting Functionality:
// This package contains pure functions for splitting large FHIR Bundles to prevent
// HTTP 413 "Payload Too Large" errors when sending to the DIMP pseudonymization service.
//
// Design Principles:
//   - Pure Functions: All public functions take inputs and return outputs without side effects
//   - Immutability: Input data structures are never mutated; new structures are created
//   - Simplicity: Greedy partitioning algorithm (KISS principle) over complex optimization
//   - Order Preservation: Bundle entry order is guaranteed to be preserved during splitting and reassembly
//
// Splitting Algorithm (Greedy Partitioning):
//  1. Calculate total Bundle size using JSON marshaling
//  2. If size <= threshold: return unchanged (no splitting needed)
//  3. Otherwise, partition entries greedily:
//     - Iterate through entries in order
//     - Accumulate entries in current chunk while under threshold
//     - When adding next entry would exceed threshold, start new chunk
//     - Continue until all entries are partitioned
//  4. Create chunks as valid FHIR R4 Bundles with metadata from original
//
// Performance Characteristics:
//   - Size Calculation: O(n) where n = Bundle size in bytes (JSON marshal)
//   - Partitioning: O(m) where m = number of entries (single pass greedy scan)
//   - Reassembly: O(m) concatenation of entry arrays
//   - Memory: O(Bundle size) - all data structures fit in memory for chunks <10MB
//
// Example Usage:
//
//	// Check if splitting needed
//	size, _ := models.CalculateJSONSize(bundle)
//	if ShouldSplit(size, thresholdBytes) {
//	    // Split Bundle
//	    result, _ := SplitBundle(bundle, thresholdBytes)
//
//	    // Process each chunk
//	    for _, chunk := range result.Chunks {
//	        pseudonymized := callDIMPService(models.ConvertChunkToBundle(chunk))
//	        // Collect pseudonymized chunk...
//	    }
//
//	    // Reassemble chunks
//	    final, _ := ReassembleBundle(result.Metadata, pseudonymizedChunks)
//	}
package services

import (
	"fmt"

	"github.com/medizininformatik-initiative/aether/internal/models"
)

// ShouldSplit determines if a Bundle exceeds the threshold and requires splitting
// Pure function: Takes bundle size and threshold, returns boolean decision
//
// Parameters:
//
//	bundleSizeBytes - The serialized size of the Bundle in bytes
//	thresholdBytes - The configured split threshold in bytes
//
// Returns:
//
//	true if Bundle should be split (size > threshold), false otherwise
//
// WHY: Provides clear decision point for splitting logic, avoiding redundant size checks
func ShouldSplit(bundleSizeBytes int, thresholdBytes int) bool {
	return bundleSizeBytes > thresholdBytes
}

// PartitionEntries splits Bundle entries into chunks using greedy partitioning algorithm
// Pure function: Takes entries array and threshold, returns partitioned entry arrays
//
// Algorithm: Greedy array partitioning (from research.md)
//   - Iterate through entries in order
//   - Accumulate entries in current chunk while under threshold
//   - When adding next entry would exceed threshold, start new chunk
//   - Preserve entry order naturally (no sorting or reordering)
//
// Parameters:
//
//	entries - Array of Bundle entry objects (must not be empty)
//	thresholdBytes - Maximum size per chunk in bytes
//
// Returns:
//
//	Array of entry partitions (each partition is an array of entries)
//	Error if any entry exceeds threshold (cannot split single entry)
//
// WHY: Maximizes chunk sizes (fewer HTTP requests) while respecting threshold
// WHY: Simple greedy algorithm over complex bin-packing (KISS principle)
func PartitionEntries(entries []map[string]any, thresholdBytes int) ([][]map[string]any, error) {
	if len(entries) == 0 {
		return nil, fmt.Errorf("cannot partition empty entry array")
	}

	var partitions [][]map[string]any
	currentPartition := []map[string]any{}
	currentSize := 0

	// Bundle wrapper overhead (approximate fixed cost per chunk)
	// Includes: resourceType, id, type, timestamp, total fields
	const bundleOverheadBytes = 200

	for i, entry := range entries {
		// Calculate size of this entry
		entrySize, err := models.CalculateJSONSize(entry)
		if err != nil {
			return nil, fmt.Errorf("failed to calculate size of entry %d: %w", i, err)
		}

		// Check if single entry exceeds threshold (cannot be split)
		if entrySize+bundleOverheadBytes > thresholdBytes {
			// Extract resource info for error message
			resourceType := "Unknown"
			resourceID := "unknown"

			if resource, ok := entry["resource"].(map[string]any); ok {
				if rt, ok := resource["resourceType"].(string); ok {
					resourceType = rt
				}
				if id, ok := resource["id"].(string); ok {
					resourceID = id
				}
			}

			guidance := fmt.Sprintf(
				"This entry contains a %s resource that cannot be split. "+
					"Solutions: (1) Review data quality - resource may contain unnecessary data; "+
					"(2) Increase DIMP server payload limit; (3) Increase bundle_split_threshold_mb configuration.",
				resourceType,
			)

			return nil, &models.OversizedResourceError{
				ResourceType: resourceType,
				ResourceID:   resourceID,
				Size:         entrySize,
				Threshold:    thresholdBytes,
				Guidance:     guidance,
			}
		}

		// Check if adding this entry would exceed threshold
		if len(currentPartition) > 0 && currentSize+entrySize+bundleOverheadBytes > thresholdBytes {
			// Current chunk would exceed limit - start new chunk
			partitions = append(partitions, currentPartition)
			currentPartition = []map[string]any{}
			currentSize = 0
		}

		// Add entry to current partition
		currentPartition = append(currentPartition, entry)
		currentSize += entrySize
	}

	// Don't forget last partition
	if len(currentPartition) > 0 {
		partitions = append(partitions, currentPartition)
	}

	return partitions, nil
}

// SplitBundle splits a large FHIR Bundle into smaller chunks for processing
// Pure function: Takes Bundle and threshold, returns SplitResult (no side effects)
//
// Process:
//  1. Calculate Bundle size
//  2. Check if splitting needed (size > threshold)
//  3. If not needed, return single-chunk result
//  4. If needed, extract metadata and entries
//  5. Partition entries using greedy algorithm
//  6. Create Bundle chunks from partitions
//  7. Return complete SplitResult
//
// Parameters:
//
//	bundle - FHIR Bundle as JSON object (must have id, type, entry fields)
//	thresholdBytes - Split threshold in bytes (typically 10MB = 10*1024*1024)
//
// Returns:
//
//	SplitResult containing metadata, chunks, and statistics
//	Error if Bundle is invalid or splitting fails
//
// WHY: Central splitting logic - encapsulates entire split operation as pure function
// WHY: Returns immutable result structure (functional programming principle)
func SplitBundle(bundle map[string]any, thresholdBytes int) (models.SplitResult, error) {
	// Extract metadata first (validates Bundle structure)
	metadata, err := models.ExtractBundleMetadata(bundle)
	if err != nil {
		return models.SplitResult{}, fmt.Errorf("invalid Bundle structure: %w", err)
	}

	// Calculate Bundle size
	bundleSize, err := models.CalculateJSONSize(bundle)
	if err != nil {
		return models.SplitResult{}, fmt.Errorf("failed to calculate Bundle size: %w", err)
	}

	// Check if splitting needed
	if !ShouldSplit(bundleSize, thresholdBytes) {
		// Bundle is small enough - return as single chunk
		entries, err := models.ExtractEntriesFromBundle(bundle)
		if err != nil {
			return models.SplitResult{}, fmt.Errorf("failed to extract entries: %w", err)
		}

		chunk, err := models.CreateBundleChunk(metadata, entries, 0, 1)
		if err != nil {
			return models.SplitResult{}, fmt.Errorf("failed to create chunk: %w", err)
		}

		return models.SplitResult{
			Metadata:     metadata,
			Chunks:       []models.BundleChunk{chunk},
			WasSplit:     false,
			OriginalSize: bundleSize,
			TotalChunks:  1,
		}, nil
	}

	// Bundle needs splitting - extract entries
	entries, err := models.ExtractEntriesFromBundle(bundle)
	if err != nil {
		return models.SplitResult{}, fmt.Errorf("failed to extract entries: %w", err)
	}

	// Partition entries using greedy algorithm
	partitions, err := PartitionEntries(entries, thresholdBytes)
	if err != nil {
		return models.SplitResult{}, fmt.Errorf("failed to partition entries: %w", err)
	}

	// Create chunks from partitions
	totalChunks := len(partitions)
	chunks := make([]models.BundleChunk, 0, totalChunks)

	for i, partition := range partitions {
		chunk, err := models.CreateBundleChunk(metadata, partition, i, totalChunks)
		if err != nil {
			return models.SplitResult{}, fmt.Errorf("failed to create chunk %d: %w", i, err)
		}
		chunks = append(chunks, chunk)
	}

	return models.SplitResult{
		Metadata:     metadata,
		Chunks:       chunks,
		WasSplit:     true,
		OriginalSize: bundleSize,
		TotalChunks:  totalChunks,
	}, nil
}

// ReassembleBundle combines pseudonymized Bundle chunks into single Bundle
// Pure function: Takes metadata and pseudonymized chunks, returns reassembled Bundle
//
// Process:
//  1. Create new Bundle with original metadata (id, type, timestamp)
//  2. Concatenate entries from all chunks in order
//  3. Set total to final entry count
//  4. Return complete Bundle as JSON object
//
// Parameters:
//
//	metadata - Original Bundle metadata (from SplitResult)
//	pseudonymizedChunks - Array of pseudonymized Bundle JSON objects (in order)
//
// Returns:
//
//	ReassembledBundle containing complete Bundle and statistics
//	Error if chunks are invalid or reassembly fails
//
// WHY: Restores original Bundle structure after chunk-by-chunk pseudonymization
// WHY: Maintains data integrity through deterministic concatenation
func ReassembleBundle(metadata models.BundleMetadata, pseudonymizedChunks []map[string]any) (models.ReassembledBundle, error) {
	if len(pseudonymizedChunks) == 0 {
		return models.ReassembledBundle{}, fmt.Errorf("cannot reassemble from empty chunk array")
	}

	// Start with the first chunk as the base Bundle (preserves pseudonymized Bundle-level metadata)
	// WHY: DIMP adds Bundle-level metadata (meta.security tags, pseudonymized ID) which must be preserved
	firstChunk := pseudonymizedChunks[0]

	// Validate first chunk structure
	if resourceType, ok := firstChunk["resourceType"].(string); !ok || resourceType != "Bundle" {
		return models.ReassembledBundle{}, fmt.Errorf("first chunk is not a Bundle")
	}

	// Start with first chunk as base - this preserves all Bundle-level fields (id, meta, etc.)
	reassembledBundle := make(map[string]any)
	for k, v := range firstChunk {
		reassembledBundle[k] = v
	}

	// Extract entries from first chunk
	firstChunkEntries, err := models.ExtractEntriesFromBundle(firstChunk)
	if err != nil {
		return models.ReassembledBundle{}, fmt.Errorf("failed to extract entries from first chunk: %w", err)
	}
	allEntries := firstChunkEntries

	// Collect entries from remaining chunks
	for i := 1; i < len(pseudonymizedChunks); i++ {
		chunk := pseudonymizedChunks[i]

		// Validate chunk structure
		if resourceType, ok := chunk["resourceType"].(string); !ok || resourceType != "Bundle" {
			return models.ReassembledBundle{}, fmt.Errorf("chunk %d is not a Bundle", i)
		}

		// Extract entries from chunk
		chunkEntries, err := models.ExtractEntriesFromBundle(chunk)
		if err != nil {
			return models.ReassembledBundle{}, fmt.Errorf("failed to extract entries from chunk %d: %w", i, err)
		}

		// Concatenate to master list
		allEntries = append(allEntries, chunkEntries...)
	}

	// Replace entries array with complete reassembled entries
	reassembledBundle["entry"] = allEntries

	// Update Bundle type to match original (chunks use "collection", but original might be different)
	reassembledBundle["type"] = metadata.Type

	// Add timestamp if present in original (may have been added by DIMP, preserve if present)
	if !metadata.Timestamp.IsZero() {
		reassembledBundle["timestamp"] = metadata.Timestamp.Format("2006-01-02T15:04:05Z07:00") // RFC3339
	}

	// Add total field ONLY for searchset and history bundles (FHIR R4 invariant: "total only when a search or history")
	// For collection/document bundles, the total field must NOT be present
	if metadata.Type == "searchset" || metadata.Type == "history" {
		reassembledBundle["total"] = len(allEntries)
	} else {
		// Remove total field if present for non-searchset/history bundles
		delete(reassembledBundle, "total")
	}

	// Extract the pseudonymized ID from the reassembled Bundle (which came from first chunk)
	pseudonymizedID, _ := reassembledBundle["id"].(string)

	return models.ReassembledBundle{
		Bundle:         reassembledBundle,
		EntryCount:     len(allEntries),
		OriginalID:     pseudonymizedID,              // Use pseudonymized ID from first chunk (not original metadata)
		WasReassembled: len(pseudonymizedChunks) > 1, // Only true if actually split
	}, nil
}

// CalculateChunkStats computes statistics about Bundle splitting operation
// Pure function: Takes SplitResult and returns statistics for logging/monitoring
//
// Parameters:
//
//	result - SplitResult from SplitBundle operation
//
// Returns:
//
//	SplitStats containing metrics about the split operation
//
// WHY: Provides observability data for monitoring and debugging
// WHY: Helps users understand splitting behavior and performance
func CalculateChunkStats(result models.SplitResult) models.SplitStats {
	stats := models.SplitStats{
		BundleID:        result.Metadata.ID,
		OriginalSize:    result.OriginalSize,
		OriginalEntries: 0, // Calculate from chunks
		ChunksCreated:   result.TotalChunks,
	}

	if len(result.Chunks) == 0 {
		return stats
	}

	// Calculate statistics from chunks
	totalEntries := 0
	minSize := result.Chunks[0].EstimatedSize
	maxSize := result.Chunks[0].EstimatedSize
	totalSize := 0

	for _, chunk := range result.Chunks {
		entryCount := len(chunk.Entries)
		totalEntries += entryCount

		size := chunk.EstimatedSize
		totalSize += size

		if size < minSize {
			minSize = size
		}
		if size > maxSize {
			maxSize = size
		}
	}

	stats.OriginalEntries = totalEntries
	stats.SmallestChunkSize = minSize
	stats.LargestChunkSize = maxSize

	if result.TotalChunks > 0 {
		stats.AverageChunkSize = totalSize / result.TotalChunks
	}

	return stats
}
