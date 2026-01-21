package unit

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/lib"
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

	// Test successful setup (no compression)
	ctx, err := pipeline.SetupFileProcessing(inputFile, outputFile, false, "")
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

	ctx, err := pipeline.SetupFileProcessing(nonExistentInput, outputFile, false, "")
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

	ctx, err := pipeline.SetupFileProcessing(inputFile, outputFile, false, "")
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
	ctx, err := pipeline.SetupFileProcessing(inputFile, outputFile, false, "")
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
	ctx, err := pipeline.SetupFileProcessing(inputFile, outputFile, false, "")
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
	ctx, err := pipeline.SetupFileProcessing(inputFile, outputFile, false, "")
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
	ctx, err := pipeline.SetupFileProcessing(inputFile, outputFile, false, "")
	require.NoError(t, err)
	assert.NotNil(t, ctx)

	// Verify files are open
	assert.NotNil(t, ctx.InFile)
	assert.NotNil(t, ctx.OutFile)

	// Cleanup
	require.NoError(t, ctx.InFile.Close())
	require.NoError(t, ctx.OutFile.Close())
}


// =============================================
// Compression Tests for FileProcessor
// =============================================

// TestSetupFileProcessing_WithCompression verifies file processing setup with compression enabled
func TestSetupFileProcessing_WithCompression(t *testing.T) {
	tempDir := t.TempDir()
	inputFile := filepath.Join(tempDir, "input.ndjson")
	outputFile := filepath.Join(tempDir, "output.ndjson.zst")

	// Create a test input file
	inputContent := `{"resourceType":"Patient","id":"1"}
{"resourceType":"Patient","id":"2"}`
	err := os.WriteFile(inputFile, []byte(inputContent), 0644)
	require.NoError(t, err)

	// Test successful setup with compression
	ctx, err := pipeline.SetupFileProcessing(inputFile, outputFile, true, "default")
	require.NoError(t, err)
	assert.NotNil(t, ctx)
	assert.NotNil(t, ctx.InFile)
	assert.NotNil(t, ctx.OutFile)
	assert.NotNil(t, ctx.OutWriter)
	// OutWriter should be different from OutFile when compression is enabled
	assert.NotEqual(t, ctx.OutWriter, ctx.OutFile, "OutWriter should be a separate compressed writer")
	assert.Equal(t, outputFile+".part", ctx.TempFile)

	// Verify .part file was created
	_, err = os.Stat(ctx.TempFile)
	assert.NoError(t, err)

	// Cleanup
	require.NoError(t, ctx.OutWriter.Close())
	require.NoError(t, ctx.OutFile.Close())
	require.NoError(t, ctx.InFile.Close())
	ctx.Cleanup()
}

// TestSetupFileProcessing_WithCompressedInput verifies reading compressed input files
func TestSetupFileProcessing_WithCompressedInput(t *testing.T) {
	tempDir := t.TempDir()
	inputFile := filepath.Join(tempDir, "input.ndjson.zst")
	outputFile := filepath.Join(tempDir, "output.ndjson.zst")

	// Create a compressed input file
	inputContent := `{"resourceType":"Patient","id":"1"}`
	writer, err := lib.CreateCompressedFileWriter(inputFile, "default")
	require.NoError(t, err)
	_, err = writer.Write([]byte(inputContent))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	// Setup file processing with compressed input and output
	ctx, err := pipeline.SetupFileProcessing(inputFile, outputFile, true, "default")
	require.NoError(t, err)
	assert.NotNil(t, ctx)

	// Read from input to verify decompression works (use io.ReadAll for complete read)
	content, err := io.ReadAll(ctx.InFile)
	require.NoError(t, err)
	assert.Equal(t, inputContent, string(content))

	// Cleanup
	require.NoError(t, ctx.OutWriter.Close())
	require.NoError(t, ctx.OutFile.Close())
	require.NoError(t, ctx.InFile.Close())
	ctx.Cleanup()
}

// TestFinalizeFileProcessing_WithCompression verifies finalization with compression
func TestFinalizeFileProcessing_WithCompression(t *testing.T) {
	tempDir := t.TempDir()
	inputFile := filepath.Join(tempDir, "input.ndjson")
	outputFile := filepath.Join(tempDir, "output.ndjson.zst")

	// Create input file
	err := os.WriteFile(inputFile, []byte("test"), 0644)
	require.NoError(t, err)

	// Setup with compression
	ctx, err := pipeline.SetupFileProcessing(inputFile, outputFile, true, "default")
	require.NoError(t, err)

	// Write some data using the compressed writer
	testData := map[string]any{"resourceType": "Patient", "id": "1"}
	err = pipeline.WriteProcessedResource(testData, ctx.OutWriter)
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

	// Verify content is readable as compressed
	reader, err := lib.OpenFileForReading(outputFile)
	require.NoError(t, err)
	content, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())
	assert.Contains(t, string(content), "Patient")
}

