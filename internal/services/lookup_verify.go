package services

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/medizininformatik-initiative/aether/internal/lib/flattenlookup"
)

// VerifyLookupFile validates a flatten-lookup file with the flattenlookup
// library. It returns the warning findings as formatted strings. If the file
// has error findings, the returned error lists them all.
func VerifyLookupFile(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read lookup file: %w", err)
	}
	return verifyLookupData(data)
}

// verifyLookupData validates raw lookup-file bytes and splits the findings
// into warnings and one combined error. Warning findings that share a code
// collapse into one warning, so a job logs at most one line per code.
func verifyLookupData(data []byte) ([]string, error) {
	result := flattenlookup.Validate(data)

	var errorMessages []string
	var warningFindings []flattenlookup.Finding
	for _, finding := range result.Findings {
		if finding.Severity == flattenlookup.SeverityError {
			errorMessages = append(errorMessages, formatFinding(finding))
		} else {
			warningFindings = append(warningFindings, finding)
		}
	}
	warnings := groupWarningsByCode(warningFindings)

	// The library validates each table alone; uniqueness across tables is
	// checked here because a JSON Schema cannot express it.
	if len(errorMessages) == 0 {
		errorMessages = append(errorMessages, duplicateURLMessages(data)...)
	}

	if len(errorMessages) > 0 {
		return warnings, fmt.Errorf("invalid lookup file: %s", strings.Join(errorMessages, "; "))
	}
	return warnings, nil
}

// duplicateURLMessages reports each profile URL that more than one table uses.
// It expects bytes that passed the schema validation, so the unmarshal into
// the minimal shape cannot fail.
func duplicateURLMessages(data []byte) []string {
	var tables []struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(data, &tables); err != nil {
		return []string{fmt.Sprintf("failed to parse lookup file: %v", err)}
	}

	var messages []string
	urlSet := make(map[string]bool)
	for _, table := range tables {
		if urlSet[table.URL] {
			messages = append(messages, fmt.Sprintf("duplicate profile URL: %s", table.URL))
		}
		urlSet[table.URL] = true
	}
	return messages
}

// groupWarningsByCode renders one warning per finding code, in the order the
// codes first occur. The locations of all findings with the same code join
// into that one warning.
func groupWarningsByCode(findings []flattenlookup.Finding) []string {
	var codes []string
	locations := make(map[string][]string)
	for _, finding := range findings {
		if _, seen := locations[finding.Code]; !seen {
			codes = append(codes, finding.Code)
		}
		locations[finding.Code] = append(locations[finding.Code], formatFindingLocation(finding))
	}

	var warnings []string
	for _, code := range codes {
		warnings = append(warnings, fmt.Sprintf("[%s] %s", code, strings.Join(locations[code], "; ")))
	}
	return warnings
}

// formatFinding renders one finding with its location context.
func formatFinding(finding flattenlookup.Finding) string {
	return fmt.Sprintf("[%s] %s", finding.Code, formatFindingLocation(finding))
}

// formatFindingLocation renders the message of a finding with its location
// context, without the code prefix.
func formatFindingLocation(finding flattenlookup.Finding) string {
	msg := finding.Message
	if finding.Element != "" {
		msg = fmt.Sprintf("element '%s': %s", finding.Element, msg)
	}
	if finding.Table != "" {
		msg = fmt.Sprintf("profile '%s': %s", finding.Table, msg)
	}
	return msg
}
