package services

import (
	"fmt"
	"slices"

	"github.com/medizininformatik-initiative/aether/internal/models"
)

// ViewDefinitionBuilder constructs ViewDefinitions from CRTDL groups and lookup tables
type ViewDefinitionBuilder struct {
	lookupTables []models.LookupTable
}

// NewViewDefinitionBuilder creates a new ViewDefinitionBuilder with the given lookup tables
func NewViewDefinitionBuilder(tables []models.LookupTable) *ViewDefinitionBuilder {
	return &ViewDefinitionBuilder{
		lookupTables: tables,
	}
}

// BuildViewDefinition creates a complete ViewDefinition for an attributeGroup
// Implements the algorithm from test.py:
// 1. Create base ViewDefinition with metadata
// 2. For each attribute, look up element and resolve children
// 3. Add fixed id/patient columns at front
func (b *ViewDefinitionBuilder) BuildViewDefinition(group models.AttributeGroup) (*models.ViewDefinition, error) {
	// Find the matching lookup table by groupReference
	lookup := GetProfileLookup(b.lookupTables, group.GroupReference)
	if lookup == nil {
		return nil, fmt.Errorf("no lookup table found for profile: %s", group.GroupReference)
	}

	// Create base ViewDefinition
	viewDef := models.NewBaseViewDefinition(group.Name, lookup.ResourceType)

	// Fixed columns come first
	selectClauses := []models.SelectClause{{Column: b.buildFixedColumns(lookup.ResourceType)}}

	// Build set of attribute refs for fast lookup (used to detect overlapping parent-child)
	attrRefSet := make(map[string]bool)
	for _, attr := range group.Attributes {
		attrRefSet[attr.AttributeRef] = true
	}

	// Process each attribute in the group, skipping children whose parent is already in the list
	for _, attr := range group.Attributes {
		// Skip if this element's parent (or any ancestor) is already in the attribute list
		// Parent's downward traversal will include this child automatically
		if isParentInAttributeList(lookup, attr.AttributeRef, attrRefSet) {
			continue
		}

		attrSelects, err := b.buildAttributeSelect(lookup, attr.AttributeRef)
		if err != nil {
			// Log warning but continue - some attributes might not have lookup entries
			continue
		}
		selectClauses = append(selectClauses, attrSelects...)
	}

	viewDef.Select = selectClauses
	return &viewDef, nil
}

// isParentInAttributeList checks if the element's parent (or any ancestor) is in the attribute list.
// NormalizeLookupTables derives Parent links at load time, so the parent chain is complete here.
// This is used to avoid duplicates when both a parent and its children are specified in the CRTDL.
// When a parent is in the list, its downward traversal includes all children automatically,
// so the children should be skipped to avoid duplicates.
func isParentInAttributeList(lookup *models.LookupTable, elementID string, attrRefs map[string]bool) bool {
	element := GetElement(lookup, elementID)
	if element == nil {
		return false
	}
	if element.Parent == "" {
		return false
	}
	// Check if parent is in the attribute list
	if attrRefs[element.Parent] {
		return true
	}
	// Recursively check grandparent (handle multi-level hierarchy)
	return isParentInAttributeList(lookup, element.Parent, attrRefs)
}

// buildFixedColumns creates the fixed columns (id, and optionally patient) for a ViewDefinition.
// The patient column path varies by resource type because FHIR R4 Patient compartment resources
// reference patients through different element names (subject, patient, beneficiary, etc.).
// Non-compartment resources (e.g. Organization) have no patient column at all.
func (b *ViewDefinitionBuilder) buildFixedColumns(resourceType string) []models.ColumnDefinition {
	columns := []models.ColumnDefinition{
		models.GetFixedIDColumn(),
	}

	if path, ok := models.GetPatientReferencePath(resourceType); ok {
		columns = append(columns, models.ColumnDefinition{
			Name: "patient",
			Path: path,
		})
	}

	return columns
}

// buildAttributeSelect creates the select clauses for a single attribute
func (b *ViewDefinitionBuilder) buildAttributeSelect(lookup *models.LookupTable, attributeRef string) ([]models.SelectClause, error) {
	element := GetElement(lookup, attributeRef)
	if element == nil {
		return nil, fmt.Errorf("element not found: %s", attributeRef)
	}

	// The ancestors only give their forEach context. Their other children are not resolved,
	// so sibling attributes that share an ancestor do not change the output of each other.
	return b.wrapInAncestors(lookup, element.Parent, b.resolveWithChildren(lookup, element)), nil
}

