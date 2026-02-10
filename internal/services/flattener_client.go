package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
)

// FlattenerClient handles communication with the fhir-flattener service
type FlattenerClient struct {
	baseURL    string
	httpClient *HTTPClient
	logger     *lib.Logger
}

// NewFlattenerClient creates a new flattener client with the given base URL
func NewFlattenerClient(baseURL string, httpClient *HTTPClient, logger *lib.Logger) *FlattenerClient {
	return &FlattenerClient{
		baseURL:    baseURL,
		httpClient: httpClient,
		logger:     logger,
	}
}

// Flatten sends resources to the fhir-flattener service and returns CSV data
// The API returns CSV body WITHOUT header - caller must construct header from ViewDefinition
func (c *FlattenerClient) Flatten(viewDef models.ViewDefinition, resources []map[string]any) (string, error) {
	if len(resources) == 0 {
		c.logger.Debug("No resources to flatten", "viewDefinition", viewDef.Name)
		return "", nil
	}

	c.logger.Debug("Sending resources to flattener",
		"viewDefinition", viewDef.Name,
		"resourceCount", len(resources),
		"resourceType", viewDef.Resource)

	// Create the FHIR Parameters request
	request := models.NewFlatteningRequest(viewDef, resources)

	// Marshal to JSON
	jsonBody, err := json.Marshal(request)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	c.logger.Debug("Request body size", "bytes", len(jsonBody))

	// Create HTTP request
	url := c.baseURL + "/fhir/ViewDefinition/$run"
	req, err := http.NewRequest("POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/fhir+json")
	req.Header.Set("Accept", "text/csv")

	// Execute request via shared HTTPClient (handles retries, logging, backoff)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.logger.Error("Flattener request failed",
			"viewDefinition", viewDef.Name,
			"error", err)
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			c.logger.Error("Failed to close response body", "error", err)
		}
	}()

	// Check for HTTP error status
	if resp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		errorType := lib.ClassifyHTTPError(resp.StatusCode)

		c.logger.Error("Flattener service returned error",
			"status_code", resp.StatusCode,
			"viewDefinition", viewDef.Name,
			"error_body", string(bodyBytes))

		return "", &FlattenerError{
			StatusCode: resp.StatusCode,
			Status:     resp.Status,
			ErrorType:  errorType,
			Body:       string(bodyBytes),
		}
	}

	// Read CSV response body
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		c.logger.Error("Failed to read flattener response",
			"viewDefinition", viewDef.Name,
			"error", err)
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	c.logger.Debug("Flattener returned CSV data",
		"viewDefinition", viewDef.Name,
		"bytes", len(bodyBytes))

	return string(bodyBytes), nil
}

// HealthCheck verifies the flattener service is available
func (c *FlattenerClient) HealthCheck() error {
	resp, err := c.httpClient.Get(c.baseURL + "/health")
	if err != nil {
		return fmt.Errorf("flattener health check failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("flattener health check returned HTTP %d", resp.StatusCode)
	}

	return nil
}

// FlattenerError represents an error response from the fhir-flattener service
type FlattenerError struct {
	StatusCode int
	Status     string
	ErrorType  models.ErrorType
	Body       string
}

func (e *FlattenerError) Error() string {
	msg := fmt.Sprintf("Flattener service error: HTTP %d: %s", e.StatusCode, e.Status)

	if e.Body != "" {
		msg += fmt.Sprintf("\nResponse: %s", e.Body)
	}

	// Add helpful context for common errors
	switch e.StatusCode {
	case 400:
		msg += "\n\nPossible causes:"
		msg += "\n  - Invalid ViewDefinition structure"
		msg += "\n  - Malformed FHIR resources"
		msg += "\n  - Missing required fields in request"
	case 500:
		msg += "\n\nPossible causes:"
		msg += "\n  - ViewDefinition processing error"
		msg += "\n  - fhir-flattener service configuration issue"
	}

	return msg
}

// IsRetryable returns true if this error should be retried
func (e *FlattenerError) IsRetryable() bool {
	return e.ErrorType == models.ErrorTypeTransient
}
