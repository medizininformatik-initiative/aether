package pipeline

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

// defaultS3UploaderFactory is the production S3 uploader constructor, shared
// by package-init and ResetS3UploaderFactory so both stay in sync.
var defaultS3UploaderFactory = func(s3Config models.S3Config, auth models.AuthConfig, tlsConfig models.TLSConfig, logger *lib.Logger) (services.S3Uploader, error) {
	return services.NewAWSS3Uploader(s3Config, auth, tlsConfig, logger)
}

// s3UploaderFactory creates an S3Uploader. Overridable in tests.
var s3UploaderFactory = defaultS3UploaderFactory

// SetS3UploaderFactoryForTesting replaces the S3 uploader factory for tests.
func SetS3UploaderFactoryForTesting(factory func(models.S3Config, models.AuthConfig, models.TLSConfig, *lib.Logger) (services.S3Uploader, error)) {
	s3UploaderFactory = factory
}

// ResetS3UploaderFactory restores the default S3 uploader factory.
func ResetS3UploaderFactory() {
	s3UploaderFactory = defaultS3UploaderFactory
}

// ClearOAuth2TokenCacheForTesting clears the OAuth2 token cache - exported for testing
func ClearOAuth2TokenCacheForTesting() {
	services.ClearOAuth2TokenCache()
}