// resolveWithChildren recursively resolves an element and its children
// This matches the Python reference implementation (test.py lines 52-57)
func (b *ViewDefinitionBuilder) resolveWithChildren(lookup *models.LookupTable, element *models.LookupElement) []models.SelectClause {
	snippet := element.ViewDefinition
	if len(element.Children) == 0 {
		return leafSelects(snippet)
	}

	var childSelects []models.SelectClause
	for _, childID := range element.Children {
		if childElement := GetElement(lookup, childID); childElement != nil {
			childSelects = append(childSelects, b.resolveWithChildren(lookup, childElement)...)
		}
	}

	// If element has root forEach, create wrapper SelectClause with children inside
	if snippet.HasForEach() {
		wrapper := viewDefSnippetToSelectClause(snippet)
		wrapper.Select = append(wrapper.Select, childSelects...)
		return []models.SelectClause{wrapper}
	}

	// No root forEach - include element's own selects plus children
	result := make([]models.SelectClause, 0, len(snippet.Select)+len(childSelects))
	result = append(result, snippet.Select...)
	result = append(result, childSelects...)
	return result
}

// leafSelects converts the snippet of an element without children to select clauses.
// A root forEach or a root Column makes the snippet one select clause of its own.
func leafSelects(snippet models.ViewDefSnippet) []models.SelectClause {
	if snippet.HasForEach() || len(snippet.Column) > 0 {
		return []models.SelectClause{viewDefSnippetToSelectClause(snippet)}
	}
	return snippet.Select
}

// wrapInAncestors walks up the parent chain starting at parentID and wraps
// selects in each ancestor's forEach context. Placeholder ancestors (no root
// forEach and no select-level forEach) are passed through unchanged so the
// selects bubble up to the next ancestor that provides context.
func (b *ViewDefinitionBuilder) wrapInAncestors(lookup *models.LookupTable, parentID string, selects []models.SelectClause) []models.SelectClause {
	if parentID == "" {
		return selects
	}
	parent := GetElement(lookup, parentID)
	if parent == nil {
		return selects
	}

	return b.wrapInAncestors(lookup, parent.Parent, wrapInParentContext(parent, selects))
}

// wrapInParentContext wraps selects in the forEach context of parent. A root forEach
// comes first. Else the first select-level forEach of parent gives the context.
// A parent without a forEach returns selects unchanged.
func wrapInParentContext(parent *models.LookupElement, selects []models.SelectClause) []models.SelectClause {
	snippet := parent.ViewDefinition
	if snippet.HasForEach() {
		return []models.SelectClause{{
			ForEach:       snippet.ForEach,
			ForEachOrNull: snippet.ForEachOrNull,
			Select:        selects,
		}}
	}
	for _, ps := range snippet.Select {
		if ps.HasForEach() {
			ps.Column = slices.Clone(ps.Column)
			ps.Select = selects
			return []models.SelectClause{ps}
		}
	}
	return selects
}

// viewDefSnippetToSelectClause converts a ViewDefSnippet into a SelectClause.
// This handles the case where forEach/forEachOrNull is at the viewDefinition level
// in the lookup JSON, rather than inside a select clause.
// Also supports Column at the viewDefinition level (for leaf elements in hierarchies).
func viewDefSnippetToSelectClause(snippet models.ViewDefSnippet) models.SelectClause {
	return models.SelectClause{
		ForEach:       snippet.ForEach,
		ForEachOrNull: snippet.ForEachOrNull,
		Column:        snippet.Column,
		Select:        snippet.Select,
	}
}

// ExtractColumnNames traverses a ViewDefinition and extracts all column names in order.
// This is used to construct the CSV header and to map the flattener's NDJSON
// rows to columns by name.
func ExtractColumnNames(viewDef models.ViewDefinition) []string {
	var names []string
	for _, sel := range viewDef.Select {
		names = append(names, extractColumnNamesFromSelect(sel)...)
	}
	return names
}

// extractColumnNamesFromSelect recursively extracts column names from a select clause
func extractColumnNamesFromSelect(sel models.SelectClause) []string {
	var names []string

	// Add column names from this select
	for _, col := range sel.Column {
		names = append(names, col.Name)
	}

	// Recursively extract from nested selects
	for _, nested := range sel.Select {
		names = append(names, extractColumnNamesFromSelect(nested)...)
	}

	return names
}

// BuildAllViewDefinitions builds ViewDefinitions for all attribute groups in a CRTDL document
func (b *ViewDefinitionBuilder) BuildAllViewDefinitions(doc *models.CRTDLDocument) (map[string]*models.ViewDefinition, error) {
	result := make(map[string]*models.ViewDefinition)

	for _, group := range doc.DataExtraction.AttributeGroups {
		viewDef, err := b.BuildViewDefinition(group)
		if err != nil {
			return nil, fmt.Errorf("failed to build ViewDefinition for group '%s': %w", group.Name, err)
		}
		result[group.Name] = viewDef
	}

	return result, nil
}

// ValidateViewDefinition performs basic validation on a ViewDefinition
func ValidateViewDefinition(viewDef *models.ViewDefinition) error {
	if viewDef == nil {
		return fmt.Errorf("viewDefinition is nil")
	}
	if viewDef.Name == "" {
		return fmt.Errorf("viewDefinition name is required")
	}
	if viewDef.Resource == "" {
		return fmt.Errorf("viewDefinition resource is required")
	}
	if len(viewDef.Select) == 0 {
		return fmt.Errorf("viewDefinition must have at least one select clause")
	}
	return nil
}
