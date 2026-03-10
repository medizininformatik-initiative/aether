package lib

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"

	"github.com/medizininformatik-initiative/aether/internal/models"
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

// ParseNDJSONLine parses a single line of NDJSON into a FHIR resource
func ParseNDJSONLine(line []byte) (FHIRResource, error) {
	if len(line) == 0 {
		return nil, fmt.Errorf("empty line")
	}

	var resource FHIRResource
	if err := json.Unmarshal(line, &resource); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	return resource, nil
}

// ReadNDJSONFile reads a FHIR NDJSON file line-by-line
// Calls the callback function for each valid resource
// Returns total lines processed and any fatal error
// Automatically handles both compressed (.ndjson.zst) and uncompressed (.ndjson) files
func ReadNDJSONFile(filePath string, callback func(FHIRResource) error) (int, error) {
	reader, err := OpenFileForReading(filePath)
	if err != nil {
		return 0, fmt.Errorf("failed to open file: %w", err)
	}
	defer func() { _ = reader.Close() }()

	return ReadNDJSON(reader, callback)
}

// DefaultMaxNDJSONLineSize is an alias for models.DefaultMaxNDJSONLineSize.
// Kept here for convenience so callers of the scanner API don't need to import models.
const DefaultMaxNDJSONLineSize = models.DefaultMaxNDJSONLineSize

// NewNDJSONScanner creates a bufio.Scanner configured to handle large FHIR NDJSON lines.
// Uses DefaultMaxNDJSONLineSize (100MB) as the maximum line size.
func NewNDJSONScanner(r io.Reader) *bufio.Scanner {
	return NewNDJSONScannerWithMaxSize(r, DefaultMaxNDJSONLineSize)
}

// NewNDJSONScannerWithMaxSize creates a bufio.Scanner with a custom maximum line size.
// The buffer starts at min(64KB, maxSize) and grows on demand up to maxSize bytes.
func NewNDJSONScannerWithMaxSize(r io.Reader, maxSize int) *bufio.Scanner {
	scanner := bufio.NewScanner(r)
	initialSize := bufio.MaxScanTokenSize
	if maxSize < initialSize {
		initialSize = maxSize
	}
	buf := make([]byte, initialSize)
	scanner.Buffer(buf, maxSize)
	return scanner
}

// ReadNDJSON reads FHIR NDJSON from an io.Reader
// Calls the callback function for each valid resource
// Returns total lines processed and any fatal error
func ReadNDJSON(reader io.Reader, callback func(FHIRResource) error) (int, error) {
	scanner := NewNDJSONScanner(reader)

	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Bytes()

		// Skip empty lines
		if len(line) == 0 {
			continue
		}

		// Parse FHIR resource
		resource, err := ParseNDJSONLine(line)
		if err != nil {
			return lineNum, fmt.Errorf("line %d: %w", lineNum, err)
		}

		// Call callback with parsed resource
		if err := callback(resource); err != nil {
			return lineNum, fmt.Errorf("callback failed at line %d: %w", lineNum, err)
		}
	}

	if err := scanner.Err(); err != nil {
		return lineNum, fmt.Errorf("scanner error: %w", err)
	}

	return lineNum, nil
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
