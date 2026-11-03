package unit

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

func TestValidateImportSource_ErrorMessages(t *testing.T) {
	dir := t.TempDir()
	ndjsonDir := filepath.Join(dir, "with-data")
	require.NoError(t, os.Mkdir(ndjsonDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(ndjsonDir, "a.ndjson"), []byte("{}\n"), 0o644))
	emptyDir := filepath.Join(dir, "empty")
	require.NoError(t, os.Mkdir(emptyDir, 0o755))
	nestedDir := filepath.Join(dir, "nested")
	require.NoError(t, os.MkdirAll(filepath.Join(nestedDir, "sub"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(nestedDir, "sub", "b.ndjson"), []byte("{}\n"), 0o644))

	jsonFile := filepath.Join(dir, "x.JSON")
	ndjsonFile := filepath.Join(dir, "x.ndjson")
	txtFile := filepath.Join(dir, "x.txt")
	for _, f := range []string{jsonFile, ndjsonFile, txtFile} {
		require.NoError(t, os.WriteFile(f, []byte("{}"), 0o644))
	}
	missing := filepath.Join(dir, "missing")
	throughFile := filepath.Join(txtFile, "child")

	const recursiveHintText = " (only the top-level directory was scanned; set services.local_import.recursive: true to also scan subdirectories)"

	tests := []struct {
		name      string
		path      string
		inputType models.InputType
		recursive bool
		want      string // exact message; empty means no error
		contains  string
	}{
		{name: "local ok", path: ndjsonDir, inputType: models.InputTypeLocal},
		{name: "local recursive finds nested", path: nestedDir, inputType: models.InputTypeLocal, recursive: true},
		{name: "local missing", path: missing, inputType: models.InputTypeLocal, want: "directory does not exist: " + missing},
		{name: "local inaccessible", path: throughFile, inputType: models.InputTypeLocal, contains: "cannot access directory: "},
		{
			name: "local json file", path: jsonFile, inputType: models.InputTypeLocal,
			want: "expected directory but got file: " + jsonFile + "\n\nThis appears to be a JSON file. Possible issues:\n  - File may not have valid CRTDL structure (missing cohortDefinition or dataExtraction)\n  - File may be using FHIR Parameters format instead of flat CRTDL format\n\nRun with verbose logging to see detailed validation errors.",
		},
		{
			name: "local ndjson file", path: ndjsonFile, inputType: models.InputTypeLocal,
			want: "expected directory but got file: " + ndjsonFile + "\n\nThis is an NDJSON file. Please provide the directory containing it, not the file itself.",
		},
		{name: "local other file", path: txtFile, inputType: models.InputTypeLocal, want: "expected directory but got file: " + txtFile},
		{
			name: "local empty dir", path: emptyDir, inputType: models.InputTypeLocal,
			want: "no FHIR NDJSON files found in directory: " + emptyDir + recursiveHintText + "\n\nExpected files with extensions: .ndjson",
		},
		{
			name: "local empty dir recursive", path: emptyDir, inputType: models.InputTypeLocal, recursive: true,
			want: "no FHIR NDJSON files found in directory: " + emptyDir + "\n\nExpected files with extensions: .ndjson",
		},
		{
			name: "local nested not found when flat", path: nestedDir, inputType: models.InputTypeLocal,
			want: "no FHIR NDJSON files found in directory: " + nestedDir + recursiveHintText + "\n\nExpected files with extensions: .ndjson",
		},
		{name: "http empty", path: "", inputType: models.InputTypeHTTP, want: "URL cannot be empty"},
		{name: "http ok", path: "http://x", inputType: models.InputTypeHTTP},
		{name: "crtdl empty", path: "", inputType: models.InputTypeCRTDL, want: "CRTDL file path cannot be empty"},
		{name: "crtdl ok", path: jsonFile, inputType: models.InputTypeCRTDL},
		{name: "crtdl missing", path: missing, inputType: models.InputTypeCRTDL, want: "CRTDL file does not exist: " + missing},
		{name: "crtdl inaccessible", path: throughFile, inputType: models.InputTypeCRTDL, contains: "cannot access CRTDL file: "},
		{name: "crtdl directory", path: dir, inputType: models.InputTypeCRTDL, want: "CRTDL path is a directory, not a file: " + dir},
		{name: "torch empty", path: "", inputType: models.InputTypeTORCHURL, want: "TORCH result URL cannot be empty"},
		{name: "torch bad scheme", path: "ftp://x", inputType: models.InputTypeTORCHURL, want: "TORCH URL must start with http:// or https://"},
		{name: "torch http", path: "http://x/r", inputType: models.InputTypeTORCHURL},
		{name: "torch https", path: "https://x/r", inputType: models.InputTypeTORCHURL},
		{name: "unknown type", path: "x", inputType: models.InputType("bogus"), want: "unknown input type: bogus"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := services.ValidateImportSource(tt.path, tt.inputType, tt.recursive)
			switch {
			case tt.want != "":
				require.Error(t, err)
				assert.Equal(t, tt.want, err.Error())
			case tt.contains != "":
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.contains)
			default:
				assert.NoError(t, err)
			}
		})
	}
}
