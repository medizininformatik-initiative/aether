package services

import (
	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
)

// DIMPClient handles communication with the DIMP pseudonymization service
// Per contracts/dimp-service.md
type DIMPClient struct {
	baseURL    string
	auth       models.AuthConfig
	httpClient *HTTPClient
	logger     *lib.Logger
}

// NewDIMPClient creates a new DIMP client from the DIMP service config
func NewDIMPClient(config models.DIMPConfig, httpClient *HTTPClient, logger *lib.Logger) *DIMPClient {
	return &DIMPClient{
		baseURL:    config.URL,
		auth:       config.Auth,
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
		Auth:        c.auth,
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

// DIMPProcessor is the seam the DIMP client satisfies so pipeline steps can be
// tested against a fake adapter. DIMP is De-identification, Minimization, and
// Pseudonymization: the Pseudonymize method drives the full $de-identify
// operation, of which pseudonymization is only one part.
type DIMPProcessor interface {
	Pseudonymize(resource map[string]any) (map[string]any, error)
}

var _ DIMPProcessor = (*DIMPClient)(nil)
