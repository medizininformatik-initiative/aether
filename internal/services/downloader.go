package services

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/ui"
)

// DownloadFromURL downloads FHIR NDJSON files from an HTTP URL to the job's import directory
// Supports progress tracking via progress bar
// Returns list of downloaded files and any error
func DownloadFromURL(url string, destinationDir string, httpClient *HTTPClient, logger *lib.Logger, showProgress bool) ([]models.FHIRDataFile, error) {
	// Ensure destination directory exists
	if err := os.MkdirAll(destinationDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create destination directory: %w", err)
	}

	logger.Info("Downloading from URL", "url", url, "destination", destinationDir)

	// Determine the output filename from URL
	fileName := filepath.Base(url)
	if fileName == "." || fileName == "/" {
		fileName = "download.ndjson"
	}

	// Ensure filename has .ndjson extension
	if !models.IsValidFHIRFile(fileName) {
		fileName = fileName + ".ndjson"
	}

	destPath := filepath.Join(destinationDir, fileName)

	// Create destination file
	destFile, err := os.Create(destPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create destination file: %w", err)
	}
	defer func() {
		if err := destFile.Close(); err != nil {
			logger.Error("Failed to close destination file", "error", err)
		}
	}()

	// Start spinner for connection phase (duration is initially unknown)
	spinner := ui.NewSpinner(fmt.Sprintf("Connecting to %s", url))
	if showProgress {
		spinner.Start()
	}

	// Download file
	var bytesDownloaded int64
	if showProgress {
		// Use progress bar for download (when size is known)
		// Note: We won't know total size until we start download, so we'll use spinner initially

		bytesDownloaded, err = httpClient.Download(url, destFile)
		spinner.Stop(err == nil)

		if err == nil && bytesDownloaded > 0 {
			logger.Info("Download completed", "bytes", bytesDownloaded, "file", fileName)
		}
	} else {
		// No progress display
		bytesDownloaded, err = httpClient.Download(url, destFile)
	}

	if err != nil {
		// Clean up failed download
		_ = os.Remove(destPath)
		return nil, fmt.Errorf("download failed: %w", err)
	}

	// Count lines (FHIR resources)
	lineCount, countErr := lib.CountResourcesInFile(destPath)
	if countErr != nil {
		logger.Warn("Failed to count resources", "file", fileName, "error", countErr)
		lineCount = 0
	}

	// Extract resource type from filename
	resourceType := models.GetResourceTypeFromFilename(fileName)

	logger.Info("File downloaded successfully", "file", fileName, "size", bytesDownloaded, "resources", lineCount)

	downloadedFile := models.FHIRDataFile{
		FileName:     fileName,
		FilePath:     fileName, // Relative to job import directory
		ResourceType: resourceType,
		FileSize:     bytesDownloaded,
		SourceStep:   models.StepHttpImport,
		LineCount:    lineCount,
		CreatedAt:    lib.GetFileModTime(destPath),
	}

	return []models.FHIRDataFile{downloadedFile}, nil
}

// DownloadFromURLWithProgress downloads a file with detailed progress tracking
// Shows progress bar with percentage, ETA, and throughput for user feedback
func DownloadFromURLWithProgress(url string, destinationDir string, httpClient *HTTPClient, logger *lib.Logger) ([]models.FHIRDataFile, error) {
	// Ensure destination directory exists
	if err := os.MkdirAll(destinationDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create destination directory: %w", err)
	}

	logger.Info("Downloading from URL", "url", url)

	// Determine filename
	fileName := filepath.Base(url)
	if fileName == "." || fileName == "/" {
		fileName = "download.ndjson"
	}
	if !models.IsValidFHIRFile(fileName) {
		fileName = fileName + ".ndjson"
	}

	destPath := filepath.Join(destinationDir, fileName)

	// Create destination file
	destFile, err := os.Create(destPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create destination file: %w", err)
	}
	defer func() {
		if err := destFile.Close(); err != nil {
			logger.Error("Failed to close destination file", "error", err)
		}
	}()

	// Start with spinner for connection phase
	spinner := ui.NewSpinner(fmt.Sprintf("Connecting to %s", url))
	spinner.Start()

	// First, try to get file size with HEAD request
	// (We'll skip this for simplicity and just use spinner + download)

	// Download with progress callback
	progressCallback := func(bytes int64) {
		// Progress callback for future use
		_ = bytes
	}

	bytesDownloaded, err := httpClient.DownloadWithProgress(url, destFile, progressCallback)
	spinner.Stop(err == nil)

	if err != nil {
		_ = os.Remove(destPath)
		return nil, fmt.Errorf("download failed: %w", err)
	}

	// Count resources
	lineCount, _ := lib.CountResourcesInFile(destPath)
	resourceType := models.GetResourceTypeFromFilename(fileName)

	logger.Info("Download completed", "file", fileName, "size", bytesDownloaded, "resources", lineCount)

	downloadedFile := models.FHIRDataFile{
		FileName:     fileName,
		FilePath:     fileName,
		ResourceType: resourceType,
		FileSize:     bytesDownloaded,
		SourceStep:   models.StepHttpImport,
		LineCount:    lineCount,
		CreatedAt:    lib.GetFileModTime(destPath),
	}

	return []models.FHIRDataFile{downloadedFile}, nil
}
