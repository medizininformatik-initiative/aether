package unit

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
)

// TestDetectOversizedResource verifies that oversized non-Bundle resources are detected
// Unit test for oversized resource detection
func TestDetectOversizedResource(t *testing.T) {
	thresholdBytes := 10 * 1024 * 1024 // 10MB threshold

	testCases := []struct {
		name            string
		resource        map[string]any
		threshold       int
		expectError     bool
		errorContains   string
		expectedMinSize int
		expectedMaxSize int
	}{
		{
			name: "Patient resource - normal size (100KB)",
			resource: map[string]any{
				"resourceType": "Patient",
				"id":           "pat-001",
				"name": []map[string]any{
					{
						"given":  []string{"John", "James"},
						"family": "Doe",
					},
				},
				"birthDate": "1990-01-01",
				"address": []map[string]any{
					{
						"line":       []string{"123 Main St"},
						"city":       "Springfield",
						"state":      "IL",
						"postalCode": "62701",
					},
				},
			},
			threshold:       thresholdBytes,
			expectError:     false,
			expectedMinSize: 0,
			expectedMaxSize: 1024 * 1024, // Should be much smaller than 1MB
		},
		{
			name: "Observation resource - normal size (500KB)",
			resource: func() map[string]any {
				obs := CreateTestBundle(1, 500)
				obs["resourceType"] = "Observation"
				obs["id"] = "obs-001"
				return obs
			}(),
			threshold:       thresholdBytes,
			expectError:     false,
			expectedMinSize: 0,
			expectedMaxSize: 1024 * 1024, // Much smaller than threshold
		},
		{
			name: "Oversized Observation resource (35MB)",
			resource: func() map[string]any {
				// Create an oversized observation (35MB)
				obs := CreateTestBundle(50, 700) // ~35MB
				obs["resourceType"] = "Observation"
				obs["id"] = "obs-oversized-001"
				return obs
			}(),
			threshold:       thresholdBytes,
			expectError:     true,
			errorContains:   "exceeds threshold",
			expectedMinSize: 30 * 1024 * 1024,
			expectedMaxSize: 40 * 1024 * 1024,
		},
		{
			name: "Condition resource - normal size (500KB)",
			resource: func() map[string]any {
				cond := CreateTestBundle(1, 500)
				cond["resourceType"] = "Condition"
				cond["id"] = "cond-001"
				return cond
			}(),
			threshold:       thresholdBytes,
			expectError:     false,
			expectedMinSize: 0,
			expectedMaxSize: 1024 * 1024,
		},
		{
			name: "Bundle resource - should NOT be flagged (Bundles handled by automatic Bundle splitting feature)",
			resource: func() map[string]any {
				// Even if large, Bundles are NOT flagged by DetectOversizedResource
				// They're handled by the splitting logic in automatic Bundle splitting feature
				bundle := CreateTestBundle(100, 400) // ~40MB
				bundle["resourceType"] = "Bundle"
				bundle["id"] = "bundle-large-001"
				return bundle
			}(),
			threshold:       thresholdBytes,
			expectError:     false,
			expectedMinSize: 0,
			expectedMaxSize: 100 * 1024 * 1024, // Bundles are not checked
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := lib.DetectOversizedResource(tc.resource, tc.threshold)

			if tc.expectError {
				require.NotNil(t, result, "Expected error but got none")
				assert.Contains(t, result.Error(), tc.errorContains)

				// Verify error has proper fields
				assert.NotEmpty(t, result.ResourceType)
				assert.NotEmpty(t, result.ResourceID)
				assert.Greater(t, result.Size, tc.expectedMinSize)
				if tc.expectedMaxSize > 0 {
					assert.Less(t, result.Size, tc.expectedMaxSize)
				}
				assert.Equal(t, result.Threshold, tc.threshold)
				assert.NotEmpty(t, result.Guidance)
			} else {
				assert.Nil(t, result, "Expected no error but got one: %v", result)
			}
		})
	}
}

// TestDetectOversizedResource_BundleExclusion verifies that Bundles are NOT flagged
// even if they exceed the threshold (handled by splitting in automatic Bundle splitting feature)
func TestDetectOversizedResource_BundleExclusion(t *testing.T) {
	thresholdBytes := 1 * 1024 * 1024 // 1MB threshold

	// Create a 10MB Bundle
	largeBundle := CreateTestBundle(50, 200)
	largeBundle["resourceType"] = "Bundle"
	largeBundle["id"] = "large-bundle-001"

	result := lib.DetectOversizedResource(largeBundle, thresholdBytes)

	// Bundle should NOT be flagged even though it's 10MB > 1MB threshold
	assert.Nil(t, result, "Bundles should not be flagged for oversized - they're handled by splitting")
}

// TestDetectOversizedResource_ThresholdBoundary verifies edge cases
func TestDetectOversizedResource_ThresholdBoundary(t *testing.T) {
	// Create a resource that's right at the threshold
	resource := CreateTestBundle(5, 100) // Should be ~5MB
	resource["resourceType"] = "Patient"
	resource["id"] = "boundary-test"

	size, err := models.CalculateJSONSize(resource)
	require.NoError(t, err)

	// Test with threshold BELOW the resource size
	result := lib.DetectOversizedResource(resource, size-1)
	assert.NotNil(t, result, "Resource should be flagged when size > threshold")

	// Test with threshold EQUAL to the resource size
	result = lib.DetectOversizedResource(resource, size)
	assert.Nil(t, result, "Resource should NOT be flagged when size == threshold (must be >)")

	// Test with threshold ABOVE the resource size
	result = lib.DetectOversizedResource(resource, size+1)
	assert.Nil(t, result, "Resource should NOT be flagged when size < threshold")
}

// TestDetectOversizedResource_ErrorMessage verifies error messages are helpful
func TestDetectOversizedResource_ErrorMessage(t *testing.T) {
	resource := CreateTestBundle(50, 400) // ~40MB
	resource["resourceType"] = "Observation"
	resource["id"] = "obs-help-test"

	thresholdBytes := 10 * 1024 * 1024 // 10MB

	result := lib.DetectOversizedResource(resource, thresholdBytes)
	require.NotNil(t, result)

	errorMsg := result.Error()
	assert.Contains(t, errorMsg, "Observation")
	assert.Contains(t, errorMsg, "obs-help-test")
	assert.Contains(t, errorMsg, "exceeds threshold")
	assert.NotEmpty(t, result.Guidance, "Error should include actionable guidance")

	// Verify guidance contains helpful info
	assert.Contains(t, errorMsg, result.Guidance)
}
