package unit

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
)

// Unit tests for InputType detection

func TestDetectInputType_LocalDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	testDir := filepath.Join(tmpDir, "test-data")
	err := os.MkdirAll(testDir, 0755)
	require.NoError(t, err)

	// Test detects directory as InputTypeLocal
	inputType, err := lib.DetectInputType(testDir)
	assert.NoError(t, err)
	assert.Equal(t, models.InputTypeLocal, inputType, "Directory should be detected as InputTypeLocal")
}

func TestDetectInputType_HTTPUrl(t *testing.T) {
	// Test detects HTTP URL (not TORCH)
	inputType, err := lib.DetectInputType("http://example.com/data.ndjson")
	assert.NoError(t, err)
	assert.Equal(t, models.InputTypeHTTP, inputType, "HTTP URL should be detected as InputTypeHTTP")
}

func TestDetectInputType_HTTPSUrl(t *testing.T) {
	// Test detects HTTPS URL (not TORCH)
	inputType, err := lib.DetectInputType("https://example.com/data.ndjson")
	assert.NoError(t, err)
	assert.Equal(t, models.InputTypeHTTP, inputType, "HTTPS URL should be detected as InputTypeHTTP")
}

func TestDetectInputType_TORCHUrl(t *testing.T) {
	// Test detects TORCH result URL pattern
	inputType, err := lib.DetectInputType("http://localhost:8080/fhir/extraction/result-123")
	assert.NoError(t, err)
	assert.Equal(t, models.InputTypeTORCHURL, inputType, "TORCH URL should be detected as InputTypeTORCHURL")
}

func TestDetectInputType_TORCHUrlHTTPS(t *testing.T) {
	// Test detects HTTPS TORCH result URL
	inputType, err := lib.DetectInputType("https://torch.example.com/fhir/result/abc-xyz")
	assert.NoError(t, err)
	assert.Equal(t, models.InputTypeTORCHURL, inputType, "HTTPS TORCH URL should be detected as InputTypeTORCHURL")
}

func TestDetectInputType_CRTDLFile(t *testing.T) {
	tmpDir := t.TempDir()
	crtdlFile := filepath.Join(tmpDir, "query.crtdl")

	// Create valid CRTDL file
	validCRTDL := `{
		"cohortDefinition": {
			"version": "1.0",
			"inclusionCriteria": []
		},
		"dataExtraction": {
			"attributeGroups": []
		}
	}`
	err := os.WriteFile(crtdlFile, []byte(validCRTDL), 0644)
	require.NoError(t, err)

	// Test detects CRTDL file
	inputType, err := lib.DetectInputType(crtdlFile)
	assert.NoError(t, err)
	assert.Equal(t, models.InputTypeCRTDL, inputType, "CRTDL file should be detected as InputTypeCRTDL")
}

func TestDetectInputType_JSONFileIsCRTDL(t *testing.T) {
	tmpDir := t.TempDir()
	jsonFile := filepath.Join(tmpDir, "query.json")

	// Create valid CRTDL file with .json extension
	validCRTDL := `{
		"cohortDefinition": {
			"version": "1.0",
			"inclusionCriteria": []
		},
		"dataExtraction": {
			"attributeGroups": []
		}
	}`
	err := os.WriteFile(jsonFile, []byte(validCRTDL), 0644)
	require.NoError(t, err)

	// Test detects CRTDL even with .json extension
	inputType, err := lib.DetectInputType(jsonFile)
	assert.NoError(t, err)
	assert.Equal(t, models.InputTypeCRTDL, inputType, "CRTDL with .json extension should be detected as InputTypeCRTDL")
}

func TestDetectInputType_JSONFileNotCRTDL(t *testing.T) {
	tmpDir := t.TempDir()
	jsonFile := filepath.Join(tmpDir, "data.json")

	// Create JSON file that is NOT a CRTDL
	notCRTDL := `{
		"resourceType": "Patient",
		"id": "example"
	}`
	err := os.WriteFile(jsonFile, []byte(notCRTDL), 0644)
	require.NoError(t, err)

	// Non-CRTDL JSON files default to local type (error occurs during import validation)
	inputType, err := lib.DetectInputType(jsonFile)
	assert.NoError(t, err, "Detection should succeed, validation happens later")
	assert.Equal(t, models.InputTypeLocal, inputType, "Non-CRTDL JSON file should default to local type")
}

func TestDetectInputType_NonExistentPath(t *testing.T) {
	// Non-existent paths default to local type (error occurs during import validation)
	inputType, err := lib.DetectInputType("/path/does/not/exist")
	assert.NoError(t, err, "Detection should succeed, validation happens later")
	assert.Equal(t, models.InputTypeLocal, inputType, "Non-existent path should default to local type")
}

func TestDetectInputType_EmptyString(t *testing.T) {
	// Test returns error for empty input
	_, err := lib.DetectInputType("")
	assert.Error(t, err, "Empty input should error")
	assert.Contains(t, err.Error(), "input source cannot be empty")
}

func TestDetectInputType_InvalidCRTDLExtension(t *testing.T) {
	tmpDir := t.TempDir()
	crtdlFile := filepath.Join(tmpDir, "query.crtdl")

	// Create invalid CRTDL file (missing required fields)
	invalidCRTDL := `{
		"someOtherField": "value"
	}`
	err := os.WriteFile(crtdlFile, []byte(invalidCRTDL), 0644)
	require.NoError(t, err)

	// Invalid CRTDL files with .crtdl extension default to local type
	// (CRTDL validation errors occur during job creation, not input type detection)
	inputType, err := lib.DetectInputType(crtdlFile)
	assert.NoError(t, err, "Detection should succeed, CRTDL validation happens later")
	assert.Equal(t, models.InputTypeLocal, inputType, "Invalid CRTDL should default to local type")
}

func TestDetectInputType_MalformedJSON(t *testing.T) {
	tmpDir := t.TempDir()
	crtdlFile := filepath.Join(tmpDir, "query.crtdl")

	// Create malformed JSON
	malformedJSON := `{
		"cohortDefinition": {
			"version": "1.0"
		// missing closing braces
	`
	err := os.WriteFile(crtdlFile, []byte(malformedJSON), 0644)
	require.NoError(t, err)

	// Malformed JSON files default to local type (error occurs during CRTDL validation)
	inputType, err := lib.DetectInputType(crtdlFile)
	assert.NoError(t, err, "Detection should succeed, JSON parsing errors occur during validation")
	assert.Equal(t, models.InputTypeLocal, inputType, "Malformed JSON should default to local type")
}

func TestDetectInputType_TORCHUrlWithoutFHIRPath(t *testing.T) {
	// Test URL that looks like HTTP but not TORCH pattern
	// Should be classified as regular HTTP URL
	inputType, err := lib.DetectInputType("http://localhost:8080/data/result-123")
	assert.NoError(t, err)
	assert.Equal(t, models.InputTypeHTTP, inputType, "URL without /fhir/ should be HTTP, not TORCH")
}

func TestDetectInputType_CaseInsensitiveTORCHPattern(t *testing.T) {
	// Test TORCH URL detection is case-sensitive for /fhir/ path
	// The spec says /fhir/ pattern - assume case-sensitive per REST API conventions
	inputType, err := lib.DetectInputType("http://localhost:8080/FHIR/extraction/123")
	assert.NoError(t, err)
	// URL with /FHIR/ (uppercase) should NOT match TORCH pattern
	assert.Equal(t, models.InputTypeHTTP, inputType, "URL with uppercase /FHIR/ should be HTTP, not TORCH")
}
