package lib

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
)

// TestValidateSplitConfig tests Bundle split threshold validation
func TestValidateSplitConfig(t *testing.T) {
	testCases := []struct {
		name          string
		thresholdMB   int
		expectError   bool
		errorContains string
	}{
		{
			name:        "Valid threshold - 1MB",
			thresholdMB: 1,
			expectError: false,
		},
		{
			name:        "Valid threshold - 10MB (default)",
			thresholdMB: 10,
			expectError: false,
		},
		{
			name:        "Valid threshold - 50MB",
			thresholdMB: 50,
			expectError: false,
		},
		{
			name:        "Valid threshold - 100MB (max)",
			thresholdMB: 100,
			expectError: false,
		},
		{
			name:          "Invalid - zero",
			thresholdMB:   0,
			expectError:   true,
			errorContains: "must be > 0",
		},
		{
			name:          "Invalid - negative",
			thresholdMB:   -5,
			expectError:   true,
			errorContains: "must be > 0",
		},
		{
			name:          "Invalid - exceeds max (101MB)",
			thresholdMB:   101,
			expectError:   true,
			errorContains: "must be <= 100MB",
		},
		{
			name:          "Invalid - far exceeds max (1000MB)",
			thresholdMB:   1000,
			expectError:   true,
			errorContains: "must be <= 100MB",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := lib.ValidateSplitConfig(tc.thresholdMB)

			if tc.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.errorContains)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestDetectInputType tests input source type detection
func TestDetectInputType(t *testing.T) {
	// Create temporary test directory
	tmpDir := t.TempDir()

	testCases := []struct {
		name          string
		inputSource   string
		expectedType  models.InputType
		expectError   bool
		errorContains string
		setup         func() string // Returns actual input path
	}{
		{
			name:         "Empty input",
			inputSource:  "",
			expectedType: "",
			expectError:  true,
		},
		{
			name:        "Local directory",
			inputSource: "",
			setup: func() string {
				return tmpDir
			},
			expectedType: models.InputTypeLocal,
			expectError:  false,
		},
		{
			name:         "HTTP URL",
			inputSource:  "http://example.com/data",
			expectedType: models.InputTypeHTTP,
			expectError:  false,
		},
		{
			name:         "HTTPS URL",
			inputSource:  "https://example.com/data",
			expectedType: models.InputTypeHTTP,
			expectError:  false,
		},
		{
			name:         "TORCH extraction URL",
			inputSource:  "https://torch.example.com/fhir/extraction/12345",
			expectedType: models.InputTypeTORCHURL,
			expectError:  false,
		},
		{
			name:         "TORCH result URL",
			inputSource:  "https://torch.example.com/fhir/result/abc-def-123",
			expectedType: models.InputTypeTORCHURL,
			expectError:  false,
		},
		{
			name:        "Valid CRTDL file",
			inputSource: "",
			setup: func() string {
				crtdlPath := filepath.Join(tmpDir, "test.json")
				crtdlContent := `{
					"cohortDefinition": {
						"inclusionCriteria": [[]]
					},
					"dataExtraction": {
						"attributeGroups": []
					}
				}`
				_ = os.WriteFile(crtdlPath, []byte(crtdlContent), 0644)
				return crtdlPath
			},
			expectedType: models.InputTypeCRTDL,
			expectError:  false,
		},
		{
			name:        "Invalid CRTDL file (wrong structure)",
			inputSource: "",
			setup: func() string {
				crtdlPath := filepath.Join(tmpDir, "invalid.json")
				crtdlContent := `{"invalid": "structure"}`
				_ = os.WriteFile(crtdlPath, []byte(crtdlContent), 0644)
				return crtdlPath
			},
			expectError:   true,
			errorContains: "cohortDefinition",
		},
		{
			name:        "JSON file with CRTDL structure",
			inputSource: "",
			setup: func() string {
				jsonPath := filepath.Join(tmpDir, "crtdl.json")
				jsonContent := `{
					"cohortDefinition": {
						"inclusionCriteria": [[]]
					},
					"dataExtraction": {
						"attributeGroups": []
					}
				}`
				_ = os.WriteFile(jsonPath, []byte(jsonContent), 0644)
				return jsonPath
			},
			expectedType: models.InputTypeCRTDL,
			expectError:  false,
		},
		{
			name:         "Non-existent path (defaults to local)",
			inputSource:  "/non/existent/path",
			expectedType: models.InputTypeLocal,
			expectError:  false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			inputSource := tc.inputSource
			if tc.setup != nil {
				inputSource = tc.setup()
			}

			result, err := lib.DetectInputType(inputSource)

			if tc.expectError {
				require.Error(t, err)
				if tc.errorContains != "" {
					assert.Contains(t, err.Error(), tc.errorContains)
				}
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.expectedType, result)
			}
		})
	}
}

