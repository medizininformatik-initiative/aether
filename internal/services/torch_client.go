package services

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/ui"
)

// TORCHClient handles communication with TORCH server for CRTDL-based data extraction
// Per contracts/torch-api.md
type TORCHClient struct {
	config     models.TORCHConfig
	httpClient *HTTPClient
	logger     *lib.Logger
}

// TORCHExtractionRequest represents the FHIR Parameters resource for extraction submission
type TORCHExtractionRequest struct {
	ResourceType string           `json:"resourceType"`
	Parameter    []TORCHParameter `json:"parameter"`
}

// TORCHParameter represents a parameter in the FHIR Parameters resource
type TORCHParameter struct {
	Name              string `json:"name"`
	ValueBase64Binary string `json:"valueBase64Binary,omitempty"`
}

// TORCHExtractionResult represents the FHIR Parameters response with extraction results
type TORCHExtractionResult struct {
	ResourceType string                 `json:"resourceType"`
	Parameter    []TORCHResultParameter `json:"parameter"`
}

// TORCHResultParameter represents an output parameter containing file URLs
type TORCHResultParameter struct {
	Name string            `json:"name"`
	Part []TORCHResultPart `json:"part,omitempty"`
}

// TORCHResultPart represents a part of an output parameter (e.g., file URL)
type TORCHResultPart struct {
	Name     string `json:"name"`
	ValueURL string `json:"valueUrl,omitempty"`
}

// TORCHSimpleResponse represents the simplified TORCH response format (non-FHIR)
// This is the actual format returned by TORCH server
type TORCHSimpleResponse struct {
	RequiresAccessToken bool                `json:"requiresAccessToken"`
	Output              []TORCHSimpleOutput `json:"output"`
}

// TORCHSimpleOutput represents a single output file in the simplified format
type TORCHSimpleOutput struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

// OperationOutcome represents a FHIR OperationOutcome resource used for progress diagnostics
type OperationOutcome struct {
	ResourceType string                  `json:"resourceType"`
	Issue        []OperationOutcomeIssue `json:"issue"`
}

// OperationOutcomeIssue represents an issue within an OperationOutcome
type OperationOutcomeIssue struct {
	Severity    string `json:"severity"`
	Code        string `json:"code"`
	Diagnostics string `json:"diagnostics"`
}

// TORCHError represents errors from TORCH operations
type TORCHError struct {
	Operation  string // "submit", "poll", "download"
	StatusCode int
	Message    string
	ErrorType  models.ErrorType
}

func (e *TORCHError) Error() string {
	return fmt.Sprintf("TORCH %s error: HTTP %d: %s", e.Operation, e.StatusCode, e.Message)
}

// IsRetryable returns true if this error should be retried
func (e *TORCHError) IsRetryable() bool {
	return e.ErrorType == models.ErrorTypeTransient
}

// ErrExtractionTimeout is returned when extraction polling exceeds configured timeout
var ErrExtractionTimeout = fmt.Errorf("TORCH extraction timeout exceeded")

// ErrHandleDead is returned from polling when the status endpoint reports the
// job handle no longer exists (404 Not Found or 410 Gone). The caller should
// clear the stored handle and re-submit a fresh extraction.
var ErrHandleDead = fmt.Errorf("TORCH job handle is gone (404 or 410)")

// ErrInvalidCRTDL is returned when CRTDL file is malformed
var ErrInvalidCRTDL = fmt.Errorf("invalid CRTDL file")

// NewTORCHClient creates a new TORCH client with the given configuration
func NewTORCHClient(config models.TORCHConfig, httpClient *HTTPClient, logger *lib.Logger) *TORCHClient {
	return &TORCHClient{
		config:     config,
		httpClient: httpClient,
		logger:     logger,
	}
}

// Extractor is the seam the TORCH client satisfies so the import step can be
// tested against a fake adapter.
type Extractor interface {
	SubmitExtraction(crtdlPath string) (string, error)
	PollExtractionStatus(extractionURL string, showProgress bool) ([]string, error)
	DownloadExtractionFiles(fileURLs []string, destinationDir string, showProgress, compress bool, compressionLevel string) ([]models.FHIRDataFile, error)
}

