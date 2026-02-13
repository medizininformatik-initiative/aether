package models

import (
	"fmt"
	"net/url"
	"time"
)

// FlatteningConfig holds configuration for the fhir-flattener service
type FlatteningConfig struct {
	ServiceURL string        `yaml:"service_url" json:"service_url"` // URL to fhir-flattener service
	LookupPath string        `yaml:"lookup_path" json:"lookup_path"` // Path to flatten-lookup.json file
	Formats    []string      `yaml:"formats" json:"formats"`         // Output formats: ["csv"] for now
	Timeout    time.Duration `yaml:"timeout" json:"timeout"`         // Request timeout
}

// DefaultFlatteningConfig returns the default flattening configuration
func DefaultFlatteningConfig() FlatteningConfig {
	return FlatteningConfig{
		ServiceURL: "",
		LookupPath: "",
		Formats:    []string{"csv"},
		Timeout:    30 * time.Minute,
	}
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

// LookupTable represents the flatten-lookup.json structure for a single profile
type LookupTable struct {
	URL          string                   `json:"url"`          // Profile URL (e.g., https://www.medizininformatik-initiative.de/fhir/core/modul-person/StructureDefinition/Patient)
	ResourceType string                   `json:"resourceType"` // FHIR resource type (Patient, Condition, etc.)
	Elements     map[string]LookupElement `json:"elements"`     // elementId -> definition
}

// LookupElement represents a single element's flattening configuration
type LookupElement struct {
	Parent         string         `json:"parent,omitempty"`   // Parent element ID (for nested elements)
	ViewDefinition ViewDefSnippet `json:"viewDefinition"`     // The ViewDefinition snippet for this element
	Children       []string       `json:"children,omitempty"` // Child element IDs
}

// ViewDefSnippet represents the viewDefinition snippet within a lookup element
// This contains partial ViewDefinition data that will be merged into the final ViewDefinition
type ViewDefSnippet struct {
	ForEach       string             `json:"forEach,omitempty"`       // ForEach expression at viewDefinition level
	ForEachOrNull string             `json:"forEachOrNull,omitempty"` // ForEachOrNull expression at viewDefinition level
	Column        []ColumnDefinition `json:"column,omitempty"`        // Column definitions at viewDefinition level (for leaf elements)
	Select        []SelectClause     `json:"select,omitempty"`        // Select clauses for this element
}

// ViewDefinition represents a complete SQL-on-FHIR ViewDefinition
// See: https://sql-on-fhir.org/ig/StructureDefinition/ViewDefinition
type ViewDefinition struct {
	ResourceType string         `json:"resourceType"` // Always "https://sql-on-fhir.org/ig/StructureDefinition/ViewDefinition"
	Name         string         `json:"name"`         // ViewDefinition name (from attributeGroup.name)
	Status       string         `json:"status"`       // Status (always "draft")
	Resource     string         `json:"resource"`     // FHIR resource type from lookup (e.g., "Patient", "Condition")
	Select       []SelectClause `json:"select"`       // Selection clauses
}

// SelectClause represents a select clause in a ViewDefinition
type SelectClause struct {
	Column        []ColumnDefinition `json:"column,omitempty"`        // Column definitions
	Select        []SelectClause     `json:"select,omitempty"`        // Nested select clauses
	ForEach       string             `json:"forEach,omitempty"`       // ForEach expression
	ForEachOrNull string             `json:"forEachOrNull,omitempty"` // ForEachOrNull expression
}

// ColumnDefinition represents a column in a ViewDefinition select clause
type ColumnDefinition struct {
	Name        string `json:"name"`                  // Column name
	Path        string `json:"path,omitempty"`        // FHIRPath expression
	Type        string `json:"type,omitempty"`        // Column type (string, integer, etc.)
	Collection  bool   `json:"collection,omitempty"`  // Whether this is a collection
	Description string `json:"description,omitempty"` // Column description
}

// CRTDLDocument represents the CRTDL file structure
type CRTDLDocument struct {
	DataExtraction DataExtraction `json:"dataExtraction"`
}

// DataExtraction represents the dataExtraction section of a CRTDL document
type DataExtraction struct {
	AttributeGroups []AttributeGroup `json:"attributeGroups"`
}

// AttributeGroup represents an attributeGroup in the CRTDL dataExtraction
type AttributeGroup struct {
	ID             string      `json:"id,omitempty"`   // Optional ID
	Name           string      `json:"name"`           // Group name (used for CSV filename)
	GroupReference string      `json:"groupReference"` // Profile URL for resource matching
	Attributes     []Attribute `json:"attributes"`     // List of attributes to extract
}

// Attribute represents a single attribute within an attributeGroup
type Attribute struct {
	AttributeRef string `json:"attributeRef"` // Element ID reference to lookup
	MustHave     bool   `json:"mustHave"`     // Whether this attribute is required
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

// GetFixedIDColumn returns the fixed "id" column definition
func GetFixedIDColumn() ColumnDefinition {
	return ColumnDefinition{
		Name: "id",
		Path: "id",
	}
}

// GetFixedPatientColumn returns the fixed "patient" column definition for non-Patient resources
func GetFixedPatientColumn() ColumnDefinition {
	return ColumnDefinition{
		Name: "patient",
		Path: "subject.reference",
	}
}

// NewBaseViewDefinition creates a base ViewDefinition with required fields
func NewBaseViewDefinition(name string, resourceType string) ViewDefinition {
	return ViewDefinition{
		ResourceType: "https://sql-on-fhir.org/ig/StructureDefinition/ViewDefinition",
		Name:         name,
		Status:       "draft",
		Resource:     resourceType,
		Select:       []SelectClause{},
	}
}
