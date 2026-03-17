package services

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
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

// WriteCSV writes CSV data with header to a file
// The header is constructed from the ViewDefinition column names
// The data comes from the flattener service (CSV body without header)
func (w *CSVWriter) WriteCSV(filename string, header []string, data string) error {
	outputPath := filepath.Join(w.outputDir, filename)

	// Ensure output directory exists
	if err := os.MkdirAll(w.outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Create the output file
	file, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create CSV file: %w", err)
	}
	defer func() { _ = file.Close() }()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	// Write header row
	if len(header) > 0 {
		if err := writer.Write(header); err != nil {
			return fmt.Errorf("failed to write CSV header: %w", err)
		}
	}

	// Parse and write data rows
	if data != "" {
		// The data from flattener is already in CSV format (without header)
		// We need to parse it and write it row by row
		dataReader := csv.NewReader(strings.NewReader(data))
		records, err := dataReader.ReadAll()
		if err != nil {
			return fmt.Errorf("failed to parse CSV data from flattener: %w", err)
		}

		for _, record := range records {
			if err := writer.Write(record); err != nil {
				return fmt.Errorf("failed to write CSV row: %w", err)
			}
		}
	}

	return nil
}

// AppendCSVData appends CSV data to a file. On the first batch, it creates the file
// and writes the header followed by data rows. On subsequent batches, it appends
// data rows only (no duplicate header).
func (w *CSVWriter) AppendCSVData(filename string, header []string, data string, isFirstBatch bool) error {
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
		defer writer.Flush()

		if len(header) > 0 {
			if err := writer.Write(header); err != nil {
				return fmt.Errorf("failed to write CSV header: %w", err)
			}
		}

		if data != "" {
			if err := w.writeCSVRows(writer, data); err != nil {
				return err
			}
		}

		return nil
	}

	// Subsequent batch: append data rows only
	if data == "" {
		return nil
	}

	file, err := os.OpenFile(outputPath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open CSV file for append: %w", err)
	}
	defer func() { _ = file.Close() }()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	return w.writeCSVRows(writer, data)
}

// writeCSVRows parses CSV data string and writes rows to the writer
func (w *CSVWriter) writeCSVRows(writer *csv.Writer, data string) error {
	dataReader := csv.NewReader(strings.NewReader(data))
	records, err := dataReader.ReadAll()
	if err != nil {
		return fmt.Errorf("failed to parse CSV data from flattener: %w", err)
	}

	for _, record := range records {
		if err := writer.Write(record); err != nil {
			return fmt.Errorf("failed to write CSV row: %w", err)
		}
	}

	return nil
}

// WriteCSVDirect writes raw CSV data (with header already included) directly to a file
func (w *CSVWriter) WriteCSVDirect(filename string, data string) error {
	outputPath := filepath.Join(w.outputDir, filename)

	// Ensure output directory exists
	if err := os.MkdirAll(w.outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Write data directly to file
	if err := os.WriteFile(outputPath, []byte(data), 0644); err != nil {
		return fmt.Errorf("failed to write CSV file: %w", err)
	}

	return nil
}

// BuildCSVFilename creates a CSV filename from an attribute group name
func BuildCSVFilename(groupName string) string {
	return lib.SanitizeFilename(groupName) + ".csv"
}

// ExtractHeaderFromViewDefinition extracts column names from a ViewDefinition
// This is used to construct the CSV header since fhir-flattener returns data without headers
func ExtractHeaderFromViewDefinition(viewDef models.ViewDefinition) []string {
	return ExtractColumnNames(viewDef)
}

// CountCSVRows counts the number of data rows in a CSV string (excluding header if present)
func CountCSVRows(csvData string, hasHeader bool) (int, error) {
	if csvData == "" {
		return 0, nil
	}

	reader := csv.NewReader(strings.NewReader(csvData))
	records, err := reader.ReadAll()
	if err != nil {
		return 0, fmt.Errorf("failed to parse CSV: %w", err)
	}

	count := len(records)
	if hasHeader && count > 0 {
		count--
	}

	return count, nil
}

// ValidateCSVHeader checks if a CSV header matches expected columns
func ValidateCSVHeader(csvData string, expectedColumns []string) error {
	if csvData == "" {
		return fmt.Errorf("CSV data is empty")
	}

	reader := csv.NewReader(strings.NewReader(csvData))
	header, err := reader.Read()
	if err != nil {
		return fmt.Errorf("failed to read CSV header: %w", err)
	}

	if len(header) != len(expectedColumns) {
		return fmt.Errorf("header column count mismatch: got %d, expected %d", len(header), len(expectedColumns))
	}

	for i, col := range expectedColumns {
		if header[i] != col {
			return fmt.Errorf("header column mismatch at index %d: got '%s', expected '%s'", i, header[i], col)
		}
	}

	return nil
}
