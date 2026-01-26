package models

import (
	"encoding/json"
	"fmt"
	"strings"
)

// CRTDLPreprocessingConfig holds configuration for CRTDL enrichment.
// Supports two mutually exclusive configuration methods:
// 1. EnrichmentsPath - external JSON file containing enrichment rules
// 2. Enrichments - inline rules in aether.yaml
// If both are provided, validation returns an error (explicit choice required).
type CRTDLPreprocessingConfig struct {
	Enabled         bool              `yaml:"enabled" json:"enabled" mapstructure:"enabled"`
	EnrichmentsPath string            `yaml:"enrichments_path" json:"enrichments_path" mapstructure:"enrichments_path"` // Path to JSON file
	Enrichments     []GroupEnrichment `yaml:"enrichments" json:"enrichments" mapstructure:"enrichments"`                // Inline rules
}

// CreateGroupConfig holds configuration for creating a new group when it doesn't exist.
type CreateGroupConfig struct {
	GroupName string `json:"groupName,omitempty" yaml:"group_name,omitempty" mapstructure:"group_name"`
}

// GroupEnrichment defines attributes to add to a CRTDL group.
type GroupEnrichment struct {
	GroupReference    string                `json:"groupReference" yaml:"group_reference" mapstructure:"group_reference"`
	CreateIfNotExists *CreateGroupConfig    `json:"createIfNotExists,omitempty" yaml:"create_if_not_exists,omitempty" mapstructure:"create_if_not_exists"`
	AttributesToAdd   []EnrichmentAttribute `json:"attributesToAdd" yaml:"attributes_to_add" mapstructure:"attributes_to_add"`
}

// ShouldCreateIfNotExists returns true if a new group should be created when not found.
func (e *GroupEnrichment) ShouldCreateIfNotExists() bool {
	return e.CreateIfNotExists != nil
}

// GetGroupName returns the group name to use when creating a new group.
func (e *GroupEnrichment) GetGroupName() string {
	if e.CreateIfNotExists != nil {
		return e.CreateIfNotExists.GroupName
	}
	return ""
}

// EnrichmentAttribute represents an attribute to add during CRTDL preprocessing.
type EnrichmentAttribute struct {
	AttributeRef string   `json:"attributeRef" yaml:"attribute_ref" mapstructure:"attribute_ref"`
	MustHave     bool     `json:"mustHave" yaml:"must_have" mapstructure:"must_have"`
	LinkedGroups []string `json:"linkedGroups,omitempty" yaml:"linked_groups,omitempty" mapstructure:"linked_groups"` // Profile URLs → resolved to IDs
}

// DefaultCRTDLPreprocessingConfig returns the default CRTDL preprocessing configuration.
// Preprocessing is disabled by default.
func DefaultCRTDLPreprocessingConfig() CRTDLPreprocessingConfig {
	return CRTDLPreprocessingConfig{
		Enabled:         false,
		EnrichmentsPath: "",
		Enrichments:     nil,
	}
}

// Validate checks if the CRTDLPreprocessingConfig is valid when preprocessing is enabled.
func (c *CRTDLPreprocessingConfig) Validate() error {
	if !c.Enabled {
		return nil // No validation needed when disabled
	}

	// Check for mutually exclusive configuration
	hasPath := c.EnrichmentsPath != ""
	hasInline := len(c.Enrichments) > 0

	if hasPath && hasInline {
		return fmt.Errorf("crtdl_preprocessing: cannot specify both enrichments_path and enrichments; choose one")
	}

	if !hasPath && !hasInline {
		return fmt.Errorf("crtdl_preprocessing: enabled but no enrichments configured; specify enrichments_path or enrichments")
	}

	// Validate inline enrichments if provided
	if hasInline {
		for i, enrichment := range c.Enrichments {
			if err := enrichment.Validate(); err != nil {
				return fmt.Errorf("crtdl_preprocessing.enrichments[%d]: %w", i, err)
			}
		}
	}

	return nil
}

// Validate checks if the GroupEnrichment is valid.
func (e *GroupEnrichment) Validate() error {
	if e.GroupReference == "" {
		return fmt.Errorf("groupReference is required")
	}

	if e.CreateIfNotExists != nil && e.CreateIfNotExists.GroupName == "" {
		return fmt.Errorf("groupName is required in createIfNotExists")
	}

	if len(e.AttributesToAdd) == 0 {
		return fmt.Errorf("attributesToAdd must contain at least one attribute")
	}

	for i, attr := range e.AttributesToAdd {
		if err := attr.Validate(); err != nil {
			return fmt.Errorf("attributesToAdd[%d]: %w", i, err)
		}
	}

	return nil
}

// Validate checks if the EnrichmentAttribute is valid.
func (a *EnrichmentAttribute) Validate() error {
	if a.AttributeRef == "" {
		return fmt.Errorf("attributeRef is required")
	}

	return nil
}

// findUnknownFields returns field names from rawMap that are not in knownFields.
func findUnknownFields(rawMap map[string]json.RawMessage, knownFields map[string]bool) []string {
	var unknowns []string
	for key := range rawMap {
		if !knownFields[key] {
			unknowns = append(unknowns, key)
		}
	}
	return unknowns
}

