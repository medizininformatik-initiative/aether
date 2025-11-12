package unit

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

// TestImportFromLocalDirectory_Success verifies successful import from a valid local directory
func TestImportFromLocalDirectory_Success(t *testing.T) {
	// Setup temporary test directories
	tempDir := t.TempDir()
	sourceDir := filepath.Join(tempDir, "source")
	destDir := filepath.Join(tempDir, "dest")

	// Create source directory with test FHIR files
	require.NoError(t, os.MkdirAll(sourceDir, 0755))

	// Create test NDJSON files
	testFiles := map[string]string{
		"Patient.ndjson":     `{"resourceType":"Patient","id":"1"}`,
		"Observation.ndjson": `{"resourceType":"Observation","id":"1"}`,
		"Bundle.ndjson":      `{"resourceType":"Bundle","id":"1"}`,
	}

	for filename, content := range testFiles {
		path := filepath.Join(sourceDir, filename)
		require.NoError(t, os.WriteFile(path, []byte(content), 0644))
	}

	// Create logger
	logger := lib.NewLogger(lib.LogLevelInfo)

	// Execute import
	importedFiles, err := services.ImportFromLocalDirectory(sourceDir, destDir, logger)

	// Verify results
	assert.NoError(t, err, "Import should succeed")
	assert.Len(t, importedFiles, 3, "Should import all 3 FHIR files")

	// Verify files were copied
	for _, imported := range importedFiles {
		destPath := filepath.Join(destDir, imported.FileName)
		assert.FileExists(t, destPath, "File should be copied to destination")

		// Verify metadata
		assert.NotEmpty(t, imported.FileName, "FileName should be set")
		assert.Greater(t, imported.FileSize, int64(0), "FileSize should be > 0")
		assert.Equal(t, models.StepLocalImport, imported.SourceStep, "SourceStep should be import")
		assert.Equal(t, 1, imported.LineCount, "LineCount should be 1 for single-line files")
	}

	// Verify resource types are extracted correctly
	resourceTypes := make(map[string]bool)
	for _, file := range importedFiles {
		resourceTypes[file.ResourceType] = true
	}
	assert.True(t, resourceTypes["Patient"], "Patient resource type should be identified")
	assert.True(t, resourceTypes["Observation"], "Observation resource type should be identified")
	assert.True(t, resourceTypes["Bundle"], "Bundle resource type should be identified")
}

// TestImportFromLocalDirectory_NonexistentDirectory verifies error handling for missing source directory
func TestImportFromLocalDirectory_NonexistentDirectory(t *testing.T) {
	tempDir := t.TempDir()
	sourceDir := filepath.Join(tempDir, "nonexistent")
	destDir := filepath.Join(tempDir, "dest")
	logger := lib.NewLogger(lib.LogLevelInfo)

	// Execute import
	importedFiles, err := services.ImportFromLocalDirectory(sourceDir, destDir, logger)

	// Verify error
	assert.Error(t, err, "Should fail for nonexistent directory")
	assert.Contains(t, err.Error(), "does not exist", "Error should mention nonexistent directory")
	assert.Nil(t, importedFiles, "Should not return files on error")
}

// TestImportFromLocalDirectory_NotADirectory verifies error handling when source is a file, not directory
func TestImportFromLocalDirectory_NotADirectory(t *testing.T) {
	tempDir := t.TempDir()
	sourceFile := filepath.Join(tempDir, "file.txt")
	destDir := filepath.Join(tempDir, "dest")
	logger := lib.NewLogger(lib.LogLevelInfo)

	// Create source as a file, not directory
	require.NoError(t, os.WriteFile(sourceFile, []byte("test"), 0644))

	// Execute import
	importedFiles, err := services.ImportFromLocalDirectory(sourceFile, destDir, logger)

	// Verify error
	assert.Error(t, err, "Should fail when source is not a directory")
	assert.Contains(t, err.Error(), "not a directory", "Error should mention path is not a directory")
	assert.Nil(t, importedFiles, "Should not return files on error")
}

// TestImportFromLocalDirectory_NoNDJSONFiles verifies error handling when directory has no FHIR files
func TestImportFromLocalDirectory_NoNDJSONFiles(t *testing.T) {
	tempDir := t.TempDir()
	sourceDir := filepath.Join(tempDir, "source")
	destDir := filepath.Join(tempDir, "dest")
	logger := lib.NewLogger(lib.LogLevelInfo)

	// Create empty source directory
	require.NoError(t, os.MkdirAll(sourceDir, 0755))

	// Create non-FHIR files
	require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "readme.txt"), []byte("test"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "data.json"), []byte("{}"), 0644))

	// Execute import
	importedFiles, err := services.ImportFromLocalDirectory(sourceDir, destDir, logger)

	// Verify error
	assert.Error(t, err, "Should fail when no NDJSON files found")
	assert.Contains(t, err.Error(), "no FHIR NDJSON files found", "Error should mention no FHIR files")
	assert.Nil(t, importedFiles, "Should not return files on error")
}

