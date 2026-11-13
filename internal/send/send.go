// Package send uploads files to the configured destination. It does not know
// about pipeline jobs, so callers with and without a job can use it.
package send

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

// File is one file to send.
type File struct {
	// Path is the location of the file on disk.
	Path string
	// Name is the path relative to the input root. s3_upload uses it as the
	// suffix of the S3 key.
	Name string
	// EntryName is the name of the file in its transfer_load zip archive. When
	// EntryName is empty, the name is the base name of Path.
	EntryName string
}

// State holds the paths where a send writes its own files.
type State struct {
	// ManifestPath is the S3 upload manifest file.
	ManifestPath string
	// FailedRequestsDir receives the dumps of rejected requests when
	// Config.DumpFailedRequests is set.
	FailedRequestsDir string
}

// Request describes one send.
type Request struct {
	// JobID is the S3 key prefix and the manifest ID. It must not be empty.
	JobID  string
	Config models.SendConfig
	Retry  models.RetryConfig
	TLS    models.TLSConfig
	Files  []File
	State  State
}

func (r Request) validate() error {
	if len(r.Files) == 0 {
		return errors.New("no files to send")
	}
	if r.JobID == "" {
		return errors.New("job ID is empty")
	}
	if r.Config.SendAs == models.SendModeS3Upload && r.State.ManifestPath == "" {
		return errors.New("manifest path is empty")
	}
	if r.Config.DumpFailedRequests && r.State.FailedRequestsDir == "" {
		return errors.New("failed requests directory is empty")
	}
	return nil
}

// Result reports what a send did.
type Result struct {
	FilesProcessed int
}

// Send sends the files of the request, dispatching on Config.SendAs:
//   - direct_resource_load: NDJSON files to a FHIR server via transaction bundles
//   - transfer_load: to a transfer FHIR server via Binary/DocumentReference
//   - s3_upload: to an S3 bucket
//
// The context limits only the S3 upload.
func Send(ctx context.Context, req Request, logger *lib.Logger) (Result, error) {
	if err := req.Config.Validate(); err != nil {
		return Result{}, err
	}
	if err := req.validate(); err != nil {
		return Result{}, err
	}

	switch req.Config.SendAs {
	case models.SendModeDirectResourceLoad:
		return sendDirectResourceLoad(req, logger)
	case models.SendModeTransferLoad:
		return sendTransferLoad(req, logger)
	case models.SendModeS3Upload:
		return sendS3Upload(ctx, req, logger)
	default:
		return Result{}, fmt.Errorf("unknown send mode: %s", req.Config.SendAs)
	}
}

// defaultS3UploaderFactory is the factory that production code uses.
// ResetS3UploaderFactory restores it.
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

// ClearOAuth2TokenCacheForTesting clears the OAuth2 token cache. Tests use it
// to start without cached tokens.
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

// sendTransferLoad sends files to a transfer FHIR server.
// For each file: zips it, base64-encodes it, and wraps it in a FHIR Binary resource.
// Then creates a DocumentReference linking all Binaries and uploads them.
func sendTransferLoad(req Request, logger *lib.Logger) (Result, error) {
	files := req.Files

	fmt.Printf("Preparing %d file(s) for transfer...\n\n", len(files))

	binaryIDs := make([]string, len(files))
	for i := range files {
		binaryIDs[i] = uuid.New().String()
	}

	sendConfig := req.Config
	docRef := buildDocumentReference(binaryIDs, sendConfig.Transfer)

	logger.Debug("Uploading resources individually",
		"binary_count", len(binaryIDs),
		"job_id", req.JobID)

	httpClient := newSendHTTPClient(req.Retry, req.TLS, logger)
	dump := newRequestDump(req, logger)

	for i, file := range files {
		fileName := displayName(file)

		binary, err := createBinaryFromFile(file, binaryIDs[i])
		if err != nil {
			return Result{}, fmt.Errorf("failed to process %s: %w", fileName, err)
		}
		fmt.Printf("  ✓ %s (%s)\n", fileName, binary.contentType)

		logger.Debug("Created Binary resource",
			"file", fileName,
			"content_type", binary.contentType,
			"job_id", req.JobID)

		fmt.Printf("  Uploading Binary %d/%d...\n", i+1, len(files))
		if err := uploadBinary(binary, sendConfig, httpClient, logger, dump); err != nil {
			return Result{}, fmt.Errorf("failed to upload Binary %s: %w", binary.id, err)
		}
	}

	fmt.Println("  Uploading DocumentReference...")
	if err := uploadDocumentReference(docRef, sendConfig, httpClient, logger, dump); err != nil {
		return Result{}, fmt.Errorf("failed to upload DocumentReference: %w", err)
	}

	fmt.Printf("\n✓ Transfer complete (%d files sent)\n", len(files))

	logger.Debug("Transfer load send step completed",
		"files_sent", len(files),
		"job_id", req.JobID)

	return Result{FilesProcessed: len(files)}, nil
}