var _ Extractor = (*TORCHClient)(nil)

// MockExtractor is a test double for Extractor. Unset funcs return zero values.
type MockExtractor struct {
	SubmitFunc   func(crtdlPath string) (string, error)
	PollFunc     func(extractionURL string, showProgress bool) ([]string, error)
	DownloadFunc func(fileURLs []string, destinationDir string, showProgress, compress bool, compressionLevel string) ([]models.FHIRDataFile, error)
}

var _ Extractor = (*MockExtractor)(nil)

func (m *MockExtractor) SubmitExtraction(crtdlPath string) (string, error) {
	if m.SubmitFunc != nil {
		return m.SubmitFunc(crtdlPath)
	}
	return "", nil
}

func (m *MockExtractor) PollExtractionStatus(extractionURL string, showProgress bool) ([]string, error) {
	if m.PollFunc != nil {
		return m.PollFunc(extractionURL, showProgress)
	}
	return nil, nil
}

func (m *MockExtractor) DownloadExtractionFiles(fileURLs []string, destinationDir string, showProgress, compress bool, compressionLevel string) ([]models.FHIRDataFile, error) {
	if m.DownloadFunc != nil {
		return m.DownloadFunc(fileURLs, destinationDir, showProgress, compress, compressionLevel)
	}
	return nil, nil
}

// SubmitExtraction submits a CRTDL file for extraction to TORCH server
// Returns the Content-Location URL for polling extraction status
// Per TORCH API: POST /fhir/$extract-data with base64-encoded CRTDL
func (c *TORCHClient) SubmitExtraction(crtdlPath string) (string, error) {
	c.logger.Info("Submitting CRTDL extraction to TORCH", "file", crtdlPath, "server", c.config.BaseURL)

	// Encode CRTDL to base64
	base64Content, err := c.encodeCRTDLToBase64(crtdlPath)
	if err != nil {
		return "", fmt.Errorf("failed to encode CRTDL: %w", err)
	}

	// Build FHIR Parameters request
	requestBody := TORCHExtractionRequest{
		ResourceType: "Parameters",
		Parameter: []TORCHParameter{
			{
				Name:              "crtdl",
				ValueBase64Binary: base64Content,
			},
		},
	}

	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	c.logger.Debug("TORCH extraction request", "body_size", len(jsonBody))

	// Construct URL
	url := c.config.BaseURL + "/fhir/$extract-data"

	// Create HTTP request with authentication
	req, err := http.NewRequest("POST", url, strings.NewReader(string(jsonBody)))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/fhir+json")
	if err := c.httpClient.ApplyAuth(req, c.authConfig()); err != nil {
		return "", fmt.Errorf("failed to add auth header: %w", err)
	}

	// Send request without retry: $extract-data is a non-idempotent job
	// creation, so a retried timeout could spawn a duplicate extraction.
	resp, err := c.httpClient.DoOnce(req)
	if err != nil {
		c.logger.Error("TORCH submission failed", "error", err)
		return "", &TORCHError{
			Operation:  "submit",
			StatusCode: 0,
			Message:    err.Error(),
			ErrorType:  models.ErrorTypeTransient,
		}
	}
	defer func() { _ = resp.Body.Close() }()

	// Check for errors
	if resp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		errorType := lib.ClassifyHTTPError(resp.StatusCode)

		c.logger.Error("TORCH submission returned error",
			"status_code", resp.StatusCode,
			"status", resp.Status,
			"error_body", string(bodyBytes))

		return "", &TORCHError{
			Operation:  "submit",
			StatusCode: resp.StatusCode,
			Message:    string(bodyBytes),
			ErrorType:  errorType,
		}
	}

	// Extract Content-Location header
	contentLocation := resp.Header.Get("Content-Location")
	if contentLocation == "" {
		return "", fmt.Errorf("TORCH server did not return Content-Location header")
	}

	// Ensure URL is absolute (handle relative URLs from TORCH)
	contentLocation = makeAbsoluteURL(c.config.BaseURL, contentLocation, c.logger)
	c.logger.Info("TORCH extraction submitted successfully", "extraction_url", contentLocation)

	return contentLocation, nil
}

