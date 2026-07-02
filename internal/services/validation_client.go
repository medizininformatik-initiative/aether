package services

import (
	"github.com/medizininformatik-initiative/aether/internal/lib"
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
	url := c.baseURL + "/validateResource"

	c.logger.Debug("Sending resource to validator",
		"resourceType", resourceType,
		"id", resourceID,
		"url", url)

	var outcome map[string]any
	err := c.httpClient.DoFHIRJSON(FHIRRequest{
		Method:      "POST",
		URL:         url,
		ContentType: fhirJSONContentType,
		Body:        resource,
		Service:     "validation",
	}, &outcome)
	if err != nil {
		c.logger.Error("Validation request failed",
			"resourceType", resourceType,
			"id", resourceID,
			"error", err)
		return nil, err
	}

	return &ValidationResult{OperationOutcome: outcome}, nil
}

// ResourceValidator is the seam the validation client satisfies so pipeline
// steps can be tested against a fake adapter.
type ResourceValidator interface {
	ValidateResource(resource map[string]any) (*ValidationResult, error)
}

var _ ResourceValidator = (*ValidationClient)(nil)

// MockResourceValidator is a test double for ResourceValidator. It records
// calls and, when ValidateFunc is unset, returns an issue-free OperationOutcome.
type MockResourceValidator struct {
	ValidateFunc func(map[string]any) (*ValidationResult, error)
	Calls        []map[string]any
}

var _ ResourceValidator = (*MockResourceValidator)(nil)

func (m *MockResourceValidator) ValidateResource(resource map[string]any) (*ValidationResult, error) {
	m.Calls = append(m.Calls, resource)
	if m.ValidateFunc != nil {
		return m.ValidateFunc(resource)
	}
	return &ValidationResult{OperationOutcome: map[string]any{"resourceType": "OperationOutcome"}}, nil
}
