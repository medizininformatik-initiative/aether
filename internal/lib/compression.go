package lib

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/klauspost/compress/zstd"
)

// CompressedFileExtension is the extension added to compressed files.
const CompressedFileExtension = ".zst"

// IsCompressedFile checks if a filename indicates a compressed file.
func IsCompressedFile(filename string) bool {
	return strings.HasSuffix(strings.ToLower(filename), CompressedFileExtension)
}

// GetUncompressedFilename strips the .zst extension if present.
func GetUncompressedFilename(filename string) string {
	if IsCompressedFile(filename) {
		return strings.TrimSuffix(filename, CompressedFileExtension)
	}
	return filename
}

// GetCompressedFilename adds the .zst extension if compression is enabled
// and the file is not already compressed.
func GetCompressedFilename(filename string, compress bool) string {
	if !compress {
		return filename
	}
	if IsCompressedFile(filename) {
		return filename
	}
	return filename + CompressedFileExtension
}

// compressionReader wraps a file with optional zstd decompression.
type compressionReader struct {
	file    *os.File
	decoder *zstd.Decoder
}

// Read implements io.Reader, delegating to the decoder if present.
func (r *compressionReader) Read(p []byte) (int, error) {
	if r.decoder != nil {
		return r.decoder.Read(p)
	}
	return r.file.Read(p)
}

// Close closes the decoder (if present) and the underlying file.
func (r *compressionReader) Close() error {
	var errs []error
	if r.decoder != nil {
		r.decoder.Close()
	}
	if err := r.file.Close(); err != nil {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

// OpenFileForReading opens a file for reading, auto-detecting compression.
// If the file has a .zst extension, it will be decompressed transparently.
// The caller must close the returned ReadCloser when done.
func OpenFileForReading(filePath string) (io.ReadCloser, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}

	reader := &compressionReader{file: file}

	if IsCompressedFile(filePath) {
		decoder, err := zstd.NewReader(file)
		if err != nil {
			_ = file.Close()
			return nil, fmt.Errorf("failed to create zstd decoder: %w", err)
		}
		reader.decoder = decoder
	}

	return reader, nil
}

// compressionWriter wraps a file with optional zstd compression.
type compressionWriter struct {
	file    *os.File
	encoder *zstd.Encoder
}

// Write implements io.Writer, delegating to the encoder if present.
func (w *compressionWriter) Write(p []byte) (int, error) {
	if w.encoder != nil {
		return w.encoder.Write(p)
	}
	return w.file.Write(p)
}

// Close closes the encoder (if present) and the underlying file.
func (w *compressionWriter) Close() error {
	var firstErr error
	if w.encoder != nil {
		if err := w.encoder.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if err := w.file.Close(); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

// getZstdEncoderLevel converts a string level to zstd.EncoderLevel.
func getZstdEncoderLevel(level string) zstd.EncoderLevel {
	switch strings.ToLower(level) {
	case "fastest":
		return zstd.SpeedFastest
	case "better":
		return zstd.SpeedBetterCompression
	case "best":
		return zstd.SpeedBestCompression
	default:
		return zstd.SpeedDefault
	}
}

// CreateCompressedWriter creates a writer that compresses data with zstd.
// The level parameter can be "fastest", "default", "better", or "best".
// The caller must close the returned WriteCloser when done.
func CreateCompressedWriter(w io.Writer, level string) (io.WriteCloser, error) {
	encoderLevel := getZstdEncoderLevel(level)
	encoder, err := zstd.NewWriter(w, zstd.WithEncoderLevel(encoderLevel))
	if err != nil {
		return nil, fmt.Errorf("failed to create zstd encoder: %w", err)
	}
	return encoder, nil
}

// CreateCompressedFileWriter creates a file writer with zstd compression.
// This is a convenience function that combines file creation and compression.
// The caller must close the returned WriteCloser when done.
func CreateCompressedFileWriter(filePath string, level string) (io.WriteCloser, error) {
	file, err := os.Create(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to create file: %w", err)
	}

	encoder, err := CreateCompressedWriter(file, level)
	if err != nil {
		_ = file.Close()
		return nil, err
	}

	return &compressionWriter{
		file:    file,
		encoder: encoder.(*zstd.Encoder),
	}, nil
}

// ValidateCompressionLevel checks if the compression level is valid.
func ValidateCompressionLevel(level string) error {
	switch strings.ToLower(level) {
	case "fastest", "default", "better", "best", "":
		return nil
	default:
		return fmt.Errorf("invalid compression level %q: must be one of fastest, default, better, best", level)
	}
}
