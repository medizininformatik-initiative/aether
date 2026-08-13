package crtdl

import (
	"encoding/json"
	"fmt"
	"time"
)

// document and its parts mirror only the fields the cross-reference checks
// need. Structural soundness is already guaranteed by the schema.
type document struct {
	DataExtraction struct {
		AttributeGroups []attributeGroup `json:"attributeGroups"`
	} `json:"dataExtraction"`
}

type attributeGroup struct {
	ID         string      `json:"id"`
	Attributes []attribute `json:"attributes"`
	Filter     []filter    `json:"filter"`
}

type filter struct {
	Type  string `json:"type"`
	Name  string `json:"name"`
	Start string `json:"start"`
	End   string `json:"end"`
}

type attribute struct {
	AttributeRef string   `json:"attributeRef"`
	LinkedGroups []string `json:"linkedGroups"`
}

// crossRefFindings checks the invariants that the JSON Schema cannot express.
func crossRefFindings(data []byte) []Finding {
	var doc document
	if err := json.Unmarshal(data, &doc); err != nil {
		// The schema pass accepts only a structurally sound document,
		// so this cannot happen for input that got this far.
		panic(fmt.Sprintf("cross-reference unmarshal after schema pass: %v", err))
	}

	groups := doc.DataExtraction.AttributeGroups
	var findings []Finding
	findings = append(findings, duplicateGroupFindings(groups)...)
	findings = append(findings, linkedGroupFindings(groups)...)
	findings = append(findings, dateFilterFindings(groups)...)
	return findings
}

// dateFilterFindings checks that in a date filter the end date is not before
// the start date. The schema already asserts the date format, so unparseable
// dates never reach this check.
func dateFilterFindings(groups []attributeGroup) []Finding {
	var findings []Finding
	for _, group := range groups {
		for _, f := range group.Filter {
			if f.Type != "date" || f.Start == "" || f.End == "" {
				continue
			}
			start, startErr := time.Parse(time.DateOnly, f.Start)
			end, endErr := time.Parse(time.DateOnly, f.End)
			if startErr != nil || endErr != nil {
				continue
			}
			if end.Before(start) {
				findings = append(findings, Finding{
					Code:  "filter-date-order",
					Group: group.ID,
					Message: fmt.Sprintf("filter %q: end date %s is before start date %s",
						f.Name, f.End, f.Start),
				})
			}
		}
	}
	return findings
}

// duplicateGroupFindings checks that attribute group ids are unique. A
// duplicate id makes linkedGroups references ambiguous.
func duplicateGroupFindings(groups []attributeGroup) []Finding {
	count := make(map[string]int, len(groups))
	for _, group := range groups {
		count[group.ID]++
	}

	var findings []Finding
	reported := make(map[string]bool)
	for _, group := range groups {
		if count[group.ID] < 2 || reported[group.ID] {
			continue
		}
		reported[group.ID] = true
		findings = append(findings, Finding{
			Code:    "duplicate-group-id",
			Group:   group.ID,
			Message: fmt.Sprintf("%d attribute groups share the id %q", count[group.ID], group.ID),
		})
	}
	return findings
}

// linkedGroupFindings checks that every linkedGroups entry names the id of an
// attribute group in the same document.
func linkedGroupFindings(groups []attributeGroup) []Finding {
	ids := make(map[string]bool, len(groups))
	for _, group := range groups {
		ids[group.ID] = true
	}

	var findings []Finding
	for _, group := range groups {
		for _, attr := range group.Attributes {
			for _, linked := range attr.LinkedGroups {
				if ids[linked] {
					continue
				}
				findings = append(findings, Finding{
					Code:  "linked-group-not-found",
					Group: group.ID,
					Message: fmt.Sprintf("attribute %q links group %q, but no attribute group has that id",
						attr.AttributeRef, linked),
				})
			}
		}
	}
	return findings
}