// JobIDFromStatusURL returns the TORCH job ID — the last path segment of a
// status / Content-Location URL (e.g. ".../fhir/__status/{jobId}") — or "" if
// none can be parsed. The job ID is the handle aether persists to re-attach.
func JobIDFromStatusURL(statusURL string) string {
	trimmed := strings.TrimRight(statusURL, "/")
	if trimmed == "" {
		return ""
	}

	if u, err := url.Parse(trimmed); err == nil && u.Path != "" {
		trimmed = strings.TrimRight(u.Path, "/")
	}

	base := path.Base(trimmed)
	if base == "." || base == "/" {
		return ""
	}
	return base
}

// SubmitExtractionWithContent submits already-encoded CRTDL content for extraction to TORCH server.
// This is used when CRTDL preprocessing has enriched the document in-memory.
// Returns the Content-Location URL for polling extraction status.
// Per TORCH API: POST /fhir/$extract-data with base64-encoded CRTDL
func (c *TORCHClient) SubmitExtractionWithContent(crtdlContent []byte) (string, error) {
	c.logger.Info("Submitting enriched CRTDL extraction to TORCH", "content_size", len(crtdlContent), "server", c.config.BaseURL)

	if len(crtdlContent) == 0 {
		return "", fmt.Errorf("CRTDL content is empty")
	}

	// Validate it's valid JSON
	var crtdl map[string]any
	if err := json.Unmarshal(crtdlContent, &crtdl); err != nil {
		return "", fmt.Errorf("CRTDL content is not valid JSON: %w", err)
	}

	// Encode to base64
	base64Content := base64.StdEncoding.EncodeToString(crtdlContent)

	c.logger.Debug("Encoded CRTDL content to base64",
		"original_size", len(crtdlContent),
		"encoded_size", len(base64Content))

	// Build FHIR Parameters request
	requestBody := TORCHExtractionRequest{
		ResourceType: "Parameters",
		Parameter: []TORCHParameter{
			{
				Name:              "crtdl",
				ValueBase64Binary: base64Content,
			},
		},
	}

	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	c.logger.Debug("TORCH extraction request", "body_size", len(jsonBody))

	// Construct URL
	url := c.config.BaseURL + "/fhir/$extract-data"

	// Create HTTP request with authentication
	req, err := http.NewRequest("POST", url, strings.NewReader(string(jsonBody)))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if err := c.httpClient.ApplyAuth(req, c.authConfig()); err != nil {
		return "", fmt.Errorf("failed to add auth header: %w", err)
	}

	// Send request without retry: $extract-data is a non-idempotent job
	// creation, so a retried timeout could spawn a duplicate extraction.
	resp, err := c.httpClient.DoOnce(req)
	if err != nil {
		c.logger.Error("TORCH submission failed", "error", err)
		return "", &TORCHError{
			Operation:  "submit",
			StatusCode: 0,
			Message:    err.Error(),
			ErrorType:  models.ErrorTypeTransient,
		}
	}
	defer func() { _ = resp.Body.Close() }()

	// Check for errors
	if resp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		errorType := lib.ClassifyHTTPError(resp.StatusCode)

		c.logger.Error("TORCH submission returned error",
			"status_code", resp.StatusCode,
			"status", resp.Status,
			"error_body", string(bodyBytes))

		return "", &TORCHError{
			Operation:  "submit",
			StatusCode: resp.StatusCode,
			Message:    string(bodyBytes),
			ErrorType:  errorType,
		}
	}

	// Extract Content-Location header
	contentLocation := resp.Header.Get("Content-Location")
	if contentLocation == "" {
		return "", fmt.Errorf("TORCH server did not return Content-Location header")
	}

	// Ensure URL is absolute (handle relative URLs from TORCH)
	contentLocation = makeAbsoluteURL(c.config.BaseURL, contentLocation, c.logger)
	c.logger.Info("TORCH extraction submitted successfully", "extraction_url", contentLocation)

	return contentLocation, nil
}

