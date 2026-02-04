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
)

// ExecuteFlatteningStep transforms FHIR NDJSON data into CSV files using the fhir-flattener API
// Reads from pseudonymized/ (if DIMP enabled) or import/ directory
// Outputs to csv/ directory
func ExecuteFlatteningStep(job *models.PipelineJob, jobDir string, logger *lib.Logger) error {
	stepName := models.StepFlattening

	if !isStepEnabled(job.Config, stepName) {
		logger.Info("Flattening step not enabled, skipping", "job_id", job.JobID)
		return nil
	}

	logger.Debug("Flattening step starting", "job_id", job.JobID)

	step := getOrCreateStep(job, stepName)
	step.Status = models.StepStatusInProgress
	now := time.Now()
	step.StartedAt = &now

	// Validate flattening configuration
	if err := job.Config.Services.Flattening.Validate(); err != nil {
		lib.LogStepFailed(logger, string(stepName), job.JobID, err, false)
		recordStepError(step, err, models.ErrorTypeNonTransient)
		return err
	}

	// CRTDL file is required for flattening
	if job.InputType != models.InputTypeCRTDL {
		err := fmt.Errorf("flattening step requires CRTDL input (got %s)", job.InputType)
		lib.LogStepFailed(logger, string(stepName), job.JobID, err, false)
		recordStepError(step, err, models.ErrorTypeNonTransient)
		return err
	}

	// Load CRTDL document
	crtdlPath := job.InputSource
	logger.Debug("Loading CRTDL file", "path", crtdlPath)
	crtdl, err := services.ParseCRTDL(crtdlPath)
	if err != nil {
		lib.LogStepFailed(logger, string(stepName), job.JobID, err, false)
		recordStepError(step, err, models.ErrorTypeNonTransient)
		return fmt.Errorf("failed to parse CRTDL file: %w", err)
	}

	// Load lookup tables
	lookupPath := job.Config.Services.Flattening.LookupPath
	logger.Debug("Loading lookup tables", "path", lookupPath)
	lookupTables, err := services.LoadLookupTables(lookupPath)
	if err != nil {
		lib.LogStepFailed(logger, string(stepName), job.JobID, err, false)
		recordStepError(step, err, models.ErrorTypeNonTransient)
		return fmt.Errorf("failed to load lookup tables: %w", err)
	}

	// Validate lookup tables
	if err := services.ValidateLookupTables(lookupTables); err != nil {
		lib.LogStepFailed(logger, string(stepName), job.JobID, err, false)
		recordStepError(step, err, models.ErrorTypeNonTransient)
		return fmt.Errorf("invalid lookup tables: %w", err)
	}

	// Setup directories
	jobsBaseDir := filepath.Dir(jobDir)
	jobID := filepath.Base(jobDir)
	stepIndex := getStepIndexInEnabledSteps(job.Config.Pipeline.EnabledSteps, stepName)
	inputDir := services.GetStepInputDir(jobsBaseDir, jobID, job.Config.Pipeline.EnabledSteps, stepIndex)
	outputDir := filepath.Join(jobDir, "csv")
	viewDefDir := filepath.Join(jobDir, "viewdefinitions")

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		lib.LogStepFailed(logger, string(stepName), job.JobID, err, false)
		recordStepError(step, err, models.ErrorTypeNonTransient)
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Find FHIR NDJSON files in input directory
	files, err := findFHIRFiles(inputDir)
	if err != nil {
		lib.LogStepFailed(logger, string(stepName), job.JobID, err, false)
		recordStepError(step, err, models.ErrorTypeNonTransient)
		return fmt.Errorf("failed to list input files: %w", err)
	}

	if len(files) == 0 {
		err := fmt.Errorf("no FHIR NDJSON files found in %s", inputDir)
		lib.LogStepFailed(logger, string(stepName), job.JobID, err, false)
		recordStepError(step, err, models.ErrorTypeNonTransient)
		return err
	}

	logger.Info("Loading FHIR resources from input files",
		"input_dir", inputDir,
		"file_count", len(files),
		"job_id", job.JobID)

	// Load all resources from input files
	allResources, err := LoadAllResources(files, logger)
	if err != nil {
		lib.LogStepFailed(logger, string(stepName), job.JobID, err, false)
		recordStepError(step, err, models.ErrorTypeNonTransient)
		return fmt.Errorf("failed to load resources: %w", err)
	}

	logger.Info("Loaded FHIR resources",
		"total_resources", len(allResources),
		"job_id", job.JobID)

	// Create clients
	flattenerClient := services.NewFlattenerClient(job.Config.Services.Flattening, logger)
	viewDefBuilder := services.NewViewDefinitionBuilder(lookupTables)
	csvWriter := services.NewCSVWriter(outputDir)
	viewDefWriter := services.NewViewDefinitionWriter(viewDefDir)

	attributeGroups := services.GetAttributeGroups(crtdl)
	fmt.Printf("Processing %d attribute group(s)...\n\n", len(attributeGroups))

	totalFilesWritten := 0

	// Process each attribute group
	for _, group := range attributeGroups {
		logger.Debug("Processing attribute group",
			"group_name", group.Name,
			"group_reference", group.GroupReference,
			"job_id", job.JobID)

		// Build ViewDefinition for this group
		viewDef, err := viewDefBuilder.BuildViewDefinition(group)
		if err != nil {
			logger.Warn("Failed to build ViewDefinition for group, skipping",
				"group_name", group.Name,
				"error", err)
			fmt.Printf("  ⚠ %s (skipped: %v)\n", group.Name, err)
			continue
		}

		// Save ViewDefinition to disk for debugging/auditing (before flattener call)
		viewDefFilename := services.BuildViewDefinitionFilename(group.Name)
		if err := viewDefWriter.WriteViewDefinition(viewDefFilename, *viewDef); err != nil {
			logger.Warn("Failed to save ViewDefinition, continuing",
				"group_name", group.Name,
				"filename", viewDefFilename,
				"error", err)
		}

		// Filter resources by profile URL
		matchingResources := FilterResourcesByProfile(allResources, group.GroupReference)
		if len(matchingResources) == 0 {
			logger.Debug("No resources match profile",
				"group_name", group.Name,
				"profile", group.GroupReference)
			fmt.Printf("  ⊙ %s (no matching resources)\n", group.Name)
			continue
		}

		logger.Debug("Found matching resources",
			"group_name", group.Name,
			"count", len(matchingResources))

		// Call flattener service
		csvData, err := flattenerClient.Flatten(*viewDef, matchingResources)
		if err != nil {
			lib.LogStepFailed(logger, string(stepName), job.JobID, err, IsFlatteningErrorRetryable(err))
			recordStepError(step, err, ClassifyFlatteningError(err))
			return fmt.Errorf("flattener failed for group '%s': %w", group.Name, err)
		}

		// Extract header from ViewDefinition
		header := services.ExtractColumnNames(*viewDef)

		// Write CSV file
		filename := services.BuildCSVFilename(group.Name)
		if err := csvWriter.WriteCSV(filename, header, csvData); err != nil {
			lib.LogStepFailed(logger, string(stepName), job.JobID, err, false)
			recordStepError(step, err, models.ErrorTypeNonTransient)
			return fmt.Errorf("failed to write CSV for group '%s': %w", group.Name, err)
		}

		totalFilesWritten++
		fmt.Printf("  ✓ %s (%d resources → %s)\n", group.Name, len(matchingResources), filename)

		logger.Debug("CSV file written",
			"group_name", group.Name,
			"filename", filename,
			"resource_count", len(matchingResources),
			"job_id", job.JobID)
	}

	step.Status = models.StepStatusCompleted
	step.FilesProcessed = totalFilesWritten
	completedAt := time.Now()
	step.CompletedAt = &completedAt

	duration := completedAt.Sub(*step.StartedAt)

	logger.Debug("Flattening step completed",
		"files_written", totalFilesWritten,
		"duration", duration,
		"job_id", job.JobID)

	return nil
}

