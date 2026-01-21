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
// If compress is true, output files will be compressed with zstd (.ndjson.zst)
// Returns list of imported files and any error
func ImportFromLocalDirectory(sourcePath string, destinationDir string, logger *lib.Logger, compress bool, compressionLevel string) ([]models.FHIRDataFile, error) {
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

	if err := os.MkdirAll(destinationDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create destination directory: %w", err)
	}

	ndjsonFiles, err := findNDJSONFiles(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("failed to scan source directory: %w", err)
	}

	if len(ndjsonFiles) == 0 {
		return nil, fmt.Errorf("no FHIR NDJSON files found in %s", sourcePath)
	}

	if err := models.DetectDuplicateFHIRFiles(ndjsonFiles); err != nil {
		return nil, err
	}

	logger.Info("Found FHIR files", "count", len(ndjsonFiles), "source", sourcePath)

	var importedFiles []models.FHIRDataFile
	for _, srcFile := range ndjsonFiles {
		imported, err := copyFile(srcFile, destinationDir, logger, compress, compressionLevel)
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

		if info.IsDir() {
			return nil
		}

		if models.IsValidFHIRFile(info.Name()) {
			files = append(files, path)
		}

		return nil
	})

	return files, err
}

// copyFile copies a single file to the destination directory
// If compress is true, output will be compressed with zstd
// Handles both compressed and uncompressed source files transparently
// Returns FHIRDataFile metadata
func copyFile(sourcePath string, destDir string, logger *lib.Logger, compress bool, compressionLevel string) (models.FHIRDataFile, error) {
	srcReader, err := lib.OpenFileForReading(sourcePath)
	if err != nil {
		return models.FHIRDataFile{}, fmt.Errorf("failed to open source file: %w", err)
	}
	defer func() {
		if err := srcReader.Close(); err != nil {
			logger.Error("Failed to close source file", "error", err)
		}
	}()

	srcInfo, err := os.Stat(sourcePath)
	if err != nil {
		return models.FHIRDataFile{}, fmt.Errorf("failed to stat source file: %w", err)
	}

	baseFileName := lib.GetUncompressedFilename(filepath.Base(sourcePath))
	outputFileName := lib.GetCompressedFilename(baseFileName, compress)
	destPath := filepath.Join(destDir, outputFileName)

	destFile, err := os.Create(destPath)
	if err != nil {
		return models.FHIRDataFile{}, fmt.Errorf("failed to create destination file: %w", err)
	}

	var writer io.WriteCloser = destFile
	if compress {
		compWriter, err := lib.CreateCompressedWriter(destFile, compressionLevel)
		if err != nil {
			_ = destFile.Close()
			return models.FHIRDataFile{}, fmt.Errorf("failed to create compressed writer: %w", err)
		}
		writer = compWriter
	}

	bytesWritten, err := io.Copy(writer, srcReader)
	if err != nil {
		_ = writer.Close()
		_ = destFile.Close()
		return models.FHIRDataFile{}, fmt.Errorf("failed to copy file: %w", err)
	}

	if err := writer.Close(); err != nil {
		if compress {
			_ = destFile.Close()
		}
		return models.FHIRDataFile{}, fmt.Errorf("failed to close writer: %w", err)
	}

	if compress {
		if err := destFile.Close(); err != nil {
			logger.Error("Failed to close destination file", "error", err)
		}
	}

	fileInfo, err := os.Stat(destPath)
	var fileSize int64
	if err == nil {
		fileSize = fileInfo.Size()
	} else {
		fileSize = bytesWritten
	}

	lineCount, err := lib.CountResourcesInFile(destPath)
	if err != nil {
		logger.Warn("Failed to count resources", "file", outputFileName, "error", err)
		lineCount = 0
	}

	resourceType := models.GetResourceTypeFromFilename(outputFileName)

	logger.Debug("File imported", "file", outputFileName, "size", fileSize, "resources", lineCount, "compressed", compress)

	return models.FHIRDataFile{
		FileName:     outputFileName,
		FilePath:     outputFileName, // Relative to job import directory
		ResourceType: resourceType,
		FileSize:     fileSize,
		SourceStep:   models.StepLocalImport,
		LineCount:    lineCount,
		CreatedAt:    srcInfo.ModTime(),
	}, nil
}

// ValidateImportSource checks if an import source is valid
func ValidateImportSource(sourcePath string, inputType models.InputType) error {
	switch inputType {
	case models.InputTypeLocal:
		info, err := os.Stat(sourcePath)
		if err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("directory does not exist: %s", sourcePath)
			}
			return fmt.Errorf("cannot access directory: %w", err)
		}

		if !info.IsDir() {
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

		files, err := findNDJSONFiles(sourcePath)
		if err != nil {
			return fmt.Errorf("failed to scan directory: %w", err)
		}

		if len(files) == 0 {
			return fmt.Errorf("no FHIR NDJSON files found in directory: %s\n\nExpected files with extensions: .ndjson", sourcePath)
		}

		return nil

	case models.InputTypeHTTP:
		if sourcePath == "" {
			return fmt.Errorf("URL cannot be empty")
		}
		return nil

	case models.InputTypeCRTDL:
		if sourcePath == "" {
			return fmt.Errorf("CRTDL file path cannot be empty")
		}
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
		if sourcePath == "" {
			return fmt.Errorf("TORCH result URL cannot be empty")
		}
		if !strings.HasPrefix(sourcePath, "http://") && !strings.HasPrefix(sourcePath, "https://") {
			return fmt.Errorf("TORCH URL must start with http:// or https://")
		}
		return nil

	default:
		return fmt.Errorf("unknown input type: %s", inputType)
	}
}
