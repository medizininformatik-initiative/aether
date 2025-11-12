package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/pipeline"
)

// TestDIMPConsistency_SplitVsUnsplit verifies that bundle splitting produces
// identical output to processing without splitting
//
// WHY: This is critical - pseudonymization must be deterministic regardless of
// whether bundles are split. Different IDs or missing metadata would indicate
// data corruption.
func TestDIMPConsistency_SplitVsUnsplit(t *testing.T) {
	// Create mock DIMP service that adds metadata to Bundles
	dimpServer := createRealisticMockDIMPServer(t)
	defer dimpServer.Close()

	// Create test Bundle with 100 entries (~10MB when configured appropriately)
	testBundle := createLargeTestBundle(t, 100, 100) // 100 entries, ~100KB each = ~10MB

	// Scenario 1: Process with high threshold (no splitting)
	t.Run("Process without splitting", func(t *testing.T) {
		tmpDir := t.TempDir()
		jobID := "test-unsplit"
		jobDir := filepath.Join(tmpDir, "jobs", jobID)
		importDir := filepath.Join(jobDir, "import")
		pseudonymizedDir := filepath.Join(jobDir, "pseudonymized")

		require.NoError(t, os.MkdirAll(importDir, 0755))
		require.NoError(t, os.MkdirAll(pseudonymizedDir, 0755))

		// Write test bundle
		bundleNDJSON := filepath.Join(importDir, "test_bundle.ndjson")
		writeTestNDJSON(t, bundleNDJSON, []map[string]any{testBundle})

		job := &models.PipelineJob{
			JobID: jobID,
			Config: models.ProjectConfig{
				Services: models.ServiceConfig{
					DIMP: models.DIMPConfig{
						URL:                    dimpServer.URL,
						BundleSplitThresholdMB: 100, // High threshold - no splitting
					},
				},
				Pipeline: models.PipelineConfig{
					EnabledSteps: []models.StepName{models.StepDIMP},
				},
				Retry: models.RetryConfig{
					MaxAttempts:      3,
					InitialBackoffMs: 100,
					MaxBackoffMs:     5000,
				},
			},
			Steps: make([]models.PipelineStep, 0),
		}

		logger := lib.NewLogger(lib.LogLevelDebug)
		err := pipeline.ExecuteDIMPStep(job, jobDir, logger)
		require.NoError(t, err, "DIMP step should succeed without splitting")

		// Read output
		outputFile := filepath.Join(pseudonymizedDir, "dimped_test_bundle.ndjson")
		outputData := readTestNDJSON(t, outputFile)
		require.Len(t, outputData, 1, "Should have exactly one Bundle")

		unsplitBundle := outputData[0]

		// Verify Bundle-level pseudonymization occurred
		unsplitID := unsplitBundle["id"].(string)
		assert.NotEqual(t, testBundle["id"], unsplitID, "ID should be pseudonymized")
		assert.Contains(t, unsplitID, "pseudo-", "ID should have pseudo prefix")

		// Verify meta.security tags are present
		require.Contains(t, unsplitBundle, "meta", "Bundle should have meta field")
		meta := unsplitBundle["meta"].(map[string]any)
		require.Contains(t, meta, "security", "meta should have security tags")
		security := meta["security"].([]any)
		assert.Greater(t, len(security), 0, "Should have security tags from DIMP")

		t.Logf("✓ Unsplit Bundle: ID=%s, entries=%d, security_tags=%d",
			unsplitID, len(unsplitBundle["entry"].([]any)), len(security))
	})

	// Scenario 2: Process with low threshold (with splitting)
	t.Run("Process with splitting", func(t *testing.T) {
		tmpDir := t.TempDir()
		jobID := "test-split"
		jobDir := filepath.Join(tmpDir, "jobs", jobID)
		importDir := filepath.Join(jobDir, "import")
		pseudonymizedDir := filepath.Join(jobDir, "pseudonymized")

		require.NoError(t, os.MkdirAll(importDir, 0755))
		require.NoError(t, os.MkdirAll(pseudonymizedDir, 0755))

		// Write same test bundle
		bundleNDJSON := filepath.Join(importDir, "test_bundle.ndjson")
		writeTestNDJSON(t, bundleNDJSON, []map[string]any{testBundle})

		job := &models.PipelineJob{
			JobID: jobID,
			Config: models.ProjectConfig{
				Services: models.ServiceConfig{
					DIMP: models.DIMPConfig{
						URL:                    dimpServer.URL,
						BundleSplitThresholdMB: 1, // Low threshold - force splitting
					},
				},
				Pipeline: models.PipelineConfig{
					EnabledSteps: []models.StepName{models.StepDIMP},
				},
				Retry: models.RetryConfig{
					MaxAttempts:      3,
					InitialBackoffMs: 100,
					MaxBackoffMs:     5000,
				},
			},
			Steps: make([]models.PipelineStep, 0),
		}

		logger := lib.NewLogger(lib.LogLevelDebug)
		err := pipeline.ExecuteDIMPStep(job, jobDir, logger)
		require.NoError(t, err, "DIMP step should succeed with splitting")

		// Read output
		outputFile := filepath.Join(pseudonymizedDir, "dimped_test_bundle.ndjson")
		outputData := readTestNDJSON(t, outputFile)
		require.Len(t, outputData, 1, "Should have exactly one Bundle")

		splitBundle := outputData[0]

		// Verify Bundle-level pseudonymization occurred
		splitID := splitBundle["id"].(string)
		assert.NotEqual(t, testBundle["id"], splitID, "ID should be pseudonymized")
		assert.Contains(t, splitID, "pseudo-", "ID should have pseudo prefix")

		// Verify meta.security tags are present (THIS IS THE BUG - they're missing!)
		require.Contains(t, splitBundle, "meta", "Bundle should have meta field")
		meta := splitBundle["meta"].(map[string]any)
		require.Contains(t, meta, "security", "meta should have security tags")
		security := meta["security"].([]any)
		assert.Greater(t, len(security), 0, "Should have security tags from DIMP")

		t.Logf("✓ Split Bundle: ID=%s, entries=%d, security_tags=%d",
			splitID, len(splitBundle["entry"].([]any)), len(security))
	})
}