// LoadAllResources loads all FHIR resources from the given NDJSON files
func LoadAllResources(files []string, logger *lib.Logger) ([]map[string]any, error) {
	var allResources []map[string]any

	for _, filePath := range files {
		resources, err := LoadResourcesFromFile(filePath, logger)
		if err != nil {
			return nil, fmt.Errorf("failed to load %s: %w", filepath.Base(filePath), err)
		}
		allResources = append(allResources, resources...)
	}

	return allResources, nil
}

// LoadResourcesFromFile loads FHIR resources from a single NDJSON file
// Handles both compressed (.ndjson.zst) and uncompressed (.ndjson) files
func LoadResourcesFromFile(filePath string, logger *lib.Logger) ([]map[string]any, error) {
	// Use lib.OpenFileForReading to auto-detect compression
	reader, err := lib.OpenFileForReading(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer func() { _ = reader.Close() }()

	var resources []map[string]any
	scanner := bufio.NewScanner(reader)
	// Increase buffer size for large resources
	buf := make([]byte, 0, 100*1024*1024)
	scanner.Buffer(buf, 100*1024*1024)

	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var resource map[string]any
		if err := json.Unmarshal([]byte(line), &resource); err != nil {
			logger.Warn("Failed to parse resource",
				"file", filepath.Base(filePath),
				"line", lineNum,
				"error", err)
			continue
		}

		// If resource is a Bundle, extract individual resources from entries
		if resourceType, ok := resource["resourceType"].(string); ok && resourceType == "Bundle" {
			entries, ok := resource["entry"].([]any)
			if ok {
				for _, entry := range entries {
					entryMap, ok := entry.(map[string]any)
					if !ok {
						continue
					}
					entryResource, ok := entryMap["resource"].(map[string]any)
					if ok {
						resources = append(resources, entryResource)
					}
				}
			}
		} else {
			resources = append(resources, resource)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading file: %w", err)
	}

	return resources, nil
}

// FilterResourcesByProfile filters resources by matching meta.profile[0] to the profile URL
func FilterResourcesByProfile(resources []map[string]any, profileURL string) []map[string]any {
	var matching []map[string]any

	for _, resource := range resources {
		meta, ok := resource["meta"].(map[string]any)
		if !ok {
			continue
		}

		profiles, ok := meta["profile"].([]any)
		if !ok || len(profiles) == 0 {
			continue
		}

		firstProfile, ok := profiles[0].(string)
		if !ok {
			continue
		}

		if firstProfile == profileURL {
			matching = append(matching, resource)
		}
	}

	return matching
}

// IsFlatteningErrorRetryable checks if a flattening error should be retried
func IsFlatteningErrorRetryable(err error) bool {
	if flattenerErr, ok := err.(*services.FlattenerError); ok {
		return flattenerErr.IsRetryable()
	}
	return lib.IsNetworkError(err)
}

// ClassifyFlatteningError classifies a flattening error as transient or non-transient
func ClassifyFlatteningError(err error) models.ErrorType {
	if flattenerErr, ok := err.(*services.FlattenerError); ok {
		return flattenerErr.ErrorType
	}
	if lib.IsNetworkError(err) {
		return models.ErrorTypeTransient
	}
	return models.ErrorTypeNonTransient
}
