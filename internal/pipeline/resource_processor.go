package pipeline

import (
	"fmt"
	"path/filepath"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

// ResourceProcessor handles pseudonymization of FHIR resources
// Encapsulates Bundle splitting logic and oversized resource detection
type ResourceProcessor struct {
	dimpClient         *services.DIMPClient
	logger             *lib.Logger
	thresholdBytes     int
	inputFile          string
	resourcesProcessed int
}

// NewResourceProcessor creates a new resource processor
func NewResourceProcessor(dimpClient *services.DIMPClient, logger *lib.Logger, thresholdBytes int, inputFile string) *ResourceProcessor {
	return &ResourceProcessor{
		dimpClient:         dimpClient,
		logger:             logger,
		thresholdBytes:     thresholdBytes,
		inputFile:          inputFile,
		resourcesProcessed: 0,
	}
}

// ProcessBundle handles Bundle resources with automatic splitting for large Bundles
func (rp *ResourceProcessor) ProcessBundle(resource map[string]any, resourceID string) (map[string]any, error) {
	// Calculate Bundle size
	bundleSize, err := models.CalculateJSONSize(resource)
	if err != nil {
		rp.logger.Error("Failed to calculate Bundle size",
			"file", filepath.Base(rp.inputFile),
			"line_number", rp.resourcesProcessed+1,
			"id", resourceID,
			"error", err)
		return nil, fmt.Errorf("failed to calculate Bundle size at line %d: %w", rp.resourcesProcessed+1, err)
	}

	// Check if splitting is needed
	if services.ShouldSplit(bundleSize, rp.thresholdBytes) {
		return rp.processLargeBundle(resource, resourceID, bundleSize)
	}

	// Bundle is small enough - use direct DIMP path
	return rp.processSmallBundle(resource, resourceID, bundleSize)
}

// processSmallBundle processes a Bundle without splitting
func (rp *ResourceProcessor) processSmallBundle(resource map[string]any, resourceID string, bundleSize int) (map[string]any, error) {
	rp.logger.Debug("Bundle size below threshold, processing directly",
		"bundle_id", resourceID,
		"size_bytes", bundleSize,
		"threshold_bytes", rp.thresholdBytes)

	pseudonymized, err := rp.dimpClient.Pseudonymize(resource)
	if err != nil {
		rp.logger.Error("Failed to pseudonymize Bundle",
			"file", filepath.Base(rp.inputFile),
			"line_number", rp.resourcesProcessed+1,
			"id", resourceID,
			"error", err)
		return nil, fmt.Errorf("failed to pseudonymize Bundle at line %d: %w", rp.resourcesProcessed+1, err)
	}

	return pseudonymized, nil
}

// processLargeBundle orchestrates Bundle splitting and chunk processing
func (rp *ResourceProcessor) processLargeBundle(resource map[string]any, resourceID string, bundleSize int) (map[string]any, error) {
	// Split the Bundle into chunks
	splitResult, err := rp.splitLargeBundle(resource, resourceID, bundleSize)
	if err != nil {
		return nil, err
	}

	// Process chunks and reassemble
	return rp.ProcessBundleChunks(*splitResult, resourceID)
}

// splitLargeBundle splits a large Bundle into smaller chunks based on threshold
func (rp *ResourceProcessor) splitLargeBundle(resource map[string]any, resourceID string, bundleSize int) (*models.SplitResult, error) {
	thresholdMB := rp.thresholdBytes / (1024 * 1024)

	// Log Bundle splitting operation
	rp.logger.Info("Bundle size exceeds threshold, splitting",
		"bundle_id", resourceID,
		"size_bytes", bundleSize,
		"threshold_bytes", rp.thresholdBytes,
		"size_mb", float64(bundleSize)/(1024*1024),
		"threshold_mb", thresholdMB)

	// Split the Bundle
	splitResult, err := services.SplitBundle(resource, rp.thresholdBytes)
	if err != nil {
		rp.logger.Error("Failed to split Bundle",
			"file", filepath.Base(rp.inputFile),
			"line_number", rp.resourcesProcessed+1,
			"bundle_id", resourceID,
			"error", err)
		return nil, fmt.Errorf("failed to split Bundle at line %d: %w", rp.resourcesProcessed+1, err)
	}

	// Log split results
	rp.logger.Info("Split Bundle into chunks",
		"bundle_id", resourceID,
		"chunks", splitResult.TotalChunks)

	return &splitResult, nil
}

// ProcessBundleChunks orchestrates pseudonymization and reassembly of Bundle chunks
func (rp *ResourceProcessor) ProcessBundleChunks(splitResult models.SplitResult, resourceID string) (map[string]any, error) {
	// Process each chunk through DIMP
	pseudonymizedChunks, err := rp.pseudonymizeBundleChunks(splitResult.Chunks, resourceID)
	if err != nil {
		return nil, err
	}

	// Reassemble pseudonymized chunks
	reassembledBundle, err := rp.reassembleBundleChunks(splitResult.Metadata, pseudonymizedChunks, resourceID)
	if err != nil {
		return nil, err
	}

	return reassembledBundle, nil
}

// pseudonymizeBundleChunks sends each Bundle chunk through DIMP for pseudonymization
func (rp *ResourceProcessor) pseudonymizeBundleChunks(chunks []models.BundleChunk, resourceID string) ([]map[string]any, error) {
	pseudonymizedChunks := make([]map[string]any, 0, len(chunks))
	for _, chunk := range chunks {
		rp.logger.Debug("Processing Bundle chunk",
			"bundle_id", resourceID,
			"chunk", fmt.Sprintf("%d/%d", chunk.Index+1, chunk.TotalChunks),
			"entries", len(chunk.Entries),
			"estimated_bytes", chunk.EstimatedSize)

		// Convert chunk to FHIR Bundle format
		chunkBundle := models.ConvertChunkToBundle(chunk)

		// Send chunk to DIMP
		pseudonymizedChunk, err := rp.dimpClient.Pseudonymize(chunkBundle)
		if err != nil {
			rp.logger.Error("Failed to pseudonymize Bundle chunk",
				"file", filepath.Base(rp.inputFile),
				"line_number", rp.resourcesProcessed+1,
				"bundle_id", resourceID,
				"chunk_id", chunk.ChunkID,
				"chunk", fmt.Sprintf("%d/%d", chunk.Index+1, chunk.TotalChunks),
				"error", err)
			return nil, fmt.Errorf("failed to pseudonymize Bundle chunk %d/%d at line %d: %w",
				chunk.Index+1, chunk.TotalChunks, rp.resourcesProcessed+1, err)
		}

		pseudonymizedChunks = append(pseudonymizedChunks, pseudonymizedChunk)
	}

	return pseudonymizedChunks, nil
}

// reassembleBundleChunks combines pseudonymized chunks back into a single Bundle
func (rp *ResourceProcessor) reassembleBundleChunks(metadata models.BundleMetadata, pseudonymizedChunks []map[string]any, resourceID string) (map[string]any, error) {
	reassembled, err := services.ReassembleBundle(metadata, pseudonymizedChunks)
	if err != nil {
		rp.logger.Error("Failed to reassemble Bundle chunks",
			"file", filepath.Base(rp.inputFile),
			"line_number", rp.resourcesProcessed+1,
			"bundle_id", resourceID,
			"chunks", len(pseudonymizedChunks),
			"error", err)
		return nil, fmt.Errorf("failed to reassemble Bundle at line %d: %w", rp.resourcesProcessed+1, err)
	}

	// Log reassembly completion
	rp.logger.Info("Reassembled Bundle from chunks",
		"bundle_id", resourceID,
		"entries", reassembled.EntryCount,
		"chunks", len(pseudonymizedChunks))

	return reassembled.Bundle, nil
}

// ProcessNonBundle handles non-Bundle resources with oversized detection and pseudonymization
func (rp *ResourceProcessor) ProcessNonBundle(resource map[string]any, resourceType, resourceID string) (map[string]any, error) {
	// Check for oversized non-Bundle resources
	err := rp.checkOversizedResource(resource, resourceType, resourceID)
	if err != nil {
		return nil, err
	}

	// Pseudonymize through DIMP
	return rp.pseudonymizeNonBundleResource(resource, resourceType, resourceID)
}

// checkOversizedResource detects if a non-Bundle resource exceeds the size threshold
func (rp *ResourceProcessor) checkOversizedResource(resource map[string]any, resourceType, resourceID string) error {
	oversizedErr := lib.DetectOversizedResource(resource, rp.thresholdBytes)
	if oversizedErr != nil {
		rp.logger.Error("Oversized resource detected",
			"file", filepath.Base(rp.inputFile),
			"line_number", rp.resourcesProcessed+1,
			"resourceType", resourceType,
			"id", resourceID,
			"size_bytes", oversizedErr.Size,
			"threshold_bytes", oversizedErr.Threshold,
		)
		return fmt.Errorf("oversized resource at line %d: %w", rp.resourcesProcessed+1, oversizedErr)
	}

	return nil
}

// pseudonymizeNonBundleResource sends a non-Bundle resource through DIMP for pseudonymization
func (rp *ResourceProcessor) pseudonymizeNonBundleResource(resource map[string]any, resourceType, resourceID string) (map[string]any, error) {
	pseudonymized, err := rp.dimpClient.Pseudonymize(resource)
	if err != nil {
		rp.logger.Error("Failed to pseudonymize FHIR resource",
			"file", filepath.Base(rp.inputFile),
			"line_number", rp.resourcesProcessed+1,
			"resourceType", resourceType,
			"id", resourceID,
			"error", err)
		return nil, fmt.Errorf("failed to pseudonymize resource at line %d: %w", rp.resourcesProcessed+1, err)
	}

	return pseudonymized, nil
}

// IncrementResourceCount increments the processed resource counter
func (rp *ResourceProcessor) IncrementResourceCount() {
	rp.resourcesProcessed++
}

// GetResourceCount returns the current resource count
func (rp *ResourceProcessor) GetResourceCount() int {
	return rp.resourcesProcessed
}
