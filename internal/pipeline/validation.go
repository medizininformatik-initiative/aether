package pipeline

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/services"
	"github.com/medizininformatik-initiative/aether/internal/ui"
)

// validationReportSuffix is appended to input filenames to form the report filename.
// e.g. Patient.ndjson -> Patient.validation.ndjson
const validationReportSuffix = ".validation.ndjson"

// ExecuteValidationStep validates FHIR resources against a FHIR validation service.
// Reads from the previous data-producing step's output directory.
// Writes per-file OperationOutcome reports for all validated chunks to jobs/<job-id>/validation/.
// Fails the step if any chunk has error-level or fatal-level issues.
func ExecuteValidationStep(job *models.PipelineJob, jobDir string, logger *lib.Logger) error {
	stepName := models.StepValidation

	if !isStepEnabled(job.Config, stepName) {
		logger.Info("Validation step not enabled, skipping", "job_id", job.JobID)
		return nil
	}

	logger.Debug("Validation step starting", "job_id", job.JobID)

	step := getOrCreateStep(job, stepName)
	step.Status = models.StepStatusInProgress
	now := time.Now()
	step.StartedAt = &now

	if job.Config.Services.Validation.URL == "" {
		err := fmt.Errorf("validation service URL not configured")
		lib.LogStepFailed(logger, string(stepName), job.JobID, err, false)
		recordStepError(step, err, models.ErrorTypeNonTransient)
		return err
	}

	httpClient := services.NewHTTPClient(30*time.Second, job.Config.Retry, job.Config.TLS, logger)
	validationClient := services.NewValidationClient(job.Config.Services.Validation.URL, httpClient, logger)

	// Resolve input directory (skipping transparent steps like wait)
	jobsBaseDir := filepath.Dir(jobDir)
	jobID := filepath.Base(jobDir)
	stepIndex := getStepIndexInEnabledSteps(job.Config.Pipeline.EnabledSteps, stepName)
	inputDir := services.GetStepInputDir(jobsBaseDir, jobID, job.Config.Pipeline.EnabledSteps, stepIndex)
	outputDir := filepath.Join(jobDir, "validation")

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		lib.LogStepFailed(logger, string(stepName), job.JobID, err, false)
		recordStepError(step, err, models.ErrorTypeNonTransient)
		return fmt.Errorf("failed to create output directory: %w", err)
	}

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

	fmt.Printf("Validating %d FHIR file(s)...\n\n", len(files))

	// Clean up stale partial files from previous runs
	partFiles, _ := filepath.Glob(filepath.Join(outputDir, "*.part"))
	for _, partFile := range partFiles {
		logger.Debug("Removing stale partial file from previous run", "file", filepath.Base(partFile))
		_ = os.Remove(partFile)
	}

	maxConcurrent := job.Config.Services.Validation.MaxConcurrentRequests
	if maxConcurrent <= 0 {
		maxConcurrent = 4
	}

	chunkSizeMB := job.Config.Services.Validation.BundleChunkSizeMB
	if chunkSizeMB <= 0 {
		chunkSizeMB = 10
	}
	thresholdBytes := chunkSizeMB * 1024 * 1024

	var totalChunksWithErrors int
	totalResourcesValidated := 0
	filesProcessed := 0

	for _, inputFile := range files {
		baseName := lib.GetUncompressedFilename(filepath.Base(inputFile))
		reportName := strings.TrimSuffix(baseName, ".ndjson") + validationReportSuffix
		reportFile := filepath.Join(outputDir, reportName)

		// Resumption: skip already-validated files
		if _, err := os.Stat(reportFile); err == nil {
			fmt.Printf("  ⊙ %s (already validated, skipping)\n", baseName)
			logger.Debug("Skipping already validated file",
				"filename", baseName,
				"report_file", reportFile,
				"job_id", job.JobID)
			filesProcessed++
			continue
		}

		resourceCount, chunksWithErrors, err := validateFile(inputFile, reportFile, validationClient, logger, maxConcurrent, thresholdBytes)
		if err != nil {
			logger.Error("Failed to validate FHIR file",
				"filename", baseName,
				"error", err,
				"job_id", job.JobID)
			lib.LogStepFailed(logger, string(stepName), job.JobID, err, isValidationErrorRetryable(err))
			recordStepError(step, err, classifyValidationError(err))
			return fmt.Errorf("failed to validate %s: %w", baseName, err)
		}

		if chunksWithErrors > 0 {
			fmt.Printf("  ⚠ %s (%d resources in %d chunk(s) with errors)\n", baseName, resourceCount, chunksWithErrors)
		} else {
			fmt.Printf("  ✓ %s (%d resources, all valid)\n", baseName, resourceCount)
		}

		totalResourcesValidated += resourceCount
		totalChunksWithErrors += chunksWithErrors
		filesProcessed++
	}

	step.Status = models.StepStatusCompleted
	step.FilesProcessed = len(files)
	completedAt := time.Now()
	step.CompletedAt = &completedAt

	duration := completedAt.Sub(*step.StartedAt)

	if totalChunksWithErrors > 0 {
		logger.Warn("Validation completed with errors in data",
			"chunks_with_errors", totalChunksWithErrors,
			"files_processed", len(files),
			"resources_validated", totalResourcesValidated,
			"duration", duration,
			"job_id", job.JobID,
		)
		fmt.Printf("\n⚠ Validation completed: found errors in %d chunk(s) across %d file(s). See reports in validation/ directory.\n", totalChunksWithErrors, len(files))

		if job.Config.Services.Validation.FailOnError != nil && *job.Config.Services.Validation.FailOnError {
			return fmt.Errorf("validation found errors in %d chunk(s) across %d file(s) — stopping pipeline (fail_on_error is enabled)", totalChunksWithErrors, len(files))
		}
	} else {
		logger.Debug("Validation step completed",
			"files_processed", len(files),
			"resources_validated", totalResourcesValidated,
			"duration", duration,
			"job_id", job.JobID,
		)
	}

	return nil
}