// TestImportFromLocalDirectory_RecursiveScan verifies that subdirectories are scanned
func TestImportFromLocalDirectory_RecursiveScan(t *testing.T) {
	tempDir := t.TempDir()
	sourceDir := filepath.Join(tempDir, "source")
	destDir := filepath.Join(tempDir, "dest")
	logger := lib.NewLogger(lib.LogLevelInfo)

	// Create nested directory structure
	require.NoError(t, os.MkdirAll(filepath.Join(sourceDir, "subdir1"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(sourceDir, "subdir2"), 0755))

	// Create NDJSON files in different locations
	testFiles := map[string]string{
		"Patient.ndjson":             `{"resourceType":"Patient"}`,
		"subdir1/Observation.ndjson": `{"resourceType":"Observation"}`,
		"subdir2/Encounter.ndjson":   `{"resourceType":"Encounter"}`,
	}

	for relPath, content := range testFiles {
		fullPath := filepath.Join(sourceDir, relPath)
		require.NoError(t, os.WriteFile(fullPath, []byte(content), 0644))
	}

	// Execute import
	importedFiles, err := services.ImportFromLocalDirectory(sourceDir, destDir, logger)

	// Verify all files are found recursively
	assert.NoError(t, err, "Import should succeed")
	assert.Len(t, importedFiles, 3, "Should find all 3 NDJSON files recursively")
}

// TestImportFromLocalDirectory_MultilineFiles verifies correct line counting
func TestImportFromLocalDirectory_MultilineFiles(t *testing.T) {
	tempDir := t.TempDir()
	sourceDir := filepath.Join(tempDir, "source")
	destDir := filepath.Join(tempDir, "dest")
	logger := lib.NewLogger(lib.LogLevelInfo)

	require.NoError(t, os.MkdirAll(sourceDir, 0755))

	// Create multi-line NDJSON file (3 resources)
	multilineContent := `{"resourceType":"Patient","id":"1"}
{"resourceType":"Patient","id":"2"}
{"resourceType":"Patient","id":"3"}`

	require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "Patient.ndjson"), []byte(multilineContent), 0644))

	// Execute import
	importedFiles, err := services.ImportFromLocalDirectory(sourceDir, destDir, logger)

	// Verify line count
	assert.NoError(t, err, "Import should succeed")
	require.Len(t, importedFiles, 1, "Should import 1 file")
	assert.Equal(t, 3, importedFiles[0].LineCount, "Should count 3 lines/resources")
}

// TestValidateImportSource_LocalDirectory tests input validation for local directories
func TestValidateImportSource_LocalDirectory(t *testing.T) {
	tempDir := t.TempDir()

	tests := []struct {
		name        string
		setupFunc   func() string
		expectError bool
		errorMsg    string
	}{
		{
			name: "Valid directory with NDJSON files",
			setupFunc: func() string {
				dir := filepath.Join(tempDir, "valid")
				_ = os.MkdirAll(dir, 0755)
				_ = os.WriteFile(filepath.Join(dir, "test.ndjson"), []byte("{}"), 0644)
				return dir
			},
			expectError: false,
		},
		{
			name: "Nonexistent directory",
			setupFunc: func() string {
				return filepath.Join(tempDir, "nonexistent")
			},
			expectError: true,
			errorMsg:    "does not exist",
		},
		{
			name: "Path is a file, not directory",
			setupFunc: func() string {
				file := filepath.Join(tempDir, "file.txt")
				_ = os.WriteFile(file, []byte("test"), 0644)
				return file
			},
			expectError: true,
			errorMsg:    "expected directory but got file",
		},
		{
			name: "Directory with no NDJSON files",
			setupFunc: func() string {
				dir := filepath.Join(tempDir, "empty")
				_ = os.MkdirAll(dir, 0755)
				_ = os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("test"), 0644)
				return dir
			},
			expectError: true,
			errorMsg:    "no FHIR NDJSON files",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sourcePath := tt.setupFunc()
			err := services.ValidateImportSource(sourcePath, models.InputTypeLocal)

			if tt.expectError {
				assert.Error(t, err, "Should return error")
				assert.Contains(t, err.Error(), tt.errorMsg, "Error message should be descriptive")
			} else {
				assert.NoError(t, err, "Should not return error")
			}
		})
	}
}

