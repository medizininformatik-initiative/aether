package pipeline

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/services"
	"github.com/medizininformatik-initiative/aether/internal/ui"
)

// ExecuteDIMPStep processes FHIR resources through the DIMP pseudonymization service
// Reads from import/ directory, writes to pseudonymized/ directory
// Orchestrates Bundle splitting and oversized resource detection before pseudonymization
func ExecuteDIMPStep(job *models.PipelineJob, jobDir string, logger *lib.Logger) error {
	stepName := models.StepDIMP

	// Check if DIMP step is enabled
	if !isStepEnabled(job.Config, stepName) {
		logger.Info("DIMP step not enabled, skipping", "job_id", job.JobID)
		return nil
	}

	// Log step start (DEBUG level to avoid polluting progress bar display)
	logger.Debug("DIMP step starting", "job_id", job.JobID)

	// Get or create DIMP step in job
	step := getOrCreateStep(job, stepName)
	step.Status = models.StepStatusInProgress
	now := time.Now()
	step.StartedAt = &now

	// Validate DIMP service URL is configured
	if job.Config.Services.DIMP.URL == "" {
		err := fmt.Errorf("DIMP service URL not configured")
		lib.LogStepFailed(logger, string(stepName), job.JobID, err, false)
		recordStepError(step, err, models.ErrorTypeNonTransient)
		return err
	}

	// Create DIMP client
	httpClient := services.DefaultHTTPClient()
	dimpClient := services.NewDIMPClient(job.Config.Services.DIMP.URL, httpClient, logger)

	// Setup directories
	importDir := filepath.Join(jobDir, "import")
	outputDir := filepath.Join(jobDir, "pseudonymized")

	// Ensure output directory exists
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		lib.LogStepFailed(logger, string(stepName), job.JobID, err, false)
		recordStepError(step, err, models.ErrorTypeNonTransient)
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Find all NDJSON files in import directory
	files, err := filepath.Glob(filepath.Join(importDir, "*.ndjson"))
	if err != nil {
		lib.LogStepFailed(logger, string(stepName), job.JobID, err, false)
		recordStepError(step, err, models.ErrorTypeNonTransient)
		return fmt.Errorf("failed to list import files: %w", err)
	}

	if len(files) == 0 {
		err := fmt.Errorf("no FHIR NDJSON files found in import directory")
		lib.LogStepFailed(logger, string(stepName), job.JobID, err, false)
		recordStepError(step, err, models.ErrorTypeNonTransient)
		return err
	}

	// Print user-friendly message instead of logger (logger pollutes progress bar)
	fmt.Printf("Processing %d FHIR file(s) through DIMP...\n\n", len(files))

	// Clean up any stale .part files from previous interrupted runs
	partFiles, _ := filepath.Glob(filepath.Join(outputDir, "*.part"))
	for _, partFile := range partFiles {
		logger.Debug("Removing stale partial file from previous run", "file", filepath.Base(partFile))
		_ = os.Remove(partFile)
	}

	// Process each file
	totalResourcesProcessed := 0
	filesProcessed := 0
	for fileIdx, inputFile := range files {
		// Create output filename: dimped_<original-filename>
		baseName := filepath.Base(inputFile)
		outputFile := filepath.Join(outputDir, "dimped_"+baseName)

		// Check if output file already exists (resume support)
		if _, err := os.Stat(outputFile); err == nil {
			// Output file exists - skip processing
			fmt.Printf("  ⊙ %s (already processed, skipping)\n", baseName)
			logger.Debug("Skipping already processed file",
				"filename", baseName,
				"output_file", outputFile,
				"job_id", job.JobID)
			filesProcessed++

			// Count resources in existing file for accurate totals
			if lineCount, err := lib.CountResourcesInFile(outputFile); err == nil {
				totalResourcesProcessed += lineCount
			}
			continue
		}

		// Process file through DIMP using atomic write (writes to .part first)
		resourcesProcessed, err := processDIMPFile(inputFile, outputFile, dimpClient, logger, job)
		if err != nil {
			logger.Error("Failed to process FHIR file",
				"filename", baseName,
				"file_number", fileIdx+1,
				"total_files", len(files),
				"resources_processed_so_far", totalResourcesProcessed,
				"error", err,
				"job_id", job.JobID)
			lib.LogStepFailed(logger, string(stepName), job.JobID, err, isDIMPErrorRetryable(err))
			recordStepError(step, err, classifyDIMPError(err))
			return fmt.Errorf("failed to process %s: %w", baseName, err)
		}

		// Log completion for this file
		fmt.Printf("  ✓ %s (%d resources)\n", baseName, resourcesProcessed)

		totalResourcesProcessed += resourcesProcessed
		filesProcessed++
	}

	// Update step status
	step.Status = models.StepStatusCompleted
	step.FilesProcessed = len(files)
	completedAt := time.Now()
	step.CompletedAt = &completedAt

	duration := completedAt.Sub(*step.StartedAt)

	// Log to structured logger at DEBUG level
	logger.Debug("DIMP step completed",
		"files_processed", len(files),
		"resources_processed", totalResourcesProcessed,
		"duration", duration,
		"job_id", job.JobID,
	)

	return nil
}

