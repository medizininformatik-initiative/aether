package services

import (
	"fmt"
	"os"
	"strings"

	"github.com/medizininformatik-initiative/aether/internal/lib/crtdl"
)

// VerifyCRTDLFile validates a CRTDL file with the crtdl library. If the file
// has findings, the returned error lists them all.
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
	result := crtdl.Validate(data)
	if result.Valid() {
		return nil
	}

	messages := make([]string, 0, len(result.Findings))
	for _, finding := range result.Findings {
		messages = append(messages, formatCRTDLFinding(finding))
	}
	return fmt.Errorf("invalid CRTDL: %s", strings.Join(messages, "; "))
}

// formatCRTDLFinding renders one finding with its location context.
func formatCRTDLFinding(finding crtdl.Finding) string {
	msg := finding.Message
	if finding.Group != "" {
		msg = fmt.Sprintf("group '%s': %s", finding.Group, msg)
	}
	return fmt.Sprintf("[%s] %s", finding.Code, msg)
}