// TestValidateImportSource_HTTPValidation tests HTTP URL validation
func TestValidateImportSource_HTTPValidation(t *testing.T) {
	tests := []struct {
		name        string
		url         string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "Valid HTTP URL",
			url:         "http://example.com/data.ndjson",
			expectError: false,
		},
		{
			name:        "Valid HTTPS URL",
			url:         "https://secure.example.com/api/data",
			expectError: false,
		},
		{
			name:        "Empty URL",
			url:         "",
			expectError: true,
			errorMsg:    "cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := services.ValidateImportSource(tt.url, models.InputTypeHTTP)

			if tt.expectError {
				assert.Error(t, err, "Should return error for: "+tt.name)
				assert.Contains(t, err.Error(), tt.errorMsg, "Error message should match")
			} else {
				assert.NoError(t, err, "Should not return error for: "+tt.name)
			}
		})
	}
}

// TestValidateImportSource_CRTDLValidation tests CRTDL file validation
func TestValidateImportSource_CRTDLValidation(t *testing.T) {
	tempDir := t.TempDir()

	tests := []struct {
		name        string
		setupFunc   func() string
		expectError bool
		errorMsg    string
	}{
		{
			name: "Valid CRTDL file",
			setupFunc: func() string {
				file := filepath.Join(tempDir, "valid.crtdl")
				_ = os.WriteFile(file, []byte("{}"), 0644)
				return file
			},
			expectError: false,
		},
		{
			name: "Empty CRTDL path",
			setupFunc: func() string {
				return ""
			},
			expectError: true,
			errorMsg:    "cannot be empty",
		},
		{
			name: "Non-existent CRTDL file",
			setupFunc: func() string {
				return filepath.Join(tempDir, "nonexistent.crtdl")
			},
			expectError: true,
			errorMsg:    "does not exist",
		},
		{
			name: "CRTDL path is directory",
			setupFunc: func() string {
				dir := filepath.Join(tempDir, "crtdl_dir")
				_ = os.MkdirAll(dir, 0755)
				return dir
			},
			expectError: true,
			errorMsg:    "directory, not a file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sourcePath := tt.setupFunc()
			err := services.ValidateImportSource(sourcePath, models.InputTypeCRTDL)

			if tt.expectError {
				assert.Error(t, err, "Should return error for: "+tt.name)
				assert.Contains(t, err.Error(), tt.errorMsg, "Error message should match")
			} else {
				assert.NoError(t, err, "Should not return error for: "+tt.name)
			}
		})
	}
}

// TestValidateImportSource_TORCHValidation tests TORCH URL validation
func TestValidateImportSource_TORCHValidation(t *testing.T) {
	tests := []struct {
		name        string
		url         string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "Valid TORCH HTTP URL",
			url:         "http://torch.example.com/results",
			expectError: false,
		},
		{
			name:        "Valid TORCH HTTPS URL",
			url:         "https://secure-torch.example.com/api/results",
			expectError: false,
		},
		{
			name:        "Empty TORCH URL",
			url:         "",
			expectError: true,
			errorMsg:    "cannot be empty",
		},
		{
			name:        "Invalid scheme (FTP)",
			url:         "ftp://torch.example.com/results",
			expectError: true,
			errorMsg:    "must start with http",
		},
		{
			name:        "Invalid scheme (file)",
			url:         "file:///path/to/file",
			expectError: true,
			errorMsg:    "must start with http",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := services.ValidateImportSource(tt.url, models.InputTypeTORCHURL)

			if tt.expectError {
				assert.Error(t, err, "Should return error for: "+tt.name)
				assert.Contains(t, err.Error(), tt.errorMsg, "Error message should match")
			} else {
				assert.NoError(t, err, "Should not return error for: "+tt.name)
			}
		})
	}
}

// TestValidateImportSource_UnknownType tests unknown input type handling
func TestValidateImportSource_UnknownType(t *testing.T) {
	err := services.ValidateImportSource("/some/path", "unknown-input-type")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown input type")
}