// processDIMPFile processes a single NDJSON file through DIMP
// Returns the number of resources processed
// Uses atomic write pattern: writes to .part file, renames on success
// Implements Bundle splitting for large Bundles to prevent HTTP 413 errors
func processDIMPFile(inputFile, outputFile string, dimpClient *services.DIMPClient, logger *lib.Logger, job *models.PipelineJob) (int, error) {
	// Setup file I/O with atomic write pattern
	fileCtx, err := SetupFileProcessing(inputFile, outputFile)
	if err != nil {
		return 0, err
	}

	// Count resources for progress tracking
	totalResources := countResourcesInFile(inputFile)

	// Create progress bar if we know total count
	var progressBar *ui.ProgressBar
	if totalResources > 0 {
		progressBar = ui.NewProgressBar(int64(totalResources), fmt.Sprintf("Pseudonymizing %s", filepath.Base(inputFile)))
	} else {
		// Use spinner for unknown count
		logger.Info("Processing FHIR resources (unknown count)", "file", filepath.Base(inputFile))
	}

	// Get Bundle split threshold from config (convert MB to bytes)
	thresholdMB := job.Config.Services.DIMP.BundleSplitThresholdMB
	if thresholdMB <= 0 {
		thresholdMB = 10 // Default to 10MB if not configured
	}
	thresholdBytes := thresholdMB * 1024 * 1024

	// Create resource processor for Bundle and non-Bundle processing
	processor := NewResourceProcessor(dimpClient, logger, thresholdBytes, inputFile)

	// Process line by line with large buffer to handle very large FHIR resources
	scanner := newLargeBufferScanner(fileCtx.InFile)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		// Parse FHIR resource
		var resource map[string]any
		if err := json.Unmarshal([]byte(line), &resource); err != nil {
			// Clear progress bar before logging error
			if progressBar != nil {
				_ = progressBar.Clear()
			}
			logger.Error("Failed to parse FHIR resource",
				"file", filepath.Base(inputFile),
				"line_number", processor.GetResourceCount()+1,
				"error", err)
			return processor.GetResourceCount(), fmt.Errorf("failed to parse resource at line %d: %w", processor.GetResourceCount()+1, err)
		}

		resourceType, _ := resource["resourceType"].(string)
		resourceID, _ := resource["id"].(string)

		// Only log individual resources at DEBUG level to avoid interfering with progress bar
		logger.Debug("Processing FHIR resource",
			"file", filepath.Base(inputFile),
			"line_number", processor.GetResourceCount()+1,
			"resourceType", resourceType,
			"id", resourceID)

		var pseudonymized map[string]any

		// Process resource based on type
		if resourceType == "Bundle" {
			pseudonymized, err = processor.ProcessBundle(resource, resourceID)
		} else {
			pseudonymized, err = processor.ProcessNonBundle(resource, resourceType, resourceID)
		}

		if err != nil {
			// Clear progress bar before logging error
			if progressBar != nil {
				_ = progressBar.Clear()
			}

			// Print user-friendly error message
			fmt.Printf("\n✗ DIMP pseudonymization failed\n")
			fmt.Printf("  File: %s (line %d)\n", filepath.Base(inputFile), processor.GetResourceCount()+1)
			fmt.Printf("  Resource: %s/%s\n", resourceType, resourceID)
			fmt.Printf("  Error: %v\n\n", err)

			return processor.GetResourceCount(), err
		}

		// Write pseudonymized resource to output
		if err := WriteProcessedResource(pseudonymized, fileCtx.OutFile); err != nil {
			return processor.GetResourceCount(), err
		}

		processor.IncrementResourceCount()

		// Update progress
		if progressBar != nil {
			_ = progressBar.Add(1)
		}
	}

	if err := scanner.Err(); err != nil {
		return processor.GetResourceCount(), fmt.Errorf("error reading file: %w", err)
	}

	// Finish progress bar
	if progressBar != nil {
		_ = progressBar.Finish()
	}

	// Finalize file processing with atomic rename
	if err := FinalizeFileProcessing(fileCtx, outputFile, true); err != nil {
		return processor.GetResourceCount(), err
	}

	return processor.GetResourceCount(), nil
}

