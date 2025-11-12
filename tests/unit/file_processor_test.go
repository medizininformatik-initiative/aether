package unit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/pipeline"
)

func TestSetupFileProcessing(t *testing.T) {
	tempDir := t.TempDir()
	inputFile := filepath.Join(tempDir, "input.ndjson")
	outputFile := filepath.Join(tempDir, "output.ndjson")

	// Create a test input file
	inputContent := `{"resourceType":"Patient","id":"1"}
{"resourceType":"Patient","id":"2"}`
	err := os.WriteFile(inputFile, []byte(inputContent), 0644)
	require.NoError(t, err)

	// Test successful setup
	ctx, err := pipeline.SetupFileProcessing(inputFile, outputFile)
	require.NoError(t, err)
	assert.NotNil(t, ctx)
	assert.NotNil(t, ctx.InFile)
	assert.NotNil(t, ctx.OutFile)
	assert.Equal(t, outputFile+".part", ctx.TempFile)

	// Verify .part file was created
	_, err = os.Stat(ctx.TempFile)
	assert.NoError(t, err)

	// Cleanup
	require.NoError(t, ctx.InFile.Close())
	require.NoError(t, ctx.OutFile.Close())
	ctx.Cleanup()

	// Verify .part file was removed on cleanup (no success marked)
	_, err = os.Stat(ctx.TempFile)
	assert.Error(t, err)
}

func TestSetupFileProcessing_InputFileNotFound(t *testing.T) {
	tempDir := t.TempDir()
	nonExistentInput := filepath.Join(tempDir, "nonexistent.ndjson")
	outputFile := filepath.Join(tempDir, "output.ndjson")

	ctx, err := pipeline.SetupFileProcessing(nonExistentInput, outputFile)
	assert.Error(t, err)
	assert.Nil(t, ctx)
}

func TestSetupFileProcessing_CannotCreateOutput(t *testing.T) {
	tempDir := t.TempDir()
	inputFile := filepath.Join(tempDir, "input.ndjson")
	err := os.WriteFile(inputFile, []byte("test"), 0644)
	require.NoError(t, err)

	// Try to create output in a non-existent directory
	outputFile := filepath.Join(tempDir, "nonexistent", "output.ndjson")

	ctx, err := pipeline.SetupFileProcessing(inputFile, outputFile)
	assert.Error(t, err)
	assert.Nil(t, ctx)
}

func TestFinalizeFileProcessing_Success(t *testing.T) {
	tempDir := t.TempDir()
	inputFile := filepath.Join(tempDir, "input.ndjson")
	outputFile := filepath.Join(tempDir, "output.ndjson")

	// Create input file
	err := os.WriteFile(inputFile, []byte("test"), 0644)
	require.NoError(t, err)

	// Setup
	ctx, err := pipeline.SetupFileProcessing(inputFile, outputFile)
	require.NoError(t, err)

	// Write some data
	testData := map[string]any{"resourceType": "Patient", "id": "1"}
	jsonBytes, err := json.Marshal(testData)
	require.NoError(t, err)
	_, err = ctx.OutFile.Write(jsonBytes)
	require.NoError(t, err)

	// Finalize with success
	err = pipeline.FinalizeFileProcessing(ctx, outputFile, true)
	assert.NoError(t, err)

	// Verify final file exists with correct name
	_, err = os.Stat(outputFile)
	assert.NoError(t, err)

	// Verify .part file no longer exists
	_, err = os.Stat(outputFile + ".part")
	assert.Error(t, err)
}

func TestFinalizeFileProcessing_NoSuccess(t *testing.T) {
	tempDir := t.TempDir()
	inputFile := filepath.Join(tempDir, "input.ndjson")
	outputFile := filepath.Join(tempDir, "output.ndjson")

	// Create input file
	err := os.WriteFile(inputFile, []byte("test"), 0644)
	require.NoError(t, err)

	// Setup
	ctx, err := pipeline.SetupFileProcessing(inputFile, outputFile)
	require.NoError(t, err)

	// Finalize without success
	err = pipeline.FinalizeFileProcessing(ctx, outputFile, false)
	assert.NoError(t, err)

	// Verify final file doesn't exist
	_, err = os.Stat(outputFile)
	assert.Error(t, err)

	// Verify .part file was cleaned up
	_, err = os.Stat(outputFile + ".part")
	assert.Error(t, err)
}

func TestWriteProcessedResource(t *testing.T) {
	tempDir := t.TempDir()
	outputFile := filepath.Join(tempDir, "output.ndjson")

	f, err := os.Create(outputFile)
	require.NoError(t, err)
	defer func() {
		_ = f.Close()
	}()

	resource := map[string]any{
		"resourceType": "Patient",
		"id":           "123",
		"name": []map[string]any{
			{"use": "official", "given": []string{"John"}},
		},
	}

	err = pipeline.WriteProcessedResource(resource, f)
	assert.NoError(t, err)

	// Read back and verify
	require.NoError(t, f.Close())
	content, err := os.ReadFile(outputFile)
	require.NoError(t, err)

	var written map[string]any
	err = json.Unmarshal(content[:len(content)-1], &written) // Remove trailing newline
	assert.NoError(t, err)
	assert.Equal(t, "Patient", written["resourceType"])
	assert.Equal(t, "123", written["id"])
}

