// Package services provides CRTDL preprocessing functionality for DIMP enrichment.
//
// CRTDL Preprocessing:
// This package contains pure functions for enriching CRTDL documents with additional
// attributes required by DIMP (pseudonymization) before sending to TORCH.
//
// Design Principles:
//   - Pure Functions: All public functions take inputs and return outputs without side effects
//   - Immutability: Input data structures are never mutated; new structures are created
//   - LinkedGroups Resolution: Profile URLs are resolved to group IDs for DIMP processing
//
// Example Usage:
//
//	enrichments, _ := LoadEnrichments(config.CRTDLPreprocessing)
//	enrichedDoc, _ := EnrichCRTDL(originalDoc, enrichments)
//	// enrichedDoc contains additional attributes required by DIMP
package services

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/google/uuid"

	"github.com/medizininformatik-initiative/aether/internal/models"
)

// EnrichCRTDL applies enrichment rules to a CRTDL document and returns a new enriched document.
// Pure function: Takes CRTDL document and enrichment rules, returns new enriched document.
//
// Process:
//  1. Deep copy the original document to ensure immutability
//  2. For each enrichment rule:
//     a. Find matching group by groupReference
//     b. If found: add/update attributes in the group
//     c. If not found and addGroupIfNotExists: create new group
//  3. Resolve all linkedGroups profile URLs against the complete document
//  4. Return enriched document
//
// Step 3 runs after all rules apply, so a rule can link a group that a later
// rule creates. A profile URL that matches no attribute group is an error:
// DIMP needs the link, and silent removal gives a document that looks correct.
//
// Parameters:
//
//	doc - Original CRTDL document (will not be modified)
//	enrichments - List of enrichment rules to apply
//
// Returns:
//
//	Enriched CRTDL document with additional attributes
//	Error if a linkedGroups entry matches no attribute group
func EnrichCRTDL(doc models.CRTDLDocument, enrichments []models.GroupEnrichment) (models.CRTDLDocument, error) {
	if len(enrichments) == 0 {
		return deepCopyCRTDL(doc), nil
	}

	// Deep copy the document to ensure immutability
	result := deepCopyCRTDL(doc)

	// Apply each enrichment rule
	for _, enrichment := range enrichments {
		// Find group by groupReference
		groupIndex := findGroupIndexByReference(result, enrichment.GroupReference)

		if groupIndex >= 0 {
			// Group found - add/update attributes
			result.DataExtraction.AttributeGroups[groupIndex] = AddAttributesToGroup(
				result.DataExtraction.AttributeGroups[groupIndex],
				enrichment.AttributesToAdd,
				result,
			)
		} else if enrichment.ShouldCreateIfNotExists() {
			// Group not found but should be created
			result.DataExtraction.AttributeGroups = append(
				result.DataExtraction.AttributeGroups,
				CreateNewGroup(enrichment),
			)
		}
		// If group not found and addGroupIfNotExists is false, skip silently
	}

	// A rule can link a group that a later rule creates. Resolve again over the
	// complete document, so the order of the rules does not change the result.
	for i, group := range result.DataExtraction.AttributeGroups {
		result.DataExtraction.AttributeGroups[i] = resolveLinkedGroupsForGroup(group, result)
	}

	if err := checkLinkedGroupsResolved(result); err != nil {
		return models.CRTDLDocument{}, err
	}

	return result, nil
}

// AddAttributesToGroup adds or updates attributes in an attribute group.
// Pure function: Returns new group with added/updated attributes.
//
// Behavior:
//   - If attribute already exists (same attributeRef): update mustHave if enrichment has mustHave=true
//   - If attribute doesn't exist: add it with resolved linkedGroups
//
// Parameters:
//
//	group - Original attribute group (will not be modified)
//	attrs - Attributes to add or update
//	doc - Full CRTDL document (used for linkedGroups resolution)
//
// Returns:
//
//	New attribute group with added/updated attributes
func AddAttributesToGroup(group models.AttributeGroup, attrs []models.EnrichmentAttribute, doc models.CRTDLDocument) models.AttributeGroup {
	// Deep copy group
	result := deepCopyGroup(group)

	for _, enrichAttr := range attrs {
		// Check if attribute already exists
		existingIndex := findAttributeIndex(result.Attributes, enrichAttr.AttributeRef)

		if existingIndex >= 0 {
			// Update existing attribute - only update mustHave if enrichment has it true
			if enrichAttr.MustHave {
				result.Attributes[existingIndex].MustHave = true
			}
			// Also update linkedGroups if provided
			if len(enrichAttr.LinkedGroups) > 0 {
				result.Attributes[existingIndex].LinkedGroups = ResolveLinkedGroups(doc, enrichAttr.LinkedGroups)
			}
		} else {
			// Add new attribute
			// Enrichment attributes use mustHave=false to avoid filtering out
			// resources that don't have the enriched attribute (e.g., when test
			// data doesn't have the specific identifier slice like
			// Patient.identifier:PseudonymisierterIdentifier)
			newAttr := models.Attribute{
				AttributeRef: enrichAttr.AttributeRef,
				MustHave:     false,
			}
			// Resolve linkedGroups if provided
			if len(enrichAttr.LinkedGroups) > 0 {
				newAttr.LinkedGroups = ResolveLinkedGroups(doc, enrichAttr.LinkedGroups)
			}
			result.Attributes = append(result.Attributes, newAttr)
		}
	}

	return result
}