// chunkValidationResult holds the result of validating a single Bundle chunk
type chunkValidationResult struct {
	chunkIndex int
	outcome    map[string]any
	hasError   bool
	err        error
}

// validateFile validates all resources in a single NDJSON file by chunking them into
// FHIR Bundles and validating concurrently. All OperationOutcomes are written to the report.
// Returns the total resource count, the number of chunks with errors, and any processing error.
func validateFile(inputFile, reportFile string, client *services.ValidationClient, logger *lib.Logger, maxConcurrent int, thresholdBytes int) (int, int, error) {
	inFile, err := lib.OpenFileForReading(inputFile)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to open input file: %w", err)
	}
	defer func() { _ = inFile.Close() }()

	// Parse all resources from NDJSON
	dec := json.NewDecoder(inFile)
	var resources []map[string]any

	for {
		var resource map[string]any
		if err := dec.Decode(&resource); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return len(resources), 0, fmt.Errorf("failed to parse resource %d: %w", len(resources)+1, err)
		}
		resources = append(resources, resource)
	}

	if len(resources) == 0 {
		// Create empty report for resumption
		if err := os.WriteFile(reportFile, []byte{}, 0644); err != nil {
			return 0, 0, fmt.Errorf("failed to create empty report: %w", err)
		}
		return 0, 0, nil
	}

	// Chunk resources into Bundle-sized partitions
	chunks, err := chunkResources(resources, thresholdBytes, logger)
	if err != nil {
		return len(resources), 0, fmt.Errorf("failed to chunk resources: %w", err)
	}

	logger.Debug("Chunked resources for validation",
		"file", filepath.Base(inputFile),
		"resources", len(resources),
		"chunks", len(chunks))

	// Initialize progress bar
	progressBar := ui.NewProgressBar(int64(len(chunks)), fmt.Sprintf("Validating %s", filepath.Base(inputFile)))

	// Validate chunks concurrently
	results := make([]chunkValidationResult, len(chunks))
	sem := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup
	var firstErr atomic.Value

	for i, chunk := range chunks {
		if v := firstErr.Load(); v != nil {
			break
		}

		wg.Add(1)
		sem <- struct{}{}

		go func(idx int, entries []map[string]any) {
			defer wg.Done()
			defer func() { <-sem }()

			bundle := buildCollectionBundle(entries)
			result, err := client.ValidateResource(bundle)
			if err != nil {
				results[idx] = chunkValidationResult{chunkIndex: idx, err: err}
				firstErr.CompareAndSwap(nil, err)
				return
			}

			results[idx] = chunkValidationResult{
				chunkIndex: idx,
				outcome:    result.OperationOutcome,
				hasError:   result.HasErrors(),
			}

			if progressBar != nil {
				_ = progressBar.Add(1)
			}
		}(i, chunk)
	}

	wg.Wait()

	if progressBar != nil {
		_ = progressBar.Finish()
	}

	// Check for HTTP/network errors
	if v := firstErr.Load(); v != nil {
		return 0, 0, v.(error)
	}

	// Write all OperationOutcomes to the report file
	partFile := reportFile + ".part"
	outFile, err := os.Create(partFile)
	if err != nil {
		return len(resources), 0, fmt.Errorf("failed to create report file: %w", err)
	}
	finalized := false
	defer func() {
		if !finalized {
			_ = outFile.Close()
			_ = os.Remove(partFile)
		}
	}()
	outWriter := bufio.NewWriter(outFile)

	chunksWithErrors := 0
	for _, result := range results {
		if result.outcome == nil {
			continue
		}

		if result.hasError {
			chunksWithErrors++
		}

		outcomeJSON, err := json.Marshal(result.outcome)
		if err != nil {
			return len(resources), chunksWithErrors, fmt.Errorf("failed to marshal OperationOutcome for chunk %d: %w", result.chunkIndex, err)
		}

		if _, err := outWriter.Write(outcomeJSON); err != nil {
			return len(resources), chunksWithErrors, fmt.Errorf("failed to write report: %w", err)
		}
		if _, err := outWriter.WriteString("\n"); err != nil {
			return len(resources), chunksWithErrors, fmt.Errorf("failed to write newline: %w", err)
		}
	}

	if err := outWriter.Flush(); err != nil {
		return len(resources), chunksWithErrors, fmt.Errorf("failed to flush report: %w", err)
	}

	if err := outFile.Close(); err != nil {
		return len(resources), chunksWithErrors, fmt.Errorf("failed to close report: %w", err)
	}

	if err := os.Rename(partFile, reportFile); err != nil {
		return len(resources), chunksWithErrors, fmt.Errorf("failed to finalize report: %w", err)
	}

	finalized = true
	return len(resources), chunksWithErrors, nil
}

