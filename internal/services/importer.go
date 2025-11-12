package services

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
)

// ImportFromLocalDirectory copies FHIR NDJSON files from a local directory to the job's import directory
// Returns list of imported files and any error
func ImportFromLocalDirectory(sourcePath string, destinationDir string, logger *lib.Logger) ([]models.FHIRDataFile, error) {
	// Validate source directory exists
	sourceInfo, err := os.Stat(sourcePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("source directory does not exist: %s", sourcePath)
		}
		return nil, fmt.Errorf("cannot access source directory: %w", err)
	}

	if !sourceInfo.IsDir() {
		fileExt := strings.ToLower(filepath.Ext(sourcePath))
		switch fileExt {
		case ".json", ".crtdl":
			return nil, fmt.Errorf("source path is a JSON/CRTDL file, not a directory: %s. For CRTDL input, the file should have been detected as InputTypeCRTDL. This suggests the CRTDL file is invalid or missing required fields", sourcePath)
		default:
			return nil, fmt.Errorf("source path is not a directory: %s", sourcePath)
		}
	}

	// Ensure destination directory exists
	if err := os.MkdirAll(destinationDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create destination directory: %w", err)
	}

	// Find all NDJSON files in source directory
	ndjsonFiles, err := findNDJSONFiles(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("failed to scan source directory: %w", err)
	}

	if len(ndjsonFiles) == 0 {
		return nil, fmt.Errorf("no FHIR NDJSON files found in %s", sourcePath)
	}

	logger.Info("Found FHIR files", "count", len(ndjsonFiles), "source", sourcePath)

	// Import each file
	var importedFiles []models.FHIRDataFile
	for _, srcFile := range ndjsonFiles {
		imported, err := copyFile(srcFile, destinationDir, logger)
		if err != nil {
			return importedFiles, fmt.Errorf("failed to import %s: %w", srcFile, err)
		}
		importedFiles = append(importedFiles, imported)
	}

	logger.Info("Import completed", "files", len(importedFiles))
	return importedFiles, nil
}

// findNDJSONFiles recursively finds all .ndjson files in a directory
func findNDJSONFiles(rootPath string) ([]string, error) {
	var files []string

	err := filepath.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		// Check if file is NDJSON
		if models.IsValidFHIRFile(info.Name()) {
			files = append(files, path)
		}

		return nil
	})

	return files, err
}

// copyFile copies a single file to the destination directory
// Returns FHIRDataFile metadata
func copyFile(sourcePath string, destDir string, logger *lib.Logger) (models.FHIRDataFile, error) {
	// Open source file
	srcFile, err := os.Open(sourcePath)
	if err != nil {
		return models.FHIRDataFile{}, fmt.Errorf("failed to open source file: %w", err)
	}
	defer func() {
		if err := srcFile.Close(); err != nil {
			logger.Error("Failed to close source file", "error", err)
		}
	}()

	// Get source file info
	srcInfo, err := srcFile.Stat()
	if err != nil {
		return models.FHIRDataFile{}, fmt.Errorf("failed to stat source file: %w", err)
	}

	// Create destination file path
	fileName := filepath.Base(sourcePath)
	destPath := filepath.Join(destDir, fileName)

	// Create destination file
	destFile, err := os.Create(destPath)
	if err != nil {
		return models.FHIRDataFile{}, fmt.Errorf("failed to create destination file: %w", err)
	}
	defer func() {
		if err := destFile.Close(); err != nil {
			logger.Error("Failed to close destination file", "error", err)
		}
	}()

	// Copy file contents
	bytesWritten, err := io.Copy(destFile, srcFile)
	if err != nil {
		return models.FHIRDataFile{}, fmt.Errorf("failed to copy file: %w", err)
	}

	// Count lines (FHIR resources)
	lineCount, err := lib.CountResourcesInFile(destPath)
	if err != nil {
		logger.Warn("Failed to count resources", "file", fileName, "error", err)
		lineCount = 0
	}

	// Extract resource type from filename
	resourceType := models.GetResourceTypeFromFilename(fileName)

	logger.Debug("File imported", "file", fileName, "size", bytesWritten, "resources", lineCount)

	return models.FHIRDataFile{
		FileName:     fileName,
		FilePath:     fileName, // Relative to job import directory
		ResourceType: resourceType,
		FileSize:     bytesWritten,
		SourceStep:   models.StepLocalImport,
		LineCount:    lineCount,
		CreatedAt:    srcInfo.ModTime(),
	}, nil
}

// ValidateImportSource checks if an import source is valid
func ValidateImportSource(sourcePath string, inputType models.InputType) error {
	switch inputType {
	case models.InputTypeLocal:
		// Check if directory exists and is accessible
		info, err := os.Stat(sourcePath)
		if err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("directory does not exist: %s", sourcePath)
			}
			return fmt.Errorf("cannot access directory: %w", err)
		}

		if !info.IsDir() {
			// Provide helpful hint if it's a file that was misdetected
			fileExt := strings.ToLower(filepath.Ext(sourcePath))
			var hint string
			switch fileExt {
			case ".json", ".crtdl":
				hint = "\n\nThis appears to be a JSON/CRTDL file. Possible issues:\n  - File may not have valid CRTDL structure (missing cohortDefinition or dataExtraction)\n  - File may be using FHIR Parameters format instead of flat CRTDL format\n\nRun with verbose logging to see detailed validation errors."
			case ".ndjson":
				hint = "\n\nThis is an NDJSON file. Please provide the directory containing it, not the file itself."
			}
			return fmt.Errorf("expected directory but got file: %s%s", sourcePath, hint)
		}

		// Check if directory contains NDJSON files
		files, err := findNDJSONFiles(sourcePath)
		if err != nil {
			return fmt.Errorf("failed to scan directory: %w", err)
		}

		if len(files) == 0 {
			return fmt.Errorf("no FHIR NDJSON files found in directory: %s\n\nExpected files with extensions: .ndjson", sourcePath)
		}

		return nil

	case models.InputTypeHTTP:
		// URL validation already done in models.Validate()
		// Just check format
		if sourcePath == "" {
			return fmt.Errorf("URL cannot be empty")
		}
		return nil

	case models.InputTypeCRTDL:
		// CRTDL file validation
		if sourcePath == "" {
			return fmt.Errorf("CRTDL file path cannot be empty")
		}
		// Check file exists and is readable
		info, err := os.Stat(sourcePath)
		if err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("CRTDL file does not exist: %s", sourcePath)
			}
			return fmt.Errorf("cannot access CRTDL file: %w", err)
		}
		if info.IsDir() {
			return fmt.Errorf("CRTDL path is a directory, not a file: %s", sourcePath)
		}
		return nil

	case models.InputTypeTORCHURL:
		// TORCH result URL validation
		if sourcePath == "" {
			return fmt.Errorf("TORCH result URL cannot be empty")
		}
		// Basic URL format check
		if !strings.HasPrefix(sourcePath, "http://") && !strings.HasPrefix(sourcePath, "https://") {
			return fmt.Errorf("TORCH URL must start with http:// or https://")
		}
		return nil

	default:
		return fmt.Errorf("unknown input type: %s", inputType)
	}
}
