package lib_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/lib"
)

func TestFileExists(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "present.txt")
	require.NoError(t, os.WriteFile(file, []byte("data"), 0644))

	tests := []struct {
		name string
		path string
		want bool
	}{
		{"existing file", file, true},
		{"existing directory", dir, true},
		{"missing path", filepath.Join(dir, "absent.txt"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, lib.FileExists(tt.path))
		})
	}
}

func TestDirExists(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "regular.txt")
	require.NoError(t, os.WriteFile(file, []byte("data"), 0644))

	tests := []struct {
		name string
		path string
		want bool
	}{
		{"existing directory", dir, true},
		{"file is not a directory", file, false},
		{"missing path", filepath.Join(dir, "nope"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, lib.DirExists(tt.path))
		})
	}
}

func TestGetFileSize(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "sized.txt")
	require.NoError(t, os.WriteFile(file, []byte("hello"), 0644))

	tests := []struct {
		name string
		path string
		want int64
	}{
		{"size of existing file", file, 5},
		{"missing file yields zero", filepath.Join(dir, "absent.txt"), 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, lib.GetFileSize(tt.path))
		})
	}
}

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple name",
			input:    "Patient",
			expected: "Patient",
		},
		{
			name:     "name with spaces",
			input:    "standard pat",
			expected: "standard pat",
		},
		{
			name:     "forward slash",
			input:    "Condition/Diagnosis",
			expected: "Condition_Diagnosis",
		},
		{
			name:     "backslash",
			input:    "path\\name",
			expected: "path_name",
		},
		{
			name:     "colon",
			input:    "name:value",
			expected: "name_value",
		},
		{
			name:     "asterisk",
			input:    "name*pattern",
			expected: "name_pattern",
		},
		{
			name:     "question mark",
			input:    "name?query",
			expected: "name_query",
		},
		{
			name:     "double quotes",
			input:    "name\"quoted\"",
			expected: "name_quoted_",
		},
		{
			name:     "angle brackets",
			input:    "name<Test>",
			expected: "name_Test_",
		},
		{
			name:     "pipe",
			input:    "name|value",
			expected: "name_value",
		},
		{
			name:     "complex combination",
			input:    "MII_PR_Person:Patient<Test>",
			expected: "MII_PR_Person_Patient_Test_",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "only special characters",
			input:    "/:*?\"<>|",
			expected: "________",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := lib.SanitizeFilename(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}
