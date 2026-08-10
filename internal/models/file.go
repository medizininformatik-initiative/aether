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

// trimSuffixFold removes suffix from s if it matches case-insensitively,
// reporting whether a match was found.
func trimSuffixFold(s, suffix string) (string, bool) {
	if len(s) >= len(suffix) && strings.EqualFold(s[len(s)-len(suffix):], suffix) {
		return s[:len(s)-len(suffix)], true
	}
	return s, false
}

// GetNormalizedBaseName returns the base filename without compression extension.
// The .zst and .ndjson extensions are matched case-insensitively and lowercased
// in the result; the case of the file stem is preserved.
// Example: "Patient.ndjson.zst" -> "Patient.ndjson"
// Example: "Patient.NDJSON.ZST" -> "Patient.ndjson"
// Example: "patient.ndjson" -> "patient.ndjson"
func GetNormalizedBaseName(filename string) string {
	base := filepath.Base(filename)
	base, _ = trimSuffixFold(base, ".zst")
	if stem, ok := trimSuffixFold(base, ".ndjson"); ok {
		base = stem + ".ndjson"
	}
	return base
}

// DetectDuplicateFHIRFiles checks if two or more files normalize to the same
// basename once a .zst compression suffix is stripped, since local_import
// would otherwise silently overwrite one with the other. Returns an error
// listing the full source path of every colliding file, or nil if none exist.
func DetectDuplicateFHIRFiles(files []string) error {
	seen := make(map[string][]string)

	for _, file := range files {
		normalized := GetNormalizedBaseName(file)
		seen[normalized] = append(seen[normalized], file)
	}

	var duplicates []string
	for baseName, fileList := range seen {
		if len(fileList) > 1 {
			duplicates = append(duplicates, fmt.Sprintf("%s: [%s]", baseName, strings.Join(fileList, ", ")))
		}
	}

	if len(duplicates) > 0 {
		return fmt.Errorf("found duplicate input files that normalize to the same basename: %s", strings.Join(duplicates, "; "))
	}

	return nil
}