// PollExtractionStatus polls the extraction status URL until completion or timeout
// Returns the list of file URLs when extraction is complete
// Per TORCH API: GET Content-Location URL until HTTP 200, handle HTTP 202 as in-progress
// Uses spinner for polling (duration unknown until extraction completes)
func (c *TORCHClient) PollExtractionStatus(extractionURL string, showProgress bool) (fileURLs []string, err error) {
	c.logger.Info("Polling TORCH extraction status", "url", extractionURL)

	// Setup polling configuration
	pollConfig := NewPollConfig(c.config)

	// Start spinner for polling (duration unknown).
	// UpdateMessage during polling overwrites the spinner's description with the
	// most recent TORCH progress diagnostic, which can be a stale in-progress
	// value (e.g., "cohort Size: 0") by the time the extraction completes. The
	// deferred Stop is therefore primed with a definitive final message based on
	// the actual outcome before the named return values are observed.
	var spinner *ui.Spinner
	if showProgress {
		spinner = ui.NewSpinner("Waiting for TORCH extraction to complete")
		spinner.Start()
		defer func() {
			if err == nil {
				spinner.UpdateMessage(fmt.Sprintf("TORCH extraction complete (%s)", filesLabel(len(fileURLs))))
			} else {
				spinner.UpdateMessage("TORCH extraction failed")
			}
			spinner.Stop(err == nil)
		}()
	}

	var lastDiagnostics string

	for {
		// Check timeout
		if pollConfig.CheckTimeout() {
			c.logger.Error("TORCH extraction timeout",
				"duration", pollConfig.GetElapsedTime(),
				"timeout", pollConfig.Timeout,
				"polls", pollConfig.PollCount)
			return nil, ErrExtractionTimeout
		}

		pollConfig.IncrementPollCount()
		c.logger.Debug("Polling TORCH extraction", "attempt", pollConfig.PollCount, "interval", pollConfig.PollInterval)

		// Create poll request with authentication
		req, reqErr := createPollRequest(extractionURL, c)
		if reqErr != nil {
			return nil, fmt.Errorf("failed to create poll request: %w", reqErr)
		}

		// Send request (single attempt; the poll loop owns the retry cadence)
		resp, doErr := c.httpClient.DoOnce(req)
		if doErr != nil {
			// Treat transient HTTP errors (timeouts, connection resets) as recoverable
			// during polling — the extraction may still be running on the server.
			// The overall extraction timeout provides the safety net.
			c.logger.Warn("TORCH poll request failed, will retry", "error", doErr, "attempt", pollConfig.PollCount)
			time.Sleep(pollConfig.PollInterval)
			pollConfig.UpdateInterval()
			continue
		}

		// Handle response
		outcome := c.handlePollResponse(resp)
		if outcome.err != nil {
			if outcome.retryable {
				time.Sleep(pollConfig.PollInterval)
				pollConfig.UpdateInterval()
				continue
			}
			return nil, outcome.err
		}

		// 200/202 are contact with a live job — reset the liveness window so a
		// long but responsive extraction never trips extraction_timeout.
		pollConfig.RecordContact()

		if outcome.complete {
			c.logger.Info("TORCH extraction completed", "polls", pollConfig.PollCount)
			return outcome.fileURLs, nil
		}

		// Log progress diagnostics from OperationOutcome (only when changed)
		if outcome.diagnostics != "" && outcome.diagnostics != lastDiagnostics {
			c.logger.Info("TORCH extraction progress", "diagnostics", outcome.diagnostics)
			if spinner != nil {
				spinner.UpdateMessage(outcome.diagnostics)
			}
			lastDiagnostics = outcome.diagnostics
		}

		// Still in progress - wait with exponential backoff
		time.Sleep(pollConfig.PollInterval)
		pollConfig.UpdateInterval()
	}
}

// filesLabel returns a singular/plural file label for a count.
func filesLabel(n int) string {
	if n == 1 {
		return "1 file"
	}
	return fmt.Sprintf("%d files", n)
}

