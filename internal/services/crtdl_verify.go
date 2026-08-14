package services

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/medizininformatik-initiative/aether/internal/lib/crtdl"
	"github.com/medizininformatik-initiative/aether/internal/models"
)

// VerifyCRTDLFile validates a CRTDL file. If the file has findings, the
// returned error lists them all.
func VerifyCRTDLFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read CRTDL file: %w", err)
	}
	return VerifyCRTDLBytes(data)
}

// VerifyCRTDLBytes validates a CRTDL document that is held in memory. Use it
// for a document that aether builds itself, such as the enriched CRTDL, which
// never exists as an input file.
func VerifyCRTDLBytes(data []byte) error {
	_, err := parseAndVerifyCRTDL(data)
	return err
}

// parseAndVerifyCRTDL runs the full validation: the CRTDL specification
// checks from the crtdl library, then the rules that only aether enforces.
// On success it returns the parsed document.
func parseAndVerifyCRTDL(data []byte) (*models.CRTDLDocument, error) {
	result := crtdl.Validate(data)

	var doc models.CRTDLDocument
	if result.Valid() {
		if err := json.Unmarshal(data, &doc); err != nil {
			return nil, fmt.Errorf("failed to parse CRTDL: %w", err)
		}
		result.Findings = aetherRuleFindings(&doc)
	}

	if !result.Valid() {
		messages := make([]string, 0, len(result.Findings))
		for _, finding := range result.Findings {
			messages = append(messages, formatCRTDLFinding(finding))
		}
		return nil, fmt.Errorf("invalid CRTDL: %s", strings.Join(messages, "; "))
	}
	return &doc, nil
}

// aetherRuleFindings checks the rules that only aether enforces, beyond the
// CRTDL specification: each attributeGroup has a name, the names map to
// unique CSV file names, no field is empty, and each group has at least one
// attribute.
func aetherRuleFindings(doc *models.CRTDLDocument) []crtdl.Finding {
	groups := doc.DataExtraction.AttributeGroups
	if len(groups) == 0 {
		return []crtdl.Finding{{
			Code:    "no-attribute-groups",
			Message: "CRTDL must have at least one attributeGroup",
		}}
	}

	var findings []crtdl.Finding
	// The name of an attributeGroup becomes the name of its CSV file. Two groups
	// that share a file name write to one file, which loses the rows of one group.
	firstGroupWithFile := make(map[string]models.AttributeGroup, len(groups))

	for i, group := range groups {
		if group.ID == "" {
			findings = append(findings, crtdl.Finding{
				Code:    "missing-id",
				Message: fmt.Sprintf("attributeGroup at index %d missing 'id' field", i),
			})
		}
		if group.Name == "" {
			findings = append(findings, crtdl.Finding{
				Code:    "missing-name",
				Group:   group.ID,
				Message: fmt.Sprintf("attributeGroup at index %d missing 'name' field", i),
			})
		} else {
			csvFilename := BuildCSVFilename(group.Name)
			if first, isDuplicate := firstGroupWithFile[csvFilename]; isDuplicate {
				findings = append(findings, crtdl.Finding{
					Code:  "duplicate-csv-filename",
					Group: group.ID,
					Message: duplicateGroupMessage(first, group, csvFilename) +
						". The name of an attributeGroup becomes the name of its CSV file; give each attributeGroup a unique name",
				})
			} else {
				firstGroupWithFile[csvFilename] = group
			}
		}

		if group.GroupReference == "" {
			findings = append(findings, crtdl.Finding{
				Code:    "missing-group-reference",
				Group:   group.ID,
				Message: fmt.Sprintf("attributeGroup '%s' missing 'groupReference' field", group.Name),
			})
		}
		if len(group.Attributes) == 0 {
			findings = append(findings, crtdl.Finding{
				Code:    "no-attributes",
				Group:   group.ID,
				Message: fmt.Sprintf("attributeGroup '%s' must have at least one attribute", group.Name),
			})
		}
		for j, attr := range group.Attributes {
			if attr.AttributeRef == "" {
				findings = append(findings, crtdl.Finding{
					Code:    "missing-attribute-ref",
					Group:   group.ID,
					Message: fmt.Sprintf("attribute at index %d in group '%s' missing 'attributeRef' field", j, group.Name),
				})
			}
		}
	}
	return findings
}

// duplicateGroupMessage tells which two attributeGroups collide, and whether
// their names are equal or only become equal as a CSV file name
func duplicateGroupMessage(first, second models.AttributeGroup, csvFilename string) string {
	if first.Name == second.Name {
		return fmt.Sprintf("attributeGroups '%s' and '%s' have the same name '%s'",
			first.ID, second.ID, first.Name)
	}
	return fmt.Sprintf("attributeGroups '%s' (name '%s') and '%s' (name '%s') both write to the CSV file '%s'",
		first.ID, first.Name, second.ID, second.Name, csvFilename)
}

// formatCRTDLFinding renders one finding with its location context.
func formatCRTDLFinding(finding crtdl.Finding) string {
	msg := finding.Message
	if finding.Group != "" {
		msg = fmt.Sprintf("group '%s': %s", finding.Group, msg)
	}
	return fmt.Sprintf("[%s] %s", finding.Code, msg)
}
