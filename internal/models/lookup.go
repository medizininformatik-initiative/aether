package models

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

// HasForEach tells if the snippet iterates with forEach or forEachOrNull.
func (s ViewDefSnippet) HasForEach() bool {
	return s.ForEach != "" || s.ForEachOrNull != ""
}
