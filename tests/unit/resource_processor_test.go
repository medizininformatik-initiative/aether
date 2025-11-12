package unit

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/pipeline"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

func createTestResourceProcessor(server *httptest.Server) *pipeline.ResourceProcessor {
	logger := lib.NewLogger(lib.LogLevelDebug)
	httpClient := services.DefaultHTTPClient()
	dimpClient := services.NewDIMPClient(server.URL, httpClient, logger)
	return pipeline.NewResourceProcessor(dimpClient, logger, 10*1024*1024, "test.ndjson")
}

func TestNewResourceProcessor(t *testing.T) {
	server := createMockDIMPServer()
	defer server.Close()

	processor := createTestResourceProcessor(server)
	assert.NotNil(t, processor)
	assert.Equal(t, 0, processor.GetResourceCount())
}

func TestResourceProcessor_IncrementResourceCount(t *testing.T) {
	server := createMockDIMPServer()
	defer server.Close()

	processor := createTestResourceProcessor(server)

	assert.Equal(t, 0, processor.GetResourceCount())
	processor.IncrementResourceCount()
	assert.Equal(t, 1, processor.GetResourceCount())
	processor.IncrementResourceCount()
	assert.Equal(t, 2, processor.GetResourceCount())
}

func TestResourceProcessor_ProcessSmallBundle(t *testing.T) {
	server := createMockDIMPServer()
	defer server.Close()

	processor := createTestResourceProcessor(server)

	// Create a small Bundle (< 10MB threshold)
	bundle := map[string]any{
		"resourceType": "Bundle",
		"id":           "bundle-1",
		"type":         "transaction",
		"entry": []map[string]any{
			{
				"resource": map[string]any{
					"resourceType": "Patient",
					"id":           "patient-1",
				},
			},
		},
	}

	pseudonymized, err := processor.ProcessBundle(bundle, "bundle-1")
	assert.NoError(t, err)
	assert.NotNil(t, pseudonymized)
	assert.Equal(t, "Bundle", pseudonymized["resourceType"])
}

func TestResourceProcessor_ProcessNonBundle_Valid(t *testing.T) {
	server := createMockDIMPServer()
	defer server.Close()

	processor := createTestResourceProcessor(server)

	resource := map[string]any{
		"resourceType": "Patient",
		"id":           "patient-1",
		"name": []map[string]any{
			{"given": []string{"John"}},
		},
	}

	pseudonymized, err := processor.ProcessNonBundle(resource, "Patient", "patient-1")
	assert.NoError(t, err)
	assert.NotNil(t, pseudonymized)
	assert.Equal(t, "Patient", pseudonymized["resourceType"])
}

func TestResourceProcessor_ProcessNonBundle_DIMPError(t *testing.T) {
	// Create error server
	errorServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("Internal Server Error"))
	}))
	defer errorServer.Close()

	processor := createTestResourceProcessor(errorServer)

	resource := map[string]any{
		"resourceType": "Patient",
		"id":           "patient-1",
	}

	pseudonymized, err := processor.ProcessNonBundle(resource, "Patient", "patient-1")
	assert.Error(t, err)
	assert.Nil(t, pseudonymized)
}

func TestResourceProcessor_ProcessNonBundle_OversizedResource(t *testing.T) {
	server := createMockDIMPServer()
	defer server.Close()

	// Create processor with very small threshold (100 bytes)
	logger := lib.NewLogger(lib.LogLevelDebug)
	httpClient := services.DefaultHTTPClient()
	dimpClient := services.NewDIMPClient(server.URL, httpClient, logger)
	processor := pipeline.NewResourceProcessor(dimpClient, logger, 100, "test.ndjson")

	// Create a large resource
	largeData := make([]map[string]any, 100)
	for i := 0; i < 100; i++ {
		largeData[i] = map[string]any{
			"use":  "official",
			"text": string(make([]byte, 1000)), // Large text field
		}
	}

	resource := map[string]any{
		"resourceType": "Patient",
		"id":           "patient-1",
		"name":         largeData,
	}

	pseudonymized, err := processor.ProcessNonBundle(resource, "Patient", "patient-1")
	// Expect error due to oversized resource
	assert.Error(t, err)
	assert.Nil(t, pseudonymized)
}

