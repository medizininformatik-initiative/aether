package pipeline

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
		// TORCH import requires CRTDL or TORCH URL
		switch job.InputType {
		case models.InputTypeCRTDL:
			// Validate CRTDL source
			if err := services.ValidateImportSource(job.InputSource, models.InputTypeCRTDL); err != nil {
				updatedJob := failImportStep(job, err, models.ErrorTypeNonTransient, 0)
				lib.LogStepFailed(logger, string(currentStep), job.JobID, err, false)
				return &updatedJob, err
			}

			logger.Info("Extracting data from TORCH using CRTDL", "source", job.InputSource, "compress", compress)
			importedFiles, err = executeTORCHExtraction(job, importDir, httpClient, logger, showProgress, compress, compressionLevel)

		case models.InputTypeTORCHURL:
			// Validate TORCH URL
			if err := services.ValidateImportSource(job.InputSource, models.InputTypeTORCHURL); err != nil {
				updatedJob := failImportStep(job, err, models.ErrorTypeNonTransient, 0)
				lib.LogStepFailed(logger, string(currentStep), job.JobID, err, false)
				return &updatedJob, err
			}

			logger.Info("Downloading from TORCH result URL", "source", job.InputSource, "compress", compress)
			importedFiles, err = executeTORCHDownload(job, importDir, httpClient, logger, showProgress, compress, compressionLevel)

		default:
			err = fmt.Errorf("torch import requires CRTDL file or TORCH URL, got input type: %s", job.InputType)
		}

	default:
		err = fmt.Errorf("unsupported import step: %s", currentStep)
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
func executeTORCHExtraction(job *models.PipelineJob, importDir string, httpClient *services.HTTPClient, logger *lib.Logger, showProgress bool, compress bool, compressionLevel string) ([]models.FHIRDataFile, error) {
	// Create TORCH client
	torchClient := services.NewTORCHClient(job.Config.Services.TORCH, httpClient, logger)

	var extractionURL string
	var err error

	// Check if CRTDL preprocessing is enabled
	if job.Config.Services.CRTDLPreprocessing.Enabled {
		logger.Info("CRTDL preprocessing enabled, enriching CRTDL before submission")

		enrichedContent, preprocessErr := preprocessCRTDL(job, logger)
		if preprocessErr != nil {
			return nil, fmt.Errorf("failed to preprocess CRTDL: %w", preprocessErr)
		}

		// Submit enriched CRTDL content
		extractionURL, err = torchClient.SubmitExtractionWithContent(enrichedContent)
	} else {
		// Submit original CRTDL file
		extractionURL, err = torchClient.SubmitExtraction(job.InputSource)
	}

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

// preprocessCRTDL loads, enriches, and saves the CRTDL document for DIMP requirements.
// Returns the serialized enriched CRTDL content ready for TORCH submission.
//
// Process:
//  1. Parse original CRTDL from job input source
//  2. Load enrichments from configuration (file or inline)
//  3. Apply enrichments using EnrichCRTDL
//  4. Save enriched CRTDL to job directory for debugging/auditing
//  5. Return serialized JSON content
func preprocessCRTDL(job *models.PipelineJob, logger *lib.Logger) ([]byte, error) {
	// 1. Parse original CRTDL
	logger.Info("Parsing original CRTDL", "path", job.InputSource)
	originalDoc, err := services.ParseCRTDL(job.InputSource)
	if err != nil {
		return nil, fmt.Errorf("failed to parse CRTDL: %w", err)
	}

	// 2. Load enrichments from configuration
	enrichments, err := services.LoadEnrichments(job.Config.Services.CRTDLPreprocessing)
	if err != nil {
		return nil, fmt.Errorf("failed to load enrichments: %w", err)
	}

	if len(enrichments) == 0 {
		logger.Warn("CRTDL preprocessing enabled but no enrichments configured")
		// Return original file content
		return os.ReadFile(job.InputSource)
	}

	logger.Info("Applying CRTDL enrichments",
		"enrichment_count", len(enrichments),
		"original_groups", len(originalDoc.DataExtraction.AttributeGroups))

	// 3. Apply enrichments
	enrichedDoc, err := services.EnrichCRTDL(*originalDoc, enrichments)
	if err != nil {
		return nil, fmt.Errorf("failed to enrich CRTDL: %w", err)
	}

	logger.Info("CRTDL enriched successfully",
		"enriched_groups", len(enrichedDoc.DataExtraction.AttributeGroups))

	// 4. Serialize enriched document
	enrichedContent, err := json.MarshalIndent(enrichedDoc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to serialize enriched CRTDL: %w", err)
	}

	// 5. Save enriched CRTDL to job directory for debugging/auditing
	jobDir := services.GetJobDir(job.Config.JobsDir, job.JobID)
	enrichedPath := filepath.Join(jobDir, "enriched-crtdl.json")

	if err := os.MkdirAll(jobDir, 0755); err != nil {
		logger.Warn("Failed to create job directory for enriched CRTDL", "error", err)
	} else if err := os.WriteFile(enrichedPath, enrichedContent, 0644); err != nil {
		logger.Warn("Failed to save enriched CRTDL for debugging", "error", err, "path", enrichedPath)
	} else {
		logger.Info("Enriched CRTDL saved for debugging", "path", enrichedPath)
	}

	return enrichedContent, nil
}
