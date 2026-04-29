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

// TestReadNDJSON_LargeBundle verifies ReadNDJSON parses a Bundle whose
// serialized size exceeds the prior 100MB scanner cap. This is the regression
// test for issue #335 — json.Decoder does not impose a per-line buffer limit.
func TestReadNDJSON_LargeBundle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping >100MB test in short mode")
	}

	// Generate a Bundle that serializes to >100MB to prove the old cap is gone.
	largeBundle := generateLargeFHIRBundle(120000)
	bundleJSON, err := json.Marshal(largeBundle)
	require.NoError(t, err)
	require.Greater(t, len(bundleJSON), 100*1024*1024,
		"test bundle must exceed 100MB to verify the cap removal")

	reader := bytes.NewReader(append(bundleJSON, '\n'))

	count, err := lib.ReadNDJSON(reader, func(r lib.FHIRResource) error {
		rt, terr := r.GetResourceType()
		require.NoError(t, terr)
		assert.Equal(t, "Bundle", rt)
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

// TestReadNDJSON_MultipleResources confirms decoding a multi-line stream still
// yields one resource per JSON value.
func TestReadNDJSON_MultipleResources(t *testing.T) {
	patient := []byte(`{"resourceType":"Patient","id":"p1"}`)
	observation := []byte(`{"resourceType":"Observation","id":"o1","status":"final"}`)

	var content []byte
	content = append(content, patient...)
	content = append(content, '\n')
	content = append(content, observation...)
	content = append(content, '\n')

	var seen []string
	count, err := lib.ReadNDJSON(bytes.NewReader(content), func(r lib.FHIRResource) error {
		rt, _ := r.GetResourceType()
		seen = append(seen, rt)
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, 2, count)
	assert.Equal(t, []string{"Patient", "Observation"}, seen)
}

// TestReadNDJSON_ConcatenatedJSON confirms json.Decoder accepts both
// newline-delimited and concatenated JSON streams (no separator required).
func TestReadNDJSON_ConcatenatedJSON(t *testing.T) {
	concat := []byte(`{"resourceType":"Patient","id":"p1"}{"resourceType":"Patient","id":"p2"}`)

	count, err := lib.ReadNDJSON(bytes.NewReader(concat), func(r lib.FHIRResource) error { return nil })
	require.NoError(t, err)
	assert.Equal(t, 2, count)
}

// TestReadNDJSON_DecodeError surfaces parse errors with the resource index.
func TestReadNDJSON_DecodeError(t *testing.T) {
	bad := []byte(`{"resourceType":"Patient"}` + "\n" + `{not json}`)

	count, err := lib.ReadNDJSON(bytes.NewReader(bad), func(r lib.FHIRResource) error { return nil })
	require.Error(t, err)
	assert.Equal(t, 1, count)
	assert.Contains(t, err.Error(), "decode at resource 2")
}

// TestCountResourcesInFile_LargeBundle verifies CountResourcesInFile works
// with NDJSON files containing large Bundles (regression for issue #335).
func TestCountResourcesInFile_LargeBundle(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "large.ndjson")

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
	require.NoError(t, err)
	assert.Equal(t, 2, count)
}

// generateLargeFHIRBundle creates a FHIR Bundle with the requested number of
// Observation entries. Each entry serializes to roughly 1KB.
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