// newSendHTTPClient returns the client for the uploads of the transfer and the
// FHIR send modes.
func newSendHTTPClient(retry models.RetryConfig, tls models.TLSConfig, logger *lib.Logger) *services.HTTPClient {
	return services.NewHTTPClient(30*time.Second, retry, tls, logger)
}

// sendDirectResourceLoad uploads NDJSON files to a FHIR server using transaction bundles.
// Core files (core.ndjson) are loaded first before other NDJSON files to ensure
// base resources are created before dependent resources.
func sendDirectResourceLoad(req Request, logger *lib.Logger) (Result, error) {
	paths := make([]string, len(req.Files))
	for i, file := range req.Files {
		paths[i] = file.Path
	}

	coreFiles, otherFiles := partitionCoreFiles(paths)
	orderedFiles := append(coreFiles, otherFiles...)

	logger.Debug("FHIR send file ordering",
		"core_files", len(coreFiles),
		"other_files", len(otherFiles),
		"total_files", len(orderedFiles))

	fhirClient := services.NewFHIRClient(req.Config, newSendHTTPClient(req.Retry, req.TLS, logger), logger)
	fhirClient.DumpFailedRequestsTo(newRequestDump(req, logger))

	fhirURL := req.Config.URL
	fmt.Printf("Uploading %d NDJSON file(s) to FHIR server: %s\n\n", len(orderedFiles), fhirURL)

	if len(coreFiles) > 0 {
		fmt.Printf("Processing core files first (%d file(s)):\n", len(coreFiles))
	}

	filesProcessed, totalResources, err := uploadNDJSONFiles(fhirClient, orderedFiles, len(coreFiles), logger)
	if err != nil {
		return Result{}, err
	}

	logger.Debug("Direct resource load send step completed",
		"files_processed", filesProcessed,
		"resources_uploaded", totalResources,
		"fhir_url", fhirURL,
		"job_id", req.JobID)

	fmt.Printf("\n✓ Uploaded %d resources from %d files to FHIR server\n", totalResources, filesProcessed)

	return Result{FilesProcessed: filesProcessed}, nil
}

// uploadNDJSONFiles uploads each file in order and returns the number of files
// and of resources that went to the server.
func uploadNDJSONFiles(fhirClient *services.FHIRClient, orderedFiles []string, coreCount int, logger *lib.Logger) (filesProcessed, totalResources int, err error) {
	for i, filePath := range orderedFiles {
		if i == coreCount && coreCount > 0 {
			fmt.Printf("\nProcessing other files (%d file(s)):\n", len(orderedFiles)-coreCount)
		}

		stats, err := uploadNDJSONFile(fhirClient, filePath, logger)
		if err != nil {
			return filesProcessed, totalResources, err
		}

		filesProcessed++
		totalResources += stats.ResourcesUploaded

		fmt.Printf("  ✓ %s (%d resources, %d batches)\n",
			filepath.Base(filePath), stats.ResourcesUploaded, stats.BatchesSent)
	}

	return filesProcessed, totalResources, nil
}

// uploadNDJSONFile opens one file, compressed or not, and uploads it.
func uploadNDJSONFile(fhirClient *services.FHIRClient, filePath string, logger *lib.Logger) (*services.FHIRUploadStats, error) {
	reader, err := lib.OpenFileForReading(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open %s: %w", filepath.Base(filePath), err)
	}

	stats, err := fhirClient.UploadNDJSON(filePath, reader)
	if closeErr := reader.Close(); closeErr != nil {
		logger.Warn("Failed to close file reader", "error", closeErr)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to upload %s: %w", filepath.Base(filePath), err)
	}

	return stats, nil
}