// TestFinalizeFileProcessing_WithCompressionFailure verifies cleanup on failure with compression
func TestFinalizeFileProcessing_WithCompressionNoSuccess(t *testing.T) {
	tempDir := t.TempDir()
	inputFile := filepath.Join(tempDir, "input.ndjson")
	outputFile := filepath.Join(tempDir, "output.ndjson.zst")

	// Create input file
	err := os.WriteFile(inputFile, []byte("test"), 0644)
	require.NoError(t, err)

	// Setup with compression
	ctx, err := pipeline.SetupFileProcessing(inputFile, outputFile, true, "default")
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

// TestWriteProcessedResource_ToCompressedWriter verifies writing to compressed output
func TestWriteProcessedResource_ToCompressedWriter(t *testing.T) {
	tempDir := t.TempDir()
	outputFile := filepath.Join(tempDir, "output.ndjson.zst")

	// Create compressed writer
	writer, err := lib.CreateCompressedFileWriter(outputFile, "default")
	require.NoError(t, err)

	// Write resources
	resources := []map[string]any{
		{"resourceType": "Patient", "id": "1"},
		{"resourceType": "Patient", "id": "2"},
		{"resourceType": "Observation", "id": "3"},
	}

	for _, r := range resources {
		err = pipeline.WriteProcessedResource(r, writer)
		assert.NoError(t, err)
	}

	require.NoError(t, writer.Close())

	// Read back and verify
	reader, err := lib.OpenFileForReading(outputFile)
	require.NoError(t, err)
	content, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())

	// Count lines
	lines := 0
	for _, c := range string(content) {
		if c == '\n' {
			lines++
		}
	}
	assert.Equal(t, 3, lines, "Should have 3 resources")
}

// TestSetupFileProcessing_CompressionLevels verifies all compression levels work
func TestSetupFileProcessing_CompressionLevels(t *testing.T) {
	levels := []string{"fastest", "default", "better", "best"}

	for _, level := range levels {
		t.Run(level, func(t *testing.T) {
			tempDir := t.TempDir()
			inputFile := filepath.Join(tempDir, "input.ndjson")
			outputFile := filepath.Join(tempDir, "output.ndjson.zst")

			err := os.WriteFile(inputFile, []byte(`{"resourceType":"Patient"}`), 0644)
			require.NoError(t, err)

			ctx, err := pipeline.SetupFileProcessing(inputFile, outputFile, true, level)
			require.NoError(t, err)
			assert.NotNil(t, ctx)

			// Write and finalize
			err = pipeline.WriteProcessedResource(map[string]any{"resourceType": "Patient", "id": "1"}, ctx.OutWriter)
			require.NoError(t, err)

			err = pipeline.FinalizeFileProcessing(ctx, outputFile, true)
			assert.NoError(t, err)

			// Verify file exists
			_, err = os.Stat(outputFile)
			assert.NoError(t, err)
		})
	}
}


// TestFinalizeFileProcessing_RenameError verifies error handling when rename fails
func TestFinalizeFileProcessing_RenameError(t *testing.T) {
	tempDir := t.TempDir()
	inputFile := filepath.Join(tempDir, "input.ndjson")
	outputDir := filepath.Join(tempDir, "output_dir")
	outputFile := filepath.Join(outputDir, "output.ndjson")

	// Create input file
	err := os.WriteFile(inputFile, []byte("test"), 0644)
	require.NoError(t, err)

	// Create output directory for setup
	require.NoError(t, os.MkdirAll(outputDir, 0755))

	// Setup should succeed
	ctx, err := pipeline.SetupFileProcessing(inputFile, outputFile, false, "")
	require.NoError(t, err)

	// Remove the output directory to cause rename to fail
	require.NoError(t, os.RemoveAll(outputDir))

	// Finalize should fail on rename because the output directory was removed
	err = pipeline.FinalizeFileProcessing(ctx, outputFile, true)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to rename")
}

// TestWriteProcessedResource_WriterError verifies error handling on write failure
func TestWriteProcessedResource_WriterError(t *testing.T) {
	// Create a writer that will fail
	closedWriter := &failingWriter{}

	resource := map[string]any{
		"resourceType": "Patient",
		"id":           "123",
	}

	err := pipeline.WriteProcessedResource(resource, closedWriter)
	assert.Error(t, err)
}

