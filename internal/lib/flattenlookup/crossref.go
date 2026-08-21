package flattenlookup

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
)

// lookupTable and lookupElement mirror only the fields the cross-reference
// checks need. Structural soundness is already guaranteed by the schema.
type lookupTable struct {
	URL      string                   `json:"url"`
	Elements map[string]lookupElement `json:"elements"`
}

type lookupElement struct {
	// Parent is the deprecated stored parent link. The schema rejects an empty
	// string, so a non-empty value means the field is present.
	Parent   string   `json:"parent"`
	Children []string `json:"children"`
}

// crossRefFindings checks the invariants that the JSON Schema cannot express.
func crossRefFindings(data []byte, cfg options) []Finding {
	var tables []lookupTable
	if err := json.Unmarshal(data, &tables); err != nil {
		// The schema pass accepts only a structurally sound document,
		// so this cannot happen for input that got this far.
		panic(fmt.Sprintf("cross-reference unmarshal after schema pass: %v", err))
	}

	var findings []Finding
	for _, table := range tables {
		parentOf, ambiguous, resolveFindings := deriveParents(table)
		findings = append(findings, resolveFindings...)
		findings = append(findings, storedParentFindings(table, parentOf, ambiguous, cfg)...)
		findings = append(findings, prefixFindings(table, parentOf)...)
		findings = append(findings, childCycleFindings(table, parentOf)...)
	}
	return findings
}

// deriveParents builds the parent link of every element from the children lists
// of the other elements. The lookup format stores only the children direction.
// The second result holds the elements that more than one element claims.
func deriveParents(table lookupTable) (map[string]string, map[string]bool, []Finding) {
	ids := sortedIDs(table)
	claims := make(map[string][]string, len(table.Elements))

	var findings []Finding
	for _, id := range ids {
		for _, child := range table.Elements[id].Children {
			if _, ok := table.Elements[child]; !ok {
				findings = append(findings, Finding{
					Severity: SeverityWarning,
					Code:     "unresolved-child",
					Table:    table.URL,
					Element:  id,
					Message:  fmt.Sprintf("children entry %q does not resolve to an element in this table", child),
				})
				continue
			}
			claims[child] = append(claims[child], id)
		}
	}

	// An element that more than one element claims as a child has no single
	// derived parent. Leave it out of parentOf, so that the checks that build
	// on the parent link do not report a second, arbitrary finding for it.
	parentOf := make(map[string]string, len(claims))
	ambiguous := make(map[string]bool)
	for _, child := range ids {
		parents := claims[child]
		switch len(parents) {
		case 0:
		case 1:
			parentOf[child] = parents[0]
		default:
			ambiguous[child] = true
			findings = append(findings, Finding{
				Severity: SeverityError,
				Code:     "multiple-parents",
				Table:    table.URL,
				Element:  child,
				Message: fmt.Sprintf("element is listed in the children of %s; the parent link is derived from children and must be unique",
					quoteJoin(parents)),
			})
		}
	}
	return parentOf, ambiguous, findings
}

// quoteJoin renders element IDs as a quoted, comma-separated list for messages.
func quoteJoin(ids []string) string {
	quoted := make([]string, len(ids))
	for i, id := range ids {
		quoted[i] = fmt.Sprintf("%q", id)
	}
	return strings.Join(quoted, ", ")
}

// storedParentFindings reports the deprecated parent field. The presence of the
// field is only a note about style, so it stays quiet outside verbose mode. A
// stored value that disagrees with the derived link is a defect and is always
// reported.
func storedParentFindings(table lookupTable, parentOf map[string]string, ambiguous map[string]bool, cfg options) []Finding {
	var findings []Finding
	for _, id := range sortedIDs(table) {
		stored := table.Elements[id].Parent
		if stored == "" {
			continue
		}
		if cfg.verbose {
			findings = append(findings, Finding{
				Severity: SeverityWarning,
				Code:     "deprecated-parent",
				Table:    table.URL,
				Element:  id,
				Message:  "the parent field is deprecated; the parent link is derived from the children lists",
			})
		}
		// An element that more than one element claims already has a
		// multiple-parents error. A mismatch finding on top of it reports the
		// same defect twice.
		if ambiguous[id] {
			continue
		}
		derived, ok := parentOf[id]
		if ok && derived == stored {
			continue
		}
		message := fmt.Sprintf("the deprecated parent field says %q, but the children lists derive %q; consumers use the derived link", stored, derived)
		if !ok {
			message = fmt.Sprintf("the deprecated parent field says %q, but no element lists this element in its children; consumers derive no parent", stored)
		}
		findings = append(findings, Finding{
			Severity: SeverityWarning,
			Code:     "parent-mismatch",
			Table:    table.URL,
			Element:  id,
			Message:  message,
		})
	}
	return findings
}

// prefixFindings checks the naming convention: a child ID extends the ID of its
// derived parent at a '.' or ':' boundary.
func prefixFindings(table lookupTable, parentOf map[string]string) []Finding {
	var findings []Finding
	for _, id := range sortedIDs(table) {
		parent, ok := parentOf[id]
		if !ok {
			continue
		}
		if !isPrefixAtBoundary(parent, id) {
			findings = append(findings, Finding{
				Severity: SeverityWarning,
				Code:     "parent-not-prefix",
				Table:    table.URL,
				Element:  id,
				Message:  fmt.Sprintf("parent %q is not a prefix of this element ID at a '.' or ':' boundary", parent),
			})
		}
	}
	return findings
}

// isPrefixAtBoundary reports whether ancestor is a prefix of id that ends at
// a '.' or ':' boundary, e.g. "Condition.code" in "Condition.code.coding:sct".
func isPrefixAtBoundary(ancestor, id string) bool {
	rest, ok := strings.CutPrefix(id, ancestor)
	if !ok || rest == "" {
		return false
	}
	return rest[0] == '.' || rest[0] == ':'
}

// sortedIDs returns the element IDs of a table in a stable order, so that
// findings do not depend on Go map iteration order.
func sortedIDs(table lookupTable) []string {
	ids := make([]string, 0, len(table.Elements))
	for id := range table.Elements {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// childCycleFindings reports one error per distinct cycle in the children
// graph. It walks the derived parent links, which point along the same edges
// in the opposite direction.
func childCycleFindings(table lookupTable, parentOf map[string]string) []Finding {
	const (
		unvisited = 0
		inPath    = 1
		done      = 2
	)
	state := make(map[string]int, len(table.Elements))

	var findings []Finding
	for _, start := range sortedIDs(table) {
		if state[start] != unvisited {
			continue
		}
		var path []string
		current := start
		for {
			state[current] = inPath
			path = append(path, current)
			next, ok := parentOf[current]
			if !ok || state[next] == done {
				break
			}
			if state[next] == inPath {
				cycleStart := slices.Index(path, next)
				cycle := path[cycleStart:]
				findings = append(findings, Finding{
					Severity: SeverityError,
					Code:     "child-cycle",
					Table:    table.URL,
					Element:  cycle[0],
					Message:  fmt.Sprintf("children form a cycle: %s", strings.Join(append(cycle, cycle[0]), " -> ")),
				})
				break
			}
			current = next
		}
		for _, id := range path {
			state[id] = done
		}
	}
	return findings
}
