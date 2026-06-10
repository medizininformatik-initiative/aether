package lib

import (
	"fmt"
	"time"

	"github.com/medizininformatik-initiative/aether/internal/models"
)

// ResourceType returns the resourceType of a FHIR resource, or "" if the field
// is absent or not a string.
func ResourceType(resource map[string]any) string {
	rt, _ := FHIRResource(resource).GetResourceType()
	return rt
}

// ResourceID returns the id of a FHIR resource, or "" if the field is absent or
// not a string.
func ResourceID(resource map[string]any) string {
	id, _ := FHIRResource(resource).GetID()
	return id
}

// IsProvenance reports whether the resource is a FHIR Provenance.
func IsProvenance(resource map[string]any) bool {
	return ResourceType(resource) == "Provenance"
}

// IsBundle reports whether the resource is a FHIR Bundle.
func IsBundle(resource map[string]any) bool {
	return ResourceType(resource) == "Bundle"
}

// ResourceReference returns the "ResourceType/id" reference for a resource, or
// "" if either component is missing.
func ResourceReference(resource map[string]any) string {
	rt := ResourceType(resource)
	id := ResourceID(resource)
	if rt == "" || id == "" {
		return ""
	}
	return rt + "/" + id
}

// EntryResource extracts the "resource" object from a single Bundle entry.
// The second return value reports whether the entry held a resource object.
func EntryResource(entry map[string]any) (map[string]any, bool) {
	res, ok := entry["resource"].(map[string]any)
	return res, ok
}

// BundleEntries returns a Bundle's entry objects, skipping non-object entries
// and never erroring (cf. ExtractEntriesFromBundle, which reports malformed
// structure). The returned maps alias the Bundle, so mutating them mutates it.
func BundleEntries(bundle map[string]any) []map[string]any {
	entries, ok := bundle["entry"].([]any)
	if !ok {
		return nil
	}

	result := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		if entry, ok := e.(map[string]any); ok {
			result = append(result, entry)
		}
	}
	return result
}

// BundleResources returns the resource from each Bundle entry, skipping entries
// that are malformed or carry no resource. Like BundleEntries, it never errors.
func BundleResources(bundle map[string]any) []map[string]any {
	entries := BundleEntries(bundle)

	resources := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		if res, ok := EntryResource(entry); ok {
			resources = append(resources, res)
		}
	}
	return resources
}

// ExtractEntriesFromBundle returns a Bundle's entry objects, erroring if the
// entry field is missing or any entry is not an object.
func ExtractEntriesFromBundle(bundle map[string]any) ([]map[string]any, error) {
	entries, ok := bundle["entry"].([]any)
	if !ok {
		return nil, fmt.Errorf("bundle.entry must be an array")
	}

	result := make([]map[string]any, 0, len(entries))
	for i, entry := range entries {
		entryObj, ok := entry.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("bundle.entry[%d] must be an object", i)
		}
		result = append(result, entryObj)
	}

	return result, nil
}

// ExtractBundleMetadata reads id, type and optional timestamp from a Bundle,
// erroring if the required id or type is missing.
func ExtractBundleMetadata(bundle map[string]any) (models.BundleMetadata, error) {
	metadata := models.BundleMetadata{}

	if id, ok := bundle["id"].(string); ok {
		metadata.ID = id
	} else {
		return metadata, fmt.Errorf("bundle.id must be present and a string")
	}

	if bundleType, ok := bundle["type"].(string); ok {
		metadata.Type = bundleType
	} else {
		return metadata, fmt.Errorf("bundle.type must be present and a string")
	}

	if timestamp, ok := bundle["timestamp"].(string); ok {
		if parsed, err := time.Parse(time.RFC3339, timestamp); err == nil {
			metadata.Timestamp = parsed
		}
	}

	return metadata, nil
}
