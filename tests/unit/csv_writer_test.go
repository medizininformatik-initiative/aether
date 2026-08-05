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

		content, err := os.ReadFile(filepath.Join(tempDir, "multi.csv"))
		require.NoError(t, err)

		expected := "id,value\n1,a\n2,b\n3,c\n4,d\n"
		assert.Equal(t, expected, string(content))
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

	t.Run("first batch reports a write error", func(t *testing.T) {
		dir, name := unwritableSink(t)
		writer := services.NewCSVWriter(dir)

		err := writer.AppendCSVData(name, []string{"id"}, [][]string{{"1"}}, true)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to write CSV rows")
	})

	t.Run("subsequent batch reports a write error", func(t *testing.T) {
		dir, name := unwritableSink(t)
		writer := services.NewCSVWriter(dir)

		err := writer.AppendCSVData(name, []string{"id"}, [][]string{{"1"}}, false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to write CSV rows")
	})
}

// unwritableSink returns a directory and a filename whose join is a sink that
// accepts an open but rejects every write. It makes the CSV write-error
// branches reachable without a seam in the writer. Linux provides /dev/full
// for this; on other systems the test skips.
func unwritableSink(t *testing.T) (dir, name string) {
	t.Helper()

	const path = "/dev/full"
	file, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		t.Skipf("no unwritable sink on this system: %v", err)
	}
	_, writeErr := file.Write([]byte("probe"))
	require.NoError(t, file.Close())
	if writeErr == nil {
		t.Skip("the sink accepted a write, so no CSV write error is possible")
	}

	return filepath.Dir(path), filepath.Base(path)
}
