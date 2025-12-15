package services

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/ui"
)

// DownloadFromURL downloads FHIR NDJSON files from an HTTP URL to the job's import directory
// If compress is true, the downloaded file will be compressed with zstd
// Supports progress tracking via progress bar
// Returns list of downloaded files and any error
func DownloadFromURL(url string, destinationDir string, httpClient *HTTPClient, logger *lib.Logger, showProgress bool, compress bool, compressionLevel string) ([]models.FHIRDataFile, error) {
	if err := os.MkdirAll(destinationDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create destination directory: %w", err)
	}

	logger.Info("Downloading from URL", "url", url, "destination", destinationDir)

	fileName := filepath.Base(url)
	// filepath.Base on HTTP URLs extracts from full URL string, so it always
	// returns a valid filename (e.g., "server.com" for "http://server.com/")
	if !models.IsValidFHIRFile(fileName) {
		fileName = fileName + ".ndjson"
	}

	outputFileName := lib.GetCompressedFilename(fileName, compress)
	destPath := filepath.Join(destinationDir, outputFileName)

	destFile, err := os.Create(destPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create destination file: %w", err)
	}

	var writer io.WriteCloser = destFile
	if compress {
		compWriter, err := lib.CreateCompressedWriter(destFile, compressionLevel)
		if err != nil {
			_ = destFile.Close()
			return nil, fmt.Errorf("failed to create compressed writer: %w", err)
		}
		writer = compWriter
	}

	spinner := ui.NewSpinner(fmt.Sprintf("Connecting to %s", url))
	if showProgress {
		spinner.Start()
	}

	var bytesDownloaded int64
	if showProgress {
		bytesDownloaded, err = httpClient.Download(url, writer)
		spinner.Stop(err == nil)

		if err == nil && bytesDownloaded > 0 {
			logger.Info("Download completed", "bytes", bytesDownloaded, "file", outputFileName)
		}
	} else {
		bytesDownloaded, err = httpClient.Download(url, writer)
	}

	if closeErr := writer.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	if compress {
		if closeErr := destFile.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}

	if err != nil {
		_ = os.Remove(destPath)
		return nil, fmt.Errorf("download failed: %w", err)
	}

	fileInfo, statErr := os.Stat(destPath)
	var fileSize int64
	if statErr == nil {
		fileSize = fileInfo.Size()
	} else {
		// Fallback to bytesDownloaded if stat fails (defensive)
		fileSize = bytesDownloaded
	}

	lineCount, countErr := lib.CountResourcesInFile(destPath)
	if countErr != nil {
		logger.Warn("Failed to count resources", "file", outputFileName, "error", countErr)
		lineCount = 0
	}

	resourceType := models.GetResourceTypeFromFilename(outputFileName)

	logger.Info("File downloaded successfully", "file", outputFileName, "size", fileSize, "resources", lineCount, "compressed", compress)

	downloadedFile := models.FHIRDataFile{
		FileName:     outputFileName,
		FilePath:     outputFileName, // Relative to job import directory
		ResourceType: resourceType,
		FileSize:     fileSize,
		SourceStep:   models.StepHttpImport,
		LineCount:    lineCount,
		CreatedAt:    lib.GetFileModTime(destPath),
	}

	return []models.FHIRDataFile{downloadedFile}, nil
}

// DownloadFromURLWithProgress downloads a file with detailed progress tracking
// If compress is true, the downloaded file will be compressed with zstd
// Shows progress bar with percentage, ETA, and throughput for user feedback
func DownloadFromURLWithProgress(url string, destinationDir string, httpClient *HTTPClient, logger *lib.Logger, compress bool, compressionLevel string) ([]models.FHIRDataFile, error) {
	if err := os.MkdirAll(destinationDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create destination directory: %w", err)
	}

	logger.Info("Downloading from URL", "url", url)

	fileName := filepath.Base(url)
	// filepath.Base on HTTP URLs extracts from full URL string, so it always
	// returns a valid filename (e.g., "server.com" for "http://server.com/")
	if !models.IsValidFHIRFile(fileName) {
		fileName = fileName + ".ndjson"
	}

	outputFileName := lib.GetCompressedFilename(fileName, compress)
	destPath := filepath.Join(destinationDir, outputFileName)

	destFile, err := os.Create(destPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create destination file: %w", err)
	}

	var writer io.WriteCloser = destFile
	if compress {
		compWriter, err := lib.CreateCompressedWriter(destFile, compressionLevel)
		if err != nil {
			_ = destFile.Close()
			return nil, fmt.Errorf("failed to create compressed writer: %w", err)
		}
		writer = compWriter
	}

	spinner := ui.NewSpinner(fmt.Sprintf("Connecting to %s", url))
	spinner.Start()

	progressCallback := func(bytes int64) {
		_ = bytes
	}

	bytesDownloaded, err := httpClient.DownloadWithProgress(url, writer, progressCallback)
	spinner.Stop(err == nil)

	if closeErr := writer.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	if compress {
		if closeErr := destFile.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}

	if err != nil {
		_ = os.Remove(destPath)
		return nil, fmt.Errorf("download failed: %w", err)
	}

	fileInfo, statErr := os.Stat(destPath)
	var fileSize int64
	if statErr == nil {
		fileSize = fileInfo.Size()
	} else {
		// Fallback to bytesDownloaded if stat fails (defensive)
		fileSize = bytesDownloaded
	}

	lineCount, _ := lib.CountResourcesInFile(destPath)
	resourceType := models.GetResourceTypeFromFilename(outputFileName)

	logger.Info("Download completed", "file", outputFileName, "size", fileSize, "resources", lineCount, "compressed", compress)

	downloadedFile := models.FHIRDataFile{
		FileName:     outputFileName,
		FilePath:     outputFileName,
		ResourceType: resourceType,
		FileSize:     fileSize,
		SourceStep:   models.StepHttpImport,
		LineCount:    lineCount,
		CreatedAt:    lib.GetFileModTime(destPath),
	}

	return []models.FHIRDataFile{downloadedFile}, nil
}
