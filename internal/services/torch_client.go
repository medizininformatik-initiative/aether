package services

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
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

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", c.buildBasicAuthHeader())

	// Send request
	resp, err := c.httpClient.client.Do(req)
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
	contentLocation = c.makeAbsoluteURL(contentLocation)
	c.logger.Info("TORCH extraction submitted successfully", "extraction_url", contentLocation)

	return contentLocation, nil
}

// PollExtractionStatus polls the extraction status URL until completion or timeout
// Returns the list of file URLs when extraction is complete
// Per TORCH API: GET Content-Location URL until HTTP 200, handle HTTP 202 as in-progress
// Uses spinner for polling (duration unknown until extraction completes)
func (c *TORCHClient) PollExtractionStatus(extractionURL string, showProgress bool) ([]string, error) {
	c.logger.Info("Polling TORCH extraction status", "url", extractionURL)

	// Setup polling configuration
	pollConfig := NewPollConfig(c.config.ExtractionTimeoutMinutes, c.config.PollingIntervalSeconds, c.config.MaxPollingIntervalSeconds)

	// Start spinner for polling (duration unknown)
	var spinner *ui.Spinner
	if showProgress {
		spinner = ui.NewSpinner("Waiting for TORCH extraction to complete")
		spinner.Start()
		defer func() {
			if spinner != nil {
				spinner.Stop(true)
			}
		}()
	}

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
		req, err := createPollRequest(extractionURL, c)
		if err != nil {
			return nil, fmt.Errorf("failed to create poll request: %w", err)
		}

		// Send request
		resp, err := c.httpClient.client.Do(req)
		if err != nil {
			c.logger.Error("TORCH polling failed", "error", err, "attempt", pollConfig.PollCount)
			return nil, &TORCHError{
				Operation:  "poll",
				StatusCode: 0,
				Message:    err.Error(),
				ErrorType:  models.ErrorTypeTransient,
			}
		}

		// Handle response
		complete, fileURLs, err := handlePollResponse(resp, c)
		if err != nil {
			return nil, err
		}

		if complete {
			c.logger.Info("TORCH extraction completed", "polls", pollConfig.PollCount)
			return fileURLs, nil
		}

		// Still in progress - wait with exponential backoff
		time.Sleep(pollConfig.PollInterval)
		pollConfig.UpdateInterval()
	}
}

// DownloadExtractionFiles downloads all NDJSON files from the extraction result
// Returns list of downloaded files with metadata
// Uses spinner for each file download (file size is unknown)
func (c *TORCHClient) DownloadExtractionFiles(fileURLs []string, destinationDir string, showProgress bool) ([]models.FHIRDataFile, error) {
	c.logger.Info("Downloading TORCH extraction files",
		"file_count", len(fileURLs),
		"destination", destinationDir)

	if len(fileURLs) == 0 {
		c.logger.Warn("No files to download from TORCH extraction")
		return []models.FHIRDataFile{}, nil
	}

	// Ensure destination directory exists
	if err := os.MkdirAll(destinationDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create destination directory: %w", err)
	}

	downloadedFiles := []models.FHIRDataFile{}

	for i, fileURL := range fileURLs {
		c.logger.Debug("Downloading TORCH file", "index", i+1, "total", len(fileURLs), "url", fileURL)

		// Determine filename
		fileName := filepath.Base(fileURL)
		if fileName == "." || fileName == "/" {
			fileName = fmt.Sprintf("torch-batch-%d.ndjson", i+1)
		}

		// Ensure .ndjson extension
		if !strings.HasSuffix(fileName, ".ndjson") {
			fileName = fileName + ".ndjson"
		}

		destPath := filepath.Join(destinationDir, fileName)

		// Start spinner for this download (file size unknown)
		var spinner *ui.Spinner
		if showProgress {
			spinnerMsg := fmt.Sprintf("Downloading file %d/%d: %s", i+1, len(fileURLs), fileName)
			spinner = ui.NewSpinner(spinnerMsg)
			spinner.Start()
		}

		// Download file
		file, err := c.downloadFile(fileURL, destPath)

		// Stop spinner
		if spinner != nil {
			spinner.Stop(err == nil)
		}

		if err != nil {
			c.logger.Error("Failed to download TORCH file", "url", fileURL, "error", err)
			return nil, fmt.Errorf("failed to download file %s: %w", fileURL, err)
		}

		downloadedFiles = append(downloadedFiles, file)
		c.logger.Info("Downloaded TORCH file",
			"file", fileName,
			"size", file.FileSize,
			"resources", file.LineCount)
	}

	c.logger.Info("All TORCH files downloaded successfully", "total_files", len(downloadedFiles))

	return downloadedFiles, nil
}

