package services

import (
	"encoding/base64"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
)

// DIMPV3Client talks to the experimental v3alpha1 endpoint of the DIMP
// service. The endpoint receives the anonymization configuration with each
// request, so the service does not need a restart for configuration changes.
type DIMPV3Client struct {
	baseURL string
	auth    models.AuthConfig
	// configParameter is the immutable "config" part of the Parameters
	// resource, built once and sent with every request.
	configParameter map[string]any
	httpClient      *HTTPClient
	logger          *lib.Logger
}

// NewDIMPV3Client creates a DIMP client for the experimental v3alpha1
// endpoint. anonymizationConfig is the raw content of the anonymization YAML.
func NewDIMPV3Client(config models.DIMPConfig, anonymizationConfig []byte, httpClient *HTTPClient, logger *lib.Logger) *DIMPV3Client {
	return &DIMPV3Client{
		baseURL: config.URL,
		auth:    config.Auth,
		configParameter: map[string]any{
			"name": "config",
			"valueAttachment": map[string]any{
				"contentType": "application/yaml",
				"data":        base64.StdEncoding.EncodeToString(anonymizationConfig),
			},
		},
		httpClient: httpClient,
		logger:     logger,
	}
}

// Pseudonymize sends a FHIR resource to the v3alpha1 endpoint of the DIMP
// service and returns the de-identified resource.
// Wire contract: POST /v3alpha1/fhir/$de-identify with a FHIR Parameters
// resource that holds a "config" Attachment part (base64 anonymization YAML)
// and a "resource" part (the resource to de-identify).
func (c *DIMPV3Client) Pseudonymize(resource map[string]any) (map[string]any, error) {
	resourceType := lib.ResourceType(resource)
	resourceID := lib.ResourceID(resource)

	url := c.baseURL + "/v3alpha1/fhir/$de-identify"

	c.logger.Debug("Sending resource to DIMP (v3alpha1)",
		"resourceType", resourceType,
		"id", resourceID,
		"url", url)

	parameters := map[string]any{
		"resourceType": "Parameters",
		"parameter": []any{
			c.configParameter,
			map[string]any{
				"name":     "resource",
				"resource": resource,
			},
		},
	}

	var pseudonymized map[string]any
	err := c.httpClient.DoFHIRJSON(FHIRRequest{
		Method:      "POST",
		URL:         url,
		ContentType: "application/fhir+json",
		Body:        parameters,
		Auth:        c.auth,
		Service:     "DIMP",
	}, &pseudonymized)
	if err != nil {
		c.logger.Error("DIMP v3alpha1 request failed",
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

var _ DIMPProcessor = (*DIMPV3Client)(nil)
