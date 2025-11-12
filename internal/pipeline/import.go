package pipeline

import (
	"fmt"
	"time"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

// ExecuteImportStep performs the import step of the pipeline
// Detects input type (local vs HTTP) and delegates to appropriate importer
// Updates job state with progress and imported files
func ExecuteImportStep(job *models.PipelineJob, logger *lib.Logger, httpClient *services.HTTPClient, showProgress bool) (*models.PipelineJob, error) {
	startTime := time.Now()

	currentStep := models.StepName(job.CurrentStep)
	lib.LogStepStart(logger, string(currentStep), job.JobID)

	// Get import output directory
	importDir := services.GetJobOutputDir(job.Config.JobsDir, job.JobID, currentStep)

	// Validate input source
	if err := services.ValidateImportSource(job.InputSource, job.InputType); err != nil {
		// Failed with non-transient error
		updatedJob := failImportStep(job, err, models.ErrorTypeNonTransient, 0)
		lib.LogStepFailed(logger, string(currentStep), job.JobID, err, false)
		return &updatedJob, err
	}

	// Execute import based on input type
	var importedFiles []models.FHIRDataFile
	var err error

	switch job.InputType {
	case models.InputTypeLocal:
		logger.Info("Importing from local directory", "source", job.InputSource)
		importedFiles, err = services.ImportFromLocalDirectory(job.InputSource, importDir, logger)

	case models.InputTypeHTTP:
		logger.Info("Downloading from URL", "source", job.InputSource)
		if showProgress {
			importedFiles, err = services.DownloadFromURLWithProgress(job.InputSource, importDir, httpClient, logger)
		} else {
			importedFiles, err = services.DownloadFromURL(job.InputSource, importDir, httpClient, logger, false)
		}

	case models.InputTypeCRTDL:
		logger.Info("Extracting data from TORCH using CRTDL", "source", job.InputSource)
		importedFiles, err = executeTORCHExtraction(job, importDir, httpClient, logger, showProgress)

	case models.InputTypeTORCHURL:
		logger.Info("Downloading from TORCH result URL", "source", job.InputSource)
		importedFiles, err = executeTORCHDownload(job, importDir, httpClient, logger, showProgress)

	default:
		err = fmt.Errorf("unsupported input type: %s", job.InputType)
	}

	// Handle errors
	if err != nil {
		// Classify error type
		errorType := classifyImportError(err, job.InputType)
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

// executeTORCHExtraction performs CRTDL-based data extraction from TORCH server
// Submits CRTDL, polls for completion, and downloads resulting NDJSON files
func executeTORCHExtraction(job *models.PipelineJob, importDir string, httpClient *services.HTTPClient, logger *lib.Logger, showProgress bool) ([]models.FHIRDataFile, error) {
	// Create TORCH client
	torchClient := services.NewTORCHClient(job.Config.Services.TORCH, httpClient, logger)

	// Submit extraction
	extractionURL, err := torchClient.SubmitExtraction(job.InputSource)
	if err != nil {
		return nil, fmt.Errorf("failed to submit TORCH extraction: %w", err)
	}

	// Store extraction URL in job for resumption capability
	job.TORCHExtractionURL = extractionURL
	logger.Info("TORCH extraction URL stored for resumption", "url", extractionURL)

	// Poll extraction status until complete
	fileURLs, err := torchClient.PollExtractionStatus(extractionURL, showProgress)
	if err != nil {
		return nil, fmt.Errorf("TORCH extraction failed: %w", err)
	}

	if len(fileURLs) == 0 {
		logger.Warn("TORCH extraction returned no files (empty cohort)")
		return []models.FHIRDataFile{}, nil
	}

	// Download extraction files
	files, err := torchClient.DownloadExtractionFiles(fileURLs, importDir, showProgress)
	if err != nil {
		return nil, fmt.Errorf("failed to download TORCH files: %w", err)
	}

	return files, nil
}

// executeTORCHDownload downloads files from a direct TORCH result URL
// This bypasses extraction submission and directly downloads from an existing result
func executeTORCHDownload(job *models.PipelineJob, importDir string, httpClient *services.HTTPClient, logger *lib.Logger, showProgress bool) ([]models.FHIRDataFile, error) {
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
	files, err := torchClient.DownloadExtractionFiles(fileURLs, importDir, showProgress)
	if err != nil {
		return nil, fmt.Errorf("failed to download TORCH files: %w", err)
	}

	return files, nil
}

// classifyImportError determines if an import error is transient or non-transient
func classifyImportError(err error, inputType models.InputType) models.ErrorType {
	if err == nil {
		return models.ErrorTypeNonTransient
	}

	// Check for TORCH-specific errors
	if torchErr, ok := err.(*services.TORCHError); ok {
		return torchErr.ErrorType
	}

	// For HTTP downloads and TORCH operations, network errors are transient
	if inputType == models.InputTypeHTTP || inputType == models.InputTypeCRTDL || inputType == models.InputTypeTORCHURL {
		if lib.IsNetworkError(err) {
			return models.ErrorTypeTransient
		}
	}

	// For local imports, most errors are non-transient (file not found, permissions, etc.)
	// Default to non-transient
	return models.ErrorTypeNonTransient
}

// RetryImportStep attempts to retry a failed import step
// Should only be called if the error was transient
func RetryImportStep(job *models.PipelineJob, logger *lib.Logger, httpClient *services.HTTPClient, showProgress bool) (*models.PipelineJob, error) {
	// Get current import step
	currentStep := models.StepName(job.CurrentStep)
	importStep, found := models.GetStepByName(*job, currentStep)
	if !found {
		return nil, fmt.Errorf("import step not found")
	}

	// Check if retry is allowed
	if importStep.LastError == nil {
		return nil, fmt.Errorf("no error to retry")
	}

	if !lib.ShouldRetry(importStep.LastError.Type, importStep.RetryCount, job.Config.Retry.MaxAttempts) {
		return nil, fmt.Errorf("retry not allowed: max attempts reached or non-transient error")
	}

	// Increment retry count
	retriedStep := models.IncrementRetry(importStep)
	updatedJob := models.ReplaceStep(*job, retriedStep)

	lib.LogRetry(logger, "import step", retriedStep.RetryCount, job.Config.Retry.MaxAttempts, importStep.LastError)

	// Calculate backoff
	backoff := lib.CalculateBackoff(retriedStep.RetryCount-1, job.Config.Retry.InitialBackoffMs, job.Config.Retry.MaxBackoffMs)
	logger.Info("Waiting before retry", "backoff", backoff)
	time.Sleep(backoff)

	// Retry the import
	return ExecuteImportStep(&updatedJob, logger, httpClient, showProgress)
}