// putWithAuth performs an authenticated HTTP PUT request based on the SendConfig authentication settings.
func putWithAuth(targetURL, contentType string, body []byte, config models.SendConfig, httpClient *services.HTTPClient) (*http.Response, error) {
	req, err := http.NewRequest("PUT", targetURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", contentType)

	if err := httpClient.ApplyAuth(req, config.Auth); err != nil {
		return nil, fmt.Errorf("failed to add auth header: %w", err)
	}

	return httpClient.Do(req)
}

// ExecuteSendStep sends pipeline output to the configured destination.
// Supports two modes:
// - direct_resource_load: Sends NDJSON files directly to a FHIR server using transaction bundles
// - transfer_load: Prepares and sends to a transfer FHIR server using Binary/DocumentReference
func ExecuteSendStep(job *models.PipelineJob, jobDir string, logger *lib.Logger) error {
	stepName := models.StepSend

	if !isStepEnabled(job.Config, stepName) {
		logger.Info("Send step not enabled, skipping", "job_id", job.JobID)
		return nil
	}

	logger.Debug("Send step starting", "job_id", job.JobID)

	step := getOrCreateStep(job, stepName)
	step.Status = models.StepStatusInProgress
	now := time.Now()
	step.StartedAt = &now

	if err := job.Config.Services.Send.Validate(); err != nil {
		lib.LogStepFailed(logger, string(stepName), job.JobID, err, false)
		recordStepError(step, err, models.ErrorTypeNonTransient)
		return err
	}

	// Route to appropriate send implementation based on send mode
	sendMode := job.Config.Services.Send.SendAs

	var err error
	switch sendMode {
	case models.SendModeDirectResourceLoad:
		err = executeDirectResourceLoadSend(job, jobDir, step, logger)
	case models.SendModeTransferLoad:
		err = executeTransferLoadSend(job, jobDir, step, logger)
	case models.SendModeS3Upload:
		err = executeS3UploadSend(job, jobDir, step, logger)
	default:
		err = fmt.Errorf("unknown send mode: %s", sendMode)
		lib.LogStepFailed(logger, string(stepName), job.JobID, err, false)
		recordStepError(step, err, models.ErrorTypeNonTransient)
	}

	return err
}

// executeTransferLoadSend sends files to a transfer FHIR server.
// For each file: zips it, base64-encodes it, and wraps it in a FHIR Binary resource.
// Then creates a DocumentReference linking all Binaries and uploads them.
func executeTransferLoadSend(job *models.PipelineJob, jobDir string, step *models.PipelineStep, logger *lib.Logger) error {
	stepName := models.StepSend

	inputDir := services.NewJobLayout(filepath.Dir(jobDir), filepath.Base(jobDir), job.Config.Pipeline.EnabledSteps).InputDir(stepName)

	files, err := findAllFilesRecursive(inputDir)
	if err != nil {
		lib.LogStepFailed(logger, string(stepName), job.JobID, err, false)
		recordStepError(step, err, models.ErrorTypeNonTransient)
		return fmt.Errorf("failed to list input files: %w", err)
	}

	if len(files) == 0 {
		err := fmt.Errorf("no files found in %s", inputDir)
		lib.LogStepFailed(logger, string(stepName), job.JobID, err, false)
		recordStepError(step, err, models.ErrorTypeNonTransient)
		return err
	}

	fmt.Printf("Preparing %d file(s) for transfer...\n\n", len(files))

	var binaryResources []binaryEntry
	for _, filePath := range files {
		fileName := filepath.Base(filePath)

		entry, err := createBinaryFromFile(filePath)
		if err != nil {
			lib.LogStepFailed(logger, string(stepName), job.JobID, err, false)
			recordStepError(step, err, models.ErrorTypeNonTransient)
			return fmt.Errorf("failed to process %s: %w", fileName, err)
		}

		binaryResources = append(binaryResources, entry)
		fmt.Printf("  ✓ %s (%s)\n", fileName, entry.contentType)

		logger.Debug("Created Binary resource",
			"file", fileName,
			"content_type", entry.contentType,
			"job_id", job.JobID)
	}

	sendConfig := job.Config.Services.Send
	docRef := buildDocumentReference(binaryResources, sendConfig.Transfer)

	logger.Debug("Uploading resources individually",
		"binary_count", len(binaryResources),
		"job_id", job.JobID)

	httpClient := services.NewHTTPClient(30*time.Second, job.Config.Retry, job.Config.TLS, logger)

	// Upload each Binary resource
	for i, binary := range binaryResources {
		fmt.Printf("  Uploading Binary %d/%d...\n", i+1, len(binaryResources))
		if err := uploadBinary(binary, sendConfig, httpClient, logger); err != nil {
			retryable := isSendErrorRetryable(err)
			lib.LogStepFailed(logger, string(stepName), job.JobID, err, retryable)
			recordStepError(step, err, classifySendError(err))
			return fmt.Errorf("failed to upload Binary %s: %w", binary.id, err)
		}
	}

	// Upload DocumentReference
	fmt.Println("  Uploading DocumentReference...")
	if err := uploadDocumentReference(docRef, sendConfig, httpClient, logger); err != nil {
		retryable := isSendErrorRetryable(err)
		lib.LogStepFailed(logger, string(stepName), job.JobID, err, retryable)
		recordStepError(step, err, classifySendError(err))
		return fmt.Errorf("failed to upload DocumentReference: %w", err)
	}

	fmt.Printf("\n✓ Transfer complete (%d files sent)\n", len(files))

	step.Status = models.StepStatusCompleted
	step.FilesProcessed = len(files)
	completedAt := time.Now()
	step.CompletedAt = &completedAt

	logger.Debug("Transfer load send step completed",
		"files_sent", len(files),
		"duration", completedAt.Sub(*step.StartedAt),
		"job_id", job.JobID)

	return nil
}

// executeDirectResourceLoadSend uploads NDJSON files to a FHIR server using transaction bundles.
// Core files (core.ndjson) are loaded first before other NDJSON files to ensure
// base resources are created before dependent resources.
func executeDirectResourceLoadSend(job *models.PipelineJob, jobDir string, step *models.PipelineStep, logger *lib.Logger) error {
	stepName := models.StepSend

	inputDir := services.NewJobLayout(filepath.Dir(jobDir), filepath.Base(jobDir), job.Config.Pipeline.EnabledSteps).InputDir(stepName)

	// Find NDJSON files
	files, err := findNDJSONFiles(inputDir)
	if err != nil {
		lib.LogStepFailed(logger, string(stepName), job.JobID, err, false)
		recordStepError(step, err, models.ErrorTypeNonTransient)
		return fmt.Errorf("failed to list NDJSON files in %s: %w", inputDir, err)
	}

	if len(files) == 0 {
		err := fmt.Errorf("no NDJSON files found in %s", inputDir)
		lib.LogStepFailed(logger, string(stepName), job.JobID, err, false)
		recordStepError(step, err, models.ErrorTypeNonTransient)
		return err
	}

	// Partition files: core files first, then others
	coreFiles, otherFiles := partitionCoreFiles(files)
	orderedFiles := append(coreFiles, otherFiles...)

	logger.Debug("FHIR send file ordering",
		"core_files", len(coreFiles),
		"other_files", len(otherFiles),
		"total_files", len(orderedFiles))

	// Create HTTP client
	httpClient := services.NewHTTPClient(30*time.Second, job.Config.Retry, job.Config.TLS, logger)

	// Create FHIR client
	fhirClient := services.NewFHIRClient(job.Config.Services.Send, httpClient, logger)

	fhirURL := job.Config.Services.Send.URL
	fmt.Printf("Uploading %d NDJSON file(s) to FHIR server: %s\n\n", len(orderedFiles), fhirURL)

	if len(coreFiles) > 0 {
		fmt.Printf("Processing core files first (%d file(s)):\n", len(coreFiles))
	}

	filesProcessed := 0
	totalResources := 0

	for i, filePath := range orderedFiles {
		// Print separator between core and other files
		if i == len(coreFiles) && len(coreFiles) > 0 && len(otherFiles) > 0 {
			fmt.Printf("\nProcessing other files (%d file(s)):\n", len(otherFiles))
		}

		// Open file (handles both compressed and uncompressed)
		reader, err := lib.OpenFileForReading(filePath)
		if err != nil {
			lib.LogStepFailed(logger, string(stepName), job.JobID, err, false)
			recordStepError(step, err, models.ErrorTypeNonTransient)
			return fmt.Errorf("failed to open %s: %w", filepath.Base(filePath), err)
		}

		// Upload file
		stats, err := fhirClient.UploadNDJSON(filePath, reader)
		if closeErr := reader.Close(); closeErr != nil {
			logger.Warn("Failed to close file reader", "error", closeErr)
		}

		if err != nil {
			lib.LogStepFailed(logger, string(stepName), job.JobID, err, isFHIRErrorRetryable(err))
			recordStepError(step, err, classifyFHIRError(err))
			return fmt.Errorf("failed to upload %s: %w", filepath.Base(filePath), err)
		}

		filesProcessed++
		totalResources += stats.ResourcesUploaded

		fmt.Printf("  ✓ %s (%d resources, %d batches)\n",
			filepath.Base(filePath), stats.ResourcesUploaded, stats.BatchesSent)
	}

	step.Status = models.StepStatusCompleted
	step.FilesProcessed = filesProcessed
	completedAt := time.Now()
	step.CompletedAt = &completedAt

	duration := completedAt.Sub(*step.StartedAt)

	logger.Debug("Direct resource load send step completed",
		"files_processed", filesProcessed,
		"resources_uploaded", totalResources,
		"duration", duration,
		"fhir_url", fhirURL,
		"job_id", job.JobID)

	fmt.Printf("\n✓ Uploaded %d resources from %d files to FHIR server\n", totalResources, filesProcessed)

	return nil
}

// findNDJSONFiles finds all NDJSON files in a directory (both .ndjson and .ndjson.zst)
func findNDJSONFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var files []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if strings.HasSuffix(name, ".ndjson") || strings.HasSuffix(name, ".ndjson.zst") {
			files = append(files, filepath.Join(dir, name))
		}
	}

	return files, nil
}

