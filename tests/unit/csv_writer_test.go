package unit

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/services"
)

func TestCSVWriter_AppendCSVData(t *testing.T) {
	t.Run("creates output directory", func(t *testing.T) {
		tempDir := t.TempDir()
		outputDir := filepath.Join(tempDir, "nested", "dir")
		writer := services.NewCSVWriter(outputDir)

		err := writer.AppendCSVData("test.csv", []string{"col"}, [][]string{{"value"}}, true)
		require.NoError(t, err)

		// Verify directory was created
		info, err := os.Stat(outputDir)
		require.NoError(t, err)
		assert.True(t, info.IsDir())
	})

	t.Run("quotes hostile free text", func(t *testing.T) {
		tempDir := t.TempDir()
		writer := services.NewCSVWriter(tempDir)

		header := []string{"name", "description"}
		rows := [][]string{
			{"John, Jr.", `Description with "quotes"`},
			{"=formula", "line one\nline two"},
			{`lone \ backslash`, `C:\path`},
		}

		err := writer.AppendCSVData("special.csv", header, rows, true)
		require.NoError(t, err)
		require.NoError(t, writer.Finalize("special.csv"))

		content, err := os.ReadFile(filepath.Join(tempDir, "special.csv"))
		require.NoError(t, err)

		// RFC 4180: embedded quotes are doubled, fields with commas,
		// quotes, or newlines are quoted, and backslashes stay verbatim.
		expected := "name,description\n" +
			"\"John, Jr.\",\"Description with \"\"quotes\"\"\"\n" +
			"=formula,\"line one\nline two\"\n" +
			"lone \\ backslash,C:\\path\n"
		assert.Equal(t, expected, string(content))
	})
	t.Run("first batch creates file with header and rows", func(t *testing.T) {
		tempDir := t.TempDir()
		writer := services.NewCSVWriter(tempDir)

		header := []string{"id", "name", "birthDate"}
		rows := [][]string{
			{"1", "John", "1990-01-01"},
			{"2", "Jane", "1985-05-15"},
		}

		err := writer.AppendCSVData("patients.csv", header, rows, true)
		require.NoError(t, err)
		require.NoError(t, writer.Finalize("patients.csv"))

		content, err := os.ReadFile(filepath.Join(tempDir, "patients.csv"))
		require.NoError(t, err)

		expected := "id,name,birthDate\n1,John,1990-01-01\n2,Jane,1985-05-15\n"
		assert.Equal(t, expected, string(content))
	})

	t.Run("subsequent batch appends rows only without header", func(t *testing.T) {
		tempDir := t.TempDir()
		writer := services.NewCSVWriter(tempDir)

		header := []string{"id", "name"}

		// First batch
		err := writer.AppendCSVData("test.csv", header, [][]string{{"1", "Alice"}}, true)
		require.NoError(t, err)

		// Second batch
		err = writer.AppendCSVData("test.csv", header, [][]string{{"2", "Bob"}}, false)
		require.NoError(t, err)
		require.NoError(t, writer.Finalize("test.csv"))

		content, err := os.ReadFile(filepath.Join(tempDir, "test.csv"))
		require.NoError(t, err)

		expected := "id,name\n1,Alice\n2,Bob\n"
		assert.Equal(t, expected, string(content))
	})

	t.Run("first batch with no rows writes header only", func(t *testing.T) {
		tempDir := t.TempDir()
		writer := services.NewCSVWriter(tempDir)

		header := []string{"id", "name"}

		err := writer.AppendCSVData("empty.csv", header, nil, true)
		require.NoError(t, err)
		require.NoError(t, writer.Finalize("empty.csv"))

		content, err := os.ReadFile(filepath.Join(tempDir, "empty.csv"))
		require.NoError(t, err)

		assert.Equal(t, "id,name\n", string(content))
	})

	t.Run("subsequent batch with no rows is no-op", func(t *testing.T) {
		tempDir := t.TempDir()
		writer := services.NewCSVWriter(tempDir)

		header := []string{"id", "name"}

		// First batch
		err := writer.AppendCSVData("test.csv", header, [][]string{{"1", "Alice"}}, true)
		require.NoError(t, err)

		// Second batch with no rows
		err = writer.AppendCSVData("test.csv", header, nil, false)
		require.NoError(t, err)
		require.NoError(t, writer.Finalize("test.csv"))

		content, err := os.ReadFile(filepath.Join(tempDir, "test.csv"))
		require.NoError(t, err)

		expected := "id,name\n1,Alice\n"
		assert.Equal(t, expected, string(content))
	})

	t.Run("multiple appends accumulate correctly", func(t *testing.T) {
		tempDir := t.TempDir()
		writer := services.NewCSVWriter(tempDir)

		header := []string{"id", "value"}

		err := writer.AppendCSVData("multi.csv", header, [][]string{{"1", "a"}}, true)
		require.NoError(t, err)

		err = writer.AppendCSVData("multi.csv", header, [][]string{{"2", "b"}, {"3", "c"}}, false)
		require.NoError(t, err)

		err = writer.AppendCSVData("multi.csv", header, [][]string{{"4", "d"}}, false)
		require.NoError(t, err)
		require.NoError(t, writer.Finalize("multi.csv"))

		content, err := os.ReadFile(filepath.Join(tempDir, "multi.csv"))
		require.NoError(t, err)

		expected := "id,value\n1,a\n2,b\n3,c\n4,d\n"
		assert.Equal(t, expected, string(content))
	})
}