// failingWriter implements io.Writer but always fails
type failingWriter struct{}

func (f *failingWriter) Write(p []byte) (n int, err error) {
	return 0, io.ErrClosedPipe
}

// TestSetupFileProcessing_OutputDirCreation verifies output directory is created implicitly
func TestSetupFileProcessing_OutputDirCreation(t *testing.T) {
	tempDir := t.TempDir()
	inputFile := filepath.Join(tempDir, "input.ndjson")
	// Output in a nested directory that doesn't exist
	outputFile := filepath.Join(tempDir, "nested", "dir", "output.ndjson")

	// Create input file
	err := os.WriteFile(inputFile, []byte("test"), 0644)
	require.NoError(t, err)

	// Setup should fail because output directory doesn't exist (SetupFileProcessing doesn't create it)
	ctx, err := pipeline.SetupFileProcessing(inputFile, outputFile, false, "")
	assert.Error(t, err)
	assert.Nil(t, ctx)
}

// TestFinalizeFileProcessing_AlreadyClosed verifies handling when files are already closed
func TestFinalizeFileProcessing_AlreadyClosed(t *testing.T) {
	tempDir := t.TempDir()
	inputFile := filepath.Join(tempDir, "input.ndjson")
	outputFile := filepath.Join(tempDir, "output.ndjson")

	err := os.WriteFile(inputFile, []byte("test"), 0644)
	require.NoError(t, err)

	ctx, err := pipeline.SetupFileProcessing(inputFile, outputFile, false, "")
	require.NoError(t, err)

	// Close output file early to simulate error
	require.NoError(t, ctx.OutFile.Close())

	// Finalize should fail because file is already closed
	err = pipeline.FinalizeFileProcessing(ctx, outputFile, true)
	assert.Error(t, err)
}

// =============================================
// Additional Coverage Tests for Error Paths
// Target: 100% patch coverage for file_processor.go
// =============================================

// TestFinalizeFileProcessing_InFileCloseError verifies error handling when input file close fails
// (covers lines 84-86 in file_processor.go)
func TestFinalizeFileProcessing_InFileCloseError(t *testing.T) {
	tempDir := t.TempDir()
	inputFile := filepath.Join(tempDir, "input.ndjson")
	outputFile := filepath.Join(tempDir, "output.ndjson")

	err := os.WriteFile(inputFile, []byte("test"), 0644)
	require.NoError(t, err)

	ctx, err := pipeline.SetupFileProcessing(inputFile, outputFile, false, "")
	require.NoError(t, err)

	// Close input file early to simulate error on second close
	require.NoError(t, ctx.InFile.Close())

	// Finalize should fail because input file is already closed
	err = pipeline.FinalizeFileProcessing(ctx, outputFile, true)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "input file")
}

// TestFinalizeFileProcessing_OutWriterCloseError verifies error handling when compressed writer close fails
// (covers lines 71-74 in file_processor.go)
func TestFinalizeFileProcessing_OutWriterCloseError(t *testing.T) {
	tempDir := t.TempDir()
	inputFile := filepath.Join(tempDir, "input.ndjson")
	outputFile := filepath.Join(tempDir, "output.ndjson.zst")

	err := os.WriteFile(inputFile, []byte("test"), 0644)
	require.NoError(t, err)

	ctx, err := pipeline.SetupFileProcessing(inputFile, outputFile, true, "default")
	require.NoError(t, err)

	// Close both OutWriter and OutFile early to simulate error
	// This forces the error path when FinalizeFileProcessing tries to close them
	require.NoError(t, ctx.OutWriter.Close())
	require.NoError(t, ctx.OutFile.Close())

	// Finalize should fail because writer/file is already closed
	err = pipeline.FinalizeFileProcessing(ctx, outputFile, true)
	// The error could be about compressed writer or output file, depending on which close fails
	assert.Error(t, err, "Should fail when writer/file is already closed")
}

// TestWriteProcessedResource_NewlineWriteError verifies error when newline write fails
// (covers lines 115-116 in file_processor.go)
func TestWriteProcessedResource_NewlineWriteError(t *testing.T) {
	// Create a writer that fails on the second write (the newline)
	fw := &failingWriterAfterN{successCount: 1}

	resource := map[string]any{
		"resourceType": "Patient",
		"id":           "123",
	}

	err := pipeline.WriteProcessedResource(resource, fw)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "newline")
}

