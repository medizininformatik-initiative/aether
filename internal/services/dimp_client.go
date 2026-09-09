package services

import (
	"encoding/base64"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
)

// DIMPClient handles communication with the DIMP pseudonymization service
// Per contracts/dimp-service.md
type DIMPClient struct {
	baseURL string
	auth    models.AuthConfig
	// configParameter is the immutable "config" part of the Parameters
	// resource, built once and sent with every request. It is nil when no
	// anonymization configuration is configured.
	configParameter map[string]any
	httpClient      *HTTPClient
	logger          *lib.Logger
}

// NewDIMPClient creates a new DIMP client from the DIMP service config.
// anonymizationConfig is the raw content of the anonymization YAML. The client
// sends it with each request, so the service does not need a restart for
// configuration changes. An empty anonymizationConfig makes the service use its
// own anonymization file.
func NewDIMPClient(config models.DIMPConfig, anonymizationConfig []byte, httpClient *HTTPClient, logger *lib.Logger) *DIMPClient {
	client := &DIMPClient{
		baseURL:    config.URL,
		auth:       config.Auth,
		httpClient: httpClient,
		logger:     logger,
	}
	if len(anonymizationConfig) > 0 {
		client.configParameter = map[string]any{
			"name": "config",
			"valueAttachment": map[string]any{
				"contentType": "application/yaml",
				"data":        base64.StdEncoding.EncodeToString(anonymizationConfig),
			},
		}
	}
	return client
}

// Pseudonymize sends a FHIR resource to the DIMP service for pseudonymization
// Returns the pseudonymized resource or an error.
// Per contract: POST /fhir/$de-identify with a single FHIR resource, or with a
// Parameters resource that holds a "config" Attachment part (base64
// anonymization YAML) and a "resource" part when an anonymization
// configuration is configured.
func (c *DIMPClient) Pseudonymize(resource map[string]any) (map[string]any, error) {
	resourceType := lib.ResourceType(resource)
	resourceID := lib.ResourceID(resource)

	// baseURL is the server root; the /fhir prefix is appended here.
	url := c.baseURL + "/fhir/$de-identify"

	c.logger.Debug("Sending resource to DIMP",
		"resourceType", resourceType,
		"id", resourceID,
		"url", url)

	contentType := "application/json"
	body := any(resource)
	if c.configParameter != nil {
		contentType = "application/fhir+json"
		body = map[string]any{
			"resourceType": "Parameters",
			"parameter": []any{
				c.configParameter,
				map[string]any{
					"name":     "resource",
					"resource": resource,
				},
			},
		}
	}

	var pseudonymized map[string]any
	err := c.httpClient.DoFHIRJSON(FHIRRequest{
		Method:      "POST",
		URL:         url,
		ContentType: contentType,
		Body:        body,
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