// newLargeBufferScanner creates a bufio.Scanner with a 100MB buffer to handle very large FHIR resources
// Default bufio.Scanner buffer is 64KB which can cause "token too long" errors with complex queries
func newLargeBufferScanner(r interface{ Read([]byte) (int, error) }) *bufio.Scanner {
	scanner := bufio.NewScanner(r)
	// Use 100MB buffer for very large FHIR resources (default is 64KB)
	buf := make([]byte, 0, 100*1024*1024)
	scanner.Buffer(buf, 100*1024*1024)
	return scanner
}

// countResourcesInFile counts the number of non-empty lines in an NDJSON file
func countResourcesInFile(filename string) int {
	file, err := os.Open(filename)
	if err != nil {
		return 0
	}
	defer func() { _ = file.Close() }()

	count := 0
	scanner := newLargeBufferScanner(file)
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) != "" {
			count++
		}
	}

	return count
}

// isDIMPErrorRetryable checks if a DIMP error should be retried
func isDIMPErrorRetryable(err error) bool {
	if dimpErr, ok := err.(*services.DIMPError); ok {
		return dimpErr.IsRetryable()
	}
	// Network errors are retryable
	return lib.IsNetworkError(err)
}

// classifyDIMPError classifies a DIMP error as transient or non-transient
func classifyDIMPError(err error) models.ErrorType {
	if dimpErr, ok := err.(*services.DIMPError); ok {
		return dimpErr.ErrorType
	}
	// Network errors are transient
	if lib.IsNetworkError(err) {
		return models.ErrorTypeTransient
	}
	return models.ErrorTypeNonTransient
}

// Helper functions

func isStepEnabled(config models.ProjectConfig, stepName models.StepName) bool {
	for _, enabled := range config.Pipeline.EnabledSteps {
		if enabled == stepName {
			return true
		}
	}
	return false
}

func getOrCreateStep(job *models.PipelineJob, stepName models.StepName) *models.PipelineStep {
	for i := range job.Steps {
		if job.Steps[i].Name == stepName {
			return &job.Steps[i]
		}
	}

	// Create new step
	step := models.PipelineStep{
		Name:   stepName,
		Status: models.StepStatusPending,
	}
	job.Steps = append(job.Steps, step)
	return &job.Steps[len(job.Steps)-1]
}

func recordStepError(step *models.PipelineStep, err error, errorType models.ErrorType) {
	step.Status = models.StepStatusFailed
	step.LastError = &models.StepError{
		Type:      errorType,
		Message:   err.Error(),
		Timestamp: time.Now(),
	}
}
