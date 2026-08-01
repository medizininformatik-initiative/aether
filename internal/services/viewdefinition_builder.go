package services

import (
	"fmt"

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

	// Build the select array from attributes
	selectClauses := make([]models.SelectClause, 0)

	// Add fixed columns first
	fixedColumns := b.buildFixedColumns(lookup.ResourceType)
	if len(fixedColumns) > 0 {
		selectClauses = append(selectClauses, models.SelectClause{
			Column: fixedColumns,
		})
	}

	// Build set of attribute refs for fast lookup (used to detect overlapping parent-child)
	attrRefSet := make(map[string]bool)
	for _, attr := range group.Attributes {
		attrRefSet[attr.AttributeRef] = true
	}

	// Process each attribute in the group, skipping children whose parent is already in the list
	for _, attr := range group.Attributes {
		// Skip if this element's parent (or any ancestor) is already in the attribute list
		// Parent's downward traversal will include this child automatically
		if hasAncestorRef(lookup, attr.AttributeRef, attrRefSet) || isParentInAttributeList(lookup, attr.AttributeRef, attrRefSet) {
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

// hasAncestorRef checks if any prefix of ref that ends at a "." boundary is in the
// attribute list (e.g. "Encounter.extension:A" is an ancestor of
// "Encounter.extension:A.extension:B"). Unlike isParentInAttributeList, this works
// without Parent links in the lookup table. The ancestor must resolve in the
// lookup table — otherwise it produces no columns and the child must stay.
func hasAncestorRef(lookup *models.LookupTable, ref string, attrRefs map[string]bool) bool {
	for i := len(ref) - 1; i > 0; i-- {
		if ref[i] == '.' && attrRefs[ref[:i]] && GetElement(lookup, ref[:i]) != nil {
			return true
		}
	}
	return false
}

// isParentInAttributeList checks if the element's parent (or any ancestor) is in the attribute list.
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

	// If element has a parent, wrap in parent context (upward traversal)
	if element.Parent != "" {
		return b.resolveWithParent(lookup, element, attributeRef), nil
	}

	// Otherwise just resolve children (downward traversal)
	return b.resolveWithChildren(lookup, element), nil
}

// resolveWithChildren recursively resolves an element and its children
// This matches the Python reference implementation (test.py lines 52-57)
func (b *ViewDefinitionBuilder) resolveWithChildren(lookup *models.LookupTable, element *models.LookupElement) []models.SelectClause {
	// Check if element has root-level forEach/forEachOrNull
	hasRootForEach := element.ViewDefinition.ForEach != "" || element.ViewDefinition.ForEachOrNull != ""
	// Check if element has Column at viewDefinition level (common for leaf elements)
	hasRootColumn := len(element.ViewDefinition.Column) > 0

	if len(element.Children) == 0 {
		// No children - convert viewDefSnippet to SelectClause if it has root forEach or Column
		if hasRootForEach || hasRootColumn {
			return []models.SelectClause{viewDefSnippetToSelectClause(element.ViewDefinition)}
		}
		return element.ViewDefinition.Select
	}

	// Collect children, recursively resolving those with their own children
	var childSelects []models.SelectClause
	for _, childID := range element.Children {
		childElement := GetElement(lookup, childID)
		if childElement != nil {
			childHasRootForEach := childElement.ViewDefinition.ForEach != "" || childElement.ViewDefinition.ForEachOrNull != ""

			if childHasRootForEach {
				// Child has root forEach - convert to SelectClause (preserving forEach)
				// and recursively resolve child's children if any
				childAsSelect := viewDefSnippetToSelectClause(childElement.ViewDefinition)
				if len(childElement.Children) > 0 {
					// Resolve grandchildren and add to child's select
					grandchildSelects := b.resolveGrandchildren(lookup, childElement)
					childAsSelect.Select = append(childAsSelect.Select, grandchildSelects...)
				}
				childSelects = append(childSelects, childAsSelect)
			} else {
				// Child has no root forEach - recursively resolve it
				resolved := b.resolveWithChildren(lookup, childElement)
				childSelects = append(childSelects, resolved...)
			}
		}
	}

	// If element has root forEach, create wrapper SelectClause with children inside
	if hasRootForEach {
		wrapper := viewDefSnippetToSelectClause(element.ViewDefinition)
		wrapper.Select = append(wrapper.Select, childSelects...)
		return []models.SelectClause{wrapper}
	}

	// No root forEach - include element's own selects plus children
	result := make([]models.SelectClause, 0, len(element.ViewDefinition.Select)+len(childSelects))
	result = append(result, element.ViewDefinition.Select...)
	result = append(result, childSelects...)
	return result
}

// resolveGrandchildren recursively resolves children of a child element
func (b *ViewDefinitionBuilder) resolveGrandchildren(lookup *models.LookupTable, element *models.LookupElement) []models.SelectClause {
	var result []models.SelectClause
	for _, childID := range element.Children {
		childElement := GetElement(lookup, childID)
		if childElement != nil {
			childHasRootForEach := childElement.ViewDefinition.ForEach != "" || childElement.ViewDefinition.ForEachOrNull != ""

			if childHasRootForEach {
				childAsSelect := viewDefSnippetToSelectClause(childElement.ViewDefinition)
				if len(childElement.Children) > 0 {
					grandchildSelects := b.resolveGrandchildren(lookup, childElement)
					childAsSelect.Select = append(childAsSelect.Select, grandchildSelects...)
				}
				result = append(result, childAsSelect)
			} else {
				// No forEach - recursively resolve
				resolved := b.resolveWithChildren(lookup, childElement)
				result = append(result, resolved...)
			}
		}
	}
	return result
}

// resolveWithParent wraps an element's resolved selects in its ancestor chain's
// forEach contexts. Unlike a naive downward re-resolution from an ancestor,
// this does not walk the parent's children, so sibling attributes sharing an
// ancestor do not pollute each other's output (regression fix for #300).
func (b *ViewDefinitionBuilder) resolveWithParent(lookup *models.LookupTable, element *models.LookupElement, _ string) []models.SelectClause {
	elementSelects := b.resolveWithChildren(lookup, element)
	return b.wrapInAncestors(lookup, element.Parent, elementSelects)
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

	wrapped := selects
	parentHasRootForEach := parent.ViewDefinition.ForEach != "" || parent.ViewDefinition.ForEachOrNull != ""

	if parentHasRootForEach {
		wrapper := models.SelectClause{
			ForEach:       parent.ViewDefinition.ForEach,
			ForEachOrNull: parent.ViewDefinition.ForEachOrNull,
			Select:        selects,
		}
		wrapped = []models.SelectClause{wrapper}
	} else {
		for _, ps := range parent.ViewDefinition.Select {
			if ps.ForEach != "" || ps.ForEachOrNull != "" {
				newSel := cloneSelectClause(ps)
				newSel.Select = selects
				wrapped = []models.SelectClause{newSel}
				break
			}
		}
	}

	return b.wrapInAncestors(lookup, parent.Parent, wrapped)
}

// cloneSelectClause creates a deep copy of a SelectClause
func cloneSelectClause(sel models.SelectClause) models.SelectClause {
	newSel := models.SelectClause{
		ForEach:       sel.ForEach,
		ForEachOrNull: sel.ForEachOrNull,
	}

	// Clone columns
	if len(sel.Column) > 0 {
		newSel.Column = make([]models.ColumnDefinition, len(sel.Column))
		copy(newSel.Column, sel.Column)
	}

	// Clone nested selects recursively
	if len(sel.Select) > 0 {
		newSel.Select = make([]models.SelectClause, len(sel.Select))
		for i, nested := range sel.Select {
			newSel.Select[i] = cloneSelectClause(nested)
		}
	}

	return newSel
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

// ExtractColumnNames traverses a ViewDefinition and extracts all column names in order
// This is used to construct the CSV header since the flattener API returns data without headers
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
