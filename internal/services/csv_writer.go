package services

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/medizininformatik-initiative/aether/internal/lib"
)

// PartialSuffix marks CSV files that are still being written or were left
// behind by a failed run. A file without the suffix is complete.
const PartialSuffix = ".partial"

// CSVWriter handles writing flattened data to CSV files
type CSVWriter struct {
	outputDir string
}

// partialPath returns the in-progress path for a CSV filename
func (w *CSVWriter) partialPath(filename string) string {
	return filepath.Join(w.outputDir, filename+PartialSuffix)
}

// NewCSVWriter creates a new CSVWriter with the given output directory
func NewCSVWriter(outputDir string) *CSVWriter {
	return &CSVWriter{
		outputDir: outputDir,
	}
}

// AppendCSVData appends parsed rows to the partial file for filename. On the
// first batch, it creates the file and writes the header followed by the rows.
// On subsequent batches, it appends rows only (no duplicate header).
// Call Finalize after the last batch to publish the file under its final name.
func (w *CSVWriter) AppendCSVData(filename string, header []string, rows [][]string, isFirstBatch bool) error {
	// Ensure output directory exists
	if err := os.MkdirAll(w.outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	if isFirstBatch {
		file, err := os.Create(w.partialPath(filename))
		if err != nil {
			return fmt.Errorf("failed to create CSV file: %w", err)
		}
		return w.writeRecords(file, header, rows)
	}

	// Subsequent batch: append rows only
	if len(rows) == 0 {
		return nil
	}

	file, err := os.OpenFile(w.partialPath(filename), os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open CSV file for append: %w", err)
	}

	return w.writeRecords(file, nil, rows)
}

// writeRecords writes the header and the rows to dst as CSV, then closes dst.
// It reports the error of Close, because a write fault on a network mount
// often surfaces only there.
func (w *CSVWriter) writeRecords(dst io.WriteCloser, header []string, rows [][]string) (err error) {
	defer func() {
		if cerr := dst.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("failed to close CSV file: %w", cerr)
		}
	}()

	writer := csv.NewWriter(dst)

	if len(header) > 0 {
		if err := writer.Write(header); err != nil {
			return fmt.Errorf("failed to write CSV header: %w", err)
		}
	}

	// WriteAll flushes, so a buffered write fault surfaces here
	if err := writer.WriteAll(rows); err != nil {
		return fmt.Errorf("failed to write CSV rows: %w", err)
	}

	return nil
}

// PartialFiles returns the names of the partial files in the output directory
func (w *CSVWriter) PartialFiles() ([]string, error) {
	matches, err := filepath.Glob(filepath.Join(w.outputDir, "*"+PartialSuffix))
	if err != nil {
		return nil, fmt.Errorf("failed to list partial CSV files: %w", err)
	}

	names := make([]string, 0, len(matches))
	for _, match := range matches {
		names = append(names, filepath.Base(match))
	}
	return names, nil
}

// RemovePartials deletes all partial files that an earlier run left in the
// output directory, so a retry starts from empty output
func (w *CSVWriter) RemovePartials() error {
	names, err := w.PartialFiles()
	if err != nil {
		return err
	}

	for _, name := range names {
		if err := os.Remove(filepath.Join(w.outputDir, name)); err != nil {
			return fmt.Errorf("failed to remove stale partial CSV file: %w", err)
		}
	}
	return nil
}

// Finalize renames the partial file to its final name. Call it only after the
// last batch for filename was appended without error.
func (w *CSVWriter) Finalize(filename string) error {
	if err := os.Rename(w.partialPath(filename), filepath.Join(w.outputDir, filename)); err != nil {
		return fmt.Errorf("failed to finalize CSV file: %w", err)
	}
	return nil
}

// BuildCSVFilename creates a CSV filename from an attribute group name
func BuildCSVFilename(groupName string) string {
	return lib.SanitizeFilename(groupName) + ".csv"
}
