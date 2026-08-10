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
// recursive opts into scanning subdirectories of sourcePath (see findNDJSONFiles)
// Returns list of imported files and any error
func ImportFromLocalDirectory(sourcePath string, destinationDir string, logger *lib.Logger, compress bool, compressionLevel string, recursive bool) ([]models.FHIRDataFile, error) {
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
		case ".json":
			return nil, fmt.Errorf("source path is a JSON file, not a directory: %s. For CRTDL input, the file should have been detected as InputTypeCRTDL. This suggests the CRTDL file is invalid or missing required fields", sourcePath)
		default:
			return nil, fmt.Errorf("source path is not a directory: %s", sourcePath)
		}
	}

	if err := os.MkdirAll(destinationDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create destination directory: %w", err)
	}

	ndjsonFiles, err := findNDJSONFiles(sourcePath, recursive)
	if err != nil {
		return nil, fmt.Errorf("failed to scan source directory: %w", err)
	}

	if len(ndjsonFiles) == 0 {
		return nil, fmt.Errorf("no FHIR NDJSON files found in %s%s", sourcePath, recursiveHint(recursive))
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

// findNDJSONFiles finds .ndjson/.ndjson.zst files under rootPath. By default
// (recursive=false) it only looks at the top level, since ImportFromLocalDirectory
// flattens matches into a single destination directory keyed by basename
// (see copyFile) and that's only safe when the whole tree's basenames are
// unique. Pass recursive=true for sources deliberately organized across
// subdirectories.
func findNDJSONFiles(rootPath string, recursive bool) ([]string, error) {
	if !recursive {
		return findNDJSONFilesFlat(rootPath)
	}

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

// findNDJSONFilesFlat lists NDJSON files directly under rootPath, ignoring
// subdirectories entirely.
func findNDJSONFilesFlat(rootPath string) ([]string, error) {
	entries, err := os.ReadDir(rootPath)
	if err != nil {
		return nil, err
	}

	var files []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if models.IsValidFHIRFile(entry.Name()) {
			files = append(files, filepath.Join(rootPath, entry.Name()))
		}
	}

	return files, nil
}

// recursiveHint points users at services.local_import.recursive when a
// non-recursive scan found nothing, so a subdirectory-organized source
// doesn't look like an empty one.
func recursiveHint(recursive bool) string {
	if recursive {
		return ""
	}
	return " (only the top-level directory was scanned; set services.local_import.recursive: true to also scan subdirectories)"
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

	logger.Debug("File imported", "file", outputFileName, "size", fileSize, "resources", lineCount, "compressed", compress)

	return models.FHIRDataFile{
		FileName:   outputFileName,
		FilePath:   outputFileName, // Relative to job import directory
		FileSize:   fileSize,
		SourceStep: models.StepLocalImport,
		LineCount:  lineCount,
		CreatedAt:  srcInfo.ModTime(),
	}, nil
}

// ValidateImportSource checks if an import source is valid. recursive is only
// consulted for InputTypeLocal (see findNDJSONFiles).
func ValidateImportSource(sourcePath string, inputType models.InputType, recursive bool) error {
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
			case ".json":
				hint = "\n\nThis appears to be a JSON file. Possible issues:\n  - File may not have valid CRTDL structure (missing cohortDefinition or dataExtraction)\n  - File may be using FHIR Parameters format instead of flat CRTDL format\n\nRun with verbose logging to see detailed validation errors."
			case ".ndjson":
				hint = "\n\nThis is an NDJSON file. Please provide the directory containing it, not the file itself."
			}
			return fmt.Errorf("expected directory but got file: %s%s", sourcePath, hint)
		}

		files, err := findNDJSONFiles(sourcePath, recursive)
		if err != nil {
			return fmt.Errorf("failed to scan directory: %w", err)
		}

		if len(files) == 0 {
			return fmt.Errorf("no FHIR NDJSON files found in directory: %s%s\n\nExpected files with extensions: .ndjson", sourcePath, recursiveHint(recursive))
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