// createRealisticMockDIMPServer creates a mock DIMP that adds Bundle-level metadata
// to simulate real DIMP behavior
func createRealisticMockDIMPServer(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/$de-identify" {
			http.Error(w, "Invalid path", http.StatusNotFound)
			return
		}

		var input map[string]any
		err := json.NewDecoder(r.Body).Decode(&input)
		if err != nil {
			http.Error(w, "Failed to decode request", http.StatusBadRequest)
			return
		}

		// Pseudonymize resource
		pseudonymizeResourceWithMetadata(input)

		// Return pseudonymized resource
		w.Header().Set("Content-Type", "application/fhir+json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(input)
	}))
}

// pseudonymizeResourceWithMetadata simulates real DIMP behavior:
// - Changes IDs to pseudo-* values
// - Adds meta.security tags to Bundle resources
func pseudonymizeResourceWithMetadata(obj map[string]any) {
	// Pseudonymize ID
	if id, ok := obj["id"].(string); ok {
		obj["id"] = "pseudo-" + id
	}

	// Add meta.security tags for Bundles (this is what real DIMP does)
	if resourceType, ok := obj["resourceType"].(string); ok && resourceType == "Bundle" {
		// Create or update meta field
		var meta map[string]any
		if existing, ok := obj["meta"].(map[string]any); ok {
			meta = existing
		} else {
			meta = make(map[string]any)
		}

		// Add security tags
		meta["security"] = []map[string]any{
			{
				"code":    "REDACTED",
				"display": "redacted",
				"system":  "http://terminology.hl7.org/CodeSystem/v3-ObservationValue",
			},
			{
				"code":    "CRYTOHASH",
				"display": "cryptographic hash function",
				"system":  "http://terminology.hl7.org/CodeSystem/v3-ObservationValue",
			},
			{
				"code":    "GENERALIZED",
				"display": "exact value is replaced with a general value",
			},
			{
				"code":    "PSEUDED",
				"display": "pseudonymized",
				"system":  "http://terminology.hl7.org/CodeSystem/v3-ObservationValue",
			},
		}

		obj["meta"] = meta
	}

	// Recursively pseudonymize nested entries (for Bundles)
	if entries, ok := obj["entry"].([]any); ok {
		for _, entryRaw := range entries {
			if entry, ok := entryRaw.(map[string]any); ok {
				if resource, ok := entry["resource"].(map[string]any); ok {
					pseudonymizeResourceWithMetadata(resource)
				}
			}
		}
	}
}