func TestCSVWriter_PartialLifecycle(t *testing.T) {
	t.Run("append writes to partial file until finalized", func(t *testing.T) {
		tempDir := t.TempDir()
		writer := services.NewCSVWriter(tempDir)

		err := writer.AppendCSVData("patients.csv", []string{"id", "name"}, [][]string{{"1", "Alice"}}, true)
		require.NoError(t, err)

		assert.FileExists(t, filepath.Join(tempDir, "patients.csv.partial"))
		assert.NoFileExists(t, filepath.Join(tempDir, "patients.csv"))

		err = writer.Finalize("patients.csv")
		require.NoError(t, err)

		assert.NoFileExists(t, filepath.Join(tempDir, "patients.csv.partial"))
		content, err := os.ReadFile(filepath.Join(tempDir, "patients.csv"))
		require.NoError(t, err)
		assert.Equal(t, "id,name\n1,Alice\n", string(content))
	})

	t.Run("finalize without a partial file returns an error", func(t *testing.T) {
		writer := services.NewCSVWriter(t.TempDir())

		err := writer.Finalize("missing.csv")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to finalize CSV file")
	})

	t.Run("remove partials reports a removal error", func(t *testing.T) {
		if os.Getuid() == 0 {
			t.Skip("Cannot test permission errors as root")
		}

		tempDir := t.TempDir()
		partialPath := filepath.Join(tempDir, "stuck.csv"+services.PartialSuffix)
		require.NoError(t, os.WriteFile(partialPath, []byte("id\n"), 0644))
		require.NoError(t, os.Chmod(tempDir, 0555))
		t.Cleanup(func() { _ = os.Chmod(tempDir, 0755) })

		writer := services.NewCSVWriter(tempDir)
		err := writer.RemovePartials()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to remove stale partial CSV file")
	})
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

func TestCSVWriter_AppendCSVDataErrors(t *testing.T) {
	t.Run("first batch MkdirAll error", func(t *testing.T) {
		tempDir := t.TempDir()
		blockingFile := filepath.Join(tempDir, "blocking_file")
		require.NoError(t, os.WriteFile(blockingFile, []byte("content"), 0644))

		invalidDir := filepath.Join(blockingFile, "subdir")
		writer := services.NewCSVWriter(invalidDir)

		err := writer.AppendCSVData("test.csv", []string{"id"}, [][]string{{"1"}}, true)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create output directory")
	})

	t.Run("subsequent batch append to nonexistent file", func(t *testing.T) {
		tempDir := t.TempDir()
		writer := services.NewCSVWriter(tempDir)

		// Don't create the file first, try to append directly
		err := writer.AppendCSVData("nonexistent.csv", []string{"id"}, [][]string{{"1"}}, false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to open CSV file for append")
	})
}
