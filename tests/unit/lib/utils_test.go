package lib_test

import (
	"testing"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/stretchr/testify/assert"
)

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