// DownloadExtractionFiles downloads all NDJSON files from the extraction result
// Returns list of downloaded files with metadata
// Uses spinner for each file download (file size is unknown)
func (c *TORCHClient) DownloadExtractionFiles(fileURLs []string, destinationDir string, showProgress bool, compress bool, compressionLevel string) ([]models.FHIRDataFile, error) {
	c.logger.Info("Downloading TORCH extraction files",
		"file_count", len(fileURLs),
		"destination", destinationDir)

	if len(fileURLs) == 0 {
		c.logger.Warn("No files to download from TORCH extraction")
		return []models.FHIRDataFile{}, nil
	}

	if err := os.MkdirAll(destinationDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create destination directory: %w", err)
	}

	downloadedFiles := []models.FHIRDataFile{}

	for i, fileURL := range fileURLs {
		c.logger.Debug("Downloading TORCH file", "index", i+1, "total", len(fileURLs), "url", fileURL)

		fileName := filepath.Base(fileURL)
		if fileName == "." || fileName == "/" {
			fileName = fmt.Sprintf("torch-batch-%d.ndjson", i+1)
		}

		if !strings.HasSuffix(fileName, ".ndjson") {
			fileName = fileName + ".ndjson"
		}

		outputFileName := lib.GetCompressedFilename(fileName, compress)
		destPath := filepath.Join(destinationDir, outputFileName)

		// Wait for file to become available (handles nginx eventual consistency)
		if err := c.waitForFileAvailability(fileURL); err != nil {
			c.logger.Error("File not available for download", "url", fileURL, "error", err)
			return nil, fmt.Errorf("file not available for download %s: %w", fileURL, err)
		}

		var spinner *ui.Spinner
		if showProgress {
			spinnerMsg := fmt.Sprintf("Downloading file %d/%d: %s", i+1, len(fileURLs), outputFileName)
			spinner = ui.NewSpinner(spinnerMsg)
			spinner.Start()
		}

		file, err := c.downloadFile(fileURL, destPath, compress, compressionLevel)

		if spinner != nil {
			spinner.Stop(err == nil)
		}

		if err != nil {
			c.logger.Error("Failed to download TORCH file", "url", fileURL, "error", err)
			return nil, fmt.Errorf("failed to download file %s: %w", fileURL, err)
		}

		downloadedFiles = append(downloadedFiles, file)
		c.logger.Info("Downloaded TORCH file",
			"file", outputFileName,
			"size", file.FileSize,
			"resources", file.LineCount,
			"compressed", compress)
	}

	c.logger.Info("All TORCH files downloaded successfully", "total_files", len(downloadedFiles))

	return downloadedFiles, nil
}

// downloadFile downloads a single file from URL to destination path
func (c *TORCHClient) downloadFile(fileURL, destPath string, compress bool, compressionLevel string) (models.FHIRDataFile, error) {
	req, err := http.NewRequest("GET", fileURL, nil)
	if err != nil {
		return models.FHIRDataFile{}, fmt.Errorf("failed to create download request: %w", err)
	}

	if err := c.httpClient.ApplyAuth(req, c.authConfig()); err != nil {
		return models.FHIRDataFile{}, fmt.Errorf("failed to add auth header: %w", err)
	}
	req.Header.Set("Accept", "application/fhir+ndjson")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return models.FHIRDataFile{}, &TORCHError{
			Operation:  "download",
			StatusCode: 0,
			Message:    err.Error(),
			ErrorType:  models.ErrorTypeTransient,
		}
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		errorType := lib.ClassifyHTTPError(resp.StatusCode)

		return models.FHIRDataFile{}, &TORCHError{
			Operation:  "download",
			StatusCode: resp.StatusCode,
			Message:    string(bodyBytes),
			ErrorType:  errorType,
		}
	}

	destFile, err := os.Create(destPath)
	if err != nil {
		return models.FHIRDataFile{}, fmt.Errorf("failed to create destination file: %w", err)
	}

	var writer io.WriteCloser = destFile
	if compress {
		compWriter, err := lib.CreateCompressedWriter(destFile, compressionLevel)
		if err != nil {
			_ = destFile.Close()
			return models.FHIRDataFile{}, fmt.Errorf("failed to create compressed writer: %w", err)
		}
		writer = compWriter
	}

	bytesWritten, err := io.Copy(writer, resp.Body)

	if closeErr := writer.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	if compress {
		if closeErr := destFile.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}

	if err != nil {
		_ = os.Remove(destPath)
		return models.FHIRDataFile{}, fmt.Errorf("failed to write file: %w", err)
	}

	fileInfo, statErr := os.Stat(destPath)
	var fileSize int64
	if statErr == nil {
		fileSize = fileInfo.Size()
	} else {
		fileSize = bytesWritten
	}

	lineCount, _ := lib.CountResourcesInFile(destPath)

	fileName := filepath.Base(destPath)

	return models.FHIRDataFile{
		FileName:   fileName,
		FilePath:   fileName, // Relative to job directory
		FileSize:   fileSize,
		SourceStep: models.StepTorchImport,
		LineCount:  lineCount,
		CreatedAt:  lib.GetFileModTime(destPath),
	}, nil
}

