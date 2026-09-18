package unit

import (
	"crypto/md5"
	"fmt"
	"strings"
	"time"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

// FastRetryConfig keeps the attempt count of the production default but makes
// the backoff between the attempts negligible. A test that exercises an error
// path retries the same number of times as production, without the 15 s wait
// that the 1 s initial backoff gives.
func FastRetryConfig() models.RetryConfig {
	return models.RetryConfig{
		MaxAttempts:      5,
		InitialBackoffMs: 1,
		MaxBackoffMs:     5,
	}
}

// FastHTTPClient gives an HTTP client with FastRetryConfig and a short request
// timeout, for tests that talk to a local test server or to a closed port.
func FastHTTPClient(logger *lib.Logger) *services.HTTPClient {
	return services.NewHTTPClient(2*time.Second, FastRetryConfig(), models.TLSConfig{}, logger)
}

// CreateTestBundle generates a synthetic FHIR Bundle for testing with configurable size
// entryCount: number of entries in the Bundle
// entrySizeKB: approximate size of each entry in kilobytes (used to pad resources)
func CreateTestBundle(entryCount int, entrySizeKB int) map[string]any {
	// Create entries as []any to match JSON unmarshaling behavior
	entries := make([]any, 0, entryCount)
	for i := 0; i < entryCount; i++ {
		entry := map[string]any{
			"fullUrl": fmt.Sprintf("urn:uuid:condition-%d", i),
			"resource": map[string]any{
				"resourceType": "Condition",
				"id":           fmt.Sprintf("condition-%d", i),
				"code": map[string]any{
					"text": fmt.Sprintf("Test condition %d", i),
				},
				"subject": map[string]any{
					"reference": fmt.Sprintf("Patient/patient-%d", i%100),
				},
				// Add padding to reach target entry size
				"_padding": generatePadding(entrySizeKB * 1024),
			},
		}
		entries = append(entries, entry)
	}

	bundle := map[string]any{
		"resourceType": "Bundle",
		"id":           "test-bundle-" + fmt.Sprintf("%d", time.Now().UnixNano()),
		"type":         "collection",
		"timestamp":    time.Now().Format(time.RFC3339),
		"entry":        entries,
		// Note: "total" field intentionally omitted - it's only valid for searchset/history bundles per FHIR R4 spec
	}

	return bundle
}

// CreateLargeTestBundle generates a 50MB Bundle with 100k entries for stress testing
// Each Condition resource is ~500 bytes, resulting in approximately 50MB total
func CreateLargeTestBundle() map[string]any {
	return CreateTestBundle(100000, 1) // ~1KB per entry = ~100MB, adjust down for 50MB
}

// MockPseudonymizeEntry simulates DIMP pseudonymization by deterministically modifying resource data
// This creates a consistent pseudonymized version for testing without actual DIMP service
func MockPseudonymizeEntry(entry map[string]any) map[string]any {
	// Create a copy to avoid mutating input
	pseudonymized := make(map[string]any)
	for k, v := range entry {
		pseudonymized[k] = v
	}

	// Extract resource from entry
	if resource, ok := pseudonymized["resource"].(map[string]any); ok {
		pseudoResource := make(map[string]any)
		for k, v := range resource {
			pseudoResource[k] = v
		}

		// Pseudonymize ID fields
		if id, ok := pseudoResource["id"].(string); ok {
			pseudoResource["id"] = "pseudo-" + hashValue(id)
		}

		// Pseudonymize subject reference if present
		if subject, ok := pseudoResource["subject"].(map[string]any); ok {
			if ref, ok := subject["reference"].(string); ok {
				pseudoSubject := make(map[string]any)
				pseudoSubject["reference"] = hashValue(ref)
				pseudoResource["subject"] = pseudoSubject
			}
		}

		pseudonymized["resource"] = pseudoResource
	}

	// Update fullUrl with pseudonymized ID
	if resource, ok := pseudonymized["resource"].(map[string]any); ok {
		if id, ok := resource["id"].(string); ok {
			pseudonymized["fullUrl"] = "urn:uuid:" + id
		}
	}

	return pseudonymized
}

// generatePadding creates a string of specified byte length for padding
func generatePadding(sizeBytes int) string {
	if sizeBytes <= 0 {
		return ""
	}
	// Create padding string (repeating pattern to reach target size)
	const pattern = "0123456789abcdef"
	repetitions := (sizeBytes / len(pattern)) + 1

	// Use strings.Builder for efficient string concatenation (O(n) instead of O(n²))
	var builder strings.Builder
	builder.Grow(sizeBytes) // Pre-allocate capacity to avoid multiple allocations
	for i := 0; i < repetitions; i++ {
		builder.WriteString(pattern)
	}
	return builder.String()[:sizeBytes]
}

// hashValue creates a deterministic hash of a value for pseudonymization
func hashValue(value string) string {
	hash := md5.Sum([]byte(value))
	return fmt.Sprintf("%x", hash)[:16] // Return first 16 chars of hex digest
}
