package services

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
)

// ViewDefinitionWriter handles writing ViewDefinition JSON files to disk for debugging and auditing
type ViewDefinitionWriter struct {
	outputDir string
}

// NewViewDefinitionWriter creates a new ViewDefinitionWriter with the given output directory
func NewViewDefinitionWriter(outputDir string) *ViewDefinitionWriter {
	return &ViewDefinitionWriter{
		outputDir: outputDir,
	}
}

// WriteViewDefinition writes a ViewDefinition to a JSON file
func (w *ViewDefinitionWriter) WriteViewDefinition(filename string, viewDef models.ViewDefinition) error {
	if err := os.MkdirAll(w.outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create viewdefinitions directory: %w", err)
	}

	outputPath := filepath.Join(w.outputDir, filename)

	data, err := json.MarshalIndent(viewDef, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal ViewDefinition: %w", err)
	}

	if err := os.WriteFile(outputPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write ViewDefinition file: %w", err)
	}

	return nil
}

// BuildViewDefinitionFilename creates a filename from a group name
// Format: <sanitized-group-name>_viewdefinition.json
func BuildViewDefinitionFilename(groupName string) string {
	sanitized := lib.SanitizeFilename(groupName)
	return sanitized + "_viewdefinition.json"
}
