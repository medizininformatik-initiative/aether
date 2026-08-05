package services

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// failingWriteCloser fails writes and/or the final Close.
type failingWriteCloser struct {
	writeErr error
	closeErr error
}

func (f *failingWriteCloser) Write(p []byte) (int, error) {
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	return len(p), nil
}

func (f *failingWriteCloser) Close() error {
	return f.closeErr
}

func TestWriteRecords_ReportsWriteError(t *testing.T) {
	w := NewCSVWriter(t.TempDir())
	dst := &failingWriteCloser{writeErr: errors.New("disk fault")}

	err := w.writeRecords(dst, []string{"id"}, [][]string{{"1"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to write CSV rows")
	assert.Contains(t, err.Error(), "disk fault")
}

func TestWriteRecords_ReportsHeaderWriteError(t *testing.T) {
	w := NewCSVWriter(t.TempDir())
	dst := &failingWriteCloser{writeErr: errors.New("disk fault")}

	// csv.Writer buffers through a 4096-byte bufio.Writer, so a short header
	// only fills the buffer. A header above that size flushes during Write,
	// which is where the fault of the disk surfaces.
	header := make([]string, 512)
	for i := range header {
		header[i] = strings.Repeat("x", 16)
	}

	err := w.writeRecords(dst, header, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to write CSV header")
	assert.Contains(t, err.Error(), "disk fault")
}

func TestWriteRecords_ReportsCloseError(t *testing.T) {
	w := NewCSVWriter(t.TempDir())
	dst := &failingWriteCloser{closeErr: errors.New("close fault")}

	err := w.writeRecords(dst, []string{"id"}, [][]string{{"1"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to close CSV file")
	assert.Contains(t, err.Error(), "close fault")
}

func TestWriteRecords_KeepsFirstError(t *testing.T) {
	w := NewCSVWriter(t.TempDir())
	dst := &failingWriteCloser{
		writeErr: errors.New("disk fault"),
		closeErr: errors.New("close fault"),
	}

	// The write error must win over the later close error
	err := w.writeRecords(dst, []string{"id"}, [][]string{{"1"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to write CSV rows")
	assert.NotContains(t, err.Error(), "close fault")
}
