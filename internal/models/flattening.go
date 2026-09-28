package models

import (
	"fmt"
	"net/url"
	"time"
)

// AttributeGroupNamingSystem is the FHIR NamingSystem URL used by Torch in Provenance
// resources to identify which CRTDL attribute group a clinical resource belongs to.
const AttributeGroupNamingSystem = "https://www.medizininformatik-initiative.de/fhir/fdpg/NamingSystem/attribute_group"

// ProvenanceIndex maps resource references ("ResourceType/id") to CRTDL attribute group IDs.
// A resource can belong to multiple attribute groups (e.g. two Procedure groups with different
// attribute selections), so each reference maps to a slice of group IDs.
type ProvenanceIndex map[string][]string

// FlatteningConfig holds configuration for the fhir-flattener service
type FlatteningConfig struct {
	ServiceURL  string        `yaml:"service_url" json:"service_url" mapstructure:"service_url"`       // URL to fhir-flattener service
	LookupPath  string        `yaml:"lookup_path" json:"lookup_path" mapstructure:"lookup_path"`       // Path to flatten-lookup.json file
	Formats     []string      `yaml:"formats" json:"formats" mapstructure:"formats"`                   // Output formats: ["csv"] for now
	Timeout     time.Duration `yaml:"timeout" json:"timeout" mapstructure:"timeout"`                   // Request timeout
	BatchSizeMB int           `yaml:"batch_size_mb" json:"batch_size_mb" mapstructure:"batch_size_mb"` // Total memory budget in MB for batched streaming (default 500)
}

// DefaultFlatteningConfig returns the default flattening configuration
func DefaultFlatteningConfig() FlatteningConfig {
	return FlatteningConfig{
		ServiceURL:  "",
		LookupPath:  "",
		Formats:     []string{"csv"},
		Timeout:     30 * time.Minute,
		BatchSizeMB: 500,
	}
}

// GetBatchSizeBytes returns the total memory budget in bytes, defaulting to 500MB if not set
func (c *FlatteningConfig) GetBatchSizeBytes() int {
	if c.BatchSizeMB <= 0 {
		return 500 * 1024 * 1024
	}
	return c.BatchSizeMB * 1024 * 1024
}

// Validate checks if the FlatteningConfig is valid when flattening is enabled
func (c *FlatteningConfig) Validate() error {
	if c.ServiceURL == "" {
		return fmt.Errorf("flattening service_url is required")
	}

	parsedURL, err := url.Parse(c.ServiceURL)
	if err != nil {
		return fmt.Errorf("invalid flattening service_url: %w", err)
	}

	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return fmt.Errorf("invalid flattening service_url: must use http or https scheme, got '%s'", parsedURL.Scheme)
	}

	if c.LookupPath == "" {
		return fmt.Errorf("flattening lookup_path is required")
	}

	if len(c.Formats) == 0 {
		return fmt.Errorf("flattening formats must contain at least one format")
	}

	for _, format := range c.Formats {
		if format != "csv" {
			return fmt.Errorf("invalid flattening format: %s (only 'csv' is supported)", format)
		}
	}

	if c.Timeout <= 0 {
		return fmt.Errorf("flattening timeout must be > 0, got %s", c.Timeout)
	}

	return nil
}

// FlatteningRequest represents the FHIR Parameters request body sent to fhir-flattener
type FlatteningRequest struct {
	ResourceType string                `json:"resourceType"` // Always "Parameters"
	Parameter    []FlatteningParameter `json:"parameter"`
}

// FlatteningParameter represents a parameter in the FlatteningRequest
type FlatteningParameter struct {
	Name     string `json:"name"`               // "viewDefinition" or "resources"
	Resource any    `json:"resource,omitempty"` // The actual resource (ViewDefinition or FHIR resource)
}

// NewFlatteningRequest creates a new FlatteningRequest with the given ViewDefinition and resources
func NewFlatteningRequest(viewDef ViewDefinition, resources []map[string]any) FlatteningRequest {
	params := make([]FlatteningParameter, 0, len(resources)+1)

	// Add ViewDefinition as first parameter
	params = append(params, FlatteningParameter{
		Name:     "viewDefinition",
		Resource: viewDef,
	})

	// Add each resource as a "resources" parameter
	for _, resource := range resources {
		params = append(params, FlatteningParameter{
			Name:     "resources",
			Resource: resource,
		})
	}

	return FlatteningRequest{
		ResourceType: "Parameters",
		Parameter:    params,
	}
}
