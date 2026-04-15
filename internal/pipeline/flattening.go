package pipeline

import (
	"encoding/json"
	"errors"
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

	// CRTDL file is required for flattening. It may come from the positional
	// arg (for torch) or from --crtdl (for http_import/local_import).
	if job.CRTDLPath == "" {
		err := fmt.Errorf("flattening step requires a CRTDL file: pass one as the positional input or via --crtdl")
		lib.LogStepFailed(logger, string(stepName), job.JobID, err, false)
		recordStepError(step, err, models.ErrorTypeNonTransient)
		return err
	}

	// Load CRTDL document
	crtdlPath := job.CRTDLPath
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

	// Pass 1: scan input files for provenance index
	provenanceIndex, err := scanProvenanceIndex(files, logger, job.Config.Pipeline.MaxNDJSONLineSize())
	if err != nil {
		lib.LogStepFailed(logger, string(stepName), job.JobID, err, false)
		recordStepError(step, err, models.ErrorTypeNonTransient)
		return fmt.Errorf("failed to load resources: %w", err)
	}

	logger.Info("Built provenance index",
		"provenance_entries", len(provenanceIndex),
		"provenance_source", inputDir,
		"job_id", job.JobID)

	// Create clients
	flattenerTransport, _ := services.BuildTLSTransport(job.Config.TLS, logger)
	flattenerClient := services.NewFlattenerClient(job.Config.Services.Flattening, job.Config.Retry, flattenerTransport, logger)
	viewDefBuilder := services.NewViewDefinitionBuilder(lookupTables)
	csvWriter := services.NewCSVWriter(outputDir)
	viewDefWriter := services.NewViewDefinitionWriter(viewDefDir)

	attributeGroups := services.GetAttributeGroups(crtdl)
	fmt.Printf("Processing %d attribute group(s)...\n\n", len(attributeGroups))

	// Pre-compute: build groupIDToIndex mapping and ViewDefinitions
	groupIDToIndex := make(map[string]int)
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
		groupIDToIndex[group.ID] = i
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

	// Pass 2: stream resources and flatten in batches using provenance routing
	totals, err := streamAndFlattenResources(
		files,
		attributeGroups,
		provenanceIndex,
		groupIDToIndex,
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

// scanProvenanceIndex performs a lightweight first pass over all input files,
// extracting only Provenance resources from Bundles to build the provenance index.
// Clinical resources are discarded to keep memory usage minimal.
func scanProvenanceIndex(files []string, logger *lib.Logger, maxLineSize int) (models.ProvenanceIndex, error) {
	mergedIndex := make(models.ProvenanceIndex)

	for _, filePath := range files {
		reader, err := lib.OpenFileForReading(filePath)
		if err != nil {
			return nil, fmt.Errorf("failed to load %s: %w", filepath.Base(filePath), fmt.Errorf("failed to open file: %w", err))
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
				continue // skip invalid JSON during provenance scan
			}

			// Only Bundles contain Provenance resources
			if resourceType, ok := resource["resourceType"].(string); ok && resourceType == "Bundle" {
				_, bundleIndex := extractBundleResources(resource)
				for k, v := range bundleIndex {
					mergedIndex[k] = append(mergedIndex[k], v...)
				}
			}
		}

		if err := scanner.Err(); err != nil {
			_ = reader.Close()
			return nil, fmt.Errorf("failed to load %s: %w", filepath.Base(filePath), fmt.Errorf("error reading file: %w", err))
		}

		_ = reader.Close()
	}

	return mergedIndex, nil
}

// streamAndFlattenResources performs single-pass streaming over all input files,
// routing each resource to per-group batches via provenance index and flushing
// when the byte threshold is exceeded. Returns per-group resource totals.
func streamAndFlattenResources(
	files []string,
	groups []models.AttributeGroup,
	provenanceIndex models.ProvenanceIndex,
	groupIDToIndex map[string]int,
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

			// If resource is a Bundle, extract clinical entries (skip Provenance)
			if resourceType, ok := resource["resourceType"].(string); ok && resourceType == "Bundle" {
				clinicalResources, _ := extractBundleResources(resource)
				for _, entryResource := range clinicalResources {
					entryBytes, marshalErr := json.Marshal(entryResource)
					if marshalErr != nil {
						continue
					}
					for _, groupIdx := range routeResourceToGroups(entryResource, provenanceIndex, groupIDToIndex, viewDefs) {
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
				// Non-Bundle resource: route via provenance index
				resourceSize := len(scanner.Bytes())
				for _, groupIdx := range routeResourceToGroups(resource, provenanceIndex, groupIDToIndex, viewDefs) {
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

// routeResourceToGroups determines which groups a resource belongs to based on
// the provenance index. Returns the group indices for all matching groups.
func routeResourceToGroups(resource map[string]any, provenanceIndex models.ProvenanceIndex, groupIDToIndex map[string]int, viewDefs []*models.ViewDefinition) []int {
	ref := ResourceReference(resource)
	if ref == "" {
		return nil
	}

	groupIDs, exists := provenanceIndex[ref]
	if !exists {
		return nil
	}

	var indices []int
	for _, groupID := range groupIDs {
		groupIdx, exists := groupIDToIndex[groupID]
		if !exists {
			continue
		}
		if viewDefs[groupIdx] == nil {
			continue
		}
		indices = append(indices, groupIdx)
	}

	return indices
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

// LoadAllResources loads all FHIR resources from the given NDJSON files.
// Returns clinical resources (excluding Provenance) and a provenance index
// that maps resource references to CRTDL attribute group IDs.
func LoadAllResources(files []string, logger *lib.Logger, maxLineSize int) ([]map[string]any, models.ProvenanceIndex, error) {
	var allResources []map[string]any
	mergedIndex := make(models.ProvenanceIndex)

	for _, filePath := range files {
		resources, index, err := LoadResourcesFromFile(filePath, logger, maxLineSize)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to load %s: %w", filepath.Base(filePath), err)
		}
		allResources = append(allResources, resources...)
		for k, v := range index {
			mergedIndex[k] = append(mergedIndex[k], v...)
		}
	}

	return allResources, mergedIndex, nil
}

// LoadResourcesFromFile loads FHIR resources from a single NDJSON file.
// Handles both compressed (.ndjson.zst) and uncompressed (.ndjson) files.
// For Bundles, Provenance resources are separated into a provenance index
// and excluded from the returned resource list.
func LoadResourcesFromFile(filePath string, logger *lib.Logger, maxLineSize int) ([]map[string]any, models.ProvenanceIndex, error) {
	reader, err := lib.OpenFileForReading(filePath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer func() { _ = reader.Close() }()

	var resources []map[string]any
	index := make(models.ProvenanceIndex)
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

		// If resource is a Bundle, extract clinical resources and build provenance index
		if resourceType, ok := resource["resourceType"].(string); ok && resourceType == "Bundle" {
			bundleResources, bundleIndex := extractBundleResources(resource)
			resources = append(resources, bundleResources...)
			for k, v := range bundleIndex {
				index[k] = append(index[k], v...)
			}
		} else {
			resources = append(resources, resource)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, nil, fmt.Errorf("error reading file: %w", err)
	}

	return resources, index, nil
}

// extractBundleResources separates Bundle entries into clinical resources and a provenance index.
// Provenance resources are used to build the index and excluded from the returned resources.
func extractBundleResources(bundle map[string]any) ([]map[string]any, models.ProvenanceIndex) {
	entries, ok := bundle["entry"].([]any)
	if !ok {
		return nil, nil
	}

	var resources []map[string]any
	var provenances []map[string]any

	for _, entry := range entries {
		entryMap, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		entryResource, ok := entryMap["resource"].(map[string]any)
		if !ok {
			continue
		}

		if rt, ok := entryResource["resourceType"].(string); ok && rt == "Provenance" {
			provenances = append(provenances, entryResource)
		} else {
			resources = append(resources, entryResource)
		}
	}

	index := buildProvenanceIndex(provenances)
	return resources, index
}

// buildProvenanceIndex creates a mapping from resource references to CRTDL attribute group IDs.
// Each Provenance resource's target references are mapped to the attribute group ID found
// in its entity with the attribute_group NamingSystem.
func buildProvenanceIndex(provenances []map[string]any) models.ProvenanceIndex {
	index := make(models.ProvenanceIndex)

	for _, prov := range provenances {
		groupID := extractAttributeGroupID(prov)
		if groupID == "" {
			continue
		}

		targets, ok := prov["target"].([]any)
		if !ok {
			continue
		}

		for _, target := range targets {
			targetMap, ok := target.(map[string]any)
			if !ok {
				continue
			}
			ref, ok := targetMap["reference"].(string)
			if !ok || ref == "" {
				continue
			}
			index[ref] = append(index[ref], groupID)
		}
	}

	return index
}

// extractAttributeGroupID finds the CRTDL attribute group ID from a Provenance resource's entities.
// Looks for an entity with role "source" and the attribute_group NamingSystem.
func extractAttributeGroupID(provenance map[string]any) string {
	entities, ok := provenance["entity"].([]any)
	if !ok {
		return ""
	}

	for _, entity := range entities {
		entityMap, ok := entity.(map[string]any)
		if !ok {
			continue
		}

		what, ok := entityMap["what"].(map[string]any)
		if !ok {
			continue
		}

		identifier, ok := what["identifier"].(map[string]any)
		if !ok {
			continue
		}

		system, _ := identifier["system"].(string)
		if system == models.AttributeGroupNamingSystem {
			value, _ := identifier["value"].(string)
			return value
		}
	}

	return ""
}

// FilterResourcesByProvenance returns resources whose "ResourceType/id" reference
// maps to the given attribute group ID in the provenance index.
func FilterResourcesByProvenance(resources []map[string]any, index models.ProvenanceIndex, groupID string) []map[string]any {
	var matching []map[string]any

	for _, resource := range resources {
		ref := ResourceReference(resource)
		if ref == "" {
			continue
		}
		for _, gid := range index[ref] {
			if gid == groupID {
				matching = append(matching, resource)
				break
			}
		}
	}

	return matching
}

// ResourceReference builds a "ResourceType/id" reference string from a FHIR resource.
func ResourceReference(resource map[string]any) string {
	rt, _ := resource["resourceType"].(string)
	id, _ := resource["id"].(string)
	if rt == "" || id == "" {
		return ""
	}
	return rt + "/" + id
}

// IsFlatteningErrorRetryable checks if a flattening error should be retried
func IsFlatteningErrorRetryable(err error) bool {
	var flattenerErr *services.FlattenerError
	if errors.As(err, &flattenerErr) {
		return flattenerErr.IsRetryable()
	}
	return lib.IsNetworkError(err)
}

// ClassifyFlatteningError classifies a flattening error as transient or non-transient
func ClassifyFlatteningError(err error) models.ErrorType {
	var flattenerErr *services.FlattenerError
	if errors.As(err, &flattenerErr) {
		return flattenerErr.ErrorType
	}
	if lib.IsNetworkError(err) {
		return models.ErrorTypeTransient
	}
	return models.ErrorTypeNonTransient
}