// CreateNewGroup creates a new attribute group from an enrichment rule.
// Pure function: Returns new attribute group with generated ID.
//
// Parameters:
//
//	enrichment - Enrichment rule containing group details and attributes
//
// Returns:
//
//	New attribute group with generated ID
func CreateNewGroup(enrichment models.GroupEnrichment) models.AttributeGroup {
	// Generate a unique ID for the new group
	groupID := fmt.Sprintf("generated-%s", uuid.New().String()[:8])

	// Convert enrichment attributes to CRTDL attributes
	// Enrichment attributes always have mustHave=false to avoid filtering out
	// resources that don't have the enriched attribute
	attributes := make([]models.Attribute, 0, len(enrichment.AttributesToAdd))
	for _, enrichAttr := range enrichment.AttributesToAdd {
		// Note: linkedGroups will be resolved separately after group creation
		attributes = append(attributes, models.Attribute{
			AttributeRef: enrichAttr.AttributeRef,
			MustHave:     false,
			LinkedGroups: enrichAttr.LinkedGroups,
		})
	}

	return models.AttributeGroup{
		ID:             groupID,
		Name:           enrichment.GetGroupName(),
		GroupReference: enrichment.GroupReference,
		Attributes:     attributes,
	}
}

// ResolveLinkedGroups resolves profile URLs to group IDs in the CRTDL document.
// Pure function: Takes document and profile URLs, returns resolved group IDs.
//
// An entry that matches no attribute group stays unchanged. A dropped entry
// makes the lost link invisible; a kept profile URL is visibly not a group id.
//
// Parameters:
//
//	doc - CRTDL document containing attribute groups
//	profileURLs - List of profile URLs to resolve
//
// Returns:
//
//	One entry per input: the group ID if a group matches, else the input URL
func ResolveLinkedGroups(doc models.CRTDLDocument, profileURLs []string) []string {
	if len(profileURLs) == 0 {
		return []string{}
	}

	resolvedIDs := make([]string, 0, len(profileURLs))

	for _, profileURL := range profileURLs {
		resolvedIDs = append(resolvedIDs, resolveProfileURL(doc, profileURL))
	}

	return resolvedIDs
}

// LoadEnrichments loads enrichment rules from configuration.
// Supports loading from external JSON file or inline configuration.
//
// Parameters:
//
//	config - CRTDL preprocessing configuration
//
// Returns:
//
//	List of enrichment rules
//	Error if loading fails
func LoadEnrichments(config models.CRTDLPreprocessingConfig) ([]models.GroupEnrichment, error) {
	// If external file is specified, load from file
	if config.EnrichmentsPath != "" {
		return loadEnrichmentsFromFile(config.EnrichmentsPath)
	}

	// Otherwise use inline enrichments (may be empty)
	return config.Enrichments, nil
}

// loadEnrichmentsFromFile reads enrichment rules from a JSON file.
func loadEnrichmentsFromFile(path string) ([]models.GroupEnrichment, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read enrichments file: %w", err)
	}

	var enrichments []models.GroupEnrichment
	if err := json.Unmarshal(data, &enrichments); err != nil {
		return nil, fmt.Errorf("failed to parse enrichments file: %w", err)
	}

	return enrichments, nil
}

// --- Internal Helper Functions ---

// resolveProfileURL returns the id of the group whose groupReference is
// profileURL. It returns profileURL unchanged if no group matches.
func resolveProfileURL(doc models.CRTDLDocument, profileURL string) string {
	for _, group := range doc.DataExtraction.AttributeGroups {
		if group.GroupReference == profileURL {
			return group.ID
		}
	}
	return profileURL
}