// buildCollectionBundle wraps pre-formatted Bundle entries into a FHIR collection Bundle.
// Entries must already be in {"resource": {...}} format.
func buildCollectionBundle(entries []map[string]any) map[string]any {
	return map[string]any{
		"resourceType": "Bundle",
		"type":         "collection",
		"entry":        entries,
	}
}

// validationBaseURL is a synthetic base used for fullUrl on Bundle entries.
// Using a consistent base ensures relative references (e.g., "Patient/VHF00964") resolve
// correctly within the Bundle. Without this, the validator falls back to its own default
// base URL (http://aether.local/fhir/) which causes reference resolution warnings.
const validationBaseURL = "http://aether.local/fhir/"

// validationEntryFullURL builds a synthetic fullUrl for a resource wrapped as a collection
// Bundle entry. Uses the same base URL as inner Bundle entries so cross-references resolve.
func validationEntryFullURL(resource map[string]any, index int) string {
	if ref := lib.ResourceReference(resource); ref != "" {
		return validationBaseURL + ref
	}

	return validationBaseURL + fmt.Sprintf("Resource/%d", index)
}

// injectInnerBundleFullURLs adds fullUrl to entries inside Bundle resources that lack them.
// Transaction Bundles from TORCH have entries with request.url but no fullUrl. The FHIR
// validator needs fullUrl to resolve inter-entry references; without it, it constructs
// URLs from its own base and warns about mismatches.
func injectInnerBundleFullURLs(resource map[string]any) {
	if !lib.IsBundle(resource) {
		return
	}

	for _, entry := range lib.BundleEntries(resource) {
		if _, hasFullURL := entry["fullUrl"]; hasFullURL {
			continue
		}

		// Derive fullUrl from request.url (transaction entries) or resource type+id
		if req, ok := entry["request"].(map[string]any); ok {
			if reqURL, ok := req["url"].(string); ok && reqURL != "" {
				entry["fullUrl"] = validationBaseURL + reqURL
				continue
			}
		}

		if res, ok := lib.EntryResource(entry); ok {
			if ref := lib.ResourceReference(res); ref != "" {
				entry["fullUrl"] = validationBaseURL + ref
			}
		}
	}
}

