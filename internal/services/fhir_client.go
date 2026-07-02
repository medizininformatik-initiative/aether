package services

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
)

// FHIRClient handles sending NDJSON files to a FHIR server
type FHIRClient struct {
	url        string
	batchSize  int
	auth       models.AuthConfig
	httpClient *HTTPClient
	logger     *lib.Logger
}

// FHIRUploadStats contains statistics from uploading an NDJSON file
type FHIRUploadStats struct {
	ResourcesUploaded int
	BatchesSent       int
}

// FHIRError represents an error from the FHIR server
type FHIRError struct {
	StatusCode int
	Message    string
	ErrorType  models.ErrorType
}

func (e *FHIRError) Error() string {
	return fmt.Sprintf("FHIR server error: HTTP %d: %s", e.StatusCode, e.Message)
}

// IsRetryable returns true if the error is transient
func (e *FHIRError) IsRetryable() bool {
	return e.ErrorType == models.ErrorTypeTransient
}

// NewFHIRClient creates a new FHIR client from SendConfig
func NewFHIRClient(config models.SendConfig, httpClient *HTTPClient, logger *lib.Logger) *FHIRClient {
	return &FHIRClient{
		url:        config.URL,
		batchSize:  config.GetBatchSize(),
		auth:       config.Auth,
		httpClient: httpClient,
		logger:     logger,
	}
}

// NewFHIRClientWithParams creates a FHIR client with explicit parameters
func NewFHIRClientWithParams(url string, batchSize int, auth models.AuthConfig, httpClient *HTTPClient, logger *lib.Logger) *FHIRClient {
	if batchSize <= 0 {
		batchSize = 100
	}
	return &FHIRClient{
		url:        url,
		batchSize:  batchSize,
		auth:       auth,
		httpClient: httpClient,
		logger:     logger,
	}
}

// UploadNDJSON reads resources from an NDJSON reader and uploads them to the FHIR server
// using transaction bundles. Returns upload statistics.
func (c *FHIRClient) UploadNDJSON(filePath string, reader io.Reader) (*FHIRUploadStats, error) {
	batchSize := c.batchSize
	if batchSize <= 0 {
		batchSize = 100 // default
	}

	dec := json.NewDecoder(reader)
	var batch []json.RawMessage
	stats := &FHIRUploadStats{}

	for {
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return stats, fmt.Errorf("error reading %s: %w", filepath.Base(filePath), err)
		}

		batch = append(batch, raw)

		if len(batch) >= batchSize {
			if err := c.sendBatch(batch); err != nil {
				return stats, fmt.Errorf("failed to send batch from %s: %w", filepath.Base(filePath), err)
			}
			stats.ResourcesUploaded += len(batch)
			stats.BatchesSent++
			batch = nil
		}
	}

	// Send remaining resources
	if len(batch) > 0 {
		if err := c.sendBatch(batch); err != nil {
			return stats, fmt.Errorf("failed to send final batch from %s: %w", filepath.Base(filePath), err)
		}
		stats.ResourcesUploaded += len(batch)
		stats.BatchesSent++
	}

	return stats, nil
}

// sendBatch creates a transaction bundle and POSTs it to the FHIR server
func (c *FHIRClient) sendBatch(resources []json.RawMessage) error {
	bundle := c.createTransactionBundle(resources)

	// Skip sending if no entries (e.g., empty bundles were unwrapped)
	if bundle == nil {
		return nil
	}

	jsonData, err := json.Marshal(bundle)
	if err != nil {
		return fmt.Errorf("failed to marshal bundle: %w", err)
	}

	url := strings.TrimSuffix(c.url, "/") + "/fhir"

	req, err := http.NewRequest("POST", url, bytes.NewReader(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/fhir+json")
	req.Header.Set("Accept", "application/fhir+json")

	// Add authentication header
	if err := c.addAuthHeader(req); err != nil {
		return fmt.Errorf("failed to add auth header: %w", err)
	}

	c.logger.Debug("Sending FHIR transaction bundle",
		"url", url,
		"resource_count", len(resources),
		"bundle_size_bytes", len(jsonData))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return &FHIRError{
			StatusCode: resp.StatusCode,
			Message:    string(body),
			ErrorType:  lib.ClassifyHTTPError(resp.StatusCode),
		}
	}

	c.logger.Debug("FHIR transaction bundle sent successfully",
		"status", resp.StatusCode,
		"resource_count", len(resources))

	return nil
}

// addAuthHeader adds the appropriate Authorization header based on auth config,
// delegating to the shared auth mechanism owned by HTTPClient.
func (c *FHIRClient) addAuthHeader(req *http.Request) error {
	return c.httpClient.ApplyAuth(req, c.auth)
}

// createTransactionBundle creates a FHIR transaction Bundle from raw resources.
// It unwraps collection/searchset/transaction Bundles, extracting their entries as individual resources.
// Returns nil if there are no entries to include (e.g., empty bundles).
func (c *FHIRClient) createTransactionBundle(resources []json.RawMessage) map[string]any {
	entries := make([]map[string]any, 0)

	for _, resourceRaw := range resources {
		// Parse resource to get resourceType and id
		var resource map[string]any
		if err := json.Unmarshal(resourceRaw, &resource); err != nil {
			// If we can't parse, still include it but without proper request
			entries = append(entries, map[string]any{
				"resource": json.RawMessage(resourceRaw),
				"request": map[string]any{
					"method": "POST",
					"url":    "Resource",
				},
			})
			continue
		}

		// Unwrap collection/searchset/transaction Bundles - extract entries as individual resources
		// Transaction bundles from TORCH should be unwrapped since we create our own transaction
		if lib.IsBundle(resource) {
			bundleType, _ := resource["type"].(string)
			if bundleType == "collection" || bundleType == "searchset" || bundleType == "transaction" {
				for _, entryResource := range lib.BundleResources(resource) {
					entries = append(entries, c.createEntryFromResource(entryResource))
				}
				// Don't add the Bundle itself - we've extracted its resources
				continue
			}
		}

		// Non-Bundle or non-unwrappable Bundle type (e.g., document, message)
		entries = append(entries, c.createEntryFromResource(resource))
	}

	// Return nil if no entries to send (e.g., empty bundles were unwrapped)
	if len(entries) == 0 {
		return nil
	}

	return map[string]any{
		"resourceType": "Bundle",
		"type":         "transaction",
		"entry":        entries,
	}
}

// createEntryFromResource creates a transaction Bundle entry from a parsed resource
func (c *FHIRClient) createEntryFromResource(resource map[string]any) map[string]any {
	resourceType := lib.ResourceType(resource)
	id := lib.ResourceID(resource)

	// Re-marshal the resource to JSON
	resourceJSON, err := json.Marshal(resource)
	if err != nil {
		// Fallback if marshal fails (shouldn't happen)
		return map[string]any{
			"request": map[string]any{
				"method": "POST",
				"url":    resourceType,
			},
		}
	}

	var request map[string]any
	if id != "" {
		// Use PUT to create/update with known ID
		request = map[string]any{
			"method": "PUT",
			"url":    fmt.Sprintf("%s/%s", resourceType, id),
		}
	} else {
		// Use POST to create with server-assigned ID
		request = map[string]any{
			"method": "POST",
			"url":    resourceType,
		}
	}

	return map[string]any{
		"resource": json.RawMessage(resourceJSON),
		"request":  request,
	}
}