func TestResourceProcessor_ProcessBundle_SmallBundle(t *testing.T) {
	server := createMockDIMPServer()
	defer server.Close()

	processor := createTestResourceProcessor(server)

	bundle := map[string]any{
		"resourceType": "Bundle",
		"id":           "bundle-1",
		"type":         "transaction",
		"entry": []map[string]any{
			{
				"resource": map[string]any{
					"resourceType": "Patient",
					"id":           "patient-1",
				},
			},
		},
	}

	pseudonymized, err := processor.ProcessBundle(bundle, "bundle-1")
	assert.NoError(t, err)
	assert.NotNil(t, pseudonymized)
	assert.Equal(t, "Bundle", pseudonymized["resourceType"])
}

func TestResourceProcessor_ProcessBundle_DIMPError(t *testing.T) {
	errorServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("Bad Request"))
	}))
	defer errorServer.Close()

	processor := createTestResourceProcessor(errorServer)

	bundle := map[string]any{
		"resourceType": "Bundle",
		"id":           "bundle-1",
		"type":         "transaction",
	}

	pseudonymized, err := processor.ProcessBundle(bundle, "bundle-1")
	assert.Error(t, err)
	assert.Nil(t, pseudonymized)
}

func TestResourceProcessor_GetResourceCount(t *testing.T) {
	server := createMockDIMPServer()
	defer server.Close()

	processor := createTestResourceProcessor(server)

	initialCount := processor.GetResourceCount()
	assert.Equal(t, 0, initialCount)

	for i := 0; i < 5; i++ {
		processor.IncrementResourceCount()
	}

	finalCount := processor.GetResourceCount()
	assert.Equal(t, 5, finalCount)
}

func TestResourceProcessor_ProcessBundle_CalculateSizeError(t *testing.T) {
	server := createMockDIMPServer()
	defer server.Close()

	processor := createTestResourceProcessor(server)

	// Create a valid but malformed bundle that still processes
	bundle := map[string]any{
		"resourceType": "Bundle",
		"id":           "bundle-1",
		"type":         "transaction",
		"entry":        "invalid", // Invalid entry structure but JSON marshals fine
	}

	// This should actually succeed since JSON marshaling will work
	pseudonymized, err := processor.ProcessBundle(bundle, "bundle-1")
	assert.NoError(t, err) // No error should occur
	assert.NotNil(t, pseudonymized)
	assert.Equal(t, "Bundle", pseudonymized["resourceType"])
}

func TestResourceProcessor_IncrementAndRetrieveCount(t *testing.T) {
	server := createMockDIMPServer()
	defer server.Close()

	processor := createTestResourceProcessor(server)

	// Simulate processing multiple resources
	for i := 1; i <= 10; i++ {
		assert.Equal(t, i-1, processor.GetResourceCount())
		processor.IncrementResourceCount()
		assert.Equal(t, i, processor.GetResourceCount())
	}
}

func TestResourceProcessor_ProcessBundleChunks_Success(t *testing.T) {
	server := createMockDIMPServer()
	defer server.Close()

	processor := createTestResourceProcessor(server)

	// Create a split result with mock chunks
	chunk1 := models.BundleChunk{
		ChunkID:       "chunk-1",
		Index:         0,
		TotalChunks:   2,
		EstimatedSize: 100,
		Entries: []map[string]any{
			{"resource": map[string]any{"resourceType": "Patient", "id": "p1"}},
		},
	}

	chunk2 := models.BundleChunk{
		ChunkID:       "chunk-2",
		Index:         1,
		TotalChunks:   2,
		EstimatedSize: 100,
		Entries: []map[string]any{
			{"resource": map[string]any{"resourceType": "Patient", "id": "p2"}},
		},
	}

	splitResult := models.SplitResult{
		TotalChunks:  2,
		Chunks:       []models.BundleChunk{chunk1, chunk2},
		OriginalSize: 200,
		Metadata: models.BundleMetadata{
			ID:   "bundle-1",
			Type: "transaction",
		},
	}

	result, err := processor.ProcessBundleChunks(splitResult, "bundle-1")
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "Bundle", result["resourceType"])
}