// chunkResources wraps each resource as a Bundle entry and partitions them by size threshold.
// If a single resource exceeds the threshold (OversizedResourceError), it gets its own chunk
// rather than failing — the validator decides validity, not our size check.
func chunkResources(resources []map[string]any, thresholdBytes int, logger *lib.Logger) ([][]map[string]any, error) {
	// Wrap each resource as a Bundle entry with fullUrl (required by FHIR R4 for collection Bundles)
	entries := make([]map[string]any, len(resources))
	for i, resource := range resources {
		injectInnerBundleFullURLs(resource)
		entries[i] = map[string]any{
			"fullUrl":  validationEntryFullURL(resource, i),
			"resource": resource,
		}
	}

	partitions, err := services.PartitionEntries(entries, thresholdBytes)
	if err != nil {
		// Handle oversized resources: isolate them in their own chunk
		var oversizedErr *models.OversizedResourceError
		if isOversized(err, &oversizedErr) {
			logger.Warn("Resource exceeds chunk threshold, validating individually",
				"resourceType", oversizedErr.ResourceType,
				"resourceID", oversizedErr.ResourceID,
				"size", oversizedErr.Size,
				"threshold", oversizedErr.Threshold)
			return chunkResourcesWithOversized(entries, thresholdBytes, logger)
		}
		return nil, err
	}

	return partitions, nil
}

// isOversized checks if the error is an OversizedResourceError and extracts it
func isOversized(err error, target **models.OversizedResourceError) bool {
	if oe, ok := err.(*models.OversizedResourceError); ok {
		*target = oe
		return true
	}
	return false
}

// chunkResourcesWithOversized handles the case where some resources exceed the threshold.
// Oversized resources are placed in their own single-entry chunks.
func chunkResourcesWithOversized(entries []map[string]any, thresholdBytes int, logger *lib.Logger) ([][]map[string]any, error) {
	var partitions [][]map[string]any
	var normalEntries []map[string]any

	for _, entry := range entries {
		entrySize, err := models.CalculateJSONSize(entry)
		if err != nil {
			return nil, fmt.Errorf("failed to calculate entry size: %w", err)
		}

		// ~200 bytes Bundle overhead, same constant as PartitionEntries
		if entrySize+200 > thresholdBytes {
			// Flush accumulated normal entries first
			if len(normalEntries) > 0 {
				subPartitions, err := services.PartitionEntries(normalEntries, thresholdBytes)
				if err != nil {
					return nil, fmt.Errorf("failed to partition normal entries: %w", err)
				}
				partitions = append(partitions, subPartitions...)
				normalEntries = nil
			}
			// Isolate oversized resource in its own chunk
			partitions = append(partitions, []map[string]any{entry})

			resourceType := "Unknown"
			if resource, ok := lib.EntryResource(entry); ok {
				if rt := lib.ResourceType(resource); rt != "" {
					resourceType = rt
				}
			}
			logger.Warn("Oversized resource placed in individual chunk",
				"resourceType", resourceType,
				"size", entrySize)
		} else {
			normalEntries = append(normalEntries, entry)
		}
	}

	// Flush remaining normal entries
	if len(normalEntries) > 0 {
		subPartitions, err := services.PartitionEntries(normalEntries, thresholdBytes)
		if err != nil {
			return nil, fmt.Errorf("failed to partition remaining entries: %w", err)
		}
		partitions = append(partitions, subPartitions...)
	}

	return partitions, nil
}

// isValidationErrorRetryable checks if a validation error should be retried
func isValidationErrorRetryable(err error) bool {
	if valErr, ok := err.(*services.ValidationError); ok {
		return valErr.IsRetryable()
	}
	return lib.IsNetworkError(err)
}

// classifyValidationError classifies a validation error as transient or non-transient
func classifyValidationError(err error) models.ErrorType {
	if valErr, ok := err.(*services.ValidationError); ok {
		return valErr.ErrorType
	}
	if lib.IsNetworkError(err) {
		return models.ErrorTypeTransient
	}
	return models.ErrorTypeNonTransient
}
