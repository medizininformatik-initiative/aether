package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
)

// FlattenerClient handles communication with the fhir-flattener service.
type FlattenerClient struct {
	baseURL    string
	httpClient *HTTPClient
	logger     *lib.Logger
}

// NewFlattenerClient creates a new flattener client. It wraps the shared
// HTTPClient so retry, TLS, and error classification follow the single shared
// path. If transport is non-nil it is applied for custom TLS; timeout defaults
// to 30 minutes when unset (flattening large datasets can be slow).
func NewFlattenerClient(config models.FlatteningConfig, retryConfig models.RetryConfig, transport *http.Transport, logger *lib.Logger) *FlattenerClient {
	timeout := config.Timeout
	if timeout == 0 {
		timeout = 30 * time.Minute
	}

	client := &http.Client{Timeout: timeout}
	if transport != nil {
		client.Transport = transport
	}

	return &FlattenerClient{
		baseURL: config.ServiceURL,
		httpClient: &HTTPClient{
			client:      client,
			retryConfig: lib.NewRetryConfigFromModel(retryConfig),
			logger:      logger,
		},
		logger: logger,
	}
}

// Flatten sends resources to the fhir-flattener service and returns CSV data.
// The API returns a CSV body WITHOUT header - the caller builds the header from
// the ViewDefinition. Transient errors (network + HTTP 5xx) are retried by the
// shared HTTPClient.
func (c *FlattenerClient) Flatten(viewDef models.ViewDefinition, resources []map[string]any) (string, error) {
	if len(resources) == 0 {
		c.logger.Debug("No resources to flatten", "viewDefinition", viewDef.Name)
		return "", nil
	}

	url := c.baseURL + "/fhir/ViewDefinition/$run"

	c.logger.Debug("Sending resources to flattener",
		"viewDefinition", viewDef.Name,
		"resourceCount", len(resources),
		"resourceType", viewDef.Resource,
		"url", url)

	request := models.NewFlatteningRequest(viewDef, resources)
	jsonBody, err := json.Marshal(request)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/fhir+json")
	req.Header.Set("Accept", "text/csv")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.logger.Error("Flattener HTTP request failed", "viewDefinition", viewDef.Name, "error", err)
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return "", classifyHTTPResponse("Flattener", resp)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	c.logger.Debug("Flattener returned CSV data", "viewDefinition", viewDef.Name, "bytes", len(bodyBytes))
	return string(bodyBytes), nil
}

// HealthCheck verifies the flattener service is available. Transient errors are
// retried by the shared HTTPClient.
func (c *FlattenerClient) HealthCheck() error {
	req, err := http.NewRequest("GET", c.baseURL+"/health", nil)
	if err != nil {
		return fmt.Errorf("failed to create health check request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("flattener health check failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return classifyHTTPResponse("Flattener", resp)
	}

	return nil
}

// Flattener is the seam the flattener client satisfies so pipeline steps can be
// tested against a fake adapter.
type Flattener interface {
	Flatten(viewDef models.ViewDefinition, resources []map[string]any) (string, error)
}

var _ Flattener = (*FlattenerClient)(nil)