// partitionCoreFiles separates core.ndjson files from other NDJSON files
// Core files must be loaded first to establish base resources
func partitionCoreFiles(files []string) (coreFiles, otherFiles []string) {
	for _, file := range files {
		baseName := filepath.Base(file)
		// Match core.ndjson or core.ndjson.zst
		if baseName == "core.ndjson" || baseName == "core.ndjson.zst" {
			coreFiles = append(coreFiles, file)
		} else {
			otherFiles = append(otherFiles, file)
		}
	}
	return coreFiles, otherFiles
}

// isFHIRErrorRetryable checks if a FHIR error should be retried
func isFHIRErrorRetryable(err error) bool {
	if fhirErr, ok := err.(*services.FHIRError); ok {
		return fhirErr.IsRetryable()
	}
	return lib.IsNetworkError(err)
}

// classifyFHIRError classifies a FHIR error as transient or non-transient
func classifyFHIRError(err error) models.ErrorType {
	if fhirErr, ok := err.(*services.FHIRError); ok {
		return fhirErr.ErrorType
	}
	if lib.IsNetworkError(err) {
		return models.ErrorTypeTransient
	}
	return models.ErrorTypeNonTransient
}

// binaryEntry holds a FHIR Binary resource and its metadata
type binaryEntry struct {
	id          string
	contentType string
	resource    map[string]any
}

