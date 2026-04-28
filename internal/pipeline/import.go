package pipeline

import (
	"fmt"
	"time"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

// ExecuteImportStep performs the import step of the pipeline
// Uses the current step (from job.CurrentStep) to determine import method
// For local_import: uses config.Services.LocalImport.Dir or job.InputSource
// For torch: uses TORCH extraction with CRTDL from job.InputSource
// For http_import: downloads from URL in job.InputSource
// Updates job state with progress and imported files
func ExecuteImportStep(job *models.PipelineJob, logger *lib.Logger, httpClient *services.HTTPClient, showProgress bool) (*models.PipelineJob, error) {
	startTime := time.Now()

	currentStep := models.StepName(job.CurrentStep)
	lib.LogStepStart(logger, string(currentStep), job.JobID)

	// Get import output directory
	importDir := services.GetJobOutputDir(job.Config.JobsDir, job.JobID, currentStep)

	// Get compression settings
	compress := job.Config.Compression.Enabled
	compressionLevel := job.Config.Compression.Level

	// Execute import based on the current step (enabled import step)
	var importedFiles []models.FHIRDataFile
	var err error

	switch currentStep {
	case models.StepLocalImport:
		// Determine source directory: config dir takes precedence, fallback to job.InputSource
		sourceDir := job.Config.Services.LocalImport.Dir
		if sourceDir == "" {
			sourceDir = job.InputSource
		}

		// Validate source directory
		if err := services.ValidateImportSource(sourceDir, models.InputTypeLocal); err != nil {
			updatedJob := failImportStep(job, err, models.ErrorTypeNonTransient, 0)
			lib.LogStepFailed(logger, string(currentStep), job.JobID, err, false)
			return &updatedJob, err
		}

		logger.Info("Importing from local directory", "source", sourceDir, "compress", compress)
		importedFiles, err = services.ImportFromLocalDirectory(sourceDir, importDir, logger, compress, compressionLevel)

	case models.StepHttpImport:
		// Validate HTTP URL
		if err := services.ValidateImportSource(job.InputSource, models.InputTypeHTTP); err != nil {
			updatedJob := failImportStep(job, err, models.ErrorTypeNonTransient, 0)
			lib.LogStepFailed(logger, string(currentStep), job.JobID, err, false)
			return &updatedJob, err
		}

		logger.Info("Downloading from URL", "source", job.InputSource, "compress", compress)
		if showProgress {
			importedFiles, err = services.DownloadFromURLWithProgress(job.InputSource, importDir, httpClient, logger, compress, compressionLevel)
		} else {
			importedFiles, err = services.DownloadFromURL(job.InputSource, importDir, httpClient, logger, false, compress, compressionLevel)
		}

	case models.StepTorchImport:
		// TORCH result URL: poll directly, skip extraction submission.
		// Otherwise: submit the attached CRTDL for extraction.
		if job.InputType == models.InputTypeTORCHURL {
			if err := services.ValidateImportSource(job.InputSource, models.InputTypeTORCHURL); err != nil {
				updatedJob := failImportStep(job, err, models.ErrorTypeNonTransient, 0)
				lib.LogStepFailed(logger, string(currentStep), job.JobID, err, false)
				return &updatedJob, err
			}

			logger.Info("Downloading from TORCH result URL", "source", job.InputSource, "compress", compress)
			importedFiles, err = executeTORCHDownload(job, importDir, httpClient, logger, showProgress, compress, compressionLevel)
		} else {
			if err := services.ValidateImportSource(job.CRTDLPath, models.InputTypeCRTDL); err != nil {
				updatedJob := failImportStep(job, err, models.ErrorTypeNonTransient, 0)
				lib.LogStepFailed(logger, string(currentStep), job.JobID, err, false)
				return &updatedJob, err
			}

			logger.Info("Extracting data from TORCH using CRTDL", "crtdl", job.CRTDLPath, "compress", compress)
			importedFiles, err = executeTORCHExtraction(job, importDir, httpClient, logger, showProgress, compress, compressionLevel)
		}

	default:
		err = fmt.Errorf("unsupported import step: %s", currentStep)
	}

	// Handle errors
	if err != nil {
		// Classify error type
		errorType := classifyImportError(err, currentStep)
		updatedJob := failImportStep(job, err, errorType, 0)
		lib.LogStepFailed(logger, string(currentStep), job.JobID, err, errorType == models.ErrorTypeTransient)
		return &updatedJob, err
	}

	// Calculate total bytes imported
	var totalBytes int64
	for _, file := range importedFiles {
		totalBytes += file.FileSize
	}

	// Update job with imported file metrics
	updatedJob := models.UpdateJobMetrics(*job, len(importedFiles), totalBytes)

	// Complete the import step
	importStep, _ := models.GetStepByName(updatedJob, currentStep)
	completedStep := models.CompleteStep(importStep, len(importedFiles), totalBytes)
	updatedJob = models.ReplaceStep(updatedJob, completedStep)

	duration := time.Since(startTime)
	lib.LogStepComplete(logger, string(currentStep), job.JobID, len(importedFiles), duration)

	return &updatedJob, nil
}

// failImportStep marks the import step as failed
func failImportStep(job *models.PipelineJob, err error, errorType models.ErrorType, httpStatus int) models.PipelineJob {
	currentStep := models.StepName(job.CurrentStep)
	importStep, found := models.GetStepByName(*job, currentStep)
	if !found {
		// Step not found - shouldn't happen, but handle gracefully
		return models.AddError(*job, err.Error())
	}

	failedStep := models.FailStep(importStep, errorType, err.Error(), httpStatus)
	updatedJob := models.ReplaceStep(*job, failedStep)
	updatedJob = models.AddError(updatedJob, err.Error())

	return updatedJob
}

// executeTORCHExtraction submits the (already prepared) CRTDL to TORCH,
// polls until extraction completes, and downloads the resulting NDJSON
// files. PrepareCRTDL ensures job.CRTDLPath points at the effective CRTDL
// shared by every downstream pipeline step.
func executeTORCHExtraction(job *models.PipelineJob, importDir string, httpClient *services.HTTPClient, logger *lib.Logger, showProgress bool, compress bool, compressionLevel string) ([]models.FHIRDataFile, error) {
	torchClient := services.NewTORCHClient(job.Config.Services.TORCH, httpClient, logger)

	extractionURL, err := torchClient.SubmitExtraction(job.CRTDLPath)
	if err != nil {
		return nil, fmt.Errorf("failed to submit TORCH extraction: %w", err)
	}

	job.TORCHExtractionURL = extractionURL
	logger.Info("TORCH extraction URL stored for resumption", "url", extractionURL)

	fileURLs, err := torchClient.PollExtractionStatus(extractionURL, showProgress)
	if err != nil {
		return nil, fmt.Errorf("TORCH extraction failed: %w", err)
	}

	if len(fileURLs) == 0 {
		logger.Warn("TORCH extraction returned no files (empty cohort)")
		return []models.FHIRDataFile{}, nil
	}

	files, err := torchClient.DownloadExtractionFiles(fileURLs, importDir, showProgress, compress, compressionLevel)
	if err != nil {
		return nil, fmt.Errorf("failed to download TORCH files: %w", err)
	}
	return files, nil
}

// executeTORCHDownload downloads files from a direct TORCH result URL
// This bypasses extraction submission and directly downloads from an existing result
func executeTORCHDownload(job *models.PipelineJob, importDir string, httpClient *services.HTTPClient, logger *lib.Logger, showProgress bool, compress bool, compressionLevel string) ([]models.FHIRDataFile, error) {
	// Create TORCH client
	torchClient := services.NewTORCHClient(job.Config.Services.TORCH, httpClient, logger)

	// Poll the URL directly (it should return 200 immediately if extraction is complete)
	fileURLs, err := torchClient.PollExtractionStatus(job.InputSource, showProgress)
	if err != nil {
		return nil, fmt.Errorf("failed to get TORCH result: %w", err)
	}

	if len(fileURLs) == 0 {
		logger.Warn("TORCH result URL returned no files")
		return []models.FHIRDataFile{}, nil
	}

	// Download files
	files, err := torchClient.DownloadExtractionFiles(fileURLs, importDir, showProgress, compress, compressionLevel)
	if err != nil {
		return nil, fmt.Errorf("failed to download TORCH files: %w", err)
	}

	return files, nil
}

// classifyImportError determines if an import error is transient or non-transient
func classifyImportError(err error, step models.StepName) models.ErrorType {
	if err == nil {
		return models.ErrorTypeNonTransient
	}

	// Check for TORCH-specific errors
	if torchErr, ok := err.(*services.TORCHError); ok {
		return torchErr.ErrorType
	}

	// Network-capable steps: network errors are transient
	if step == models.StepHttpImport || step == models.StepTorchImport {
		if lib.IsNetworkError(err) {
			return models.ErrorTypeTransient
		}
	}

	// For local imports, most errors are non-transient (file not found, permissions, etc.)
	// Default to non-transient
	return models.ErrorTypeNonTransient
}