// partitionCoreFiles separates core.ndjson files from other NDJSON files
// Core files must be loaded first to establish base resources
func partitionCoreFiles(files []string) (coreFiles, otherFiles []string) {
	for _, file := range files {
		baseName := filepath.Base(file)
		if baseName == "core.ndjson" || baseName == "core.ndjson.zst" {
			coreFiles = append(coreFiles, file)
		} else {
			otherFiles = append(otherFiles, file)
		}
	}
	return coreFiles, otherFiles
}

// binaryContentType is the content type of every Binary. Each file is zipped
// for transfer, and application/zip is the IANA-registered type for zip archives.
const binaryContentType = "application/zip"

// binaryEntry holds a FHIR Binary resource and its metadata
type binaryEntry struct {
	id          string
	contentType string
	resource    map[string]any
	sourcePath  string
}

// newRequestDump returns a dump for the send, or nil when the
// configuration leaves it off.
func newRequestDump(req Request, logger *lib.Logger) *services.RequestDump {
	if !req.Config.DumpFailedRequests {
		return nil
	}
	return services.NewRequestDump(req.State.FailedRequestsDir, logger)
}

// createBinaryFromFile reads a file, zips it, base64-encodes the content,
// and returns a FHIR Binary resource.
func createBinaryFromFile(file File, id string) (binaryEntry, error) {
	filePath := file.Path

	data, err := zipSingleFile(file)
	if err != nil {
		return binaryEntry{}, fmt.Errorf("failed to zip %s: %w", filepath.Base(filePath), err)
	}

	b64 := base64.StdEncoding.EncodeToString(data)

	resource := map[string]any{
		"resourceType": "Binary",
		"id":           id,
		"contentType":  binaryContentType,
		"data":         b64,
	}

	return binaryEntry{
		id:          id,
		contentType: binaryContentType,
		resource:    resource,
		sourcePath:  filePath,
	}, nil
}

// dumpResource returns a copy of the Binary without its base64 payload. The
// payload only duplicates the input file, which stays on disk anyway.
func (b binaryEntry) dumpResource() map[string]any {
	stripped := make(map[string]any, len(b.resource))
	for key, value := range b.resource {
		if key == "data" {
			continue
		}
		stripped[key] = value
	}
	return stripped
}