// createBinaryFromFile reads a file, zips it, base64-encodes the content,
// and returns a FHIR Binary resource.
func createBinaryFromFile(filePath string) (binaryEntry, error) {
	contentType := detectContentType(filePath)

	// All files are zipped for transfer
	data, err := zipSingleFile(filePath)
	if err != nil {
		return binaryEntry{}, fmt.Errorf("failed to zip %s: %w", filepath.Base(filePath), err)
	}

	id := uuid.New().String()
	b64 := base64.StdEncoding.EncodeToString(data)

	resource := map[string]any{
		"resourceType": "Binary",
		"id":           id,
		"contentType":  contentType,
		"data":         b64,
	}

	return binaryEntry{
		id:          id,
		contentType: contentType,
		resource:    resource,
	}, nil
}

// detectContentType determines the FHIR contentType for a file based on its extension.
// All files are zipped for transfer - the pipeline doesn't produce standalone .json files.
func detectContentType(filePath string) string {
	name := strings.ToLower(filepath.Base(filePath))

	switch {
	case strings.HasSuffix(name, ".ndjson.zst") || strings.HasSuffix(name, ".ndjson"):
		return "application/zip"
	case strings.HasSuffix(name, ".csv"):
		return "text/zip"
	case strings.HasSuffix(name, ".parquet"):
		return "text/zip"
	default:
		return "application/zip"
	}
}

// zipSingleFile compresses a single file into an in-memory zip archive.
// Uses lib.OpenFileForReading to transparently decompress .zst files before zipping,
// so the zip contains the raw (uncompressed) data.
func zipSingleFile(filePath string) ([]byte, error) {
	reader, err := lib.OpenFileForReading(filePath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = reader.Close() }()

	// Use the uncompressed filename inside the zip (strip .zst suffix)
	entryName := lib.GetUncompressedFilename(filepath.Base(filePath))

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	w, err := zw.Create(entryName)
	if err != nil {
		return nil, err
	}

	if _, err := io.Copy(w, reader); err != nil {
		return nil, err
	}

	if err := zw.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// buildDocumentReference creates a FHIR DocumentReference linking all Binary resources.
func buildDocumentReference(binaries []binaryEntry, config models.TransferConfig) map[string]any {
	content := make([]map[string]any, len(binaries))
	for i, b := range binaries {
		content[i] = map[string]any{
			"attachment": map[string]any{
				"contentType": b.contentType,
				"url":         fmt.Sprintf("Binary/%s", b.id),
			},
		}
	}

	return map[string]any{
		"resourceType": "DocumentReference",
		"id":           uuid.New().String(),
		"masterIdentifier": map[string]any{
			"system": "http://medizininformatik-initiative.de/sid/project-identifier",
			"value":  config.ProjectIdentifier,
		},
		"status":    "current",
		"docStatus": "final",
		"author": []map[string]any{
			{
				"type": "Organization",
				"identifier": map[string]any{
					"system": "http://dsf.dev/sid/organization-identifier",
					"value":  config.OrganizationIdentifier,
				},
			},
		},
		"date":    time.Now().Format(time.RFC3339),
		"content": content,
	}
}

// uploadBinary PUTs a single Binary resource to the FHIR server.
func uploadBinary(binary binaryEntry, config models.SendConfig, httpClient *services.HTTPClient, logger *lib.Logger) error {
	targetURL := fmt.Sprintf("%s/fhir/Binary/%s", strings.TrimSuffix(config.URL, "/"), binary.id)
	jsonData, err := json.Marshal(binary.resource)
	if err != nil {
		return fmt.Errorf("failed to marshal Binary: %w", err)
	}

	logger.Debug("Uploading Binary resource",
		"url", targetURL,
		"id", binary.id,
		"size_bytes", len(jsonData))

	resp, err := putWithAuth(targetURL, "application/fhir+json", jsonData, config, httpClient)
	if err != nil {
		return fmt.Errorf("HTTP request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return &SendError{
			StatusCode: resp.StatusCode,
			Message:    string(body),
			ErrorType:  lib.ClassifyHTTPError(resp.StatusCode),
		}
	}

	logger.Debug("Binary resource uploaded successfully", "id", binary.id, "status", resp.StatusCode)
	return nil
}

// uploadDocumentReference PUTs the DocumentReference to the FHIR server.
func uploadDocumentReference(docRef map[string]any, config models.SendConfig, httpClient *services.HTTPClient, logger *lib.Logger) error {
	id, _ := docRef["id"].(string)
	targetURL := fmt.Sprintf("%s/fhir/DocumentReference/%s", strings.TrimSuffix(config.URL, "/"), id)
	jsonData, err := json.Marshal(docRef)
	if err != nil {
		return fmt.Errorf("failed to marshal DocumentReference: %w", err)
	}

	logger.Debug("Uploading DocumentReference",
		"url", targetURL,
		"id", id,
		"size_bytes", len(jsonData))

	resp, err := putWithAuth(targetURL, "application/fhir+json", jsonData, config, httpClient)
	if err != nil {
		return fmt.Errorf("HTTP request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return &SendError{
			StatusCode: resp.StatusCode,
			Message:    string(body),
			ErrorType:  lib.ClassifyHTTPError(resp.StatusCode),
		}
	}

	logger.Debug("DocumentReference uploaded successfully", "id", id, "status", resp.StatusCode)
	return nil
}

// findAllFilesRecursive walks a directory tree and returns all file paths.
func findAllFilesRecursive(dir string) ([]string, error) {
	var files []string

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			files = append(files, path)
		}
		return nil
	})

	return files, err
}

