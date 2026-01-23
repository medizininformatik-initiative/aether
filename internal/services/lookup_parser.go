package services

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/medizininformatik-initiative/aether/internal/models"
)

// LoadLookupTables loads the lookup tables from a JSON file
// The file contains an array of LookupTable objects
func LoadLookupTables(path string) ([]models.LookupTable, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read lookup file: %w", err)
	}

	var tables []models.LookupTable
	if err := json.Unmarshal(data, &tables); err != nil {
		return nil, fmt.Errorf("failed to parse lookup file: %w", err)
	}

	// Validate each table
	for i, table := range tables {
		if table.URL == "" {
			return nil, fmt.Errorf("lookup table at index %d missing 'url' field", i)
		}
		if table.ResourceType == "" {
			return nil, fmt.Errorf("lookup table at index %d (url: %s) missing 'resourceType' field", i, table.URL)
		}
		if table.Elements == nil {
			return nil, fmt.Errorf("lookup table at index %d (url: %s) missing 'elements' field", i, table.URL)
		}
	}

	return tables, nil
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

// ValidateLookupTables performs additional validation on the lookup tables
// Checks for:
// - Duplicate profile URLs
// - Circular references in children
// - Invalid child references
func ValidateLookupTables(tables []models.LookupTable) error {
	// Check for duplicate URLs
	urlSet := make(map[string]bool)
	for _, table := range tables {
		if urlSet[table.URL] {
			return fmt.Errorf("duplicate profile URL: %s", table.URL)
		}
		urlSet[table.URL] = true
	}

	// Check for invalid child references within each table
	for _, table := range tables {
		for elementID, element := range table.Elements {
			for _, childID := range element.Children {
				if _, exists := table.Elements[childID]; !exists {
					return fmt.Errorf("element '%s' references non-existent child '%s' in profile '%s'",
						elementID, childID, table.URL)
				}
			}
		}
	}

	return nil
}
