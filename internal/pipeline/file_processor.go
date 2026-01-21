package pipeline

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/medizininformatik-initiative/aether/internal/lib"
)

// FileContext holds file handles and cleanup logic for atomic file writing
type FileContext struct {
	InFile    io.ReadCloser  // Input file (may be compressed)
	OutFile   *os.File       // Output file handle
	OutWriter io.WriteCloser // Output writer (may be compressed)
	TempFile  string
	Cleanup   func()
}

// SetupFileProcessing initializes files for atomic write pattern
// Writes to .part file first, renamed on success
// If compress is true, output will be compressed with zstd
// Handles both compressed and uncompressed input files transparently
func SetupFileProcessing(inputFile, outputFile string, compress bool, compressionLevel string) (*FileContext, error) {
	inFile, err := lib.OpenFileForReading(inputFile)
	if err != nil {
		return nil, fmt.Errorf("failed to open input file: %w", err)
	}

	tempOutputFile := outputFile + ".part"
	outFile, err := os.Create(tempOutputFile)
	if err != nil {
		_ = inFile.Close()
		return nil, fmt.Errorf("failed to create temporary output file: %w", err)
	}

	var outWriter io.WriteCloser = outFile
	if compress {
		compWriter, err := lib.CreateCompressedWriter(outFile, compressionLevel)
		if err != nil {
			_ = outFile.Close()
			_ = inFile.Close()
			return nil, fmt.Errorf("failed to create compressed writer: %w", err)
		}
		outWriter = compWriter
	}

	ctx := &FileContext{
		InFile:    inFile,
		OutFile:   outFile,
		OutWriter: outWriter,
		TempFile:  tempOutputFile,
		Cleanup: func() {
			_ = os.Remove(tempOutputFile)
		},
	}

	return ctx, nil
}

// FinalizeFileProcessing closes files and atomically renames .part to final filename
func FinalizeFileProcessing(ctx *FileContext, outputFile string, markSuccess bool) error {
	if ctx.OutWriter != nil && ctx.OutWriter != ctx.OutFile {
		if err := ctx.OutWriter.Close(); err != nil {
			ctx.Cleanup()
			return fmt.Errorf("failed to close compressed writer: %w", err)
		}
	}

	if err := ctx.OutFile.Close(); err != nil {
		ctx.Cleanup()
		return fmt.Errorf("failed to close output file: %w", err)
	}

	if err := ctx.InFile.Close(); err != nil {
		ctx.Cleanup()
		return fmt.Errorf("failed to close input file: %w", err)
	}

	if !markSuccess {
		ctx.Cleanup()
		return nil
	}

	if err := os.Rename(ctx.TempFile, outputFile); err != nil {
		ctx.Cleanup()
		return fmt.Errorf("failed to rename temporary file: %w", err)
	}

	return nil
}

// WriteProcessedResource marshals and writes a FHIR resource to a writer
// The writer may be a plain file or a compressed writer
func WriteProcessedResource(resource map[string]any, writer io.Writer) error {
	pseudonymizedJSON, err := json.Marshal(resource)
	if err != nil {
		return fmt.Errorf("failed to marshal pseudonymized resource: %w", err)
	}

	if _, err := writer.Write(pseudonymizedJSON); err != nil {
		return fmt.Errorf("failed to write output: %w", err)
	}
	if _, err := writer.Write([]byte("\n")); err != nil {
		return fmt.Errorf("failed to write newline: %w", err)
	}

	return nil
}