// TestImportFromLocalDirectory_JSONFile tests error handling when .json file is passed instead of directory (line 29-30)
func TestImportFromLocalDirectory_JSONFile(t *testing.T) {
	tempDir := t.TempDir()
	sourceFile := filepath.Join(tempDir, "data.json")
	destDir := filepath.Join(tempDir, "dest")
	logger := lib.NewLogger(lib.LogLevelInfo)

	// Create source as a JSON file, not directory
	require.NoError(t, os.WriteFile(sourceFile, []byte(`{"test": "data"}`), 0644))

	// Execute import
	importedFiles, err := services.ImportFromLocalDirectory(sourceFile, destDir, logger)

	// Verify error (line 29-30 path)
	assert.Error(t, err, "Should fail when source is a .json file")
	assert.Contains(t, err.Error(), "JSON/CRTDL file", "Error should mention JSON/CRTDL file")
	assert.Contains(t, err.Error(), "not a directory", "Error should mention not a directory")
	assert.Contains(t, err.Error(), "InputTypeCRTDL", "Error should mention InputTypeCRTDL")
	assert.Nil(t, importedFiles, "Should not return files on error")
}

// TestImportFromLocalDirectory_CRTDLFile tests error handling when .crtdl file is passed instead of directory (line 29-30)
func TestImportFromLocalDirectory_CRTDLFile(t *testing.T) {
	tempDir := t.TempDir()
	sourceFile := filepath.Join(tempDir, "cohort.crtdl")
	destDir := filepath.Join(tempDir, "dest")
	logger := lib.NewLogger(lib.LogLevelInfo)

	// Create source as a CRTDL file, not directory
	require.NoError(t, os.WriteFile(sourceFile, []byte(`{"cohortDefinition": {}}`), 0644))

	// Execute import
	importedFiles, err := services.ImportFromLocalDirectory(sourceFile, destDir, logger)

	// Verify error (line 29-30 path)
	assert.Error(t, err, "Should fail when source is a .crtdl file")
	assert.Contains(t, err.Error(), "JSON/CRTDL file", "Error should mention JSON/CRTDL file")
	assert.Contains(t, err.Error(), "not a directory", "Error should mention not a directory")
	assert.Contains(t, err.Error(), "InputTypeCRTDL", "Error should mention InputTypeCRTDL")
	assert.Nil(t, importedFiles, "Should not return files on error")
}

// TestValidateImportSource_JSONFileForLocalInput tests validation hint for .json file with InputTypeLocal (line 174-178)
func TestValidateImportSource_JSONFileForLocalInput(t *testing.T) {
	tempDir := t.TempDir()
	jsonFile := filepath.Join(tempDir, "data.json")

	// Create JSON file
	require.NoError(t, os.WriteFile(jsonFile, []byte(`{"test": "data"}`), 0644))

	// Validate with InputTypeLocal (should fail with helpful hint - line 174-178)
	err := services.ValidateImportSource(jsonFile, models.InputTypeLocal)

	assert.Error(t, err, "Should return error for JSON file with InputTypeLocal")
	assert.Contains(t, err.Error(), "expected directory but got file", "Error should mention file vs directory")
	// Verify the hint message (lines 174-178)
	assert.Contains(t, err.Error(), "JSON/CRTDL file", "Error should contain JSON/CRTDL hint")
	assert.Contains(t, err.Error(), "cohortDefinition", "Error should mention cohortDefinition")
	assert.Contains(t, err.Error(), "dataExtraction", "Error should mention dataExtraction")
	assert.Contains(t, err.Error(), "verbose logging", "Error should mention verbose logging")
}

// TestValidateImportSource_CRTDLFileForLocalInput tests validation hint for .crtdl file with InputTypeLocal (line 174-178)
func TestValidateImportSource_CRTDLFileForLocalInput(t *testing.T) {
	tempDir := t.TempDir()
	crtdlFile := filepath.Join(tempDir, "cohort.crtdl")

	// Create CRTDL file
	require.NoError(t, os.WriteFile(crtdlFile, []byte(`{"cohortDefinition": {}}`), 0644))

	// Validate with InputTypeLocal (should fail with helpful hint - line 174-178)
	err := services.ValidateImportSource(crtdlFile, models.InputTypeLocal)

	assert.Error(t, err, "Should return error for CRTDL file with InputTypeLocal")
	assert.Contains(t, err.Error(), "expected directory but got file", "Error should mention file vs directory")
	// Verify the hint message (lines 174-178)
	assert.Contains(t, err.Error(), "JSON/CRTDL file", "Error should contain JSON/CRTDL hint")
	assert.Contains(t, err.Error(), "cohortDefinition", "Error should mention cohortDefinition")
	assert.Contains(t, err.Error(), "dataExtraction", "Error should mention dataExtraction")
	assert.Contains(t, err.Error(), "verbose logging", "Error should mention verbose logging")
}
