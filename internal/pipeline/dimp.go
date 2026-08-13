package pipeline

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/services"
	"github.com/medizininformatik-initiative/aether/internal/ui"
)

// defaultDIMPFactory is the production DIMP client constructor.
var defaultDIMPFactory = func(config models.DIMPConfig, httpClient *services.HTTPClient, logger *lib.Logger) services.DIMPProcessor {
	return services.NewDIMPClient(config, httpClient, logger)
}

// dimpFactory creates a DIMPProcessor. Overridable in tests.
var dimpFactory = defaultDIMPFactory

// SetDIMPFactoryForTesting replaces the DIMP client factory for tests.
func SetDIMPFactoryForTesting(factory func(models.DIMPConfig, *services.HTTPClient, *lib.Logger) services.DIMPProcessor) {
	dimpFactory = factory
}

// ResetDIMPFactory restores the default DIMP client factory.
func ResetDIMPFactory() {
	dimpFactory = defaultDIMPFactory
}

// dimpStep pseudonymizes FHIR resources through the DIMP service.
// Reads from import/ directory, writes to dimp/ directory. Orchestrates Bundle
// splitting and oversized resource detection before pseudonymization.
type dimpStep struct{}

func (dimpStep) Name() models.StepName { return models.StepDIMP }

func (dimpStep) Run(ctx *StepContext) (StepResult, error) {
	job := ctx.Job
	logger := ctx.Logger
	stepName := models.StepDIMP

	if job.Config.Services.DIMP.URL == "" {
		return StepResult{}, fmt.Errorf("DIMP service URL not configured")
	}

	httpClient := services.NewHTTPClient(30*time.Second, job.Config.Retry, job.Config.TLS, logger)
	dimpClient := dimpFactory(job.Config.Services.DIMP, httpClient, logger)

	importDir := ctx.Layout.InputDir(stepName)
	outputDir := ctx.Layout.OutputDir(stepName)

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return StepResult{}, fmt.Errorf("failed to create output directory: %w", err)
	}

	files, err := findFHIRFiles(importDir)
	if err != nil {
		return StepResult{}, fmt.Errorf("failed to list import files: %w", err)
	}

	if len(files) == 0 {
		return StepResult{}, fmt.Errorf("no FHIR NDJSON files found in %s", importDir)
	}

	if err := models.DetectDuplicateFHIRFiles(files); err != nil {
		return StepResult{}, err
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
			return StepResult{}, fmt.Errorf("failed to process %s: %w", baseName, err)
		}

		fmt.Printf("  ✓ %s (%d resources)\n", baseName, resourcesProcessed)
		totalResourcesProcessed += resourcesProcessed
	}

	logger.Debug("DIMP step completed",
		"files_processed", len(files),
		"resources_processed", totalResourcesProcessed,
		"job_id", job.JobID,
	)

	return StepResult{FilesProcessed: len(files)}, nil
}

// processDIMPFile processes a single NDJSON file through DIMP
// Returns the number of resources processed
// Uses atomic write pattern: writes to .part file, renames on success
// Implements Bundle splitting for large Bundles to prevent HTTP 413 errors
// If compress is true, output will be compressed with zstd
func processDIMPFile(inputFile, outputFile string, dimpClient services.DIMPProcessor, logger *lib.Logger, job *models.PipelineJob, compress bool, compressionLevel string) (int, error) {
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
	dec := json.NewDecoder(fileCtx.InFile)

	for {
		var resource map[string]any
		if err := dec.Decode(&resource); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			if progressBar != nil {
				_ = progressBar.Clear()
			}
			logger.Error("Failed to parse FHIR resource",
				"file", filepath.Base(inputFile),
				"resource_number", processor.GetResourceCount()+1,
				"error", err)
			return processor.GetResourceCount(), fmt.Errorf("failed to parse resource %d: %w", processor.GetResourceCount()+1, err)
		}

		resourceType := lib.ResourceType(resource)
		resourceID := lib.ResourceID(resource)

		logger.Debug("Processing FHIR resource",
			"file", filepath.Base(inputFile),
			"resource_number", processor.GetResourceCount()+1,
			"resourceType", resourceType,
			"id", resourceID)

		var pseudonymized map[string]any
		var err error

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
			fmt.Printf("  File: %s (resource %d)\n", filepath.Base(inputFile), processor.GetResourceCount()+1)
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

	if progressBar != nil {
		_ = progressBar.Finish()
	}

	if err := FinalizeFileProcessing(fileCtx, outputFile, true); err != nil {
		return processor.GetResourceCount(), err
	}

	return processor.GetResourceCount(), nil
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
