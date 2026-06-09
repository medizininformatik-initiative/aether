package services

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
)

const fhirJSONContentType = "application/fhir+json"

// ValidationClient handles communication with the FHIR validation service (mii-fhir-validator).
// Sends FHIR resources to POST /validateResource and returns OperationOutcome results.
type ValidationClient struct {
	baseURL    string
	httpClient *HTTPClient
	logger     *lib.Logger
}

// NewValidationClient creates a new validation client with the given base URL
func NewValidationClient(baseURL string, httpClient *HTTPClient, logger *lib.Logger) *ValidationClient {
	return &ValidationClient{
		baseURL:    baseURL,
		httpClient: httpClient,
		logger:     logger,
	}
}

// ValidationResult holds the parsed OperationOutcome from a validation response
type ValidationResult struct {
	OperationOutcome map[string]any
}

// HasErrors returns true if the OperationOutcome contains any issue with severity "error" or "fatal"
func (r *ValidationResult) HasErrors() bool {
	issues, ok := r.OperationOutcome["issue"].([]any)
	if !ok {
		return false
	}
	for _, issue := range issues {
		issueMap, ok := issue.(map[string]any)
		if !ok {
			continue
		}
		severity, _ := issueMap["severity"].(string)
		if severity == "error" || severity == "fatal" {
			return true
		}
	}
	return false
}

// ValidateResource sends a FHIR resource to the validation service and returns the OperationOutcome.
// Per contract: POST /validateResource with Content-Type: application/fhir+json
func (c *ValidationClient) ValidateResource(resource map[string]any) (*ValidationResult, error) {
	resourceType := lib.ResourceType(resource)
	resourceID := lib.ResourceID(resource)

	c.logger.Debug("Sending resource to validator",
		"resourceType", resourceType,
		"id", resourceID,
		"url", c.baseURL+"/validateResource")

	jsonBody, err := json.Marshal(resource)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal resource: %w", err)
	}

	url := c.baseURL + "/validateResource"

	resp, err := c.httpClient.Post(url, fhirJSONContentType, jsonBody)
	if err != nil {
		c.logger.Error("Validation HTTP request failed",
			"resourceType", resourceType,
			"id", resourceID,
			"error", err)
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			c.logger.Error("Failed to close response body", "error", err)
		}
	}()

	if resp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		errorType := lib.ClassifyHTTPError(resp.StatusCode)

		c.logger.Error("Validation service returned error",
			"status_code", resp.StatusCode,
			"status", resp.Status,
			"resourceType", resourceType,
			"id", resourceID,
			"error_body", string(bodyBytes),
			"retryable", errorType == models.ErrorTypeTransient)

		return nil, &ValidationError{
			StatusCode: resp.StatusCode,
			Status:     resp.Status,
			ErrorType:  errorType,
			Body:       string(bodyBytes),
		}
	}

	c.logger.Debug("Validation service responded successfully",
		"status_code", resp.StatusCode,
		"resourceType", resourceType,
		"id", resourceID)

	var outcome map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&outcome); err != nil {
		c.logger.Error("Failed to decode validation response",
			"resourceType", resourceType,
			"id", resourceID,
			"error", err)
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &ValidationResult{OperationOutcome: outcome}, nil
}

// ValidationError represents an error response from the validation service
type ValidationError struct {
	StatusCode int
	Status     string
	ErrorType  models.ErrorType
	Body       string
}

func (e *ValidationError) Error() string {
	msg := fmt.Sprintf("validation service error: HTTP %d: %s", e.StatusCode, e.Status)
	if e.Body != "" {
		msg += fmt.Sprintf("\nResponse: %s", e.Body)
	}
	return msg
}

// IsRetryable returns true if this error should be retried
func (e *ValidationError) IsRetryable() bool {
	return e.ErrorType == models.ErrorTypeTransient
}
