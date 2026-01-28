package pipeline

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

// oauth2TokenCache stores cached OAuth2 tokens to avoid fetching a new token for every request.
// Thread-safe for concurrent access.
var oauth2TokenCache = struct {
	sync.RWMutex
	token     string
	expiresAt time.Time
}{}

// buildBasicAuthHeader returns the Authorization header value for Basic auth.
func buildBasicAuthHeader(username, password string) string {
	credentials := username + ":" + password
	encoded := base64.StdEncoding.EncodeToString([]byte(credentials))
	return "Basic " + encoded
}

// oauth2TokenResponse represents the response from an OAuth2 token endpoint
type oauth2TokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"` // Token lifetime in seconds
	TokenType   string `json:"token_type"`
}

// fetchOAuth2Token retrieves an access token using the OAuth2 client credentials flow.
// Uses the Keycloak standard token endpoint: {issuer_uri}/protocol/openid-connect/token
func fetchOAuth2Token(issuerURI, clientID, clientSecret string, httpClient *services.HTTPClient) (string, error) {
	// Check cache first
	oauth2TokenCache.RLock()
	if oauth2TokenCache.token != "" && time.Now().Before(oauth2TokenCache.expiresAt) {
		token := oauth2TokenCache.token
		oauth2TokenCache.RUnlock()
		return token, nil
	}
	oauth2TokenCache.RUnlock()

	// Build token endpoint URL (Keycloak standard path)
	tokenURL := strings.TrimSuffix(issuerURI, "/") + "/protocol/openid-connect/token"

	// Build request body
	data := url.Values{}
	data.Set("grant_type", "client_credentials")
	data.Set("client_id", clientID)
	data.Set("client_secret", clientSecret)

	// Create request
	req, err := http.NewRequest("POST", tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return "", fmt.Errorf("failed to create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	// Execute request
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("token request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("token request failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var tokenResp oauth2TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", fmt.Errorf("failed to parse token response: %w", err)
	}

	if tokenResp.AccessToken == "" {
		return "", fmt.Errorf("token response missing access_token")
	}

	// Cache the token with a small buffer before expiration (30 seconds)
	oauth2TokenCache.Lock()
	oauth2TokenCache.token = tokenResp.AccessToken
	if tokenResp.ExpiresIn > 30 {
		oauth2TokenCache.expiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn-30) * time.Second)
	} else {
		// Token expires very soon, don't cache
		oauth2TokenCache.expiresAt = time.Now()
	}
	oauth2TokenCache.Unlock()

	return tokenResp.AccessToken, nil
}

// clearOAuth2TokenCache clears the cached OAuth2 token (useful for testing)
func clearOAuth2TokenCache() {
	oauth2TokenCache.Lock()
	oauth2TokenCache.token = ""
	oauth2TokenCache.expiresAt = time.Time{}
	oauth2TokenCache.Unlock()
}

// ClearOAuth2TokenCacheForTesting clears the OAuth2 token cache - exported for testing
func ClearOAuth2TokenCacheForTesting() {
	clearOAuth2TokenCache()
}

// putWithAuth performs an authenticated HTTP PUT request based on the SendConfig authentication settings.
func putWithAuth(targetURL, contentType string, body []byte, config models.SendConfig, httpClient *services.HTTPClient) (*http.Response, error) {
	req, err := http.NewRequest("PUT", targetURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", contentType)

	// Add authentication header based on config
	switch config.GetAuthType() {
	case models.SendAuthBasic:
		req.Header.Set("Authorization", buildBasicAuthHeader(config.Username, config.Password))
	case models.SendAuthOAuth2:
		token, err := fetchOAuth2Token(config.OAuthIssuerURI, config.OAuthClientID, config.OAuthClientSecret, httpClient)
		if err != nil {
			return nil, fmt.Errorf("failed to get OAuth2 token: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}

	return httpClient.Do(req)
}

// ExecuteSendStep prepares and sends pipeline output to a DSF transfer FHIR server.
// For each file in the previous step's output: zips it, base64-encodes it, and wraps it
// in a FHIR Binary resource. Then creates a DocumentReference linking all Binaries,
// wraps everything in a FHIR transaction Bundle, and POSTs it to the transfer server.
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

	// Resolve input directory from previous step
	jobsBaseDir := filepath.Dir(jobDir)
	jobID := filepath.Base(jobDir)
	stepIndex := getStepIndexInEnabledSteps(job.Config.Pipeline.EnabledSteps, stepName)
	inputDir := services.GetStepInputDir(jobsBaseDir, jobID, job.Config.Pipeline.EnabledSteps, stepIndex)

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

	fmt.Printf("Preparing %d file(s) for DSF transfer...\n\n", len(files))

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
	docRef := buildDocumentReference(binaryResources, sendConfig)

	logger.Debug("Uploading resources individually",
		"binary_count", len(binaryResources),
		"job_id", job.JobID)

	httpClient := services.DefaultHTTPClient()

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

	logger.Debug("Send step completed",
		"files_sent", len(files),
		"duration", completedAt.Sub(*step.StartedAt),
		"job_id", job.JobID)

	return nil
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
func buildDocumentReference(binaries []binaryEntry, config models.SendConfig) map[string]any {
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
	targetURL := fmt.Sprintf("%s/Binary/%s", strings.TrimSuffix(config.ServerURL, "/"), binary.id)
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
	targetURL := fmt.Sprintf("%s/DocumentReference/%s", strings.TrimSuffix(config.ServerURL, "/"), id)
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
	return lib.IsNetworkError(err)
}

func classifySendError(err error) models.ErrorType {
	var sendErr *SendError
	if errors.As(err, &sendErr) {
		return sendErr.ErrorType
	}
	if lib.IsNetworkError(err) {
		return models.ErrorTypeTransient
	}
	return models.ErrorTypeNonTransient
}
