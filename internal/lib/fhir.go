package lib

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// FHIRResource represents a generic FHIR resource as a map
// We don't parse the full FHIR schema - just treat it as JSON
type FHIRResource map[string]any

// GetResourceType extracts the resourceType field from a FHIR resource
func (r FHIRResource) GetResourceType() (string, error) {
	resourceType, ok := r["resourceType"]
	if !ok {
		return "", fmt.Errorf("missing resourceType field")
	}

	typeStr, ok := resourceType.(string)
	if !ok {
		return "", fmt.Errorf("resourceType is not a string")
	}

	return typeStr, nil
}

// GetID extracts the id field from a FHIR resource
func (r FHIRResource) GetID() (string, error) {
	id, ok := r["id"]
	if !ok {
		return "", nil // ID is optional in FHIR
	}

	idStr, ok := id.(string)
	if !ok {
		return "", fmt.Errorf("id is not a string")
	}

	return idStr, nil
}

// ReadNDJSONFile reads a FHIR NDJSON file as a stream of JSON resources.
// Calls the callback function for each parsed resource.
// Returns the total number of resources processed and any fatal error.
// Automatically handles both compressed (.ndjson.zst) and uncompressed (.ndjson) files.
func ReadNDJSONFile(filePath string, callback func(FHIRResource) error) (int, error) {
	reader, err := OpenFileForReading(filePath)
	if err != nil {
		return 0, fmt.Errorf("failed to open file: %w", err)
	}
	defer func() { _ = reader.Close() }()

	return ReadNDJSON(reader, callback)
}

// ReadNDJSON streams FHIR resources from an io.Reader using json.Decoder.
// Each successfully decoded value is passed to callback. Memory is bounded
// by the largest individual JSON value rather than by a single line buffer,
// so resource size is not capped.
// Accepts NDJSON (newline-delimited) and concatenated JSON streams alike,
// because json.Decoder ignores whitespace between values.
func ReadNDJSON(reader io.Reader, callback func(FHIRResource) error) (int, error) {
	dec := json.NewDecoder(reader)
	count := 0
	for {
		var resource FHIRResource
		if err := dec.Decode(&resource); err != nil {
			if errors.Is(err, io.EOF) {
				return count, nil
			}
			return count, fmt.Errorf("decode at resource %d: %w", count+1, err)
		}
		count++

		if err := callback(resource); err != nil {
			return count, fmt.Errorf("callback failed at resource %d: %w", count, err)
		}
	}
}

// WriteNDJSONLine writes a single FHIR resource as NDJSON to a writer
func WriteNDJSONLine(writer io.Writer, resource FHIRResource) error {
	data, err := json.Marshal(resource)
	if err != nil {
		return fmt.Errorf("failed to marshal resource: %w", err)
	}

	if _, err := writer.Write(data); err != nil {
		return fmt.Errorf("failed to write resource: %w", err)
	}

	if _, err := writer.Write([]byte("\n")); err != nil {
		return fmt.Errorf("failed to write newline: %w", err)
	}

	return nil
}

// GroupByResourceType groups FHIR resources by their resourceType field
// Returns a map of resourceType -> list of resources
func GroupByResourceType(resources []FHIRResource) (map[string][]FHIRResource, error) {
	groups := make(map[string][]FHIRResource)

	for i, resource := range resources {
		resourceType, err := resource.GetResourceType()
		if err != nil {
			return nil, fmt.Errorf("resource %d: %w", i, err)
		}

		groups[resourceType] = append(groups[resourceType], resource)
	}

	return groups, nil
}

// ValidateFHIRResource performs basic validation on a FHIR resource
func ValidateFHIRResource(resource FHIRResource) error {
	// Must have resourceType
	if _, err := resource.GetResourceType(); err != nil {
		return err
	}

	// Basic structure check - must be a valid JSON object
	if resource == nil {
		return fmt.Errorf("resource is nil")
	}

	return nil
}

// CountResourcesInFile counts the number of resources in an NDJSON file
// Automatically handles both compressed (.ndjson.zst) and uncompressed (.ndjson) files
func CountResourcesInFile(filePath string) (int, error) {
	count := 0
	_, err := ReadNDJSONFile(filePath, func(r FHIRResource) error {
		count++
		return nil
	})
	return count, err
}