// downloadFile downloads a single file from URL to destination path
func (c *TORCHClient) downloadFile(fileURL, destPath string) (models.FHIRDataFile, error) {
	// Create request
	req, err := http.NewRequest("GET", fileURL, nil)
	if err != nil {
		return models.FHIRDataFile{}, fmt.Errorf("failed to create download request: %w", err)
	}

	// Add authentication header for TORCH requests
	req.Header.Set("Authorization", c.buildBasicAuthHeader())
	req.Header.Set("Accept", "application/fhir+ndjson")

	// Send request
	resp, err := c.httpClient.client.Do(req)
	if err != nil {
		return models.FHIRDataFile{}, &TORCHError{
			Operation:  "download",
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

		return models.FHIRDataFile{}, &TORCHError{
			Operation:  "download",
			StatusCode: resp.StatusCode,
			Message:    string(bodyBytes),
			ErrorType:  errorType,
		}
	}

	// Create destination file
	destFile, err := os.Create(destPath)
	if err != nil {
		return models.FHIRDataFile{}, fmt.Errorf("failed to create destination file: %w", err)
	}
	defer func() { _ = destFile.Close() }()

	// Copy content
	bytesWritten, err := io.Copy(destFile, resp.Body)
	if err != nil {
		_ = os.Remove(destPath)
		return models.FHIRDataFile{}, fmt.Errorf("failed to write file: %w", err)
	}

	// Count lines (FHIR resources)
	lineCount, _ := lib.CountResourcesInFile(destPath)

	// Extract resource type from filename
	fileName := filepath.Base(destPath)
	resourceType := models.GetResourceTypeFromFilename(fileName)

	return models.FHIRDataFile{
		FileName:     fileName,
		FilePath:     fileName, // Relative to job directory
		ResourceType: resourceType,
		FileSize:     bytesWritten,
		SourceStep:   models.StepTorchImport,
		LineCount:    lineCount,
		CreatedAt:    lib.GetFileModTime(destPath),
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

	req.Header.Set("Authorization", c.buildBasicAuthHeader())

	resp, err := c.httpClient.client.Do(req)
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
			fileURLs = append(fileURLs, c.makeAbsoluteURL(output.URL))
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
					fileURLs = append(fileURLs, c.makeAbsoluteURL(part.ValueURL))
				}
			}
		}
	}
	c.logger.Debug("Extracted URLs from FHIR format", "file_count", len(fileURLs))
	return fileURLs
}

// buildBasicAuthHeader creates Basic authentication header
func (c *TORCHClient) buildBasicAuthHeader() string {
	credentials := c.config.Username + ":" + c.config.Password
	encoded := base64.StdEncoding.EncodeToString([]byte(credentials))
	return "Basic " + encoded
}

// makeAbsoluteURL ensures a URL is absolute and uses the configured baseURL
// This handles two cases:
// 1. Relative URLs from TORCH - prepends baseURL (scheme + host)
// 2. Absolute URLs with internal TORCH hostnames - rewrites scheme + host to use baseURL
func (c *TORCHClient) makeAbsoluteURL(rawURL string) string {
	// Case 1: Relative URL - prepend base URL
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		absoluteURL := c.config.BaseURL + rawURL
		c.logger.Debug("Converted relative URL", "raw", rawURL, "absolute", absoluteURL)
		return absoluteURL
	}

	// Case 2: Absolute URL - check if it's an internal TORCH URL that needs rewriting
	torchURL, err := url.Parse(rawURL)
	if err != nil {
		// If parsing fails, return as-is
		c.logger.Warn("Failed to parse TORCH URL", "url", rawURL, "error", err)
		return rawURL
	}

	// Check if this is an internal hostname that should be rewritten to use our configured baseURL
	// Internal hostnames include:
	// - torch: TORCH API service (container internal)
	// - torch-proxy: reverse proxy service (container internal)
	// - localhost/127.0.0.1: loopback addresses used internally
	hostname := torchURL.Hostname()
	internalHosts := map[string]bool{
		"torch":       true,
		"torch-proxy": true,
		"localhost":   true,
		"127.0.0.1":   true,
	}

	if internalHosts[hostname] {
		// This is an internal TORCH URL - rewrite to use our baseURL's scheme and host
		baseURLParsed, err := url.Parse(c.config.BaseURL)
		if err != nil {
			c.logger.Warn("Failed to parse baseURL", "baseURL", c.config.BaseURL, "error", err)
			return rawURL
		}

		// Create new URL with baseURL's scheme and host, but TORCH URL's path and query
		rewrittenURL := &url.URL{
			Scheme:   baseURLParsed.Scheme,
			Host:     baseURLParsed.Host, // includes port if present
			Path:     torchURL.Path,
			RawQuery: torchURL.RawQuery,
		}

		result := rewrittenURL.String()
		c.logger.Debug("Rewrote internal URL",
			"original", rawURL,
			"hostname", hostname,
			"baseScheme", baseURLParsed.Scheme,
			"baseHost", baseURLParsed.Host,
			"rewritten", result)
		return result
	}

	// External URL - return as-is
	c.logger.Debug("Keeping external URL as-is", "url", rawURL, "hostname", hostname)
	return rawURL
}
