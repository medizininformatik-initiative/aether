package unit

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCSVWriter_WriteCSV(t *testing.T) {
	t.Run("write with header and data", func(t *testing.T) {
		tempDir := t.TempDir()
		writer := services.NewCSVWriter(tempDir)

		header := []string{"id", "name", "birthDate"}
		data := "1,John,1990-01-01\n2,Jane,1985-05-15"

		err := writer.WriteCSV("patients.csv", header, data)
		require.NoError(t, err)

		// Read and verify file contents
		content, err := os.ReadFile(filepath.Join(tempDir, "patients.csv"))
		require.NoError(t, err)

		expected := "id,name,birthDate\n1,John,1990-01-01\n2,Jane,1985-05-15\n"
		assert.Equal(t, expected, string(content))
	})

	t.Run("write with header only", func(t *testing.T) {
		tempDir := t.TempDir()
		writer := services.NewCSVWriter(tempDir)

		header := []string{"id", "name"}
		data := ""

		err := writer.WriteCSV("empty.csv", header, data)
		require.NoError(t, err)

		content, err := os.ReadFile(filepath.Join(tempDir, "empty.csv"))
		require.NoError(t, err)

		assert.Equal(t, "id,name\n", string(content))
	})

	t.Run("creates output directory", func(t *testing.T) {
		tempDir := t.TempDir()
		outputDir := filepath.Join(tempDir, "nested", "dir")
		writer := services.NewCSVWriter(outputDir)

		err := writer.WriteCSV("test.csv", []string{"col"}, "value")
		require.NoError(t, err)

		// Verify directory was created
		info, err := os.Stat(outputDir)
		require.NoError(t, err)
		assert.True(t, info.IsDir())
	})

	t.Run("handles special characters in data", func(t *testing.T) {
		tempDir := t.TempDir()
		writer := services.NewCSVWriter(tempDir)

		header := []string{"name", "description"}
		// CSV data with quoted fields containing commas
		data := `"John, Jr.","Description with ""quotes"""`

		err := writer.WriteCSV("special.csv", header, data)
		require.NoError(t, err)

		content, err := os.ReadFile(filepath.Join(tempDir, "special.csv"))
		require.NoError(t, err)

		// Verify header is present
		assert.Contains(t, string(content), "name,description")
	})
}

func TestCSVWriter_WriteCSVDirect(t *testing.T) {
	t.Run("write raw CSV", func(t *testing.T) {
		tempDir := t.TempDir()
		writer := services.NewCSVWriter(tempDir)

		data := "id,name\n1,John\n2,Jane\n"

		err := writer.WriteCSVDirect("raw.csv", data)
		require.NoError(t, err)

		content, err := os.ReadFile(filepath.Join(tempDir, "raw.csv"))
		require.NoError(t, err)
		assert.Equal(t, data, string(content))
	})

	t.Run("creates output directory", func(t *testing.T) {
		tempDir := t.TempDir()
		nestedDir := filepath.Join(tempDir, "nested", "path")
		writer := services.NewCSVWriter(nestedDir)

		err := writer.WriteCSVDirect("test.csv", "data")
		require.NoError(t, err)

		// Verify the file was created
		_, err = os.Stat(filepath.Join(nestedDir, "test.csv"))
		require.NoError(t, err)
	})
}

func TestCSVWriter_WriteCSVErrors(t *testing.T) {
	t.Run("invalid CSV data from flattener", func(t *testing.T) {
		tempDir := t.TempDir()
		writer := services.NewCSVWriter(tempDir)

		header := []string{"id", "name"}
		// Invalid CSV - mismatched quotes
		invalidData := `"unclosed quote`

		err := writer.WriteCSV("invalid.csv", header, invalidData)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse CSV data")
	})
}

