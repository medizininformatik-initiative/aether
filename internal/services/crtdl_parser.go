package services

import (
	"fmt"
	"os"

	"github.com/medizininformatik-initiative/aether/internal/models"
)

// ParseCRTDL parses a CRTDL file, validates it, and returns the document
// structure
func ParseCRTDL(path string) (*models.CRTDLDocument, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read CRTDL file: %w", err)
	}
	return parseAndVerifyCRTDL(data)
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