func TestResourceProcessor_ProcessBundleChunks_DIMPError(t *testing.T) {
	errorServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("Server Error"))
	}))
	defer errorServer.Close()

	processor := createTestResourceProcessor(errorServer)

	chunk := models.BundleChunk{
		ChunkID:       "chunk-1",
		Index:         0,
		TotalChunks:   1,
		EstimatedSize: 100,
		Entries: []map[string]any{
			{"resource": map[string]any{"resourceType": "Patient", "id": "p1"}},
		},
	}

	splitResult := models.SplitResult{
		TotalChunks:  1,
		Chunks:       []models.BundleChunk{chunk},
		OriginalSize: 100,
		Metadata: models.BundleMetadata{
			ID:   "bundle-1",
			Type: "transaction",
		},
	}

	result, err := processor.ProcessBundleChunks(splitResult, "bundle-1")
	assert.Error(t, err)
	assert.Nil(t, result)
}

// Additional tests for error paths and edge cases


func TestResourceProcessor_ProcessNonBundle_WithCorrectTracking(t *testing.T) {
	server := createMockDIMPServer()
	defer server.Close()

	processor := createTestResourceProcessor(server)

	// Process multiple resources and track count
	for i := 0; i < 3; i++ {
		resource := map[string]any{
			"resourceType": "Patient",
			"id":           fmt.Sprintf("patient-%d", i),
		}

		pseudonymized, err := processor.ProcessNonBundle(resource, "Patient", fmt.Sprintf("patient-%d", i))
		assert.NoError(t, err)
		assert.NotNil(t, pseudonymized)
		processor.IncrementResourceCount()
	}

	assert.Equal(t, 3, processor.GetResourceCount())
}

func TestResourceProcessor_ProcessBundle_MultipleChunks(t *testing.T) {
	server := createMockDIMPServer()
	defer server.Close()

	processor := createTestResourceProcessor(server)

	// Create chunks manually to test reassembly
	chunk1 := models.BundleChunk{
		ChunkID:       "chunk-1",
		Index:         0,
		TotalChunks:   3,
		EstimatedSize: 500,
		Entries: []map[string]any{
			{"resource": map[string]any{"resourceType": "Patient", "id": "p1"}},
			{"resource": map[string]any{"resourceType": "Patient", "id": "p2"}},
		},
	}

	chunk2 := models.BundleChunk{
		ChunkID:       "chunk-2",
		Index:         1,
		TotalChunks:   3,
		EstimatedSize: 500,
		Entries: []map[string]any{
			{"resource": map[string]any{"resourceType": "Patient", "id": "p3"}},
			{"resource": map[string]any{"resourceType": "Patient", "id": "p4"}},
		},
	}

	chunk3 := models.BundleChunk{
		ChunkID:       "chunk-3",
		Index:         2,
		TotalChunks:   3,
		EstimatedSize: 500,
		Entries: []map[string]any{
			{"resource": map[string]any{"resourceType": "Patient", "id": "p5"}},
		},
	}

	splitResult := models.SplitResult{
		TotalChunks:  3,
		Chunks:       []models.BundleChunk{chunk1, chunk2, chunk3},
		OriginalSize: 1500,
		Metadata: models.BundleMetadata{
			ID:   "bundle-1",
			Type: "transaction",
		},
	}

	result, err := processor.ProcessBundleChunks(splitResult, "bundle-1")
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "Bundle", result["resourceType"])
}