// checkLinkedGroupsResolved reports every linkedGroups entry that is not the id
// of an attribute group in the document. Such an entry is a profile URL that
// matches no group. DIMP needs the link, so the enrichment must not continue.
func checkLinkedGroupsResolved(doc models.CRTDLDocument) error {
	ids := make(map[string]bool, len(doc.DataExtraction.AttributeGroups))
	for _, group := range doc.DataExtraction.AttributeGroups {
		ids[group.ID] = true
	}

	var unresolved []string
	for _, group := range doc.DataExtraction.AttributeGroups {
		for _, attr := range group.Attributes {
			for _, linked := range attr.LinkedGroups {
				if ids[linked] {
					continue
				}
				unresolved = append(unresolved, fmt.Sprintf("group %q attribute %q links %q",
					group.ID, attr.AttributeRef, linked))
			}
		}
	}

	if len(unresolved) == 0 {
		return nil
	}

	return fmt.Errorf("%d linkedGroups reference(s) match no attribute group: %s",
		len(unresolved), strings.Join(unresolved, "; "))
}

// cloneExtra deep-copies a map of preserved unknown JSON fields.
func cloneExtra(in map[string]json.RawMessage) map[string]json.RawMessage {
	if in == nil {
		return nil
	}
	out := make(map[string]json.RawMessage, len(in))
	for k, v := range in {
		b := make(json.RawMessage, len(v))
		copy(b, v)
		out[k] = b
	}
	return out
}

// deepCopyCRTDL creates a deep copy of a CRTDL document.
func deepCopyCRTDL(doc models.CRTDLDocument) models.CRTDLDocument {
	result := models.CRTDLDocument{
		Display: doc.Display,
		Version: doc.Version,
		DataExtraction: models.DataExtraction{
			AttributeGroups: make([]models.AttributeGroup, len(doc.DataExtraction.AttributeGroups)),
			Extra:           cloneExtra(doc.DataExtraction.Extra),
		},
		Extra: cloneExtra(doc.Extra),
	}

	// Deep copy CohortDefinition (json.RawMessage is a []byte)
	if doc.CohortDefinition != nil {
		result.CohortDefinition = make([]byte, len(doc.CohortDefinition))
		copy(result.CohortDefinition, doc.CohortDefinition)
	}

	for i, group := range doc.DataExtraction.AttributeGroups {
		result.DataExtraction.AttributeGroups[i] = deepCopyGroup(group)
	}

	return result
}

// deepCopyGroup creates a deep copy of an attribute group.
func deepCopyGroup(group models.AttributeGroup) models.AttributeGroup {
	result := models.AttributeGroup{
		ID:             group.ID,
		Name:           group.Name,
		GroupReference: group.GroupReference,
		Attributes:     make([]models.Attribute, len(group.Attributes)),
		Extra:          cloneExtra(group.Extra),
	}

	for i, attr := range group.Attributes {
		result.Attributes[i] = deepCopyAttribute(attr)
	}

	return result
}

// deepCopyAttribute creates a deep copy of an attribute.
func deepCopyAttribute(attr models.Attribute) models.Attribute {
	result := models.Attribute{
		AttributeRef: attr.AttributeRef,
		MustHave:     attr.MustHave,
		Extra:        cloneExtra(attr.Extra),
	}

	if len(attr.LinkedGroups) > 0 {
		result.LinkedGroups = make([]string, len(attr.LinkedGroups))
		copy(result.LinkedGroups, attr.LinkedGroups)
	}

	return result
}

// findGroupIndexByReference finds the index of a group by its groupReference.
// Returns -1 if not found.
func findGroupIndexByReference(doc models.CRTDLDocument, groupReference string) int {
	for i, group := range doc.DataExtraction.AttributeGroups {
		if group.GroupReference == groupReference {
			return i
		}
	}
	return -1
}

// findAttributeIndex finds the index of an attribute by its attributeRef.
// Returns -1 if not found.
func findAttributeIndex(attrs []models.Attribute, attributeRef string) int {
	for i, attr := range attrs {
		if attr.AttributeRef == attributeRef {
			return i
		}
	}
	return -1
}

// resolveLinkedGroupsForGroup resolves linkedGroups for all attributes in a group.
func resolveLinkedGroupsForGroup(group models.AttributeGroup, doc models.CRTDLDocument) models.AttributeGroup {
	result := deepCopyGroup(group)

	for i, attr := range result.Attributes {
		if len(attr.LinkedGroups) > 0 {
			// The linkedGroups currently contain profile URLs, resolve them to IDs
			result.Attributes[i].LinkedGroups = ResolveLinkedGroups(doc, attr.LinkedGroups)
		}
	}

	return result
}
