package unit

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/lib"
)

// TestNewNDJSONScanner_DefaultBuffer verifies the scanner handles lines larger than
// the old 1MB limit (regression test for issue #195)
func TestNewNDJSONScanner_DefaultBuffer(t *testing.T) {
	// Create a FHIR Bundle JSON line that exceeds 1MB
	largeBundle := generateLargeFHIRBundle(1500)
	bundleJSON, err := json.Marshal(largeBundle)
	require.NoError(t, err)
	require.Greater(t, len(bundleJSON), 1024*1024, "Test bundle must exceed 1MB to test the fix")

	reader := bytes.NewReader(bundleJSON)
	scanner := lib.NewNDJSONScanner(reader)

	// Should successfully scan the large line
	assert.True(t, scanner.Scan(), "Scanner should handle lines > 1MB")
	assert.NoError(t, scanner.Err())
	assert.Equal(t, len(bundleJSON), len(scanner.Bytes()))
}

// TestNewNDJSONScannerWithMaxSize_CustomSize verifies custom buffer size works
func TestNewNDJSONScannerWithMaxSize_CustomSize(t *testing.T) {
	smallLine := []byte(`{"resourceType":"Patient","id":"1"}`)
	reader := bytes.NewReader(smallLine)
	scanner := lib.NewNDJSONScannerWithMaxSize(reader, 1024)

	assert.True(t, scanner.Scan())
	assert.NoError(t, scanner.Err())
	assert.Equal(t, string(smallLine), scanner.Text())
}

// TestNewNDJSONScannerWithMaxSize_ExceedsLimit verifies scanner fails gracefully
// when a line exceeds the configured max
func TestNewNDJSONScannerWithMaxSize_ExceedsLimit(t *testing.T) {
	maxSize := 4096
	// Create a line larger than maxSize (no newline, so scanner must buffer it all)
	largeLine := strings.Repeat("x", maxSize*2)
	reader := bytes.NewReader([]byte(largeLine))
	scanner := lib.NewNDJSONScannerWithMaxSize(reader, maxSize)

	// Should fail since line exceeds max
	assert.False(t, scanner.Scan())
	assert.Error(t, scanner.Err())
	assert.Contains(t, scanner.Err().Error(), "token too long")
}

// TestDefaultMaxNDJSONLineSize verifies the constant is 100MB
func TestDefaultMaxNDJSONLineSize(t *testing.T) {
	assert.Equal(t, 100*1024*1024, lib.DefaultMaxNDJSONLineSize)
}

// TestReadNDJSON_LargeBundle verifies ReadNDJSON handles Bundles > 1MB
// This is the core regression test for issue #195
func TestReadNDJSON_LargeBundle(t *testing.T) {
	largeBundle := generateLargeFHIRBundle(1500)
	bundleJSON, err := json.Marshal(largeBundle)
	require.NoError(t, err)
	require.Greater(t, len(bundleJSON), 1024*1024, "Test bundle must exceed 1MB")

	reader := bytes.NewReader(append(bundleJSON, '\n'))

	var resources []lib.FHIRResource
	lineCount, err := lib.ReadNDJSON(reader, func(r lib.FHIRResource) error {
		resources = append(resources, r)
		return nil
	})

	require.NoError(t, err, "ReadNDJSON should handle lines > 1MB (issue #195)")
	assert.Equal(t, 1, lineCount)
	assert.Len(t, resources, 1)

	resourceType, err := resources[0].GetResourceType()
	require.NoError(t, err)
	assert.Equal(t, "Bundle", resourceType)
}

// TestCountResourcesInFile_LargeBundle verifies CountResourcesInFile works
// with NDJSON files containing large Bundles (regression test for issue #195)
func TestCountResourcesInFile_LargeBundle(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "large.ndjson")

	// Write two resources: one large Bundle and one small Patient
	largeBundle := generateLargeFHIRBundle(1500)
	bundleJSON, err := json.Marshal(largeBundle)
	require.NoError(t, err)
	require.Greater(t, len(bundleJSON), 1024*1024)

	smallPatient := map[string]any{
		"resourceType": "Patient",
		"id":           "patient-1",
		"gender":       "male",
	}
	patientJSON, err := json.Marshal(smallPatient)
	require.NoError(t, err)

	content := append(bundleJSON, '\n')
	content = append(content, patientJSON...)
	content = append(content, '\n')

	err = os.WriteFile(filePath, content, 0644)
	require.NoError(t, err)

	count, err := lib.CountResourcesInFile(filePath)
	require.NoError(t, err, "CountResourcesInFile should handle large bundles (issue #195)")
	assert.Equal(t, 2, count)
}

// generateLargeFHIRBundle creates a FHIR Bundle with enough entries to exceed 1MB.
// Each entry is ~2KB, so 600 entries produce a ~1.2MB JSON line.
func generateLargeFHIRBundle(numEntries int) map[string]any {
	entries := make([]map[string]any, numEntries)
	for i := 0; i < numEntries; i++ {
		entries[i] = map[string]any{
			"resource": map[string]any{
				"resourceType": "Observation",
				"id":           fmt.Sprintf("obs-%d", i),
				"status":       "final",
				"code": map[string]any{
					"coding": []map[string]any{
						{
							"system":  "http://loinc.org",
							"code":    fmt.Sprintf("%05d-%d", i, i%10),
							"display": fmt.Sprintf("Test observation %d with a reasonably long display name to increase the overall size of this entry", i),
						},
					},
					"text": fmt.Sprintf("Observation code text for entry number %d", i),
				},
				"valueQuantity": map[string]any{
					"value":  42.0 + float64(i),
					"unit":   "mg/dL",
					"system": "http://unitsofmeasure.org",
					"code":   "mg/dL",
				},
				"subject": map[string]any{
					"reference": "Patient/patient-1",
					"display":   "Test Patient One",
				},
				"encounter": map[string]any{
					"reference": fmt.Sprintf("Encounter/encounter-%d", i),
				},
				"effectiveDateTime": "2024-01-15T10:30:00Z",
				"issued":            "2024-01-15T10:35:00Z",
				"performer": []map[string]any{
					{
						"reference": "Practitioner/practitioner-1",
						"display":   "Dr. Test Physician",
					},
				},
				"note": []map[string]any{
					{
						"text": strings.Repeat("Padding text for bundle size. ", 20),
					},
				},
			},
		}
	}

	return map[string]any{
		"resourceType": "Bundle",
		"id":           "large-bundle-1",
		"type":         "collection",
		"entry":        entries,
	}
}
