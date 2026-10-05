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

// BuildViewDefinition creates the ViewDefinition for an attribute group. The fixed
// id and patient columns come first. The attributes follow, merged in one ancestor
// tree, so that attributes with a common ancestor share its forEach context.
func (b *ViewDefinitionBuilder) BuildViewDefinition(group models.AttributeGroup) (*models.ViewDefinition, error) {
	lookup := GetProfileLookup(b.lookupTables, group.GroupReference)
	if lookup == nil {
		return nil, fmt.Errorf("no lookup table found for profile: %s", group.GroupReference)
	}

	viewDef := models.NewBaseViewDefinition(group.Name, lookup.ResourceType)

	attrRefSet := make(map[string]bool)
	for _, attr := range group.Attributes {
		attrRefSet[attr.AttributeRef] = true
	}

	root := &ancestorNode{}
	for _, attr := range group.Attributes {
		element := GetElement(lookup, attr.AttributeRef)
		if element == nil {
			continue
		}
		chain := ancestorChain(lookup, element)
		// The resolved children of a selected ancestor already include this attribute.
		if slices.ContainsFunc(chain, func(a *ancestorNode) bool { return attrRefSet[a.id] }) {
			continue
		}
		root.insert(chain, b.resolveWithChildren(lookup, element))
	}

	viewDef.Select = append([]models.SelectClause{{Column: b.buildFixedColumns(lookup.ResourceType)}}, root.render()...)
	return &viewDef, nil
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

// resolveWithChildren recursively resolves an element and its children
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
		wrapper.Select = append(slices.Clone(wrapper.Select), childSelects...)
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

// ancestorChain returns the ancestors of element as tree nodes, from the root down to its parent.
// The walk stops at an empty Parent or at an element that the lookup does not have.
func ancestorChain(lookup *models.LookupTable, element *models.LookupElement) []*ancestorNode {
	var chain []*ancestorNode
	for id := element.Parent; id != ""; {
		parent := GetElement(lookup, id)
		if parent == nil {
			break
		}
		chain = append(chain, &ancestorNode{id: id, element: parent})
		id = parent.Parent
	}
	slices.Reverse(chain)
	return chain
}

// ancestorNode is a node of the ancestor tree. A node without an element is the
// root or a leaf with the selects of one attribute. The children keep the order
// of their first insertion. Attributes with a common ancestor share its node, so
// the forEach context of the ancestor appears once and each of its rows pairs
// only its own children.
type ancestorNode struct {
	id       string
	element  *models.LookupElement
	selects  []models.SelectClause
	children []*ancestorNode
}

// insert adds selects below the node that chain describes. A chain node with
// an element ID that is already a child of n is merged into that child.
func (n *ancestorNode) insert(chain []*ancestorNode, selects []models.SelectClause) {
	if len(chain) == 0 {
		n.children = append(n.children, &ancestorNode{selects: selects})
		return
	}
	for _, c := range n.children {
		if c.id == chain[0].id {
			c.insert(chain[1:], selects)
			return
		}
	}
	n.children = append(n.children, chain[0])
	chain[0].insert(chain[1:], selects)
}

// render concatenates the selects of the node and its rendered children and
// wraps them once in the context of the node element.
func (n *ancestorNode) render() []models.SelectClause {
	selects := n.selects
	for _, c := range n.children {
		selects = append(selects, c.render()...)
	}
	if n.element == nil {
		return selects
	}
	return wrapInParentContext(n.element, selects)
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