// TestIsCRTDLFile tests CRTDL file detection
func TestIsCRTDLFile(t *testing.T) {
	tmpDir := t.TempDir()

	testCases := []struct {
		name      string
		content   string
		isCRTDL   bool
		setupFile func() string
	}{
		{
			name: "Valid CRTDL",
			content: `{
				"cohortDefinition": {
					"inclusionCriteria": [[]]
				},
				"dataExtraction": {
					"attributeGroups": []
				}
			}`,
			isCRTDL: true,
		},
		{
			name: "Missing cohortDefinition",
			content: `{
				"dataExtraction": {
					"attributeGroups": []
				}
			}`,
			isCRTDL: false,
		},
		{
			name: "Missing dataExtraction",
			content: `{
				"cohortDefinition": {
					"inclusionCriteria": [[]]
				}
			}`,
			isCRTDL: false,
		},
		{
			name:    "Empty JSON object",
			content: `{}`,
			isCRTDL: false,
		},
		{
			name:    "Invalid JSON",
			content: `{invalid json}`,
			isCRTDL: false,
		},
		{
			name: "FHIR Parameters format (not supported)",
			content: `{
				"resourceType": "Parameters",
				"parameter": []
			}`,
			isCRTDL: false,
		},
		{
			name:      "Non-existent file",
			setupFile: func() string { return "/non/existent/file.json" },
			isCRTDL:   false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var filePath string
			if tc.setupFile != nil {
				filePath = tc.setupFile()
			} else {
				filePath = filepath.Join(tmpDir, "test.json")
				err := os.WriteFile(filePath, []byte(tc.content), 0644)
				require.NoError(t, err)
			}

			result := lib.IsCRTDLFile(filePath)
			assert.Equal(t, tc.isCRTDL, result)
		})
	}
}

// TestIsCRTDLFileWithHint tests CRTDL detection with hint messages
func TestIsCRTDLFileWithHint(t *testing.T) {
	tmpDir := t.TempDir()

	testCases := []struct {
		name         string
		content      string
		expectValid  bool
		hintContains string
		setupFile    func() string
	}{
		{
			name: "Valid CRTDL",
			content: `{
				"cohortDefinition": {"inclusionCriteria": [[]]},
				"dataExtraction": {"attributeGroups": []}
			}`,
			expectValid:  true,
			hintContains: "",
		},
		{
			name:         "Missing both keys",
			content:      `{"other": "data"}`,
			expectValid:  false,
			hintContains: "missing both",
		},
		{
			name: "Missing cohortDefinition",
			content: `{
				"dataExtraction": {"attributeGroups": []}
			}`,
			expectValid:  false,
			hintContains: "missing 'cohortDefinition'",
		},
		{
			name: "Missing dataExtraction",
			content: `{
				"cohortDefinition": {"inclusionCriteria": [[]]}
			}`,
			expectValid:  false,
			hintContains: "missing 'dataExtraction'",
		},
		{
			name: "FHIR Parameters format",
			content: `{
				"resourceType": "Parameters",
				"parameter": []
			}`,
			expectValid:  false,
			hintContains: "FHIR Parameters format",
		},
		{
			name:         "Invalid JSON",
			content:      `{invalid}`,
			expectValid:  false,
			hintContains: "not valid JSON",
		},
		{
			name:         "Non-existent file",
			setupFile:    func() string { return "/non/existent/file.json" },
			expectValid:  false,
			hintContains: "cannot read file",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var filePath string
			if tc.setupFile != nil {
				filePath = tc.setupFile()
			} else {
				filePath = filepath.Join(tmpDir, "test.json")
				err := os.WriteFile(filePath, []byte(tc.content), 0644)
				require.NoError(t, err)
			}

			isValid, hint := lib.IsCRTDLFileWithHint(filePath)
			assert.Equal(t, tc.expectValid, isValid)

			if !tc.expectValid && tc.hintContains != "" {
				assert.Contains(t, hint, tc.hintContains)
			}
		})
	}
}

// TestDetectInputType_Integration tests detection with real file system scenarios
func TestDetectInputType_Integration(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("Directory with FHIR files", func(t *testing.T) {
		dataDir := filepath.Join(tmpDir, "fhir-data")
		_ = os.MkdirAll(dataDir, 0755)

		// Create some FHIR files
		_ = os.WriteFile(filepath.Join(dataDir, "patient.ndjson"), []byte(`{"resourceType":"Patient"}`), 0644)

		inputType, err := lib.DetectInputType(dataDir)
		require.NoError(t, err)
		assert.Equal(t, models.InputTypeLocal, inputType)
	})

	t.Run("CRTDL file with .json extension", func(t *testing.T) {
		crtdlPath := filepath.Join(tmpDir, "crtdl.json")
		crtdlContent := `{
			"cohortDefinition": {"inclusionCriteria": [[]]},
			"dataExtraction": {"attributeGroups": []}
		}`
		_ = os.WriteFile(crtdlPath, []byte(crtdlContent), 0644)

		inputType, err := lib.DetectInputType(crtdlPath)
		require.NoError(t, err)
		assert.Equal(t, models.InputTypeCRTDL, inputType)
	})

	t.Run("CRTDL file with .json extension", func(t *testing.T) {
		jsonPath := filepath.Join(tmpDir, "crtdl.json")
		jsonContent := `{
			"cohortDefinition": {"inclusionCriteria": [[]]},
			"dataExtraction": {"attributeGroups": []}
		}`
		_ = os.WriteFile(jsonPath, []byte(jsonContent), 0644)

		inputType, err := lib.DetectInputType(jsonPath)
		require.NoError(t, err)
		assert.Equal(t, models.InputTypeCRTDL, inputType)
	})

	t.Run("Regular JSON file (not CRTDL)", func(t *testing.T) {
		jsonPath := filepath.Join(tmpDir, "config.json")
		jsonContent := `{"setting": "value"}`
		_ = os.WriteFile(jsonPath, []byte(jsonContent), 0644)

		// .json with non-CRTDL content surfaces structural hint rather than silently demoting to local
		_, err := lib.DetectInputType(jsonPath)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cohortDefinition")
	})
}
