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

	if err := NormalizeLookupTables(tables); err != nil {
		return nil, err
	}

	return tables, nil
}

// NormalizeLookupTables derives every Parent link from the Children lists.
// Authored parent values are ignored: each Parent is cleared and set to the
// element whose Children list names it. A child that two elements claim is an error.
func NormalizeLookupTables(tables []models.LookupTable) error {
	for _, table := range tables {
		for elementID, element := range table.Elements {
			if element.Parent != "" {
				element.Parent = ""
				table.Elements[elementID] = element
			}
		}

		claimedBy := make(map[string]string)
		for elementID, element := range table.Elements {
			for _, childID := range element.Children {
				if parentID, claimed := claimedBy[childID]; claimed && parentID != elementID {
					return fmt.Errorf("element '%s' is listed as child of both '%s' and '%s' in profile '%s'",
						childID, parentID, elementID, table.URL)
				}
				claimedBy[childID] = elementID

				child, exists := table.Elements[childID]
				if !exists {
					continue
				}
				child.Parent = elementID
				table.Elements[childID] = child
			}
		}
	}
	return nil
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

// ValidateLookupTables performs additional validation on the lookup tables.
// It expects tables that NormalizeLookupTables has processed, so Parent links
// are derived and need no check of their own.
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
		if err := detectChildrenCycle(table); err != nil {
			return err
		}
	}

	return nil
}

// detectChildrenCycle reports an error when the Children lists of a table form a cycle.
// The builder recurses over Children and Parent links, so a cycle would not terminate.
func detectChildrenCycle(table models.LookupTable) error {
	const inProgress, done = 1, 2
	state := make(map[string]int)

	var visit func(elementID string) error
	visit = func(elementID string) error {
		switch state[elementID] {
		case inProgress:
			return fmt.Errorf("circular children reference involving element '%s' in profile '%s'",
				elementID, table.URL)
		case done:
			return nil
		}
		state[elementID] = inProgress
		// A childID that is not in Elements is rejected by ValidateLookupTables
		// before this runs; visiting it is a harmless no-op (no children).
		for _, childID := range table.Elements[elementID].Children {
			if err := visit(childID); err != nil {
				return err
			}
		}
		state[elementID] = done
		return nil
	}

	for elementID := range table.Elements {
		if err := visit(elementID); err != nil {
			return err
		}
	}
	return nil
}
