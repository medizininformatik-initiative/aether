package services

import (
	"fmt"
	"slices"
	"strings"

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
		root.insert(chain, &ancestorNode{id: attr.AttributeRef, selects: b.resolveWithChildren(lookup, element)})
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

	var items []siblingItem
	for _, childID := range element.Children {
		if childElement := GetElement(lookup, childID); childElement != nil {
			items = append(items, siblingItem{id: childID, selects: b.resolveWithChildren(lookup, childElement)})
		}
	}
	childSelects := unionSlices(items)

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

// siblingItem is an element with its resolved select clauses.
type siblingItem struct {
	id      string
	selects []models.SelectClause
}

// sliceBase returns the element ID of the slice without its slice name, for
// example "Condition.code.coding" for "Condition.code.coding:icd10-gm". It
// returns "" when the last path segment is not a slice.
func sliceBase(id string) string {
	i := strings.LastIndex(id, ":")
	if i <= strings.LastIndex(id, ".") {
		return ""
	}
	return id[:i]
}

// unionSlices combines sibling slices of one element into one unionAll clause,
// so that each slice gives its own rows. Without it, the forEach clauses of
// the slices are cross-joined and write every combination of their rows.
// The clause takes the position of the first slice. All other items stay as they are.
func unionSlices(items []siblingItem) []models.SelectClause {
	bases, groups, first := groupSlices(items)
	var result []models.SelectClause
	for i, item := range items {
		group := groups[bases[i]]
		switch {
		case len(group) < 2:
			result = append(result, item.selects...)
		case first[bases[i]] == i:
			result = append(result, buildSliceUnion(group))
		}
	}
	return result
}

// groupSlices returns the union base of each item, the clauses of the items
// per base, and the index of the first item per base. Items without a base
// are in no group.
func groupSlices(items []siblingItem) ([]string, map[string][]models.SelectClause, map[string]int) {
	bases := make([]string, len(items))
	groups := make(map[string][]models.SelectClause)
	first := make(map[string]int)
	for i, item := range items {
		bases[i] = unionBase(item)
		if bases[i] == "" {
			continue
		}
		if _, ok := groups[bases[i]]; !ok {
			first[bases[i]] = i
		}
		groups[bases[i]] = append(groups[bases[i]], item.selects[0])
	}
	return bases, groups, first
}

// unionBase returns the slice base of an item that can join a union, or "".
// Only an item that resolves to exactly one forEach clause can join.
func unionBase(item siblingItem) string {
	if len(item.selects) != 1 || !item.selects[0].HasForEach() {
		return ""
	}
	return sliceBase(item.id)
}

// buildSliceUnion builds a unionAll clause from slice clauses. Each branch
// has the columns of all slices. A branch fills the columns of the other
// slices with empty values. The first branch gives one empty row when all
// slices are empty. It comes first, because the flattener takes the column
// types from the first branch.
func buildSliceUnion(members []models.SelectClause) models.SelectClause {
	exprs := make([]string, len(members))
	emptyConditions := make([]string, len(members))
	empty := make([][]models.ColumnDefinition, len(members))
	fallback := make([]models.SelectClause, len(members))
	for i, m := range members {
		exprs[i] = m.ForEachExpression()
		emptyConditions[i] = "(" + exprs[i] + ").empty()"
		empty[i] = emptyColumns(m)
		m.ForEach, m.ForEachOrNull = "", exprs[i]
		fallback[i] = m
	}

	branches := []models.SelectClause{{
		ForEach: "$this.where(" + strings.Join(emptyConditions, " and ") + ")",
		Select:  fallback,
	}}
	for i, m := range members {
		parts := make([]models.SelectClause, len(members))
		for j := range members {
			parts[j] = models.SelectClause{Column: empty[j]}
		}
		parts[i] = models.SelectClause{Column: m.Column, Select: m.Select}
		branches = append(branches, models.SelectClause{ForEach: exprs[i], Select: parts})
	}
	return models.SelectClause{UnionAll: branches}
}

// emptyColumns returns the columns of a clause with a path that has no value.
func emptyColumns(sel models.SelectClause) []models.ColumnDefinition {
	columns := collectColumns(sel)
	for i := range columns {
		columns[i].Path = "{}"
	}
	return columns
}

// collectColumns returns the columns of a clause in the order of its output:
// its own columns, then those of its nested selects, then those of its unionAll.
// All branches of a unionAll have the same columns, so the first branch gives them.
func collectColumns(sel models.SelectClause) []models.ColumnDefinition {
	columns := slices.Clone(sel.Column)
	for _, nested := range sel.Select {
		columns = append(columns, collectColumns(nested)...)
	}
	if len(sel.UnionAll) > 0 {
		columns = append(columns, collectColumns(sel.UnionAll[0])...)
	}
	return columns
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

// insert adds leaf below the node that chain describes. A chain node with
// an element ID that is already a child of n is merged into that child.
func (n *ancestorNode) insert(chain []*ancestorNode, leaf *ancestorNode) {
	if len(chain) == 0 {
		n.children = append(n.children, leaf)
		return
	}
	for _, c := range n.children {
		if c.id == chain[0].id {
			c.insert(chain[1:], leaf)
			return
		}
	}
	n.children = append(n.children, chain[0])
	chain[0].insert(chain[1:], leaf)
}

// render concatenates the selects of the node and its rendered children and
// wraps them once in the context of the node element. Children that are slices
// of one element go into one unionAll clause.
func (n *ancestorNode) render() []models.SelectClause {
	items := make([]siblingItem, len(n.children))
	for i, c := range n.children {
		items[i] = siblingItem{id: c.id, selects: c.render()}
	}
	selects := append(n.selects, unionSlices(items)...)
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
		for _, col := range collectColumns(sel) {
			names = append(names, col.Name)
		}
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