// executeS3UploadSend uploads files from the input directory to an S3 bucket.
// Uses an upload manifest for retry resilience — previously uploaded files are skipped.
func executeS3UploadSend(job *models.PipelineJob, jobDir string, step *models.PipelineStep, logger *lib.Logger) error {
	stepName := models.StepSend

	inputDir := services.NewJobLayout(filepath.Dir(jobDir), filepath.Base(jobDir), job.Config.Pipeline.EnabledSteps).InputDir(stepName)

	files, err := findAllFilesRecursive(inputDir)
	if err != nil {
		lib.LogStepFailed(logger, string(stepName), job.JobID, err, false)
		recordStepError(step, err, models.ErrorTypeNonTransient)
		return fmt.Errorf("failed to list input files: %w", err)
	}

	if len(files) == 0 {
		err := fmt.Errorf("no files found in %s", inputDir)
		lib.LogStepFailed(logger, string(stepName), job.JobID, err, false)
		recordStepError(step, err, models.ErrorTypeNonTransient)
		return err
	}

	s3Config := job.Config.Services.Send.S3

	// Create uploader via factory (allows test injection)
	uploader, err := s3UploaderFactory(s3Config, job.Config.Services.Send.Auth, job.Config.TLS, logger)
	if err != nil {
		lib.LogStepFailed(logger, string(stepName), job.JobID, err, false)
		recordStepError(step, err, models.ErrorTypeNonTransient)
		return fmt.Errorf("failed to create S3 uploader: %w", err)
	}

	// Load or create upload manifest for retry resilience
	manifestPath := models.GetUploadManifestPath(jobDir)
	manifest, err := models.LoadUploadManifest(manifestPath)
	if err != nil {
		logger.Warn("Failed to load upload manifest, starting fresh", "error", err)
	}
	if manifest == nil {
		manifest = models.NewUploadManifest(job.JobID, uploader.GetBucket())
	}

	// Context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), s3Config.Timeout)
	defer cancel()

	fmt.Printf("Uploading %d file(s) to S3 bucket: %s\n\n", len(files), uploader.GetBucket())

	filesUploaded := 0
	var totalBytes int64

	for _, filePath := range files {
		// Skip already-uploaded files (resume support)
		if manifest.IsFileUploaded(filePath) {
			logger.Debug("Skipping already-uploaded file", "path", filePath)
			filesUploaded++
			continue
		}

		// Build S3 key: {job-id}/{filename}
		relPath, err := filepath.Rel(inputDir, filePath)
		if err != nil {
			relPath = filepath.Base(filePath)
		}
		s3Key := job.JobID + "/" + relPath

		// Get file size for logging
		info, err := os.Stat(filePath)
		if err != nil {
			lib.LogStepFailed(logger, string(stepName), job.JobID, err, false)
			recordStepError(step, err, models.ErrorTypeNonTransient)
			_ = models.SaveManifest(manifest, manifestPath)
			return fmt.Errorf("failed to stat file %s: %w", filepath.Base(filePath), err)
		}
		fileSize := info.Size()

		etag, err := uploader.UploadFile(ctx, filePath, s3Key)
		if err != nil {
			retryable := isSendErrorRetryable(err)
			lib.LogStepFailed(logger, string(stepName), job.JobID, err, retryable)
			recordStepError(step, err, classifySendError(err))
			// Save manifest before returning so progress is not lost
			_ = models.SaveManifest(manifest, manifestPath)
			return fmt.Errorf("failed to upload %s to S3: %w", filepath.Base(filePath), err)
		}

		manifest.AddUploadedFile(filePath, s3Key, etag, fileSize)
		filesUploaded++
		totalBytes += fileSize

		// Save manifest after each successful upload
		if saveErr := models.SaveManifest(manifest, manifestPath); saveErr != nil {
			logger.Warn("Failed to save upload manifest", "error", saveErr)
		}

		fmt.Printf("  ✓ %s → s3://%s/%s (%s)\n",
			filepath.Base(filePath), uploader.GetBucket(), s3Key, formatSize(fileSize))
	}

	manifest.MarkCompleted()
	_ = models.SaveManifest(manifest, manifestPath)

	step.Status = models.StepStatusCompleted
	step.FilesProcessed = filesUploaded
	completedAt := time.Now()
	step.CompletedAt = &completedAt

	duration := completedAt.Sub(*step.StartedAt)
	fmt.Printf("\n✓ Uploaded %d files (%s) to s3://%s in %s\n",
		filesUploaded, formatSize(totalBytes), uploader.GetBucket(), duration.Truncate(time.Second))

	logger.Debug("S3 upload send step completed",
		"files_uploaded", filesUploaded,
		"total_bytes", totalBytes,
		"bucket", uploader.GetBucket(),
		"duration", duration,
		"job_id", job.JobID)

	return nil
}

