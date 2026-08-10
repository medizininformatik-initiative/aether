package services

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/medizininformatik-initiative/aether/internal/models"
)

// ParseCRTDL parses a CRTDL file and returns the document structure
func ParseCRTDL(path string) (*models.CRTDLDocument, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read CRTDL file: %w", err)
	}

	var doc models.CRTDLDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("failed to parse CRTDL file: %w", err)
	}

	// Validate the document structure
	if err := ValidateCRTDL(&doc); err != nil {
		return nil, fmt.Errorf("invalid CRTDL document: %w", err)
	}

	return &doc, nil
}

// ValidateCRTDL validates a CRTDL document structure
func ValidateCRTDL(doc *models.CRTDLDocument) error {
	if len(doc.DataExtraction.AttributeGroups) == 0 {
		return fmt.Errorf("CRTDL must have at least one attributeGroup")
	}

	// The name of an attributeGroup becomes the name of its CSV file. Two groups
	// that share a file name write to one file, which loses the rows of one group.
	firstGroupWithFile := make(map[string]models.AttributeGroup, len(doc.DataExtraction.AttributeGroups))

	for i, group := range doc.DataExtraction.AttributeGroups {
		if group.ID == "" {
			return fmt.Errorf("attributeGroup at index %d missing 'id' field", i)
		}
		if group.Name == "" {
			return fmt.Errorf("attributeGroup at index %d missing 'name' field", i)
		}
		csvFilename := BuildCSVFilename(group.Name)
		if first, isDuplicate := firstGroupWithFile[csvFilename]; isDuplicate {
			return fmt.Errorf("%s\n\n"+
				"The name of an attributeGroup becomes the name of its CSV file.\n"+
				"Give each attributeGroup a unique name",
				duplicateGroupMessage(first, group, csvFilename))
		}
		firstGroupWithFile[csvFilename] = group

		if group.GroupReference == "" {
			return fmt.Errorf("attributeGroup '%s' missing 'groupReference' field", group.Name)
		}
		if len(group.Attributes) == 0 {
			return fmt.Errorf("attributeGroup '%s' must have at least one attribute", group.Name)
		}

		for j, attr := range group.Attributes {
			if attr.AttributeRef == "" {
				return fmt.Errorf("attribute at index %d in group '%s' missing 'attributeRef' field", j, group.Name)
			}
		}
	}

	return nil
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

// GetAttributeGroups returns all attribute groups from a CRTDL document
func GetAttributeGroups(doc *models.CRTDLDocument) []models.AttributeGroup {
	if doc == nil {
		return nil
	}
	return doc.DataExtraction.AttributeGroups
}

// GetAttributeGroupByName finds an attribute group by name
func GetAttributeGroupByName(doc *models.CRTDLDocument, name string) *models.AttributeGroup {
	if doc == nil {
		return nil
	}
	for i := range doc.DataExtraction.AttributeGroups {
		if doc.DataExtraction.AttributeGroups[i].Name == name {
			return &doc.DataExtraction.AttributeGroups[i]
		}
	}
	return nil
}

// GetAttributeGroupByReference finds an attribute group by its groupReference URL
func GetAttributeGroupByReference(doc *models.CRTDLDocument, groupReference string) *models.AttributeGroup {
	if doc == nil {
		return nil
	}
	for i := range doc.DataExtraction.AttributeGroups {
		if doc.DataExtraction.AttributeGroups[i].GroupReference == groupReference {
			return &doc.DataExtraction.AttributeGroups[i]
		}
	}
	return nil
}

// GetMustHaveAttributes returns only the attributes marked as mustHave
func GetMustHaveAttributes(group *models.AttributeGroup) []models.Attribute {
	if group == nil {
		return nil
	}
	var mustHave []models.Attribute
	for _, attr := range group.Attributes {
		if attr.MustHave {
			mustHave = append(mustHave, attr)
		}
	}
	return mustHave
}

// IsCRTDLFile checks if a file path looks like a CRTDL file by extension.
// CRTDL files use the .json extension.
func IsCRTDLFile(path string) bool {
	return len(path) > 5 && path[len(path)-5:] == ".json"
}
