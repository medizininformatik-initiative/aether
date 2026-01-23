package models

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// FHIRDataFile represents a single FHIR NDJSON file in the pipeline
type FHIRDataFile struct {
	FileName   string    `json:"file_name"`
	FilePath   string    `json:"file_path"`   // Relative to job directory
	FileSize   int64     `json:"file_size"`   // Bytes
	SourceStep StepName  `json:"source_step"` // Which step produced this file
	LineCount  int       `json:"line_count"`  // Number of FHIR resources
	CreatedAt  time.Time `json:"created_at"`
}

// IsValidFHIRFile checks if the file has valid FHIR NDJSON format
// Accepts both uncompressed (.ndjson) and compressed (.ndjson.zst) files
func IsValidFHIRFile(filename string) bool {
	lower := strings.ToLower(filename)
	return strings.HasSuffix(lower, ".ndjson") || strings.HasSuffix(lower, ".ndjson.zst")
}

// IsSafePath checks if a file path is within job directory boundaries
// Prevents path traversal attacks (e.g., ../../etc/passwd)
func IsSafePath(path string) bool {
	clean := filepath.Clean(path)

	if filepath.IsAbs(clean) {
		return false
	}

	if strings.HasPrefix(clean, "..") {
		return false
	}

	return true
}

// GetNormalizedBaseName returns the base filename without compression extension.
// Example: "Patient.ndjson.zst" -> "Patient.ndjson"
// Example: "Patient.ndjson" -> "Patient.ndjson"
func GetNormalizedBaseName(filename string) string {
	base := filepath.Base(filename)
	return strings.TrimSuffix(base, ".zst")
}

// DetectDuplicateFHIRFiles checks if there are files with both compressed and
// uncompressed versions (e.g., Patient.ndjson and Patient.ndjson.zst).
// Returns an error listing all duplicates found, or nil if no duplicates exist.
func DetectDuplicateFHIRFiles(files []string) error {
	seen := make(map[string][]string)

	for _, file := range files {
		normalized := GetNormalizedBaseName(file)
		seen[normalized] = append(seen[normalized], filepath.Base(file))
	}

	var duplicates []string
	for baseName, fileList := range seen {
		if len(fileList) > 1 {
			duplicates = append(duplicates, fmt.Sprintf("%s: [%s]", baseName, strings.Join(fileList, ", ")))
		}
	}

	if len(duplicates) > 0 {
		return fmt.Errorf("found duplicate FHIR files (both compressed and uncompressed versions exist): %s", strings.Join(duplicates, "; "))
	}

	return nil
}
