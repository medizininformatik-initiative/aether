package models

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
)

// CreateGroupConfig holds configuration for creating a new group
type CreateGroupConfig struct {
	GroupName string `json:"groupName,omitempty" yaml:"group_name,omitempty" mapstructure:"group_name"`
}

// GroupEnrichment defines attributes to add to a CRTDL group.
type GroupEnrichment struct {
	GroupReference    string                `json:"groupReference" yaml:"group_reference" mapstructure:"group_reference"`
	CreateIfNotExists *CreateGroupConfig    `json:"createIfNotExists,omitempty" yaml:"create_if_not_exists,omitempty" mapstructure:"create_if_not_exists"`
	AttributesToAdd   []EnrichmentAttribute `json:"attributesToAdd" yaml:"attributes_to_add" mapstructure:"attributes_to_add"`
}

// EnrichmentAttribute defines a single attribute to add during enrichment
type EnrichmentAttribute struct {
	AttributeRef string `json:"attributeRef" yaml:"attribute_ref" mapstructure:"attribute_ref"`
	MustHave     bool   `json:"mustHave" yaml:"must_have" mapstructure:"must_have"`
}

// ShouldCreateIfNotExists returns true if the group should be created when not found
func (e *GroupEnrichment) ShouldCreateIfNotExists() bool {
	return e.CreateIfNotExists != nil
}

// GetGroupName returns the group name from createIfNotExists
func (e *GroupEnrichment) GetGroupName() string {
	if e.CreateIfNotExists != nil {
		return e.CreateIfNotExists.GroupName
	}
	return ""
}

// Validate validates a GroupEnrichment configuration
func (e *GroupEnrichment) Validate() error {
	if e.GroupReference == "" {
		return fmt.Errorf("groupReference is required")
	}

	// When createIfNotExists is present, groupName is required
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

// Validate validates an EnrichmentAttribute
func (a *EnrichmentAttribute) Validate() error {
	if a.AttributeRef == "" {
		return fmt.Errorf("attributeRef is required")
	}
	return nil
}

// isCamelCase checks if a string is valid camelCase:
// - Starts with lowercase letter
// - No underscores (rules out snake_case)
// - No hyphens (rules out kebab-case)
// - Only letters and digits allowed
func isCamelCase(s string) bool {
	if len(s) == 0 {
		return false
	}
	// Must start with lowercase letter
	if !unicode.IsLower(rune(s[0])) {
		return false
	}
	// No underscores or hyphens allowed
	if strings.ContainsAny(s, "_-") {
		return false
	}
	return true
}

// validateJSONFieldNames checks that all keys in a JSON object are camelCase
func validateJSONFieldNames(data []byte, context string) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	for fieldName := range raw {
		if !isCamelCase(fieldName) {
			return fmt.Errorf("invalid field name '%s' in %s: JSON enrichment files must use camelCase (e.g., 'groupReference' not 'group_reference')", fieldName, context)
		}
	}
	return nil
}

// UnmarshalJSON validates field names are camelCase before unmarshaling
func (e *GroupEnrichment) UnmarshalJSON(data []byte) error {
	// Validate all field names are camelCase
	if err := validateJSONFieldNames(data, "GroupEnrichment"); err != nil {
		return err
	}

	// Standard unmarshal using type alias to avoid recursion
	type alias GroupEnrichment
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*e = GroupEnrichment(a)
	return nil
}

// UnmarshalJSON validates field names are camelCase before unmarshaling
func (c *CreateGroupConfig) UnmarshalJSON(data []byte) error {
	if err := validateJSONFieldNames(data, "createIfNotExists"); err != nil {
		return err
	}
	type alias CreateGroupConfig
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*c = CreateGroupConfig(a)
	return nil
}

// UnmarshalJSON validates field names are camelCase before unmarshaling
func (a *EnrichmentAttribute) UnmarshalJSON(data []byte) error {
	// Validate all field names are camelCase
	if err := validateJSONFieldNames(data, "EnrichmentAttribute"); err != nil {
		return err
	}

	type alias EnrichmentAttribute
	var al alias
	if err := json.Unmarshal(data, &al); err != nil {
		return err
	}
	*a = EnrichmentAttribute(al)
	return nil
}

// CRTDLPreprocessingConfig holds configuration for CRTDL preprocessing
type CRTDLPreprocessingConfig struct {
	Enabled     bool              `yaml:"enabled" json:"enabled" mapstructure:"enabled"`
	Enrichments []GroupEnrichment `yaml:"enrichments" json:"enrichments" mapstructure:"enrichments"`
}

// Validate validates the CRTDLPreprocessingConfig
func (c *CRTDLPreprocessingConfig) Validate() error {
	if !c.Enabled {
		return nil // Nothing to validate when disabled
	}

	for i, enrichment := range c.Enrichments {
		if err := enrichment.Validate(); err != nil {
			return fmt.Errorf("enrichments[%d]: %w", i, err)
		}
	}

	return nil
}
