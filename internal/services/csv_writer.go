package services

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"

	"github.com/medizininformatik-initiative/aether/internal/lib"
)

// CSVWriter handles writing flattened data to CSV files
type CSVWriter struct {
	outputDir string
}

// NewCSVWriter creates a new CSVWriter with the given output directory
func NewCSVWriter(outputDir string) *CSVWriter {
	return &CSVWriter{
		outputDir: outputDir,
	}
}

// AppendCSVData appends parsed rows to a file. On the first batch, it creates
// the file and writes the header followed by the rows. On subsequent batches,
// it appends rows only (no duplicate header).
func (w *CSVWriter) AppendCSVData(filename string, header []string, rows [][]string, isFirstBatch bool) error {
	outputPath := filepath.Join(w.outputDir, filename)

	// Ensure output directory exists
	if err := os.MkdirAll(w.outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	if isFirstBatch {
		file, err := os.Create(outputPath)
		if err != nil {
			return fmt.Errorf("failed to create CSV file: %w", err)
		}
		defer func() { _ = file.Close() }()

		writer := csv.NewWriter(file)

		if len(header) > 0 {
			if err := writer.Write(header); err != nil {
				return fmt.Errorf("failed to write CSV header: %w", err)
			}
		}

		if err := writer.WriteAll(rows); err != nil {
			return fmt.Errorf("failed to write CSV rows: %w", err)
		}
		return nil
	}

	// Subsequent batch: append rows only
	if len(rows) == 0 {
		return nil
	}

	file, err := os.OpenFile(outputPath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open CSV file for append: %w", err)
	}
	defer func() { _ = file.Close() }()

	writer := csv.NewWriter(file)
	if err := writer.WriteAll(rows); err != nil {
		return fmt.Errorf("failed to write CSV rows: %w", err)
	}
	return nil
}

// BuildCSVFilename creates a CSV filename from an attribute group name
func BuildCSVFilename(groupName string) string {
	return lib.SanitizeFilename(groupName) + ".csv"
}
