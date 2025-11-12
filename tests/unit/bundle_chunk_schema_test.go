package unit

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xeipuuv/gojsonschema"

	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

// TestBundleChunkSchema validates that Bundle chunks created by splitting
// conform to the FHIR Bundle chunk schema defined in contracts/bundle-chunk.json
//
// Contract test: Bundle chunk schema validation
// Purpose: Ensure all chunks pass JSON schema validation
func TestBundleChunkSchema(t *testing.T) {
	// Load the JSON schema from contracts directory
	schemaPath := "bundle-chunk.json"
	// Try absolute path if relative fails
	schemaBytes, err := os.ReadFile(schemaPath)
	if err != nil {
		// Try from workspace root
		schemaPath = "bundle-chunk.json"
		schemaBytes, err = os.ReadFile(schemaPath)
	}
	require.NoError(t, err, "Failed to read bundle-chunk.json schema")

	// Parse schema
	schemaLoader := gojsonschema.NewBytesLoader(schemaBytes)
	schema, err := gojsonschema.NewSchema(schemaLoader)
	require.NoError(t, err, "Failed to parse bundle-chunk.json schema")

	t.Run("Single chunk from small Bundle", func(t *testing.T) {
		// Create a small test Bundle that won't be split
		bundle := CreateTestBundle(10, 1)   // 10 entries, 1KB each
		thresholdBytes := 100 * 1024 * 1024 // 100MB - large enough not to split

		// Split the Bundle (should result in single chunk)
		splitResult, err := services.SplitBundle(bundle, thresholdBytes)
		require.NoError(t, err)
		require.Equal(t, 1, len(splitResult.Chunks), "Expected 1 chunk for small Bundle")

		// Convert chunk to FHIR Bundle
		chunkBundle := models.ConvertChunkToBundle(splitResult.Chunks[0])

		// Validate chunk against schema
		dataLoader := gojsonschema.NewBytesLoader(mustMarshalJSON(chunkBundle))
		result, err := schema.Validate(dataLoader)
		require.NoError(t, err, "Schema validation failed")
		assert.True(t, result.Valid(), fmt.Sprintf("Chunk failed schema validation: %v", result.Errors()))
	})

	t.Run("Multiple chunks from large Bundle", func(t *testing.T) {
		// Create a medium-sized test Bundle that will be split
		bundle := CreateTestBundle(100, 100) // 100 entries, ~100KB each = ~10MB
		thresholdBytes := 2 * 1024 * 1024    // 2MB threshold - should create multiple chunks

		// Split the Bundle
		splitResult, err := services.SplitBundle(bundle, thresholdBytes)
		require.NoError(t, err)
		require.Greater(t, len(splitResult.Chunks), 1, "Expected multiple chunks for large Bundle")

		// Validate each chunk against schema
		for i, chunk := range splitResult.Chunks {
			chunkBundle := models.ConvertChunkToBundle(chunk)
			dataLoader := gojsonschema.NewBytesLoader(mustMarshalJSON(chunkBundle))
			result, err := schema.Validate(dataLoader)
			require.NoError(t, err, fmt.Sprintf("Schema validation failed for chunk %d", i))
			assert.True(t, result.Valid(), fmt.Sprintf("Chunk %d failed schema validation: %v", i, result.Errors()))
		}
	})

	t.Run("Chunk ID format validation", func(t *testing.T) {
		// Create test Bundle with known ID
		bundle := CreateTestBundle(10, 1)
		thresholdBytes := 100 * 1024 * 1024

		splitResult, err := services.SplitBundle(bundle, thresholdBytes)
		require.NoError(t, err)

		// Verify chunk ID format
		for i, chunk := range splitResult.Chunks {
			chunkBundle := models.ConvertChunkToBundle(chunk)
			id := chunkBundle["id"].(string)

			// ID should be in format: {originalID}-chunk-{index}
			assert.Contains(t, id, "-chunk-", "Chunk ID should contain '-chunk-' separator")
			assert.NotEmpty(t, id, "Chunk ID should not be empty")

			// Validate this passes schema's pattern validation
			dataLoader := gojsonschema.NewBytesLoader(mustMarshalJSON(chunkBundle))
			result, err := schema.Validate(dataLoader)
			require.NoError(t, err)
			assert.True(t, result.Valid(), fmt.Sprintf("Chunk %d failed ID format validation", i))
		}
	})

	t.Run("Chunk respects FHIR bundle type rules for total field", func(t *testing.T) {
		// Create test Bundle (collection type - should NOT have total field per FHIR R4 spec)
		bundle := CreateTestBundle(50, 50) // 50 entries, collection type
		thresholdBytes := 1 * 1024 * 1024  // 1MB threshold

		splitResult, err := services.SplitBundle(bundle, thresholdBytes)
		require.NoError(t, err)

		// Verify each chunk follows FHIR R4 rules: collection bundles must NOT have total field
		for i, chunk := range splitResult.Chunks {
			chunkBundle := models.ConvertChunkToBundle(chunk)

			// Verify bundle type
			bundleType, ok := chunkBundle["type"].(string)
			require.True(t, ok, fmt.Sprintf("Chunk %d: type must be string", i))
			assert.Equal(t, "collection", bundleType, fmt.Sprintf("Chunk %d: should preserve collection type", i))

			// For collection bundles, total field must NOT be present (FHIR R4 invariant: "total only when a search or history")
			_, hasTotal := chunkBundle["total"]
			assert.False(t, hasTotal, fmt.Sprintf("Chunk %d: collection bundle must NOT have 'total' field per FHIR R4 spec", i))

			// Verify entries exist
			var entryCount int
			switch v := chunkBundle["entry"].(type) {
			case []any:
				entryCount = len(v)
			case []map[string]any:
				entryCount = len(v)
			default:
				t.Fatalf("Chunk %d: unexpected type for entry field: %T", i, v)
			}
			assert.Greater(t, entryCount, 0, fmt.Sprintf("Chunk %d: should have at least one entry", i))

			// Schema validation should pass
			dataLoader := gojsonschema.NewBytesLoader(mustMarshalJSON(chunkBundle))
			result, err := schema.Validate(dataLoader)
			require.NoError(t, err)
			assert.True(t, result.Valid(), fmt.Sprintf("Chunk %d: should pass FHIR schema validation", i))
		}
	})

	t.Run("Chunk preserves Bundle type", func(t *testing.T) {
		// Create test Bundle with explicit type
		bundle := CreateTestBundle(10, 1)
		bundle["type"] = "collection" // Set specific type
		thresholdBytes := 100 * 1024 * 1024

		splitResult, err := services.SplitBundle(bundle, thresholdBytes)
		require.NoError(t, err)

		// Verify chunk preserves type
		for i, chunk := range splitResult.Chunks {
			chunkBundle := models.ConvertChunkToBundle(chunk)
			bundleType := chunkBundle["type"].(string)

			assert.Equal(t, "collection", bundleType, fmt.Sprintf("Chunk %d should preserve Bundle type", i))

			// Schema validation should pass
			dataLoader := gojsonschema.NewBytesLoader(mustMarshalJSON(chunkBundle))
			result, err := schema.Validate(dataLoader)
			require.NoError(t, err)
			assert.True(t, result.Valid())
		}
	})

	t.Run("Chunk is valid FHIR Bundle structure", func(t *testing.T) {
		bundle := CreateTestBundle(25, 25)
		thresholdBytes := 500 * 1024 // 500KB

		splitResult, err := services.SplitBundle(bundle, thresholdBytes)
		require.NoError(t, err)

		for _, chunk := range splitResult.Chunks {
			chunkBundle := models.ConvertChunkToBundle(chunk)

			// Must have resourceType = "Bundle"
			assert.Equal(t, "Bundle", chunkBundle["resourceType"].(string))

			// Must have type field
			assert.NotEmpty(t, chunkBundle["type"])

			// Must have entry array with at least one entry
			var entries []map[string]any
			switch v := chunkBundle["entry"].(type) {
			case []any:
				for _, entryRaw := range v {
					entries = append(entries, entryRaw.(map[string]any))
				}
			case []map[string]any:
				entries = v
			default:
				t.Fatalf("Unexpected type for entry field: %T", v)
			}
			assert.GreaterOrEqual(t, len(entries), 1)

			// Each entry must have resource with resourceType
			for _, entry := range entries {
				resource := entry["resource"].(map[string]any)
				assert.NotEmpty(t, resource["resourceType"], "Entry resource must have resourceType")
			}

			// Schema validation
			dataLoader := gojsonschema.NewBytesLoader(mustMarshalJSON(chunkBundle))
			result, err := schema.Validate(dataLoader)
			require.NoError(t, err)
			assert.True(t, result.Valid())
		}
	})
}

// mustMarshalJSON marshals data to JSON bytes, panicking on error
func mustMarshalJSON(data any) []byte {
	bytes, err := json.Marshal(data)
	if err != nil {
		panic(fmt.Sprintf("Failed to marshal JSON: %v", err))
	}
	return bytes
}