// failingWriterAfterN implements io.Writer and fails after N successful writes
type failingWriterAfterN struct {
	successCount int
	callCount    int
}

func (f *failingWriterAfterN) Write(p []byte) (n int, err error) {
	f.callCount++
	if f.callCount > f.successCount {
		return 0, io.ErrClosedPipe
	}
	return len(p), nil
}

// TestFinalizeFileProcessing_CompressedOutFileCloseError verifies error when closing
// the underlying file for compressed output fails (covers lines 78-80 in file_processor.go)
func TestFinalizeFileProcessing_CompressedOutFileCloseError(t *testing.T) {
	tempDir := t.TempDir()
	inputFile := filepath.Join(tempDir, "input.ndjson")
	outputFile := filepath.Join(tempDir, "output.ndjson.zst")

	err := os.WriteFile(inputFile, []byte("test"), 0644)
	require.NoError(t, err)

	ctx, err := pipeline.SetupFileProcessing(inputFile, outputFile, true, "default")
	require.NoError(t, err)

	// Close OutFile (the underlying file) early but keep OutWriter open
	// This simulates a scenario where the underlying file is closed unexpectedly
	require.NoError(t, ctx.OutFile.Close())

	// Finalize will try to close OutWriter first (which might succeed or fail)
	// then try to close OutFile (which will fail because it's already closed)
	err = pipeline.FinalizeFileProcessing(ctx, outputFile, true)
	assert.Error(t, err)
}

// TestFinalizeFileProcessing_CleanupOnNoSuccess verifies cleanup is called when markSuccess is false
// (covers lines 90-92 in file_processor.go)
func TestFinalizeFileProcessing_CleanupOnNoSuccess(t *testing.T) {
	tempDir := t.TempDir()
	inputFile := filepath.Join(tempDir, "input.ndjson")
	outputFile := filepath.Join(tempDir, "output.ndjson")

	err := os.WriteFile(inputFile, []byte("test"), 0644)
	require.NoError(t, err)

	ctx, err := pipeline.SetupFileProcessing(inputFile, outputFile, false, "")
	require.NoError(t, err)

	// Write some data to temp file
	_, err = ctx.OutFile.Write([]byte(`{"test":"data"}`))
	require.NoError(t, err)

	// Finalize without success - should cleanup temp file
	err = pipeline.FinalizeFileProcessing(ctx, outputFile, false)
	assert.NoError(t, err)

	// Verify temp file was cleaned up
	_, statErr := os.Stat(ctx.TempFile)
	assert.True(t, os.IsNotExist(statErr), "Temp file should be removed on no success")

	// Verify output file doesn't exist
	_, statErr = os.Stat(outputFile)
	assert.True(t, os.IsNotExist(statErr), "Output file should not exist on no success")
}

// failingWriteCloser is a mock WriteCloser that fails on Close
type failingWriteCloser struct {
	io.Writer
	closeErr error
}

func (f *failingWriteCloser) Close() error {
	return f.closeErr
}

// TestFinalizeFileProcessing_OutWriterCloseErrorWithMock uses a mock writer to trigger
// the OutWriter.Close() error path (covers lines 65-68 in file_processor.go)
func TestFinalizeFileProcessing_OutWriterCloseErrorWithMock(t *testing.T) {
	tempDir := t.TempDir()
	inputFile := filepath.Join(tempDir, "input.ndjson")
	outputFile := filepath.Join(tempDir, "output.ndjson.zst")
	tempOutputFile := outputFile + ".part"

	// Create input file
	err := os.WriteFile(inputFile, []byte("test"), 0644)
	require.NoError(t, err)

	// Open input file
	inFile, err := os.Open(inputFile)
	require.NoError(t, err)

	// Create output file
	outFile, err := os.Create(tempOutputFile)
	require.NoError(t, err)

	// Create a mock OutWriter that fails on Close
	mockWriter := &failingWriteCloser{
		Writer:   outFile,
		closeErr: errors.New("mock close error"),
	}

	cleanupCalled := false
	ctx := &pipeline.FileContext{
		InFile:    inFile,
		OutFile:   outFile,
		OutWriter: mockWriter, // Different from OutFile, so the branch is taken
		TempFile:  tempOutputFile,
		Cleanup: func() {
			cleanupCalled = true
			_ = os.Remove(tempOutputFile)
		},
	}

	// Finalize should fail because OutWriter.Close() fails
	err = pipeline.FinalizeFileProcessing(ctx, outputFile, true)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "compressed writer")
	assert.True(t, cleanupCalled, "Cleanup should be called on error")
}
