package unit

import (
	"encoding/json"
	"fmt"
	"regexp"
	"time"
)

// BundleChunkValidator provides contract test helpers for validating FHIR Bundle chunks
type BundleChunkValidator struct {
	SchemaPath string
}

// NewBundleChunkValidator creates a new validator instance
func NewBundleChunkValidator() *BundleChunkValidator {
	return &BundleChunkValidator{
		SchemaPath: "specs/004-bundle-splitting/contracts/bundle-chunk.json",
	}
}

// ValidateChunkStructure performs structural validation of a Bundle chunk
// Returns an error if the chunk does not conform to FHIR R4 Bundle requirements
func (v *BundleChunkValidator) ValidateChunkStructure(chunk map[string]any) error {
	// Validate required fields exist
	if _, ok := chunk["resourceType"]; !ok {
		return fmt.Errorf("missing required field: resourceType")
	}

	if resourceType, ok := chunk["resourceType"].(string); !ok || resourceType != "Bundle" {
		return fmt.Errorf("resourceType must be 'Bundle', got %v", chunk["resourceType"])
	}

	if _, ok := chunk["type"]; !ok {
		return fmt.Errorf("missing required field: type")
	}

	bundleType, ok := chunk["type"].(string)
	if !ok {
		return fmt.Errorf("type must be string, got %T", chunk["type"])
	}

	// Validate bundle type is document or collection
	if bundleType != "document" && bundleType != "collection" {
		return fmt.Errorf("type must be 'document' or 'collection', got '%s'", bundleType)
	}

	if _, ok := chunk["entry"]; !ok {
		return fmt.Errorf("missing required field: entry")
	}

	if _, ok := chunk["entry"].([]any); !ok {
		return fmt.Errorf("entry must be array, got %T", chunk["entry"])
	}

	return nil
}

// ValidateChunkID validates that chunk ID follows the format: {originalID}-chunk-{index}
func (v *BundleChunkValidator) ValidateChunkID(chunkID string) error {
	// Pattern: anything-chunk-number
	pattern := regexp.MustCompile(`^.+-chunk-\d+$`)
	if !pattern.MatchString(chunkID) {
		return fmt.Errorf("chunk id must match pattern '{originalID}-chunk-{index}', got '%s'", chunkID)
	}
	return nil
}

// ValidateChunkTotal validates that the total field matches the entry count
func (v *BundleChunkValidator) ValidateChunkTotal(chunk map[string]any) error {
	total, ok := chunk["total"].(float64)
	if !ok {
		return fmt.Errorf("total must be a number, got %T", chunk["total"])
	}

	entries, ok := chunk["entry"].([]any)
	if !ok {
		return fmt.Errorf("entry must be array")
	}

	if int(total) != len(entries) {
		return fmt.Errorf("total (%d) must match entry count (%d)", int(total), len(entries))
	}

	if int(total) < 1 {
		return fmt.Errorf("total must be at least 1, got %d", int(total))
	}

	return nil
}

// ValidateChunkTimestamp validates the timestamp format if present
func (v *BundleChunkValidator) ValidateChunkTimestamp(timestamp any) error {
	if timestamp == nil {
		return nil // Optional field
	}

	timestampStr, ok := timestamp.(string)
	if !ok {
		return fmt.Errorf("timestamp must be string, got %T", timestamp)
	}

	// Try to parse as RFC3339 (ISO 8601)
	_, err := time.Parse(time.RFC3339, timestampStr)
	if err != nil {
		return fmt.Errorf("timestamp must be valid RFC3339 format, got '%s': %w", timestampStr, err)
	}

	return nil
}

// ValidateChunkEntries validates that all entries have valid structure
func (v *BundleChunkValidator) ValidateChunkEntries(entries []any) error {
	for i, entry := range entries {
		entryObj, ok := entry.(map[string]any)
		if !ok {
			return fmt.Errorf("entry[%d] must be object, got %T", i, entry)
		}

		// Validate required resource field
		if _, ok := entryObj["resource"]; !ok {
			return fmt.Errorf("entry[%d] missing required field: resource", i)
		}

		resource, ok := entryObj["resource"].(map[string]any)
		if !ok {
			return fmt.Errorf("entry[%d].resource must be object, got %T", i, entryObj["resource"])
		}

		// Validate resource has resourceType
		if _, ok := resource["resourceType"]; !ok {
			return fmt.Errorf("entry[%d].resource missing required field: resourceType", i)
		}

		resourceType, ok := resource["resourceType"].(string)
		if !ok || resourceType == "" {
			return fmt.Errorf("entry[%d].resource.resourceType must be non-empty string, got %v", i, resource["resourceType"])
		}
	}

	return nil
}

// ValidateBundle validates a Bundle chunk comprehensively
// This is the main contract test function
func (v *BundleChunkValidator) ValidateBundle(chunk map[string]any) error {
	// Validate basic structure
	if err := v.ValidateChunkStructure(chunk); err != nil {
		return err
	}

	// Validate chunk ID if present
	if chunkID, ok := chunk["id"].(string); ok {
		if err := v.ValidateChunkID(chunkID); err != nil {
			return err
		}
	}

	// Validate total matches entry count
	if err := v.ValidateChunkTotal(chunk); err != nil {
		return err
	}

	// Validate timestamp format if present
	if timestamp, ok := chunk["timestamp"]; ok {
		if err := v.ValidateChunkTimestamp(timestamp); err != nil {
			return err
		}
	}

	// Validate entries
	if entries, ok := chunk["entry"].([]any); ok {
		if err := v.ValidateChunkEntries(entries); err != nil {
			return err
		}
	}

	return nil
}

// ValidateBundleAsJSON validates a Bundle from JSON bytes
func (v *BundleChunkValidator) ValidateBundleAsJSON(jsonData []byte) error {
	var bundle map[string]any
	if err := json.Unmarshal(jsonData, &bundle); err != nil {
		return fmt.Errorf("failed to parse JSON: %w", err)
	}

	return v.ValidateBundle(bundle)
}

// IsFHIRBundle checks if a map represents a valid FHIR Bundle (simple check)
func IsFHIRBundle(obj map[string]any) error {
	if resourceType, ok := obj["resourceType"].(string); !ok || resourceType != "Bundle" {
		return fmt.Errorf("not a FHIR Bundle: resourceType is '%v'", obj["resourceType"])
	}
	return nil
}
