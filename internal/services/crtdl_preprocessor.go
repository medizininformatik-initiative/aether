package services

import (
	"fmt"

	"github.com/google/uuid"
	"github.com/medizininformatik-initiative/aether/internal/models"
)

// EnrichCRTDL applies enrichments to a CRTDL document
// It either adds attributes to existing groups or creates new groups with attributes
func EnrichCRTDL(doc *models.CRTDLDocument, enrichments []models.GroupEnrichment) (*models.CRTDLDocument, error) {
	if doc == nil {
		return nil, fmt.Errorf("CRTDL document cannot be nil")
	}

	// Work on a copy to avoid mutating the original
	result := copyCRTDL(doc)

	for i, enrichment := range enrichments {
		if err := enrichment.Validate(); err != nil {
			return nil, fmt.Errorf("enrichment[%d]: %w", i, err)
		}

		// Find existing group by groupReference
		existingGroup := findGroupByReference(result, enrichment.GroupReference)

		if existingGroup != nil {
			// Add attributes to existing group
			addAttributesToGroup(existingGroup, enrichment.AttributesToAdd)
		} else if enrichment.ShouldCreateIfNotExists() {
			// Group not found but should be created
			newGroup := CreateNewGroup(enrichment)
			result.DataExtraction.AttributeGroups = append(result.DataExtraction.AttributeGroups, newGroup)
		}
		// If group doesn't exist and ShouldCreateIfNotExists() is false, do nothing
	}

	return result, nil
}

// findGroupByReference finds a group in the document by its groupReference
func findGroupByReference(doc *models.CRTDLDocument, groupReference string) *models.AttributeGroup {
	for i := range doc.DataExtraction.AttributeGroups {
		if doc.DataExtraction.AttributeGroups[i].GroupReference == groupReference {
			return &doc.DataExtraction.AttributeGroups[i]
		}
	}
	return nil
}

// addAttributesToGroup adds enrichment attributes to an existing group
func addAttributesToGroup(group *models.AttributeGroup, attrs []models.EnrichmentAttribute) {
	for _, attr := range attrs {
		// Check if attribute already exists to avoid duplicates
		if !attributeExists(group, attr.AttributeRef) {
			group.Attributes = append(group.Attributes, models.Attribute{
				AttributeRef: attr.AttributeRef,
				MustHave:     attr.MustHave,
			})
		}
	}
}

// attributeExists checks if an attribute with the given ref already exists in the group
func attributeExists(group *models.AttributeGroup, attributeRef string) bool {
	for _, attr := range group.Attributes {
		if attr.AttributeRef == attributeRef {
			return true
		}
	}
	return false
}

// CreateNewGroup creates a new AttributeGroup from a GroupEnrichment
func CreateNewGroup(enrichment models.GroupEnrichment) models.AttributeGroup {
	attributes := make([]models.Attribute, 0, len(enrichment.AttributesToAdd))
	for _, attr := range enrichment.AttributesToAdd {
		attributes = append(attributes, models.Attribute{
			AttributeRef: attr.AttributeRef,
			MustHave:     attr.MustHave,
		})
	}

	return models.AttributeGroup{
		ID:             fmt.Sprintf("generated-%s", uuid.New().String()[:8]),
		Name:           enrichment.GetGroupName(),
		GroupReference: enrichment.GroupReference,
		Attributes:     attributes,
	}
}

// copyCRTDL creates a deep copy of a CRTDL document
func copyCRTDL(doc *models.CRTDLDocument) *models.CRTDLDocument {
	if doc == nil {
		return nil
	}

	groups := make([]models.AttributeGroup, len(doc.DataExtraction.AttributeGroups))
	for i, group := range doc.DataExtraction.AttributeGroups {
		attrs := make([]models.Attribute, len(group.Attributes))
		copy(attrs, group.Attributes)
		groups[i] = models.AttributeGroup{
			ID:             group.ID,
			Name:           group.Name,
			GroupReference: group.GroupReference,
			Attributes:     attrs,
		}
	}

	return &models.CRTDLDocument{
		DataExtraction: models.DataExtraction{
			AttributeGroups: groups,
		},
	}
}