// FormatSizeForTesting exposes formatSize for unit tests.
func FormatSizeForTesting(bytes int64) string {
	return formatSize(bytes)
}

// formatSize formats a byte count as a human-readable size string.
func formatSize(bytes int64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
	)

	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

// SendError represents a failure from the FHIR transfer server.
type SendError struct {
	StatusCode int
	Message    string
	ErrorType  models.ErrorType
}

func (e *SendError) Error() string {
	return fmt.Sprintf("send failed: HTTP %d: %s", e.StatusCode, e.Message)
}

func isSendErrorRetryable(err error) bool {
	var sendErr *SendError
	if errors.As(err, &sendErr) {
		return sendErr.ErrorType == models.ErrorTypeTransient
	}
	var s3Err *services.S3Error
	if errors.As(err, &s3Err) {
		return s3Err.IsRetryable()
	}
	return lib.IsNetworkError(err)
}

func classifySendError(err error) models.ErrorType {
	var sendErr *SendError
	if errors.As(err, &sendErr) {
		return sendErr.ErrorType
	}
	var s3Err *services.S3Error
	if errors.As(err, &s3Err) {
		return s3Err.ErrorType
	}
	if lib.IsNetworkError(err) {
		return models.ErrorTypeTransient
	}
	return models.ErrorTypeNonTransient
}
