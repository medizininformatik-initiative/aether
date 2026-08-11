package services

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/medizininformatik-initiative/aether/internal/models"
)

// LoadLookupTables loads the lookup tables from a JSON file
// The file contains an array of LookupTable objects
// The flattenlookup library validates the raw file, so a file whose children
// lists form a cycle is rejected here and every loaded table set is safe for
// the builder. Warning findings are dropped; the pipeline start check reports
// them.
func LoadLookupTables(path string) ([]models.LookupTable, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read lookup file: %w", err)
	}

	if _, err := verifyLookupData(data); err != nil {
		return nil, err
	}

	var tables []models.LookupTable
	if err := json.Unmarshal(data, &tables); err != nil {
		return nil, fmt.Errorf("failed to parse lookup file: %w", err)
	}

	NormalizeLookupTables(tables)

	return tables, nil
}

// NormalizeLookupTables derives every Parent link from the Children lists.
// Authored parent values are ignored: each Parent is cleared and set to the
// element whose Children list names it. The flattenlookup library rejects a
// child that more than one element claims before this runs, so the derivation
// cannot be ambiguous.
// Authored parent fields are deprecated and dropped without a warning; the
// field will be removed in a future flatten-lookup version.
func NormalizeLookupTables(tables []models.LookupTable) {
	for _, table := range tables {
		for elementID, element := range table.Elements {
			if element.Parent != "" {
				element.Parent = ""
				table.Elements[elementID] = element
			}
		}

		for elementID, element := range table.Elements {
			for _, childID := range element.Children {
				child, exists := table.Elements[childID]
				if !exists {
					continue
				}
				child.Parent = elementID
				table.Elements[childID] = child
			}
		}
	}
}

// GetProfileLookup finds a LookupTable by profile URL
// Returns nil if no matching profile is found
func GetProfileLookup(tables []models.LookupTable, profileURL string) *models.LookupTable {
	for i := range tables {
		if tables[i].URL == profileURL {
			return &tables[i]
		}
	}
	return nil
}

// GetElement retrieves an element from a LookupTable by its element ID
// Returns nil if the element is not found
func GetElement(table *models.LookupTable, elementID string) *models.LookupElement {
	if table == nil || table.Elements == nil {
		return nil
	}
	element, exists := table.Elements[elementID]
	if !exists {
		return nil
	}
	return &element
}

// GetElementChildren returns the child elements of a given element
// Recursively resolves children based on the Children field
func GetElementChildren(table *models.LookupTable, elementID string) []models.LookupElement {
	element := GetElement(table, elementID)
	if element == nil || len(element.Children) == 0 {
		return nil
	}

	var children []models.LookupElement
	for _, childID := range element.Children {
		childElement := GetElement(table, childID)
		if childElement != nil {
			children = append(children, *childElement)
		}
	}
	return children
}
