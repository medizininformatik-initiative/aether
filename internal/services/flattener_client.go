package services

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
)

// FlattenerClient handles communication with the fhir-flattener service
type FlattenerClient struct {
	baseURL     string
	httpClient  *http.Client
	retryConfig lib.RetryConfig
	timeout     time.Duration
	logger      *lib.Logger
}

// NewFlattenerClient creates a new flattener client with the given configuration.
// If transport is non-nil it is applied to the underlying http.Client for custom TLS.
func NewFlattenerClient(config models.FlatteningConfig, retryConfig models.RetryConfig, transport *http.Transport, logger *lib.Logger) *FlattenerClient {
	timeout := config.Timeout
	if timeout == 0 {
		timeout = 30 * time.Minute
	}

	client := &http.Client{
		Timeout: timeout,
	}
	if transport != nil {
		client.Transport = transport
	}

	return &FlattenerClient{
		baseURL:     config.ServiceURL,
		httpClient:  client,
		retryConfig: lib.NewRetryConfigFromModel(retryConfig),
		timeout:     timeout,
		logger:      logger,
	}
}

// Flatten sends resources to the fhir-flattener service and returns CSV data.
// The API returns CSV body WITHOUT header - caller must construct header from ViewDefinition.
// Retries on transient errors (network errors like EOF, connection reset, and HTTP 5xx).
func (c *FlattenerClient) Flatten(viewDef models.ViewDefinition, resources []map[string]any) (string, error) {
	if len(resources) == 0 {
		c.logger.Debug("No resources to flatten", "viewDefinition", viewDef.Name)
		return "", nil
	}

	c.logger.Debug("Sending resources to flattener",
		"viewDefinition", viewDef.Name,
		"resourceCount", len(resources),
		"resourceType", viewDef.Resource,
		"url", c.baseURL+"/fhir/ViewDefinition/$run")

	// Create the FHIR Parameters request
	request := models.NewFlatteningRequest(viewDef, resources)

	// Marshal JSON once — replayed on each retry attempt
	jsonBody, err := json.Marshal(request)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	c.logger.Debug("Request body size", "bytes", len(jsonBody))

	url := c.baseURL + "/fhir/ViewDefinition/$run"

	var result string
	retryErr := lib.ExecuteWithRetry(func() error {
		req, err := http.NewRequest("POST", url, bytes.NewReader(jsonBody))
		if err != nil {
			return fmt.Errorf("failed to create request: %w", err)
		}

		req.Header.Set("Content-Type", "application/fhir+json")
		req.Header.Set("Accept", "text/csv")

		startTime := time.Now()
		resp, err := c.httpClient.Do(req)
		duration := time.Since(startTime)

		if err != nil {
			c.logger.Error("Flattener HTTP request failed",
				"viewDefinition", viewDef.Name,
				"error", err,
				"duration", duration)
			return fmt.Errorf("request failed: %w", err)
		}
		defer func() {
			if closeErr := resp.Body.Close(); closeErr != nil {
				c.logger.Error("Failed to close response body", "error", closeErr)
			}
		}()

		c.logger.Debug("Flattener responded",
			"status_code", resp.StatusCode,
			"viewDefinition", viewDef.Name,
			"duration", duration)

		if resp.StatusCode >= 400 {
			bodyBytes, _ := io.ReadAll(resp.Body)
			errorType := lib.ClassifyHTTPError(resp.StatusCode)

			c.logger.Error("Flattener service returned error",
				"status_code", resp.StatusCode,
				"status", resp.Status,
				"viewDefinition", viewDef.Name,
				"error_body", string(bodyBytes))

			return &FlattenerError{
				StatusCode: resp.StatusCode,
				Status:     resp.Status,
				ErrorType:  errorType,
				Body:       string(bodyBytes),
			}
		}

		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			c.logger.Error("Failed to read flattener response",
				"viewDefinition", viewDef.Name,
				"error", err)
			return fmt.Errorf("failed to read response: %w", err)
		}

		c.logger.Debug("Flattener returned CSV data",
			"viewDefinition", viewDef.Name,
			"bytes", len(bodyBytes),
			"duration", duration)

		result = string(bodyBytes)
		return nil
	}, c.retryConfig, c.isRetryable)

	return result, retryErr
}

// HealthCheck verifies the flattener service is available.
// Retries on transient errors (network errors, HTTP 5xx).
func (c *FlattenerClient) HealthCheck() error {
	url := c.baseURL + "/health"

	return lib.ExecuteWithRetry(func() error {
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return fmt.Errorf("failed to create health check request: %w", err)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("flattener health check failed: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode >= 400 {
			errorType := lib.ClassifyHTTPError(resp.StatusCode)
			return &FlattenerError{
				StatusCode: resp.StatusCode,
				Status:     resp.Status,
				ErrorType:  errorType,
			}
		}

		return nil
	}, c.retryConfig, c.isRetryable)
}

// isRetryable checks if an error from the flattener service should be retried.
// Covers both HTTP-level transient errors (5xx, 408, 429) and network-level
// errors (EOF, connection reset, timeout).
func (c *FlattenerClient) isRetryable(err error) bool {
	var flattenerErr *FlattenerError
	if errors.As(err, &flattenerErr) {
		return flattenerErr.IsRetryable()
	}
	return lib.IsNetworkError(err)
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