// deriveGroupNameFromURL extracts the last path segment from a profile URL.
// e.g. "https://example.org/fhir/StructureDefinition/PatientPseudonymisiert" → "PatientPseudonymisiert"
func deriveGroupNameFromURL(profileURL string) string {
	parts := strings.Split(profileURL, "/")
	for i := len(parts) - 1; i >= 0; i-- {
		if parts[i] != "" {
			return parts[i]
		}
	}
	return ""
}

// validateJSONFieldNames checks that all field names in JSON data follow camelCase convention.
// Returns an error listing any fields that use snake_case or kebab-case instead of camelCase.
func validateJSONFieldNames(data []byte, context string) error {
	var rawMap map[string]json.RawMessage
	if err := json.Unmarshal(data, &rawMap); err != nil {
		return err
	}

	var invalidFields []string
	for key := range rawMap {
		if strings.Contains(key, "_") || strings.Contains(key, "-") {
			invalidFields = append(invalidFields, key)
		}
	}

	if len(invalidFields) > 0 {
		return fmt.Errorf("%s: JSON uses snake_case/kebab-case fields %v; expected camelCase (e.g., 'groupReference' not 'group_reference' or 'group-reference')",
			context, invalidFields)
	}

	return nil
}

// UnmarshalJSON validates field names and supports the addGroupIfNotExists shorthand.
// Known fields: groupReference, createIfNotExists, addGroupIfNotExists, attributesToAdd.
// Unknown fields are rejected to prevent silent misconfiguration.
func (e *GroupEnrichment) UnmarshalJSON(data []byte) error {
	if err := validateJSONFieldNames(data, "GroupEnrichment"); err != nil {
		return err
	}

	var rawMap map[string]json.RawMessage
	if err := json.Unmarshal(data, &rawMap); err != nil {
		return err
	}

	knownFields := map[string]bool{
		"groupReference":      true,
		"createIfNotExists":   true,
		"addGroupIfNotExists": true,
		"attributesToAdd":     true,
	}
	if unknowns := findUnknownFields(rawMap, knownFields); len(unknowns) > 0 {
		return fmt.Errorf("GroupEnrichment: unknown fields %v; known fields are: groupReference, createIfNotExists, addGroupIfNotExists, attributesToAdd", unknowns)
	}

	addGroupRaw, hasAddGroup := rawMap["addGroupIfNotExists"]
	_, hasCreate := rawMap["createIfNotExists"]
	if hasAddGroup && hasCreate {
		return fmt.Errorf("GroupEnrichment: cannot specify both 'addGroupIfNotExists' and 'createIfNotExists'; use one or the other")
	}

	// Standard unmarshal (excludes the addGroupIfNotExists shorthand)
	type Alias GroupEnrichment
	aux := &struct {
		*Alias
	}{
		Alias: (*Alias)(e),
	}
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}

	// Handle addGroupIfNotExists: true → auto-populate CreateIfNotExists
	if hasAddGroup {
		var addGroup bool
		if err := json.Unmarshal(addGroupRaw, &addGroup); err != nil {
			return fmt.Errorf("GroupEnrichment: 'addGroupIfNotExists' must be a boolean, got: %s", string(addGroupRaw))
		}
		if addGroup {
			e.CreateIfNotExists = &CreateGroupConfig{
				GroupName: deriveGroupNameFromURL(e.GroupReference),
			}
		}
	}

	return nil
}

// UnmarshalJSON validates field names when unmarshaling CreateGroupConfig from JSON.
func (c *CreateGroupConfig) UnmarshalJSON(data []byte) error {
	if err := validateJSONFieldNames(data, "CreateGroupConfig"); err != nil {
		return err
	}

	var rawMap map[string]json.RawMessage
	if err := json.Unmarshal(data, &rawMap); err != nil {
		return err
	}

	knownFields := map[string]bool{"groupName": true}
	if unknowns := findUnknownFields(rawMap, knownFields); len(unknowns) > 0 {
		return fmt.Errorf("CreateGroupConfig: unknown fields %v; known fields are: groupName", unknowns)
	}

	type Alias CreateGroupConfig
	aux := &struct {
		*Alias
	}{
		Alias: (*Alias)(c),
	}
	return json.Unmarshal(data, aux)
}

// UnmarshalJSON validates field names when unmarshaling EnrichmentAttribute from JSON.
func (a *EnrichmentAttribute) UnmarshalJSON(data []byte) error {
	if err := validateJSONFieldNames(data, "EnrichmentAttribute"); err != nil {
		return err
	}

	var rawMap map[string]json.RawMessage
	if err := json.Unmarshal(data, &rawMap); err != nil {
		return err
	}

	knownFields := map[string]bool{
		"attributeRef": true,
		"mustHave":     true,
		"linkedGroups": true,
	}
	if unknowns := findUnknownFields(rawMap, knownFields); len(unknowns) > 0 {
		return fmt.Errorf("EnrichmentAttribute: unknown fields %v; known fields are: attributeRef, mustHave, linkedGroups", unknowns)
	}

	type Alias EnrichmentAttribute
	aux := &struct {
		*Alias
	}{
		Alias: (*Alias)(a),
	}
	return json.Unmarshal(data, aux)
}