func TestWriteProcessedResource_InvalidJSON(t *testing.T) {
	tempDir := t.TempDir()
	outputFile := filepath.Join(tempDir, "output.ndjson")

	f, err := os.Create(outputFile)
	require.NoError(t, err)
	defer func() {
		_ = f.Close()
	}()

	// Create a resource with circular reference (if possible) or use a channel which can't be marshaled
	resource := map[string]any{
		"resourceType": "Patient",
		"id":           "123",
		"channel":      make(chan int), // This can't be marshaled to JSON
	}

	err = pipeline.WriteProcessedResource(resource, f)
	assert.Error(t, err)
}

func TestWriteProcessedResource_MultipleResources(t *testing.T) {
	tempDir := t.TempDir()
	outputFile := filepath.Join(tempDir, "output.ndjson")

	f, err := os.Create(outputFile)
	require.NoError(t, err)
	defer func() {
		_ = f.Close()
	}()

	resources := []map[string]any{
		{"resourceType": "Patient", "id": "1"},
		{"resourceType": "Patient", "id": "2"},
		{"resourceType": "Observation", "id": "3"},
	}

	for _, r := range resources {
		err = pipeline.WriteProcessedResource(r, f)
		assert.NoError(t, err)
	}

	require.NoError(t, f.Close())

	// Verify all resources are in file
	content, err := os.ReadFile(outputFile)
	require.NoError(t, err)

	lines := 0
	for _, line := range string(content) {
		if line == '\n' {
			lines++
		}
	}
	assert.Equal(t, 3, lines)
}

// Additional tests for error paths and edge cases

func TestSetupFileProcessing_WithExistingOutput(t *testing.T) {
	tempDir := t.TempDir()
	inputFile := filepath.Join(tempDir, "input.ndjson")
	outputFile := filepath.Join(tempDir, "output.ndjson")

	// Create input file
	err := os.WriteFile(inputFile, []byte("test"), 0644)
	require.NoError(t, err)

	// Create existing output file
	err = os.WriteFile(outputFile, []byte("existing"), 0644)
	require.NoError(t, err)

	// Setup should succeed even if output file exists (it's the .part file that matters)
	ctx, err := pipeline.SetupFileProcessing(inputFile, outputFile)
	require.NoError(t, err)
	assert.NotNil(t, ctx)

	// Cleanup
	require.NoError(t, ctx.InFile.Close())
	require.NoError(t, ctx.OutFile.Close())
	ctx.Cleanup()
}

func TestWriteProcessedResource_LargeResource(t *testing.T) {
	tempDir := t.TempDir()
	outputFile := filepath.Join(tempDir, "output.ndjson")

	f, err := os.Create(outputFile)
	require.NoError(t, err)
	defer func() {
		_ = f.Close()
	}()

	// Create a large resource
	largeData := make([]map[string]any, 1000)
	for i := 0; i < 1000; i++ {
		largeData[i] = map[string]any{
			"field": string(make([]byte, 100)),
		}
	}

	resource := map[string]any{
		"resourceType": "Patient",
		"id":           "large-123",
		"name":         largeData,
	}

	err = pipeline.WriteProcessedResource(resource, f)
	assert.NoError(t, err)

	require.NoError(t, f.Close())

	// Verify file exists and has content
	content, err := os.ReadFile(outputFile)
	require.NoError(t, err)
	assert.Greater(t, len(content), 0)
}

func TestSetupFileProcessing_ReadInputAndWrite(t *testing.T) {
	tempDir := t.TempDir()
	inputFile := filepath.Join(tempDir, "input.ndjson")
	outputFile := filepath.Join(tempDir, "output.ndjson")

	// Create input with multiple lines
	inputContent := `{"resourceType":"Patient","id":"1"}
{"resourceType":"Patient","id":"2"}
{"resourceType":"Observation","id":"3"}`
	err := os.WriteFile(inputFile, []byte(inputContent), 0644)
	require.NoError(t, err)

	// Setup
	ctx, err := pipeline.SetupFileProcessing(inputFile, outputFile)
	require.NoError(t, err)
	assert.NotNil(t, ctx)

	// Verify files are open
	assert.NotNil(t, ctx.InFile)
	assert.NotNil(t, ctx.OutFile)

	// Cleanup
	require.NoError(t, ctx.InFile.Close())
	require.NoError(t, ctx.OutFile.Close())
}
