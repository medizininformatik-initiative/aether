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

	if !isStepEnabled(job.Config, stepName) {
		logger.Info("DIMP step not enabled, skipping", "job_id", job.JobID)
		return nil
	}

	logger.Debug("DIMP step starting", "job_id", job.JobID)

	step := getOrCreateStep(job, stepName)
	step.Status = models.StepStatusInProgress
	now := time.Now()
	step.StartedAt = &now

	if job.Config.Services.DIMP.URL == "" {
		err := fmt.Errorf("DIMP service URL not configured")
		lib.LogStepFailed(logger, string(stepName), job.JobID, err, false)
		recordStepError(step, err, models.ErrorTypeNonTransient)
		return err
	}

	httpClient := services.DefaultHTTPClient()
	dimpClient := services.NewDIMPClient(job.Config.Services.DIMP.URL, httpClient, logger)

	// Setup directories - use GetStepInputDir to handle wait step correctly
	jobsBaseDir := filepath.Dir(jobDir)
	jobID := filepath.Base(jobDir)
	stepIndex := getStepIndexInEnabledSteps(job.Config.Pipeline.EnabledSteps, stepName)
	importDir := services.GetStepInputDir(jobsBaseDir, jobID, job.Config.Pipeline.EnabledSteps, stepIndex)
	outputDir := filepath.Join(jobDir, "pseudonymized")

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		lib.LogStepFailed(logger, string(stepName), job.JobID, err, false)
		recordStepError(step, err, models.ErrorTypeNonTransient)
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	files, err := findFHIRFiles(importDir)
	if err != nil {
		lib.LogStepFailed(logger, string(stepName), job.JobID, err, false)
		recordStepError(step, err, models.ErrorTypeNonTransient)
		return fmt.Errorf("failed to list import files: %w", err)
	}

	if len(files) == 0 {
		err := fmt.Errorf("no FHIR NDJSON files found in %s", importDir)
		lib.LogStepFailed(logger, string(stepName), job.JobID, err, false)
		recordStepError(step, err, models.ErrorTypeNonTransient)
		return err
	}

	if err := models.DetectDuplicateFHIRFiles(files); err != nil {
		lib.LogStepFailed(logger, string(stepName), job.JobID, err, false)
		recordStepError(step, err, models.ErrorTypeNonTransient)
		return err
	}

	fmt.Printf("Processing %d FHIR file(s) through DIMP...\n\n", len(files))

	partFiles, _ := filepath.Glob(filepath.Join(outputDir, "*.part"))
	for _, partFile := range partFiles {
		logger.Debug("Removing stale partial file from previous run", "file", filepath.Base(partFile))
		_ = os.Remove(partFile)
	}

	compress := job.Config.Compression.Enabled
	compressionLevel := job.Config.Compression.Level

	totalResourcesProcessed := 0
	filesProcessed := 0
	for fileIdx, inputFile := range files {
		baseName := lib.GetUncompressedFilename(filepath.Base(inputFile))
		outputBaseName := "dimped_" + baseName
		outputBaseName = lib.GetCompressedFilename(outputBaseName, compress)
		outputFile := filepath.Join(outputDir, outputBaseName)

		if _, err := os.Stat(outputFile); err == nil {
			fmt.Printf("  ⊙ %s (already processed, skipping)\n", baseName)
			logger.Debug("Skipping already processed file",
				"filename", baseName,
				"output_file", outputFile,
				"job_id", job.JobID)
			filesProcessed++
			if lineCount, err := lib.CountResourcesInFile(outputFile); err == nil {
				totalResourcesProcessed += lineCount
			}
			continue
		}

		resourcesProcessed, err := processDIMPFile(inputFile, outputFile, dimpClient, logger, job, compress, compressionLevel)
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

		fmt.Printf("  ✓ %s (%d resources)\n", baseName, resourcesProcessed)

		totalResourcesProcessed += resourcesProcessed
		filesProcessed++
	}

	step.Status = models.StepStatusCompleted
	step.FilesProcessed = len(files)
	completedAt := time.Now()
	step.CompletedAt = &completedAt

	duration := completedAt.Sub(*step.StartedAt)

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
// If compress is true, output will be compressed with zstd
func processDIMPFile(inputFile, outputFile string, dimpClient *services.DIMPClient, logger *lib.Logger, job *models.PipelineJob, compress bool, compressionLevel string) (int, error) {
	fileCtx, err := SetupFileProcessing(inputFile, outputFile, compress, compressionLevel)
	if err != nil {
		return 0, err
	}

	totalResources := countResourcesInFile(inputFile)

	var progressBar *ui.ProgressBar
	if totalResources > 0 {
		progressBar = ui.NewProgressBar(int64(totalResources), fmt.Sprintf("Pseudonymizing %s", filepath.Base(inputFile)))
	} else {
		logger.Info("Processing FHIR resources (unknown count)", "file", filepath.Base(inputFile))
	}

	thresholdMB := job.Config.Services.DIMP.BundleSplitThresholdMB
	if thresholdMB <= 0 {
		thresholdMB = 10 // Default to 10MB if not configured
	}
	thresholdBytes := thresholdMB * 1024 * 1024

	processor := NewResourceProcessor(dimpClient, logger, thresholdBytes, inputFile)
	scanner := newLargeBufferScanner(fileCtx.InFile)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var resource map[string]any
		if err := json.Unmarshal([]byte(line), &resource); err != nil {
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

		logger.Debug("Processing FHIR resource",
			"file", filepath.Base(inputFile),
			"line_number", processor.GetResourceCount()+1,
			"resourceType", resourceType,
			"id", resourceID)

		var pseudonymized map[string]any

		if resourceType == "Bundle" {
			pseudonymized, err = processor.ProcessBundle(resource, resourceID)
		} else {
			pseudonymized, err = processor.ProcessNonBundle(resource, resourceType, resourceID)
		}

		if err != nil {
			if progressBar != nil {
				_ = progressBar.Clear()
			}

			fmt.Printf("\n✗ DIMP pseudonymization failed\n")
			fmt.Printf("  File: %s (line %d)\n", filepath.Base(inputFile), processor.GetResourceCount()+1)
			fmt.Printf("  Resource: %s/%s\n", resourceType, resourceID)
			fmt.Printf("  Error: %v\n\n", err)

			return processor.GetResourceCount(), err
		}

		if err := WriteProcessedResource(pseudonymized, fileCtx.OutWriter); err != nil {
			return processor.GetResourceCount(), err
		}

		processor.IncrementResourceCount()

		if progressBar != nil {
			_ = progressBar.Add(1)
		}
	}

	if err := scanner.Err(); err != nil {
		return processor.GetResourceCount(), fmt.Errorf("error reading file: %w", err)
	}

	if progressBar != nil {
		_ = progressBar.Finish()
	}

	if err := FinalizeFileProcessing(fileCtx, outputFile, true); err != nil {
		return processor.GetResourceCount(), err
	}

	return processor.GetResourceCount(), nil
}

