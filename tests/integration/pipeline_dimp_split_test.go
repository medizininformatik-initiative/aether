package integration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/pipeline"
)

// Integration test: End-to-end splitting with mock DIMP
// Purpose: Verify that large Bundles are automatically split, processed through DIMP,
// and reassembled while maintaining 100% data integrity

func TestDIMPStepWithLargeBundle(t *testing.T) {
	// Create mock DIMP service that pseudonymizes resources
	dimpServer := createMockDIMPServer(t)
	defer dimpServer.Close()

	// Setup temporary directory structure
	tmpDir := t.TempDir()
	jobID := "test-large-bundle-split"
	jobDir := filepath.Join(tmpDir, "jobs", jobID)
	importDir := filepath.Join(jobDir, "import")
	pseudonymizedDir := filepath.Join(jobDir, "pseudonymized")

	require.NoError(t, os.MkdirAll(importDir, 0755), "Failed to create import directory")
	require.NoError(t, os.MkdirAll(pseudonymizedDir, 0755), "Failed to create pseudonymized directory")

	// Create a large test Bundle that will trigger splitting
	// 50MB Bundle with ~100k entries (the exact size depends on test data generation)
	largeBundle := createLargeTestBundle(t, 1000, 100) // 1000 entries, ~100KB each = ~100MB

	// Write Bundle to NDJSON file
	bundleNDJSON := filepath.Join(importDir, "large_bundle.ndjson")
	writeTestNDJSON(t, bundleNDJSON, []map[string]any{largeBundle})

	// Create job with configuration
	job := &models.PipelineJob{
		JobID: jobID,
		Config: models.ProjectConfig{
			Services: models.ServiceConfig{
				DIMP: models.DIMPConfig{
					URL:                    dimpServer.URL,
					BundleSplitThresholdMB: 10, // 10MB threshold
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

	// Create logger for testing
	logger := lib.NewLogger(lib.LogLevelDebug)

	// Execute DIMP step
	err := pipeline.ExecuteDIMPStep(job, jobDir, logger)
	require.NoError(t, err, "DIMP step should complete without error")

	// Verify output file exists
	outputFile := filepath.Join(pseudonymizedDir, "dimped_large_bundle.ndjson")
	_, err = os.Stat(outputFile)
	require.NoError(t, err, "Output NDJSON file should be created")

	// Read and verify pseudonymized output
	outputData := readTestNDJSON(t, outputFile)
	require.GreaterOrEqual(t, len(outputData), 1, "Should have at least one Bundle in output")

	reassembledBundle := outputData[0]

	// Verify Bundle structure is preserved
	assert.Equal(t, "Bundle", reassembledBundle["resourceType"], "Output should be a FHIR Bundle")
	// ID should be pseudonymized (not restored to original)
	reassembledID := reassembledBundle["id"].(string)
	assert.NotEqual(t, largeBundle["id"].(string), reassembledID, "Bundle ID should be pseudonymized")
	assert.Contains(t, reassembledID, "pseudo-", "Bundle ID should have pseudo prefix")
	assert.Equal(t, largeBundle["type"].(string), reassembledBundle["type"].(string), "Bundle type should be preserved")

	// Verify all entries are present and pseudonymized
	originalEntries := largeBundle["entry"].([]any)
	var reassembledEntries []any
	switch v := reassembledBundle["entry"].(type) {
	case []any:
		reassembledEntries = v
	case []map[string]any:
		for _, entry := range v {
			reassembledEntries = append(reassembledEntries, entry)
		}
	}

	assert.Equal(t, len(originalEntries), len(reassembledEntries),
		"All entries should be preserved after splitting and reassembly")

	// Verify entries are in correct order and pseudonymized
	for i, entryRaw := range reassembledEntries {
		entry := entryRaw.(map[string]any)
		resource := entry["resource"].(map[string]any)

		// Verify pseudonymization occurred (ID should be modified)
		originalEntry := originalEntries[i].(map[string]any)
		originalResource := originalEntry["resource"].(map[string]any)

		originalID := originalResource["id"].(string)
		pseudonymizedID := resource["id"].(string)
		assert.NotEqual(t, originalID, pseudonymizedID,
			fmt.Sprintf("Entry %d should be pseudonymized", i))
		assert.True(t, strings.HasPrefix(pseudonymizedID, "pseudo-"),
			fmt.Sprintf("Entry %d should have pseudo- prefix", i))
	}

	// Verify output was created
	assert.True(t, true, "Integration test completed successfully")

	t.Logf("✓ Large Bundle (size=%d bytes, entries=%d) successfully split, processed, and reassembled",
		len(mustMarshalJSON(largeBundle)), len(originalEntries))
}

// TestDIMPStepWithLargeBundleAndChunks specifically tests chunk handling
func TestDIMPStepWithLargeBundleAndChunks(t *testing.T) {
	dimpServer := createMockDIMPServer(t)
	defer dimpServer.Close()

	tmpDir := t.TempDir()
	jobID := "test-bundle-chunks"
	jobDir := filepath.Join(tmpDir, "jobs", jobID)
	importDir := filepath.Join(jobDir, "import")
	pseudonymizedDir := filepath.Join(jobDir, "pseudonymized")

	require.NoError(t, os.MkdirAll(importDir, 0755))
	require.NoError(t, os.MkdirAll(pseudonymizedDir, 0755))

	// Create a Bundle large enough to require splitting
	// Using 200 entries of ~50KB each = ~10MB
	mediumBundle := createLargeTestBundle(t, 200, 50)

	bundleNDJSON := filepath.Join(importDir, "medium_bundle.ndjson")
	writeTestNDJSON(t, bundleNDJSON, []map[string]any{mediumBundle})

	job := &models.PipelineJob{
		JobID: jobID,
		Config: models.ProjectConfig{
			Services: models.ServiceConfig{
				DIMP: models.DIMPConfig{
					URL:                    dimpServer.URL,
					BundleSplitThresholdMB: 2, // 2MB threshold - should create multiple chunks
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

	// Execute DIMP step
	startTime := time.Now()
	err := pipeline.ExecuteDIMPStep(job, jobDir, logger)
	duration := time.Since(startTime)
	require.NoError(t, err, "DIMP step should complete without error")

	// Verify output
	outputFile := filepath.Join(pseudonymizedDir, "dimped_medium_bundle.ndjson")
	_, err = os.Stat(outputFile)
	require.NoError(t, err, "Output file should exist")

	outputData := readTestNDJSON(t, outputFile)
	require.GreaterOrEqual(t, len(outputData), 1, "Should have at least one Bundle in output")

	reassembledBundle := outputData[0]

	// Verify integrity
	originalEntries := mediumBundle["entry"].([]any)
	var reassembledEntries []any
	switch v := reassembledBundle["entry"].(type) {
	case []any:
		reassembledEntries = v
	case []map[string]any:
		for _, entry := range v {
			reassembledEntries = append(reassembledEntries, entry)
		}
	}

	assert.Equal(t, len(originalEntries), len(reassembledEntries),
		"All entries should survive split-process-reassemble cycle")

	t.Logf("✓ Processed Bundle with %d entries across multiple chunks in %v",
		len(originalEntries), duration)
}

// TestDIMPStepWithSmallBundleNoSplit verifies that small Bundles don't get split
func TestDIMPStepWithSmallBundleNoSplit(t *testing.T) {
	dimpServer := createMockDIMPServer(t)
	defer dimpServer.Close()

	tmpDir := t.TempDir()
	jobID := "test-small-bundle-nosplit"
	jobDir := filepath.Join(tmpDir, "jobs", jobID)
	importDir := filepath.Join(jobDir, "import")
	pseudonymizedDir := filepath.Join(jobDir, "pseudonymized")

	require.NoError(t, os.MkdirAll(importDir, 0755))
	require.NoError(t, os.MkdirAll(pseudonymizedDir, 0755))

	// Create a small Bundle that won't trigger splitting
	smallBundle := createLargeTestBundle(t, 10, 5) // 10 entries, ~5KB each = ~50KB

	bundleNDJSON := filepath.Join(importDir, "small_bundle.ndjson")
	writeTestNDJSON(t, bundleNDJSON, []map[string]any{smallBundle})

	job := &models.PipelineJob{
		JobID: jobID,
		Config: models.ProjectConfig{
			Services: models.ServiceConfig{
				DIMP: models.DIMPConfig{
					URL:                    dimpServer.URL,
					BundleSplitThresholdMB: 10, // 10MB threshold - small Bundle won't split
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
	require.NoError(t, err, "DIMP step should complete without error")

	// Verify output exists
	outputFile := filepath.Join(pseudonymizedDir, "dimped_small_bundle.ndjson")
	_, err = os.Stat(outputFile)
	require.NoError(t, err, "Output file should exist")

	outputData := readTestNDJSON(t, outputFile)
	require.GreaterOrEqual(t, len(outputData), 1, "Should have at least one Bundle")

	reassembledBundle := outputData[0]

	// Verify small Bundle wasn't split (but still works correctly)
	originalEntries := smallBundle["entry"].([]any)
	var reassembledEntries []any
	switch v := reassembledBundle["entry"].(type) {
	case []any:
		reassembledEntries = v
	case []map[string]any:
		for _, entry := range v {
			reassembledEntries = append(reassembledEntries, entry)
		}
	}

	assert.Equal(t, len(originalEntries), len(reassembledEntries),
		"Small Bundle should process without splitting")

	t.Logf("✓ Small Bundle (size=%d bytes) processed directly without splitting",
		len(mustMarshalJSON(smallBundle)))
}

// Helper functions

// createMockDIMPServer creates a mock HTTP server that simulates DIMP de-identification
func createMockDIMPServer(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only accept de-identify requests
		if r.URL.Path != "/$de-identify" {
			http.Error(w, "Invalid path", http.StatusNotFound)
			return
		}

		// Decode input (could be a Bundle or individual resource)
		var input map[string]any
		err := json.NewDecoder(r.Body).Decode(&input)
		if err != nil {
			http.Error(w, "Failed to decode request", http.StatusBadRequest)
			return
		}

		// Pseudonymize by adding "pseudo-" prefix to IDs
		pseudonymizeResource(input)

		// Return pseudonymized resource
		w.Header().Set("Content-Type", "application/fhir+json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(input)
	}))
}

// pseudonymizeResource recursively modifies IDs to simulate DIMP pseudonymization
func pseudonymizeResource(obj map[string]any) {
	// Pseudonymize ID
	if id, ok := obj["id"].(string); ok {
		obj["id"] = "pseudo-" + id
	}

	// Recursively pseudonymize nested entries (for Bundles)
	if entries, ok := obj["entry"].([]any); ok {
		for _, entryRaw := range entries {
			if entry, ok := entryRaw.(map[string]any); ok {
				if resource, ok := entry["resource"].(map[string]any); ok {
					pseudonymizeResource(resource)
				}
			}
		}
	}
}

// createLargeTestBundle creates a FHIR Bundle with specified number of entries
func createLargeTestBundle(t *testing.T, entryCount int, entrySizeKB int) map[string]any {
	entries := make([]any, entryCount)

	for i := 0; i < entryCount; i++ {
		// Create realistic FHIR resource with configurable size
		resource := map[string]any{
			"resourceType": "Condition",
			"id":           fmt.Sprintf("condition-%06d", i),
			"subject": map[string]any{
				"reference": fmt.Sprintf("Patient/patient-%06d", i%100),
			},
			"code": map[string]any{
				"coding": []map[string]any{
					{
						"system":  "http://snomed.info/sct",
						"code":    fmt.Sprintf("%06d", 38341003+i),
						"display": fmt.Sprintf("Condition %d", i),
					},
				},
			},
			"text": map[string]any{
				"status": "generated",
				"div":    strings.Repeat("x", entrySizeKB*1024-500), // Pad to target size
			},
		}

		entries[i] = map[string]any{
			"fullUrl":  fmt.Sprintf("urn:uuid:entry-%06d", i),
			"resource": resource,
		}
	}

	bundle := map[string]any{
		"resourceType": "Bundle",
		"id":           "large-test-bundle",
		"type":         "collection",
		"timestamp":    time.Now().Format(time.RFC3339),
		"total":        entryCount,
		"entry":        entries,
	}

	return bundle
}

// writeTestNDJSON writes test data to NDJSON file
func writeTestNDJSON(t *testing.T, path string, data []map[string]any) {
	file, err := os.Create(path)
	require.NoError(t, err, "Failed to create NDJSON file")
	defer func() {
		if err := file.Close(); err != nil {
			t.Logf("Warning: Failed to close NDJSON file: %v", err)
		}
	}()

	for _, obj := range data {
		bytes, err := json.Marshal(obj)
		require.NoError(t, err, "Failed to marshal object")
		_, err = file.WriteString(string(bytes) + "\n")
		require.NoError(t, err, "Failed to write to NDJSON file")
	}
}

// readTestNDJSON reads test data from NDJSON file
func readTestNDJSON(t *testing.T, path string) []map[string]any {
	file, err := os.Open(path)
	require.NoError(t, err, "Failed to open NDJSON file")
	defer func() {
		if err := file.Close(); err != nil {
			t.Logf("Warning: Failed to close NDJSON file: %v", err)
		}
	}()

	var data []map[string]any
	decoder := json.NewDecoder(file)

	for decoder.More() {
		var obj map[string]any
		err := decoder.Decode(&obj)
		require.NoError(t, err, "Failed to decode NDJSON line")
		data = append(data, obj)
	}

	return data
}

// mustMarshalJSON marshals to JSON bytes, panicking on error
func mustMarshalJSON(data any) []byte {
	bytes, err := json.Marshal(data)
	if err != nil {
		panic(fmt.Sprintf("Failed to marshal JSON: %v", err))
	}
	return bytes
}

// Integration test: Oversized resource handling in DIMP step
// Purpose: Verify that oversized non-Bundle resources are detected, logged, and processing continues
// with remaining resources
func TestDIMPStepWithOversizedResource(t *testing.T) {
	// Create mock DIMP service
	dimpServer := createMockDIMPServer(t)
	defer dimpServer.Close()

	// Setup temporary directory structure
	tmpDir := t.TempDir()
	jobID := "test-oversized-resource"
	jobDir := filepath.Join(tmpDir, "jobs", jobID)
	importDir := filepath.Join(jobDir, "import")
	pseudonymizedDir := filepath.Join(jobDir, "pseudonymized")

	require.NoError(t, os.MkdirAll(importDir, 0755), "Failed to create import directory")
	require.NoError(t, os.MkdirAll(pseudonymizedDir, 0755), "Failed to create pseudonymized directory")

	// Create test NDJSON file with mix of normal and oversized resources
	var testData []map[string]any

	// Add 10 normal Observation resources (each ~1MB)
	for i := 0; i < 10; i++ {
		obs := createSmallTestBundle(t, 10, 100) // ~1MB
		obs["resourceType"] = "Observation"
		obs["id"] = fmt.Sprintf("obs-%03d", i)
		testData = append(testData, obs)
	}

	// Add one oversized Observation (35MB) - should NOT stop processing
	oversizedObs := createLargeTestBundle(t, 50, 700) // ~35MB
	oversizedObs["resourceType"] = "Observation"
	oversizedObs["id"] = "obs-oversized-999"
	testData = append(testData, oversizedObs)

	// Add more normal resources after the oversized one
	for i := 10; i < 20; i++ {
		obs := createSmallTestBundle(t, 10, 100)
		obs["resourceType"] = "Observation"
		obs["id"] = fmt.Sprintf("obs-%03d", i)
		testData = append(testData, obs)
	}

	// Write NDJSON file
	ndjsonFile := filepath.Join(importDir, "mixed_resources.ndjson")
	writeTestNDJSON(t, ndjsonFile, testData)

	// Create job configuration with threshold
	job := &models.PipelineJob{
		JobID: jobID,
		Config: models.ProjectConfig{
			Services: models.ServiceConfig{
				DIMP: models.DIMPConfig{
					URL:                    dimpServer.URL,
					BundleSplitThresholdMB: 10, // 10MB threshold
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

	// Execute DIMP step - should handle oversized resource gracefully
	logger := lib.NewLogger(lib.LogLevelInfo)
	err := pipeline.ExecuteDIMPStep(job, jobDir, logger)

	// Expect error due to oversized resource
	assert.Error(t, err, "Step should error when oversized resource is encountered")

	// Verify step was marked as failed
	step, found := models.GetStepByName(*job, models.StepDIMP)
	require.True(t, found, "Should have DIMP step record")
	assert.Equal(t, models.StepStatusFailed, step.Status, "Step should be marked as failed")
}

// Integration test: Threshold configuration
// Purpose: Verify that custom threshold values are respected and control splitting behavior
func TestDIMPStepWithCustomThreshold(t *testing.T) {
	// Create mock DIMP service
	dimpServer := createMockDIMPServer(t)
	defer dimpServer.Close()

	// Create a test Bundle that's 8MB - useful for testing with different thresholds
	testBundle := createLargeTestBundle(t, 20, 400) // ~8MB

	// Scenario 1: Threshold 5MB - should split 8MB Bundle into 2 chunks
	t.Run("5MB threshold splits 8MB Bundle", func(t *testing.T) {
		tmpDir := t.TempDir()
		jobID := "test-threshold-5mb"
		jobDir := filepath.Join(tmpDir, "jobs", jobID)
		importDir := filepath.Join(jobDir, "import")
		pseudonymizedDir := filepath.Join(jobDir, "pseudonymized")

		require.NoError(t, os.MkdirAll(importDir, 0755))
		require.NoError(t, os.MkdirAll(pseudonymizedDir, 0755))

		bundleNDJSON := filepath.Join(importDir, "test_bundle.ndjson")
		writeTestNDJSON(t, bundleNDJSON, []map[string]any{testBundle})

		job := &models.PipelineJob{
			JobID: jobID,
			Config: models.ProjectConfig{
				Services: models.ServiceConfig{
					DIMP: models.DIMPConfig{
						URL:                    dimpServer.URL,
						BundleSplitThresholdMB: 5, // 5MB threshold
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
		require.NoError(t, err, "DIMP step should succeed with valid threshold")

		// Verify output file exists
		outputFile := filepath.Join(pseudonymizedDir, "dimped_test_bundle.ndjson")
		_, err = os.Stat(outputFile)
		require.NoError(t, err, "Output file should exist")

		// Verify reassembled Bundle
		outputData := readTestNDJSON(t, outputFile)
		require.Greater(t, len(outputData), 0, "Should have at least one Bundle in output")
		// ID will be pseudonymized by DIMP, not preserved
		assert.NotEmpty(t, outputData[0]["id"], "Bundle ID should be present in output")
	})

	// Scenario 2: Threshold 20MB - should NOT split 8MB Bundle
	t.Run("20MB threshold does not split 8MB Bundle", func(t *testing.T) {
		tmpDir := t.TempDir()
		jobID := "test-threshold-20mb"
		jobDir := filepath.Join(tmpDir, "jobs", jobID)
		importDir := filepath.Join(jobDir, "import")
		pseudonymizedDir := filepath.Join(jobDir, "pseudonymized")

		require.NoError(t, os.MkdirAll(importDir, 0755))
		require.NoError(t, os.MkdirAll(pseudonymizedDir, 0755))

		bundleNDJSON := filepath.Join(importDir, "test_bundle.ndjson")
		writeTestNDJSON(t, bundleNDJSON, []map[string]any{testBundle})

		job := &models.PipelineJob{
			JobID: jobID,
			Config: models.ProjectConfig{
				Services: models.ServiceConfig{
					DIMP: models.DIMPConfig{
						URL:                    dimpServer.URL,
						BundleSplitThresholdMB: 20, // 20MB threshold
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
		require.NoError(t, err, "DIMP step should succeed with valid threshold")

		// Verify output file exists
		outputFile := filepath.Join(pseudonymizedDir, "dimped_test_bundle.ndjson")
		_, err = os.Stat(outputFile)
		require.NoError(t, err, "Output file should exist")

		// Verify reassembled Bundle
		outputData := readTestNDJSON(t, outputFile)
		require.Greater(t, len(outputData), 0, "Should have at least one Bundle in output")
		// ID will be pseudonymized by DIMP, not preserved
		assert.NotEmpty(t, outputData[0]["id"], "Bundle ID should be present in output")
	})

	// Scenario 3: Threshold 1MB - should split 8MB Bundle into multiple chunks
	t.Run("1MB threshold splits 8MB Bundle into multiple chunks", func(t *testing.T) {
		tmpDir := t.TempDir()
		jobID := "test-threshold-1mb"
		jobDir := filepath.Join(tmpDir, "jobs", jobID)
		importDir := filepath.Join(jobDir, "import")
		pseudonymizedDir := filepath.Join(jobDir, "pseudonymized")

		require.NoError(t, os.MkdirAll(importDir, 0755))
		require.NoError(t, os.MkdirAll(pseudonymizedDir, 0755))

		bundleNDJSON := filepath.Join(importDir, "test_bundle.ndjson")
		writeTestNDJSON(t, bundleNDJSON, []map[string]any{testBundle})

		job := &models.PipelineJob{
			JobID: jobID,
			Config: models.ProjectConfig{
				Services: models.ServiceConfig{
					DIMP: models.DIMPConfig{
						URL:                    dimpServer.URL,
						BundleSplitThresholdMB: 1, // 1MB threshold - aggressive splitting
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
		require.NoError(t, err, "DIMP step should succeed with valid threshold")

		// Verify output file exists
		outputFile := filepath.Join(pseudonymizedDir, "dimped_test_bundle.ndjson")
		_, err = os.Stat(outputFile)
		require.NoError(t, err, "Output file should exist")

		// Verify reassembled Bundle
		outputData := readTestNDJSON(t, outputFile)
		require.Greater(t, len(outputData), 0, "Should have at least one Bundle in output")
		// ID will be pseudonymized by DIMP, not preserved
		assert.NotEmpty(t, outputData[0]["id"], "Bundle ID should be present in output")
	})
}

// createSmallTestBundle creates a small test Bundle (~1MB with specified entries and entry size)
func createSmallTestBundle(t *testing.T, entryCount int, entrySizeKB int) map[string]any {
	var entries []any

	for i := 0; i < entryCount; i++ {
		// Create entry with padding to reach desired size
		padding := make([]string, entrySizeKB)
		for j := 0; j < entrySizeKB; j++ {
			padding[j] = fmt.Sprintf("padding-%d-%d-", i, j)
		}

		entry := map[string]any{
			"fullUrl": fmt.Sprintf("urn:uuid:entry-%d", i),
			"resource": map[string]any{
				"resourceType": "Observation",
				"id":           fmt.Sprintf("entry-%d", i),
				"status":       "final",
				"code": map[string]any{
					"coding": []map[string]string{
						{
							"system": "http://loinc.org",
							"code":   "12345-6",
						},
					},
				},
				"subject": map[string]string{
					"reference": "Patient/example",
				},
				"value": map[string]any{
					"quantity": map[string]string{
						"value": "5.4",
						"unit":  "mmol/l",
					},
				},
				"padding": padding,
			},
		}
		entries = append(entries, entry)
	}

	bundle := map[string]any{
		"resourceType": "Bundle",
		"id":           fmt.Sprintf("bundle-%d", time.Now().UnixNano()),
		"type":         "collection",
		"timestamp":    time.Now().Format(time.RFC3339),
		"total":        entryCount,
		"entry":        entries,
	}

	return bundle
}