// zipSingleFile compresses a single file into an in-memory zip archive.
// Uses lib.OpenFileForReading to transparently decompress .zst files before zipping,
// so the zip contains the raw (uncompressed) data.
func zipSingleFile(file File) ([]byte, error) {
	filePath := file.Path
	reader, err := lib.OpenFileForReading(filePath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = reader.Close() }()

	entryName := zipEntryName(file)

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

// zipEntryName gives the entry name of a file in its zip archive.
func zipEntryName(file File) string {
	return lib.GetUncompressedFilename(displayName(file))
}

// displayName gives the name of a file in messages and logs.
func displayName(file File) string {
	if file.EntryName != "" {
		return file.EntryName
	}
	return filepath.Base(file.Path)
}

// buildDocumentReference creates a FHIR DocumentReference linking all Binary resources.
func buildDocumentReference(binaryIDs []string, config models.TransferConfig) map[string]any {
	content := make([]map[string]any, len(binaryIDs))
	for i, id := range binaryIDs {
		content[i] = map[string]any{
			"attachment": map[string]any{
				"contentType": binaryContentType,
				"url":         fmt.Sprintf("Binary/%s", id),
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
func uploadBinary(binary binaryEntry, config models.SendConfig, httpClient *services.HTTPClient, logger *lib.Logger, dump *services.RequestDump) error {
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
		return fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		// The source file, not the base64 payload, is what an analyst needs.
		if stripped, marshalErr := json.Marshal(binary.dumpResource()); marshalErr == nil {
			dump.Write("binary-"+binary.id, stripped, resp.StatusCode,
				append(body, []byte("\n\nsource file: "+binary.sourcePath)...))
		}
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
func uploadDocumentReference(docRef map[string]any, config models.SendConfig, httpClient *services.HTTPClient, logger *lib.Logger, dump *services.RequestDump) error {
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
		return fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		dump.Write("documentreference-"+id, jsonData, resp.StatusCode, body)
		return &SendError{
			StatusCode: resp.StatusCode,
			Message:    string(body),
			ErrorType:  lib.ClassifyHTTPError(resp.StatusCode),
		}
	}

	logger.Debug("DocumentReference uploaded successfully", "id", id, "status", resp.StatusCode)
	return nil
}

// sendS3Upload uploads files to an S3 bucket.
// The upload manifest lets a repeated send skip the files that it already uploaded.
func sendS3Upload(parent context.Context, req Request, logger *lib.Logger) (Result, error) {
	startTime := time.Now()

	files := req.Files
	s3Config := req.Config.S3
	manifestPath := req.State.ManifestPath

	uploader, err := s3UploaderFactory(s3Config, req.Config.Auth, req.TLS, logger)
	if err != nil {
		return Result{}, fmt.Errorf("failed to create S3 uploader: %w", err)
	}

	manifest := loadOrCreateManifest(manifestPath, req.JobID, uploader.GetBucket(), logger)

	ctx, cancel := context.WithTimeout(parent, s3Config.Timeout)
	defer cancel()

	fmt.Printf("Uploading %d file(s) to S3 bucket: %s\n\n", len(files), uploader.GetBucket())

	filesUploaded := 0
	var totalBytes int64

	for _, file := range files {
		filePath := file.Path

		if manifest.IsFileUploaded(filePath) {
			logger.Debug("Skipping already-uploaded file", "path", filePath)
			filesUploaded++
			continue
		}

		s3Key := req.JobID + "/" + file.Name
		fileSize, err := uploadS3File(ctx, uploader, manifest, manifestPath, s3Key, filePath)
		if err != nil {
			return Result{}, err
		}
		filesUploaded++
		totalBytes += fileSize

		if saveErr := models.SaveManifest(manifest, manifestPath); saveErr != nil {
			logger.Warn("Failed to save upload manifest", "error", saveErr)
		}

		fmt.Printf("  ✓ %s → s3://%s/%s (%s)\n",
			filepath.Base(filePath), uploader.GetBucket(), s3Key, formatSize(fileSize))
	}

	manifest.MarkCompleted()
	_ = models.SaveManifest(manifest, manifestPath)

	duration := time.Since(startTime)
	fmt.Printf("\n✓ Uploaded %d files (%s) to s3://%s in %s\n",
		filesUploaded, formatSize(totalBytes), uploader.GetBucket(), duration.Truncate(time.Second))

	logger.Debug("S3 upload send step completed",
		"files_uploaded", filesUploaded,
		"total_bytes", totalBytes,
		"bucket", uploader.GetBucket(),
		"duration", duration,
		"job_id", req.JobID)

	return Result{FilesProcessed: filesUploaded}, nil
}

// loadOrCreateManifest loads the upload manifest, or starts a new one when
// loading fails or no manifest exists.
func loadOrCreateManifest(path, jobID, bucket string, logger *lib.Logger) *models.UploadManifest {
	manifest, err := models.LoadUploadManifest(path)
	if err != nil {
		logger.Warn("Failed to load upload manifest, starting fresh", "error", err)
	}
	if manifest == nil {
		manifest = models.NewUploadManifest(jobID, bucket)
	}
	return manifest
}

// uploadS3File uploads one file and records it in the manifest. The manifest
// is saved on failure so earlier progress is not lost. It returns the file size.
func uploadS3File(ctx context.Context, uploader services.S3Uploader, manifest *models.UploadManifest, manifestPath, s3Key, filePath string) (int64, error) {
	info, err := os.Stat(filePath)
	if err != nil {
		_ = models.SaveManifest(manifest, manifestPath)
		return 0, fmt.Errorf("failed to stat file %s: %w", filepath.Base(filePath), err)
	}
	fileSize := info.Size()

	etag, err := uploader.UploadFile(ctx, filePath, s3Key)
	if err != nil {
		_ = models.SaveManifest(manifest, manifestPath)
		return 0, fmt.Errorf("failed to upload %s to S3: %w", filepath.Base(filePath), err)
	}

	manifest.AddUploadedFile(filePath, s3Key, etag, fileSize)
	return fileSize, nil
}

// FormatSizeForTesting makes formatSize available to tests outside this package.
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

// IsRetryable reports whether a later attempt can succeed.
func (e *SendError) IsRetryable() bool {
	return e.ErrorType == models.ErrorTypeTransient
}