// newLargeBufferScanner creates a bufio.Scanner with a 100MB buffer to handle very large FHIR resources
// Default bufio.Scanner buffer is 64KB which can cause "token too long" errors with complex queries
func newLargeBufferScanner(r interface{ Read([]byte) (int, error) }) *bufio.Scanner {
	scanner := bufio.NewScanner(r)
	buf := make([]byte, 0, 100*1024*1024)
	scanner.Buffer(buf, 100*1024*1024)
	return scanner
}

// countResourcesInFile counts the number of non-empty lines in an NDJSON file
// Handles both compressed (.ndjson.zst) and uncompressed (.ndjson) files
func countResourcesInFile(filename string) int {
	count, err := lib.CountResourcesInFile(filename)
	if err != nil {
		return 0
	}
	return count
}

// findFHIRFiles finds all FHIR NDJSON files in a directory
// Handles both compressed (.ndjson.zst) and uncompressed (.ndjson) files
func findFHIRFiles(dir string) ([]string, error) {
	var files []string

	ndjsonFiles, err := filepath.Glob(filepath.Join(dir, "*.ndjson"))
	if err != nil {
		return nil, err
	}
	files = append(files, ndjsonFiles...)

	compressedFiles, err := filepath.Glob(filepath.Join(dir, "*.ndjson.zst"))
	if err != nil {
		return nil, err
	}
	files = append(files, compressedFiles...)

	return files, nil
}

// isDIMPErrorRetryable checks if a DIMP error should be retried
func isDIMPErrorRetryable(err error) bool {
	if dimpErr, ok := err.(*services.DIMPError); ok {
		return dimpErr.IsRetryable()
	}
	return lib.IsNetworkError(err)
}

// classifyDIMPError classifies a DIMP error as transient or non-transient
func classifyDIMPError(err error) models.ErrorType {
	if dimpErr, ok := err.(*services.DIMPError); ok {
		return dimpErr.ErrorType
	}
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

// getStepIndexInEnabledSteps finds the index of a step in the enabled steps list
func getStepIndexInEnabledSteps(steps []models.StepName, target models.StepName) int {
	for i, step := range steps {
		if step == target {
			return i
		}
	}
	return -1
}