func TestCountCSVRowsError(t *testing.T) {
	t.Run("invalid CSV returns error", func(t *testing.T) {
		// CSV with inconsistent field count and unclosed quote
		invalidCSV := `"unclosed`
		_, err := services.CountCSVRows(invalidCSV, false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse CSV")
	})
}

func TestValidateCSVHeaderError(t *testing.T) {
	t.Run("invalid CSV header returns error", func(t *testing.T) {
		// CSV with unclosed quote
		invalidCSV := `"unclosed`
		err := services.ValidateCSVHeader(invalidCSV, []string{"col"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to read CSV header")
	})
}

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"normal_name", "normal_name"},
		{"name with spaces", "name with spaces"},
		{"name/with/slashes", "name_with_slashes"},
		{"name\\with\\backslashes", "name_with_backslashes"},
		{"name:with:colons", "name_with_colons"},
		{"name*with*asterisks", "name_with_asterisks"},
		{"name?with?question", "name_with_question"},
		{`name"with"quotes`, "name_with_quotes"},
		{"name<with>angles", "name_with_angles"},
		{"name|with|pipes", "name_with_pipes"},
		{"complex/path:name*?", "complex_path_name__"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := services.SanitizeFilename(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestBuildCSVFilename(t *testing.T) {
	tests := []struct {
		groupName string
		expected  string
	}{
		{"Patients", "Patients.csv"},
		{"My Diagnoses", "My Diagnoses.csv"},
		{"MII PR Person Patient (Pseudonymisiert)", "MII PR Person Patient (Pseudonymisiert).csv"},
		{"name/with/invalid", "name_with_invalid.csv"},
	}

	for _, tt := range tests {
		t.Run(tt.groupName, func(t *testing.T) {
			result := services.BuildCSVFilename(tt.groupName)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractHeaderFromViewDefinition(t *testing.T) {
	viewDef := models.ViewDefinition{
		Select: []models.SelectClause{
			{
				Column: []models.ColumnDefinition{
					{Name: "id"},
					{Name: "patient"},
				},
			},
			{
				Column: []models.ColumnDefinition{
					{Name: "code"},
				},
			},
		},
	}

	header := services.ExtractHeaderFromViewDefinition(viewDef)
	assert.Equal(t, []string{"id", "patient", "code"}, header)
}

func TestCountCSVRows(t *testing.T) {
	t.Run("with header", func(t *testing.T) {
		csvData := "id,name\n1,John\n2,Jane\n3,Bob"
		count, err := services.CountCSVRows(csvData, true)
		require.NoError(t, err)
		assert.Equal(t, 3, count)
	})

	t.Run("without header", func(t *testing.T) {
		csvData := "1,John\n2,Jane\n3,Bob"
		count, err := services.CountCSVRows(csvData, false)
		require.NoError(t, err)
		assert.Equal(t, 3, count)
	})

	t.Run("empty data", func(t *testing.T) {
		count, err := services.CountCSVRows("", false)
		require.NoError(t, err)
		assert.Equal(t, 0, count)
	})

	t.Run("header only", func(t *testing.T) {
		csvData := "id,name"
		count, err := services.CountCSVRows(csvData, true)
		require.NoError(t, err)
		assert.Equal(t, 0, count)
	})
}

func TestCSVWriter_WriteCSVDirectoryErrors(t *testing.T) {
	// Create a file where we expect a directory to cause MkdirAll to fail
	tempDir := t.TempDir()
	blockingFile := filepath.Join(tempDir, "blocking_file")
	require.NoError(t, os.WriteFile(blockingFile, []byte("content"), 0644))

	invalidDir := filepath.Join(blockingFile, "subdir")
	writer := services.NewCSVWriter(invalidDir)

	t.Run("WriteCSV", func(t *testing.T) {
		err := writer.WriteCSV("test.csv", []string{"id"}, "1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create output directory")
	})

	t.Run("WriteCSVDirect", func(t *testing.T) {
		err := writer.WriteCSVDirect("test.csv", "content")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create output directory")
	})
}

func TestValidateCSVHeader(t *testing.T) {
	t.Run("valid header", func(t *testing.T) {
		csvData := "id,name,birthDate\n1,John,1990-01-01"
		expected := []string{"id", "name", "birthDate"}

		err := services.ValidateCSVHeader(csvData, expected)
		assert.NoError(t, err)
	})

	t.Run("wrong column count", func(t *testing.T) {
		csvData := "id,name\n1,John"
		expected := []string{"id", "name", "birthDate"}

		err := services.ValidateCSVHeader(csvData, expected)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "column count mismatch")
	})

	t.Run("wrong column name", func(t *testing.T) {
		csvData := "id,firstName,birthDate\n1,John,1990-01-01"
		expected := []string{"id", "name", "birthDate"}

		err := services.ValidateCSVHeader(csvData, expected)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "column mismatch")
	})

	t.Run("empty data", func(t *testing.T) {
		err := services.ValidateCSVHeader("", []string{"id"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "empty")
	})
}
