package services

import (
	"github.com/medizininformatik-initiative/aether/internal/lib"
)

// DIMPClient handles communication with the DIMP pseudonymization service
// Per contracts/dimp-service.md
type DIMPClient struct {
	baseURL    string
	httpClient *HTTPClient
	logger     *lib.Logger
}

// NewDIMPClient creates a new DIMP client with the given base URL
func NewDIMPClient(baseURL string, httpClient *HTTPClient, logger *lib.Logger) *DIMPClient {
	return &DIMPClient{
		baseURL:    baseURL,
		httpClient: httpClient,
		logger:     logger,
	}
}

// Pseudonymize sends a FHIR resource to the DIMP service for pseudonymization
// Returns the pseudonymized resource or an error.
// Per contract: POST /fhir/$de-identify with single FHIR resource.
func (c *DIMPClient) Pseudonymize(resource map[string]any) (map[string]any, error) {
	resourceType := lib.ResourceType(resource)
	resourceID := lib.ResourceID(resource)

	// baseURL is the server root; the /fhir prefix is appended here.
	url := c.baseURL + "/fhir/$de-identify"

	c.logger.Debug("Sending resource to DIMP",
		"resourceType", resourceType,
		"id", resourceID,
		"url", url)

	var pseudonymized map[string]any
	err := c.httpClient.DoFHIRJSON(FHIRRequest{
		Method:      "POST",
		URL:         url,
		ContentType: "application/json",
		Body:        resource,
		Service:     "DIMP",
	}, &pseudonymized)
	if err != nil {
		c.logger.Error("DIMP request failed",
			"resourceType", resourceType,
			"id", resourceID,
			"error", err)
		return nil, err
	}

	if newID := lib.ResourceID(pseudonymized); resourceID != newID {
		c.logger.Debug("Resource ID pseudonymized",
			"resourceType", resourceType,
			"original_id", resourceID,
			"new_id", newID)
	}

	return pseudonymized, nil
}

// Pseudonymizer is the seam the DIMP client satisfies so pipeline steps can be
// tested against a fake adapter.
type Pseudonymizer interface {
	Pseudonymize(resource map[string]any) (map[string]any, error)
}

var _ Pseudonymizer = (*DIMPClient)(nil)

// MockPseudonymizer is a test double for Pseudonymizer. It records calls and,
// when PseudonymizeFunc is unset, echoes the input resource.
type MockPseudonymizer struct {
	PseudonymizeFunc func(map[string]any) (map[string]any, error)
	Calls            []map[string]any
}

var _ Pseudonymizer = (*MockPseudonymizer)(nil)

func (m *MockPseudonymizer) Pseudonymize(resource map[string]any) (map[string]any, error) {
	m.Calls = append(m.Calls, resource)
	if m.PseudonymizeFunc != nil {
		return m.PseudonymizeFunc(resource)
	}
	return resource, nil
}
