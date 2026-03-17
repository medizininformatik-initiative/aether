package pipeline

import (
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

// groupBatch accumulates resources for a single attribute group until the batch
// size threshold is reached, at which point it is flushed to the flattener service.
type groupBatch struct {
	resources    []map[string]any
	byteSize     int
	isFirstBatch bool // true until first flush
}

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

	logger.Info("Streaming FHIR resources from input files",
		"input_dir", inputDir,
		"file_count", len(files),
		"job_id", job.JobID)

	// Create clients
	flattenerTransport, _ := services.BuildTLSTransport(job.Config.TLS, logger)
	flattenerClient := services.NewFlattenerClient(job.Config.Services.Flattening, flattenerTransport, logger)
	viewDefBuilder := services.NewViewDefinitionBuilder(lookupTables)
	csvWriter := services.NewCSVWriter(outputDir)
	viewDefWriter := services.NewViewDefinitionWriter(viewDefDir)

	attributeGroups := services.GetAttributeGroups(crtdl)
	fmt.Printf("Processing %d attribute group(s)...\n\n", len(attributeGroups))

	// Pre-compute: build profileToGroup mapping and ViewDefinitions
	profileToGroup := make(map[string]int)
	viewDefs := make([]*models.ViewDefinition, len(attributeGroups))
	headers := make([][]string, len(attributeGroups))
	filenames := make([]string, len(attributeGroups))

	for i, group := range attributeGroups {
		viewDef, err := viewDefBuilder.BuildViewDefinition(group)
		if err != nil {
			logger.Warn("Failed to build ViewDefinition for group, skipping",
				"group_name", group.Name,
				"error", err)
			fmt.Printf("  ⚠ %s (skipped: %v)\n", group.Name, err)
			continue
		}

		viewDefs[i] = viewDef
		profileToGroup[group.GroupReference] = i
		headers[i] = services.ExtractColumnNames(*viewDef)
		filenames[i] = services.BuildCSVFilename(group.Name)

		// Save ViewDefinition to disk
		viewDefFilename := services.BuildViewDefinitionFilename(group.Name)
		if err := viewDefWriter.WriteViewDefinition(viewDefFilename, *viewDef); err != nil {
			logger.Warn("Failed to save ViewDefinition, continuing",
				"group_name", group.Name,
				"filename", viewDefFilename,
				"error", err)
		}
	}

	// Stream resources and flatten in batches
	totals, err := streamAndFlattenResources(
		files,
		attributeGroups,
		profileToGroup,
		viewDefs,
		headers,
		filenames,
		flattenerClient,
		csvWriter,
		logger,
		job.Config.Pipeline.MaxNDJSONLineSize(),
		job.Config.Services.Flattening.GetBatchSizeBytes(),
	)
	if err != nil {
		lib.LogStepFailed(logger, string(stepName), job.JobID, err, IsFlatteningErrorRetryable(err))
		recordStepError(step, err, ClassifyFlatteningError(err))
		return err
	}

	// Print per-group progress
	totalFilesWritten := 0
	for i, group := range attributeGroups {
		if viewDefs[i] == nil {
			continue
		}
		if totals[i] == 0 {
			fmt.Printf("  ⊙ %s (no matching resources)\n", group.Name)
			continue
		}
		totalFilesWritten++
		fmt.Printf("  ✓ %s (%d resources → %s)\n", group.Name, totals[i], filenames[i])
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

// streamAndFlattenResources performs single-pass streaming over all input files,
// routing each resource to per-group batches and flushing when the byte threshold
// is exceeded. Returns per-group resource totals.
func streamAndFlattenResources(
	files []string,
	groups []models.AttributeGroup,
	profileToGroup map[string]int,
	viewDefs []*models.ViewDefinition,
	headers [][]string,
	filenames []string,
	flattenerClient *services.FlattenerClient,
	csvWriter *services.CSVWriter,
	logger *lib.Logger,
	maxLineSize int,
	batchSizeBytes int,
) ([]int, error) {
	numGroups := len(groups)
	batches := make([]groupBatch, numGroups)
	totals := make([]int, numGroups)

	// Divide total memory budget across groups so peak usage stays within batchSizeBytes
	perGroupBytes := batchSizeBytes / numGroups
	if perGroupBytes < 1 {
		perGroupBytes = 1
	}

	// Initialize all batches as first batch
	for i := range batches {
		batches[i].isFirstBatch = true
	}

	// Stream through all files
	for _, filePath := range files {
		reader, err := lib.OpenFileForReading(filePath)
		if err != nil {
			return nil, fmt.Errorf("failed to load resources: %w", fmt.Errorf("failed to load %s: %w", filepath.Base(filePath), fmt.Errorf("failed to open file: %w", err)))
		}

		scanner := lib.NewNDJSONScannerWithMaxSize(reader, maxLineSize)
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

			// If resource is a Bundle, extract and route individual entries
			if resourceType, ok := resource["resourceType"].(string); ok && resourceType == "Bundle" {
				entries, ok := resource["entry"].([]any)
				if ok {
					for _, entry := range entries {
						entryMap, ok := entry.(map[string]any)
						if !ok {
							continue
						}
						entryResource, ok := entryMap["resource"].(map[string]any)
						if !ok {
							continue
						}
						// For Bundle entries, compute size via marshal
						entryBytes, marshalErr := json.Marshal(entryResource)
						if marshalErr != nil {
							continue
						}
						groupIdx, matched := routeResourceToGroup(entryResource, profileToGroup, viewDefs)
						if !matched {
							continue
						}
						batches[groupIdx].resources = append(batches[groupIdx].resources, entryResource)
						batches[groupIdx].byteSize += len(entryBytes)
						totals[groupIdx]++

						if batches[groupIdx].byteSize >= perGroupBytes {
							if err := flushGroupBatch(&batches[groupIdx], viewDefs[groupIdx], headers[groupIdx], filenames[groupIdx], flattenerClient, csvWriter, logger, groups[groupIdx].Name); err != nil {
								_ = reader.Close()
								return nil, err
							}
						}
					}
				}
			} else {
				// Non-Bundle resource: use scanner bytes length for size tracking
				resourceSize := len(scanner.Bytes())
				groupIdx, matched := routeResourceToGroup(resource, profileToGroup, viewDefs)
				if !matched {
					continue
				}
				batches[groupIdx].resources = append(batches[groupIdx].resources, resource)
				batches[groupIdx].byteSize += resourceSize
				totals[groupIdx]++

				if batches[groupIdx].byteSize >= perGroupBytes {
					if err := flushGroupBatch(&batches[groupIdx], viewDefs[groupIdx], headers[groupIdx], filenames[groupIdx], flattenerClient, csvWriter, logger, groups[groupIdx].Name); err != nil {
						_ = reader.Close()
						return nil, err
					}
				}
			}
		}

		if err := scanner.Err(); err != nil {
			_ = reader.Close()
			return nil, fmt.Errorf("failed to load resources: %w", fmt.Errorf("failed to load %s: %w", filepath.Base(filePath), fmt.Errorf("error reading file: %w", err)))
		}

		_ = reader.Close()
	}

	// Flush remaining non-empty batches
	for i := range batches {
		if len(batches[i].resources) > 0 {
			if err := flushGroupBatch(&batches[i], viewDefs[i], headers[i], filenames[i], flattenerClient, csvWriter, logger, groups[i].Name); err != nil {
				return nil, err
			}
		}
	}

	return totals, nil
}

// routeResourceToGroup determines which group a resource belongs to based on
// its meta.profile[0] URL. Returns the group index and whether a match was found.
func routeResourceToGroup(resource map[string]any, profileToGroup map[string]int, viewDefs []*models.ViewDefinition) (int, bool) {
	meta, ok := resource["meta"].(map[string]any)
	if !ok {
		return 0, false
	}

	profiles, ok := meta["profile"].([]any)
	if !ok || len(profiles) == 0 {
		return 0, false
	}

	firstProfile, ok := profiles[0].(string)
	if !ok {
		return 0, false
	}

	groupIdx, exists := profileToGroup[firstProfile]
	if !exists {
		return 0, false
	}

	// Skip groups where ViewDefinition build failed
	if viewDefs[groupIdx] == nil {
		return 0, false
	}

	return groupIdx, true
}

// flushGroupBatch sends the accumulated batch to the flattener and appends the result to CSV.
func flushGroupBatch(
	batch *groupBatch,
	viewDef *models.ViewDefinition,
	header []string,
	filename string,
	flattenerClient *services.FlattenerClient,
	csvWriter *services.CSVWriter,
	logger *lib.Logger,
	groupName string,
) error {
	logger.Debug("Flushing batch",
		"group_name", groupName,
		"resource_count", len(batch.resources),
		"byte_size", batch.byteSize)

	csvData, err := flattenerClient.Flatten(*viewDef, batch.resources)
	if err != nil {
		return fmt.Errorf("flattener failed for group '%s': %w", groupName, err)
	}

	if err := csvWriter.AppendCSVData(filename, header, csvData, batch.isFirstBatch); err != nil {
		return fmt.Errorf("failed to write CSV for group '%s': %w", groupName, err)
	}

	// Reset batch state
	batch.resources = nil
	batch.byteSize = 0
	batch.isFirstBatch = false

	return nil
}

// LoadAllResources loads all FHIR resources from the given NDJSON files
func LoadAllResources(files []string, logger *lib.Logger, maxLineSize int) ([]map[string]any, error) {
	var allResources []map[string]any

	for _, filePath := range files {
		resources, err := LoadResourcesFromFile(filePath, logger, maxLineSize)
		if err != nil {
			return nil, fmt.Errorf("failed to load %s: %w", filepath.Base(filePath), err)
		}
		allResources = append(allResources, resources...)
	}

	return allResources, nil
}

// LoadResourcesFromFile loads FHIR resources from a single NDJSON file
// Handles both compressed (.ndjson.zst) and uncompressed (.ndjson) files
func LoadResourcesFromFile(filePath string, logger *lib.Logger, maxLineSize int) ([]map[string]any, error) {
	// Use lib.OpenFileForReading to auto-detect compression
	reader, err := lib.OpenFileForReading(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer func() { _ = reader.Close() }()

	var resources []map[string]any
	scanner := lib.NewNDJSONScannerWithMaxSize(reader, maxLineSize)

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
