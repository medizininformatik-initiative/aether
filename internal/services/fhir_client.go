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
	dump       *RequestDump
}

// DumpFailedRequestsTo makes the client write each rejected transaction bundle
// to dump. Pass nil to disable.
func (c *FHIRClient) DumpFailedRequestsTo(dump *RequestDump) {
	c.dump = dump
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
	stats := &FHIRUploadStats{}

	rest, err := c.sendFullBatches(filePath, reader, stats)
	if err != nil {
		return stats, err
	}

	if len(rest) > 0 {
		if err := c.flushBatch(filePath, rest, stats); err != nil {
			return stats, fmt.Errorf("failed to send final batch from %s: %w", filepath.Base(filePath), err)
		}
	}

	return stats, nil
}

// effectiveBatchSize gives the number of resources per transaction bundle.
func (c *FHIRClient) effectiveBatchSize() int {
	if c.batchSize <= 0 {
		return 100
	}
	return c.batchSize
}

// sendFullBatches reads the NDJSON stream and sends every complete batch. It
// returns the resources that remain after the last complete batch.
func (c *FHIRClient) sendFullBatches(filePath string, reader io.Reader, stats *FHIRUploadStats) ([]json.RawMessage, error) {
	batchSize := c.effectiveBatchSize()
	dec := json.NewDecoder(reader)
	var batch []json.RawMessage

	for {
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return batch, fmt.Errorf("error reading %s: %w", filepath.Base(filePath), err)
		}

		batch = append(batch, raw)

		if len(batch) >= batchSize {
			if err := c.flushBatch(filePath, batch, stats); err != nil {
				return nil, fmt.Errorf("failed to send batch from %s: %w", filepath.Base(filePath), err)
			}
			batch = nil
		}
	}

	return batch, nil
}

// flushBatch sends one batch and counts it in stats.
func (c *FHIRClient) flushBatch(filePath string, batch []json.RawMessage, stats *FHIRUploadStats) error {
	if err := c.sendBatch(batch, batchLabel(filePath, stats.BatchesSent+1)); err != nil {
		return err
	}
	stats.ResourcesUploaded += len(batch)
	stats.BatchesSent++
	return nil
}

// batchLabel names one batch of a file for a dump of a failed request.
func batchLabel(filePath string, batchNumber int) string {
	base := lib.GetUncompressedFilename(filepath.Base(filePath))
	return fmt.Sprintf("%s-batch-%d", strings.TrimSuffix(base, filepath.Ext(base)), batchNumber)
}

// sendBatch creates a transaction bundle and POSTs it to the FHIR server.
// label identifies the batch in a dump of a failed request.
func (c *FHIRClient) sendBatch(resources []json.RawMessage, label string) error {
	bundle := c.createTransactionBundle(resources)

	// Skip sending if no entries (e.g., empty bundles were unwrapped)
	if bundle == nil {
		return nil
	}

	req, jsonData, err := c.newBundleRequest(bundle)
	if err != nil {
		return err
	}

	c.logger.Debug("Sending FHIR transaction bundle",
		"url", req.URL.String(),
		"resource_count", len(resources),
		"bundle_size_bytes", len(jsonData))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return c.rejectedBundleError(resp, jsonData, label)
	}

	c.logger.Debug("FHIR transaction bundle sent successfully",
		"status", resp.StatusCode,
		"resource_count", len(resources))

	return nil
}

// newBundleRequest marshals the bundle and builds the authenticated POST
// request for it. It also returns the marshaled bundle for a dump.
func (c *FHIRClient) newBundleRequest(bundle map[string]any) (*http.Request, []byte, error) {
	jsonData, err := json.Marshal(bundle)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal bundle: %w", err)
	}

	url := strings.TrimSuffix(c.url, "/") + "/fhir"

	req, err := http.NewRequest("POST", url, bytes.NewReader(jsonData))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/fhir+json")
	req.Header.Set("Accept", "application/fhir+json")

	if err := c.addAuthHeader(req); err != nil {
		return nil, nil, fmt.Errorf("failed to add auth header: %w", err)
	}

	return req, jsonData, nil
}

// rejectedBundleError dumps the rejected bundle, if a dump is set, and gives
// the error for the server answer.
func (c *FHIRClient) rejectedBundleError(resp *http.Response, jsonData []byte, label string) error {
	body, _ := io.ReadAll(resp.Body)
	c.dump.Write(label, jsonData, resp.StatusCode, body)
	return &FHIRError{
		StatusCode: resp.StatusCode,
		Message:    string(body),
		ErrorType:  lib.ClassifyHTTPError(resp.StatusCode),
	}
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
