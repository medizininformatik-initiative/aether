// Package testutil provides shared mock implementations for testing error paths
// in compression, file processing, and I/O operations.
package testutil

import (
	"errors"
	"io"
)

// FailingWriter is a writer that fails after N successful writes.
// Useful for testing error handling in multi-write scenarios.
type FailingWriter struct {
	// SuccessCount is the number of writes to succeed before failing
	SuccessCount int
	// CallCount tracks the number of Write calls made
	CallCount int
	// Err is the error to return on failure (defaults to ErrWriteFailed)
	Err error
	// BytesWritten tracks total bytes written before failure
	BytesWritten int
}

// ErrWriteFailed is the default error returned by FailingWriter
var ErrWriteFailed = errors.New("mock write failure")

// Write implements io.Writer. Returns error after SuccessCount successful writes.
func (w *FailingWriter) Write(p []byte) (int, error) {
	w.CallCount++
	if w.CallCount > w.SuccessCount {
		if w.Err != nil {
			return 0, w.Err
		}
		return 0, ErrWriteFailed
	}
	w.BytesWritten += len(p)
	return len(p), nil
}

// FailingCloser wraps a writer and returns an error on Close.
// The underlying writer receives all writes normally.
type FailingCloser struct {
	io.Writer
	// CloseErr is the error to return from Close
	CloseErr error
	// Closed tracks if Close was called
	Closed bool
}

// Close implements io.Closer and returns CloseErr.
func (c *FailingCloser) Close() error {
	c.Closed = true
	return c.CloseErr
}

// FailingReader is a reader that fails after reading N bytes.
// Useful for testing error handling in copy operations.
type FailingReader struct {
	// Data is the data to return before failing
	Data []byte
	// Offset tracks current position in Data
	Offset int
	// FailAfter is the number of bytes to read before failing
	// -1 means never fail during Read
	FailAfter int
	// Err is the error to return on failure (defaults to ErrReadFailed)
	Err error
}

// ErrReadFailed is the default error returned by FailingReader
var ErrReadFailed = errors.New("mock read failure")

// Read implements io.Reader. Returns error after reading FailAfter bytes.
func (r *FailingReader) Read(p []byte) (int, error) {
	// Check if we should fail
	if r.FailAfter >= 0 && r.Offset >= r.FailAfter {
		if r.Err != nil {
			return 0, r.Err
		}
		return 0, ErrReadFailed
	}

	// Check for EOF
	if r.Offset >= len(r.Data) {
		return 0, io.EOF
	}

	// Calculate how many bytes we can read
	remaining := len(r.Data) - r.Offset
	toRead := len(p)
	if toRead > remaining {
		toRead = remaining
	}

	// Check if this read would exceed FailAfter
	if r.FailAfter >= 0 && r.Offset+toRead > r.FailAfter {
		toRead = r.FailAfter - r.Offset
	}

	// Copy data
	copy(p, r.Data[r.Offset:r.Offset+toRead])
	r.Offset += toRead

	return toRead, nil
}

// FailingReadCloser combines FailingReader with Close error capability.
type FailingReadCloser struct {
	FailingReader
	// CloseErr is the error to return from Close
	CloseErr error
	// Closed tracks if Close was called
	Closed bool
}

// Close implements io.Closer.
func (r *FailingReadCloser) Close() error {
	r.Closed = true
	return r.CloseErr
}

// LimitedWriter writes up to MaxBytes, then returns ErrShortWrite.
// Useful for testing disk-full scenarios.
type LimitedWriter struct {
	// MaxBytes is the maximum number of bytes to accept
	MaxBytes int
	// Written tracks bytes written so far
	Written int
}

// ErrShortWrite is returned when LimitedWriter reaches its limit
var ErrShortWrite = errors.New("short write: disk full simulation")

// Write implements io.Writer with a byte limit.
func (w *LimitedWriter) Write(p []byte) (int, error) {
	remaining := w.MaxBytes - w.Written
	if remaining <= 0 {
		return 0, ErrShortWrite
	}

	toWrite := len(p)
	if toWrite > remaining {
		toWrite = remaining
		w.Written += toWrite
		return toWrite, ErrShortWrite
	}

	w.Written += toWrite
	return toWrite, nil
}

// CountingWriter counts bytes written without storing them.
type CountingWriter struct {
	BytesWritten int64
}

// Write implements io.Writer, counting bytes.
func (w *CountingWriter) Write(p []byte) (int, error) {
	w.BytesWritten += int64(len(p))
	return len(p), nil
}

// NopCloser wraps a writer and provides a no-op Close.
type NopCloser struct {
	io.Writer
}

// Close implements io.Closer as a no-op.
func (NopCloser) Close() error {
	return nil
}

// NewNopCloser creates a new NopCloser wrapping the given writer.
func NewNopCloser(w io.Writer) *NopCloser {
	return &NopCloser{Writer: w}
}
