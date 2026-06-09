package models

import (
	"encoding/json"
	"fmt"
	"time"
)

// BundleMetadata captures essential metadata from original Bundle for reassembly
// Immutable once created - used to restore Bundle structure after pseudonymization
type BundleMetadata struct {
	ID        string    // Original Bundle.id
	Type      string    // Bundle.type (document, collection, etc.)
	Timestamp time.Time // Bundle.timestamp (if present)
}

// BundleChunk represents one chunk of a split FHIR Bundle
// Contains subset of original entries plus metadata for tracking
type BundleChunk struct {
	ChunkID       string           // Unique identifier: "{originalID}-chunk-{index}"
	Index         int              // 0-based chunk index
	TotalChunks   int              // Total number of chunks in split operation
	OriginalID    string           // Original Bundle.id (for reassembly)
	Metadata      BundleMetadata   // Original Bundle metadata
	Entries       []map[string]any // Bundle entries (JSON objects)
	EstimatedSize int              // Estimated serialized size in bytes
}

// SplitResult encapsulates result of Bundle splitting operation (pure function output)
// Immutable result structure following functional programming principles
type SplitResult struct {
	Metadata     BundleMetadata // Original Bundle metadata
	Chunks       []BundleChunk  // Ordered list of Bundle chunks
	WasSplit     bool           // Whether splitting was necessary
	OriginalSize int            // Original Bundle size in bytes
	TotalChunks  int            // Number of chunks created (convenience field)
}

// ReassembledBundle represents the final Bundle after pseudonymization and reassembly
// Contains all pseudonymized entries in original order with restored metadata
type ReassembledBundle struct {
	Bundle         map[string]any // Complete FHIR Bundle (JSON object)
	EntryCount     int            // Total entries in reassembled Bundle
	OriginalID     string         // Original Bundle.id
	WasReassembled bool           // Whether Bundle was reassembled from chunks
}

// SplitStats captures metrics about Bundle splitting operation
// Used for logging and monitoring purposes
type SplitStats struct {
	BundleID          string
	OriginalSize      int
	OriginalEntries   int
	ChunksCreated     int
	AverageChunkSize  int
	LargestChunkSize  int
	SmallestChunkSize int
	SplitDuration     time.Duration
}

// OversizedResourceError indicates a single resource exceeds threshold
// Cannot be split without violating FHIR semantics
type OversizedResourceError struct {
	ResourceType string // FHIR resource type (Patient, Observation, Condition, etc.)
	ResourceID   string // Resource identifier
	Size         int    // Actual size in bytes
	Threshold    int    // Configured threshold in bytes
	Guidance     string // User-facing guidance message
}

// Error implements the error interface for OversizedResourceError
func (e *OversizedResourceError) Error() string {
	return fmt.Sprintf(
		"resource %s/%s (%d bytes) exceeds threshold (%d bytes). %s",
		e.ResourceType, e.ResourceID, e.Size, e.Threshold, e.Guidance,
	)
}

// CalculateJSONSize returns the serialized byte count of a JSON object
// This is used to determine if a Bundle exceeds the split threshold
func CalculateJSONSize(obj map[string]any) (int, error) {
	jsonBytes, err := json.Marshal(obj)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal JSON: %w", err)
	}
	return len(jsonBytes), nil
}

// CreateBundleChunk constructs a valid FHIR Bundle chunk from provided parameters
// All chunks created are value types, immutable once created
func CreateBundleChunk(metadata BundleMetadata, entries []map[string]any,
	index int, totalChunks int) (BundleChunk, error) {

	if index < 0 || index >= totalChunks {
		return BundleChunk{}, fmt.Errorf("chunk index %d out of range [0, %d)", index, totalChunks)
	}

	if len(entries) == 0 {
		return BundleChunk{}, fmt.Errorf("chunk must contain at least one entry")
	}

	chunkID := fmt.Sprintf("%s-chunk-%d", metadata.ID, index)

	// Calculate estimated size of chunk
	estimatedSize, err := CalculateJSONSize(map[string]any{
		"resourceType": "Bundle",
		"id":           chunkID,
		"type":         metadata.Type,
		"timestamp":    metadata.Timestamp.Format(time.RFC3339),
		"total":        len(entries),
		"entry":        entries,
	})
	if err != nil {
		return BundleChunk{}, fmt.Errorf("failed to calculate chunk size: %w", err)
	}

	chunk := BundleChunk{
		ChunkID:       chunkID,
		Index:         index,
		TotalChunks:   totalChunks,
		OriginalID:    metadata.ID,
		Metadata:      metadata,
		Entries:       entries,
		EstimatedSize: estimatedSize,
	}

	return chunk, nil
}

// ConvertChunkToBundle converts a BundleChunk into a valid FHIR Bundle JSON object
// suitable for sending to DIMP or other FHIR-compliant services
func ConvertChunkToBundle(chunk BundleChunk) map[string]any {
	// Convert []map[string]any to []any for FHIR Bundle entry field
	entries := make([]any, len(chunk.Entries))
	for i, entry := range chunk.Entries {
		entries[i] = entry
	}

	bundle := map[string]any{
		"resourceType": "Bundle",
		"id":           chunk.ChunkID,
		"type":         chunk.Metadata.Type,
		"entry":        entries,
	}

	// Add timestamp if present
	if !chunk.Metadata.Timestamp.IsZero() {
		bundle["timestamp"] = chunk.Metadata.Timestamp.Format(time.RFC3339)
	}

	// Add total field ONLY for searchset and history bundles (FHIR R4 invariant: "total only when a search or history")
	// For collection/document bundles, the total field must NOT be present
	bundleType := chunk.Metadata.Type
	if bundleType == "searchset" || bundleType == "history" {
		bundle["total"] = len(chunk.Entries)
	}

	return bundle
}