// Ping checks connectivity to TORCH server
// Used by ValidateServiceConnectivity()
func (c *TORCHClient) Ping() error {
	c.logger.Debug("Checking TORCH server connectivity", "url", c.config.BaseURL)

	// Simple GET request to base URL
	req, err := http.NewRequest("GET", c.config.BaseURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create ping request: %w", err)
	}

	if err := c.httpClient.ApplyAuth(req, c.authConfig()); err != nil {
		return fmt.Errorf("failed to add auth header: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.logger.Error("TORCH ping failed", "error", err)
		return fmt.Errorf("TORCH server unreachable: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Accept any non-5xx response as "server is up"
	if resp.StatusCode >= 500 {
		return fmt.Errorf("TORCH server error: HTTP %d", resp.StatusCode)
	}

	c.logger.Debug("TORCH server ping successful", "status", resp.StatusCode)
	return nil
}

// encodeCRTDLToBase64 reads CRTDL file and encodes it to base64
func (c *TORCHClient) encodeCRTDLToBase64(crtdlPath string) (string, error) {
	// Read CRTDL file
	crtdlContent, err := os.ReadFile(crtdlPath)
	if err != nil {
		return "", fmt.Errorf("failed to read CRTDL file: %w", err)
	}

	if len(crtdlContent) == 0 {
		return "", fmt.Errorf("CRTDL file is empty")
	}

	// Validate it's valid JSON
	var crtdl map[string]any
	if err := json.Unmarshal(crtdlContent, &crtdl); err != nil {
		return "", fmt.Errorf("CRTDL file is not valid JSON: %w", err)
	}

	// Encode to base64
	encoded := base64.StdEncoding.EncodeToString(crtdlContent)

	c.logger.Debug("Encoded CRTDL to base64",
		"original_size", len(crtdlContent),
		"encoded_size", len(encoded))

	return encoded, nil
}

// parseExtractionResult parses TORCH response and extracts file URLs
// Supports both FHIR Parameters format and TORCH's simplified format
func (c *TORCHClient) parseExtractionResult(responseBody []byte) ([]string, error) {
	// Log the raw response for debugging
	c.logger.Debug("Parsing TORCH extraction result", "body_length", len(responseBody))

	if len(responseBody) == 0 {
		return nil, fmt.Errorf("empty response body from TORCH server")
	}

	// First, check which format we're dealing with by looking for distinctive fields
	// Try parsing as FHIR Parameters format first (documented format)
	var fhirResult TORCHExtractionResult
	if err := json.Unmarshal(responseBody, &fhirResult); err == nil && fhirResult.ResourceType == "Parameters" {
		c.logger.Debug("Parsed FHIR Parameters format response")
		return c.extractURLsFromFHIRFormat(fhirResult), nil
	}

	// Try parsing as simplified TORCH format (actual format used by server)
	var simpleResult TORCHSimpleResponse
	if err := json.Unmarshal(responseBody, &simpleResult); err == nil {
		// Check if this looks like TORCH simple format by verifying it has the expected structure
		// We need to distinguish between actual TORCH simple format and random JSON that happens to parse
		var rawMap map[string]interface{}
		_ = json.Unmarshal(responseBody, &rawMap)

		// TORCH simple format should have "output" field at minimum
		if _, hasOutput := rawMap["output"]; hasOutput {
			// Valid TORCH simple format response - check if it has data
			if len(simpleResult.Output) > 0 {
				c.logger.Debug("Parsed TORCH simple format response", "file_count", len(simpleResult.Output))
				return c.extractURLsFromSimpleFormat(simpleResult), nil
			}

			// TORCH processed request but found no data - this is the actual error
			// Try to parse error details if available
			var detailedError struct {
				Error []map[string]interface{} `json:"error"`
			}
			_ = json.Unmarshal(responseBody, &detailedError)

			if len(detailedError.Error) > 0 {
				// TORCH reported specific errors
				return nil, fmt.Errorf("TORCH extraction completed but found no data (errors reported). Check CRTDL query criteria. Error details: %v", detailedError.Error)
			}

			// No output and no error - likely CRTDL matched no resources
			return nil, fmt.Errorf("TORCH extraction completed successfully but found no matching data. This usually means:\n" +
				"  1. The CRTDL query criteria matched no resources in the source system\n" +
				"  2. The time period specified in the CRTDL is outside available data range\n" +
				"  3. The patient/cohort identifiers in the CRTDL don't exist\n" +
				"Check your CRTDL file and verify the query parameters match available data in TORCH")
		}
	}

	// Invalid JSON or neither format matched
	var jsonErr error
	if err := json.Unmarshal(responseBody, &map[string]interface{}{}); err != nil {
		jsonErr = err
		return nil, fmt.Errorf("failed to parse extraction result (invalid JSON): %w. Response body: %s", jsonErr, string(responseBody))
	}

	// Valid JSON but unexpected format
	return nil, fmt.Errorf("unexpected response format (expected FHIR Parameters or TORCH simple format). Response body: %s", string(responseBody))
}

// extractURLsFromSimpleFormat extracts file URLs from TORCH's simplified response format
func (c *TORCHClient) extractURLsFromSimpleFormat(result TORCHSimpleResponse) []string {
	fileURLs := []string{}
	for _, output := range result.Output {
		if output.URL != "" {
			// Ensure URL is absolute (handle relative URLs from TORCH)
			fileURLs = append(fileURLs, makeAbsoluteURL(c.config.BaseURL, output.URL, c.logger))
		}
	}
	c.logger.Debug("Extracted URLs from simple format", "file_count", len(fileURLs))
	return fileURLs
}

// extractURLsFromFHIRFormat extracts file URLs from FHIR Parameters format
func (c *TORCHClient) extractURLsFromFHIRFormat(result TORCHExtractionResult) []string {
	fileURLs := []string{}
	for _, param := range result.Parameter {
		if param.Name == "output" {
			for _, part := range param.Part {
				if part.Name == "url" && part.ValueURL != "" {
					// Ensure URL is absolute (handle relative URLs from TORCH)
					fileURLs = append(fileURLs, makeAbsoluteURL(c.config.BaseURL, part.ValueURL, c.logger))
				}
			}
		}
	}
	c.logger.Debug("Extracted URLs from FHIR format", "file_count", len(fileURLs))
	return fileURLs
}

// authConfig returns the TORCH server's credentials for the shared auth
// mechanism (HTTPClient.ApplyAuth). Basic auth takes precedence; OAuth2
// client-credentials is used when configured instead.
func (c *TORCHClient) authConfig() models.AuthConfig {
	return models.AuthConfig{
		Username:          c.config.Username,
		Password:          c.config.Password,
		OAuthIssuerURI:    c.config.OAuthIssuerURI,
		OAuthClientID:     c.config.OAuthClientID,
		OAuthClientSecret: c.config.OAuthClientSecret,
	}
}

// waitForFileAvailability waits until a file URL is available for download.
// Skips the check if FileReadyRetries <= 0 (zero-value means disabled).
func (c *TORCHClient) waitForFileAvailability(fileURL string) error {
	if c.config.FileReadyRetries <= 0 {
		return nil
	}

	interval := c.config.FileReadyInterval

	for attempt := 1; attempt <= c.config.FileReadyRetries; attempt++ {
		available, err := c.checkFileAvailable(fileURL)
		if err != nil {
			c.logger.Warn("File availability check error", "url", fileURL, "attempt", attempt, "error", err)
		}
		if available {
			c.logger.Debug("File is available", "url", fileURL, "attempt", attempt)
			return nil
		}

		if attempt < c.config.FileReadyRetries {
			c.logger.Debug("File not yet available, retrying", "url", fileURL, "attempt", attempt, "next_check_in", interval)
			time.Sleep(interval)
		}
	}

	return fmt.Errorf("file not available after %d retries: %s", c.config.FileReadyRetries, fileURL)
}

// checkFileAvailable checks if a file is available via HEAD request.
// Falls back to Range GET if HEAD returns 403, 404, or 405.
func (c *TORCHClient) checkFileAvailable(fileURL string) (bool, error) {
	req, err := http.NewRequest("HEAD", fileURL, nil)
	if err != nil {
		return false, fmt.Errorf("failed to create HEAD request: %w", err)
	}
	if err := c.httpClient.ApplyAuth(req, c.authConfig()); err != nil {
		return false, fmt.Errorf("failed to add auth header: %w", err)
	}

	resp, err := c.httpClient.DoOnce(req)
	if err != nil {
		return false, err
	}
	_ = resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		return resp.ContentLength > 0, nil
	case http.StatusForbidden, http.StatusMethodNotAllowed, http.StatusNotFound:
		// HEAD not supported or file not found — fall back to Range request
		return c.checkFileAvailableWithRange(fileURL)
	default:
		return false, fmt.Errorf("unexpected status %d from HEAD request", resp.StatusCode)
	}
}

// checkFileAvailableWithRange checks file availability using a GET with Range header.
// Accepts 200 or 206 as successful responses.
func (c *TORCHClient) checkFileAvailableWithRange(fileURL string) (bool, error) {
	req, err := http.NewRequest("GET", fileURL, nil)
	if err != nil {
		return false, fmt.Errorf("failed to create Range GET request: %w", err)
	}
	if err := c.httpClient.ApplyAuth(req, c.authConfig()); err != nil {
		return false, fmt.Errorf("failed to add auth header: %w", err)
	}
	req.Header.Set("Range", "bytes=0-0")

	resp, err := c.httpClient.DoOnce(req)
	if err != nil {
		return false, err
	}
	_ = resp.Body.Close()

	return resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusPartialContent, nil
}

// makeAbsoluteURL converts a TORCH-returned URL to absolute using baseURL.
// Pure function (no client state) so it can be tested directly without an
// HTTP server. It dispatches on the input shape: absolute URLs pass through,
// path-relative URLs resolve against baseURL, and scheme-less host-prefixed
// URLs (the TORCH misconfiguration) inherit baseURL's scheme.
func makeAbsoluteURL(baseURL, rawURL string, logger *lib.Logger) string {
	if strings.Contains(rawURL, "://") {
		logger.Debug("Using absolute URL from TORCH", "url", rawURL)
		return rawURL
	}

	base, err := url.Parse(baseURL)
	if err != nil || base.Scheme == "" {
		logger.Warn("Cannot resolve TORCH URL: invalid BaseURL", "base", baseURL, "raw", rawURL)
		return rawURL
	}

	if strings.HasPrefix(rawURL, "/") {
		return resolvePathRelativeURL(base, rawURL, logger)
	}
	return prependBaseScheme(base, rawURL, logger)
}

// resolvePathRelativeURL resolves a path-relative URL ("/output/x.ndjson")
// against base. ResolveReference also collapses an accidental double-slash
// when base carries a trailing slash.
func resolvePathRelativeURL(base *url.URL, rawURL string, logger *lib.Logger) string {
	ref, err := url.Parse(rawURL)
	if err != nil {
		logger.Warn("Cannot parse path-relative TORCH URL, returning as-is", "raw", rawURL, "error", err)
		return rawURL
	}
	resolved := base.ResolveReference(ref).String()
	logger.Debug("Converted relative URL", "raw", rawURL, "absolute", resolved)
	return resolved
}

// prependBaseScheme recovers a scheme-less host-prefixed URL
// ("localhost:8080/output/x.ndjson") by prepending base's scheme.
func prependBaseScheme(base *url.URL, rawURL string, logger *lib.Logger) string {
	fixed := base.Scheme + "://" + rawURL
	logger.Warn("TORCH returned scheme-less URL, prepending BaseURL scheme", "raw", rawURL, "fixed", fixed)
	return fixed
}
