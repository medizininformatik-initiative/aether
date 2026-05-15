package models

import (
	"encoding/json"
	"fmt"
	"net/url"
	"time"
)

// AttributeGroupNamingSystem is the FHIR NamingSystem URL used by Torch in Provenance
// resources to identify which CRTDL attribute group a clinical resource belongs to.
const AttributeGroupNamingSystem = "https://www.medizininformatik-initiative.de/fhir/fdpg/NamingSystem/attribute_group"

// ProvenanceIndex maps resource references ("ResourceType/id") to CRTDL attribute group IDs.
// A resource can belong to multiple attribute groups (e.g. two Procedure groups with different
// attribute selections), so each reference maps to a slice of group IDs.
type ProvenanceIndex map[string][]string

// FlatteningConfig holds configuration for the fhir-flattener service
type FlatteningConfig struct {
	ServiceURL  string        `yaml:"service_url" json:"service_url" mapstructure:"service_url"`       // URL to fhir-flattener service
	LookupPath  string        `yaml:"lookup_path" json:"lookup_path" mapstructure:"lookup_path"`       // Path to flatten-lookup.json file
	Formats     []string      `yaml:"formats" json:"formats" mapstructure:"formats"`                   // Output formats: ["csv"] for now
	Timeout     time.Duration `yaml:"timeout" json:"timeout" mapstructure:"timeout"`                   // Request timeout
	BatchSizeMB int           `yaml:"batch_size_mb" json:"batch_size_mb" mapstructure:"batch_size_mb"` // Total memory budget in MB for batched streaming (default 500)
}

// DefaultFlatteningConfig returns the default flattening configuration
func DefaultFlatteningConfig() FlatteningConfig {
	return FlatteningConfig{
		ServiceURL:  "",
		LookupPath:  "",
		Formats:     []string{"csv"},
		Timeout:     30 * time.Minute,
		BatchSizeMB: 500,
	}
}

// GetBatchSizeBytes returns the total memory budget in bytes, defaulting to 500MB if not set
func (c *FlatteningConfig) GetBatchSizeBytes() int {
	if c.BatchSizeMB <= 0 {
		return 500 * 1024 * 1024
	}
	return c.BatchSizeMB * 1024 * 1024
}

// Validate checks if the FlatteningConfig is valid when flattening is enabled
func (c *FlatteningConfig) Validate() error {
	if c.ServiceURL == "" {
		return fmt.Errorf("flattening service_url is required")
	}

	parsedURL, err := url.Parse(c.ServiceURL)
	if err != nil {
		return fmt.Errorf("invalid flattening service_url: %w", err)
	}

	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return fmt.Errorf("invalid flattening service_url: must use http or https scheme, got '%s'", parsedURL.Scheme)
	}

	if c.LookupPath == "" {
		return fmt.Errorf("flattening lookup_path is required")
	}

	if len(c.Formats) == 0 {
		return fmt.Errorf("flattening formats must contain at least one format")
	}

	for _, format := range c.Formats {
		if format != "csv" {
			return fmt.Errorf("invalid flattening format: %s (only 'csv' is supported)", format)
		}
	}

	if c.Timeout <= 0 {
		return fmt.Errorf("flattening timeout must be > 0, got %s", c.Timeout)
	}

	return nil
}

// LookupTable represents the flatten-lookup.json structure for a single profile
type LookupTable struct {
	URL          string                   `json:"url"`          // Profile URL (e.g., https://www.medizininformatik-initiative.de/fhir/core/modul-person/StructureDefinition/Patient)
	ResourceType string                   `json:"resourceType"` // FHIR resource type (Patient, Condition, etc.)
	Elements     map[string]LookupElement `json:"elements"`     // elementId -> definition
}

// LookupElement represents a single element's flattening configuration
type LookupElement struct {
	Parent         string         `json:"parent,omitempty"`   // Parent element ID (for nested elements)
	ViewDefinition ViewDefSnippet `json:"viewDefinition"`     // The ViewDefinition snippet for this element
	Children       []string       `json:"children,omitempty"` // Child element IDs
}

// ViewDefSnippet represents the viewDefinition snippet within a lookup element
// This contains partial ViewDefinition data that will be merged into the final ViewDefinition
type ViewDefSnippet struct {
	ForEach       string             `json:"forEach,omitempty"`       // ForEach expression at viewDefinition level
	ForEachOrNull string             `json:"forEachOrNull,omitempty"` // ForEachOrNull expression at viewDefinition level
	Column        []ColumnDefinition `json:"column,omitempty"`        // Column definitions at viewDefinition level (for leaf elements)
	Select        []SelectClause     `json:"select,omitempty"`        // Select clauses for this element
}

// ViewDefinition represents a complete SQL-on-FHIR ViewDefinition
// See: https://sql-on-fhir.org/ig/StructureDefinition/ViewDefinition
type ViewDefinition struct {
	ResourceType string         `json:"resourceType"` // Always "https://sql-on-fhir.org/ig/StructureDefinition/ViewDefinition"
	Name         string         `json:"name"`         // ViewDefinition name (from attributeGroup.name)
	Status       string         `json:"status"`       // Status (always "draft")
	Resource     string         `json:"resource"`     // FHIR resource type from lookup (e.g., "Patient", "Condition")
	Select       []SelectClause `json:"select"`       // Selection clauses
}

// SelectClause represents a select clause in a ViewDefinition
type SelectClause struct {
	Column        []ColumnDefinition `json:"column,omitempty"`        // Column definitions
	Select        []SelectClause     `json:"select,omitempty"`        // Nested select clauses
	ForEach       string             `json:"forEach,omitempty"`       // ForEach expression
	ForEachOrNull string             `json:"forEachOrNull,omitempty"` // ForEachOrNull expression
}

// ColumnDefinition represents a column in a ViewDefinition select clause
type ColumnDefinition struct {
	Name        string `json:"name"`                  // Column name
	Path        string `json:"path,omitempty"`        // FHIRPath expression
	Type        string `json:"type,omitempty"`        // Column type (string, integer, etc.)
	Collection  bool   `json:"collection,omitempty"`  // Whether this is a collection
	Description string `json:"description,omitempty"` // Column description
}

// CRTDLDocument represents the CRTDL file structure
// Includes all top-level fields to preserve the full document during preprocessing.
// Extra captures any unknown top-level fields so they survive JSON round-trips
// (see issue #356).
type CRTDLDocument struct {
	Display          string                     `json:"-"`
	Version          string                     `json:"-"`
	CohortDefinition json.RawMessage            `json:"-"`
	DataExtraction   DataExtraction             `json:"-"`
	Extra            map[string]json.RawMessage `json:"-"`
}

// DataExtraction represents the dataExtraction section of a CRTDL document.
// Extra preserves unknown sibling fields.
type DataExtraction struct {
	AttributeGroups []AttributeGroup           `json:"-"`
	Extra           map[string]json.RawMessage `json:"-"`
}

// AttributeGroup represents an attributeGroup in the CRTDL dataExtraction.
// Extra preserves unknown fields such as `includeReferenceOnly`.
type AttributeGroup struct {
	ID             string                     `json:"-"` // Required: used for provenance-based resource routing
	Name           string                     `json:"-"` // Group name (used for CSV filename)
	GroupReference string                     `json:"-"` // Profile URL for resource matching
	Attributes     []Attribute                `json:"-"` // List of attributes to extract
	Extra          map[string]json.RawMessage `json:"-"`
}

// Attribute represents a single attribute within an attributeGroup.
// Extra preserves unknown fields.
type Attribute struct {
	AttributeRef string                     `json:"-"` // Element ID reference to lookup
	MustHave     bool                       `json:"-"` // Whether this attribute is required
	LinkedGroups []string                   `json:"-"` // Profile URLs for linked groups (resolved to IDs during preprocessing)
	Extra        map[string]json.RawMessage `json:"-"`
}

// --- Custom JSON marshaling preserves unknown fields (issue #356) ---

func unmarshalKnown(raw map[string]json.RawMessage, key string, target any) error {
	v, ok := raw[key]
	if !ok {
		return nil
	}
	delete(raw, key)
	if len(v) == 0 || string(v) == "null" {
		return nil
	}
	return json.Unmarshal(v, target)
}

func mustMarshal(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("CRTDL marshal failed for %T: %v", v, err))
	}
	return b
}

func mergeExtra(out map[string]json.RawMessage, extra map[string]json.RawMessage) {
	for k, v := range extra {
		out[k] = v
	}
}

func (a *Attribute) UnmarshalJSON(data []byte) error {
	raw := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if err := unmarshalKnown(raw, "attributeRef", &a.AttributeRef); err != nil {
		return err
	}
	if err := unmarshalKnown(raw, "mustHave", &a.MustHave); err != nil {
		return err
	}
	if err := unmarshalKnown(raw, "linkedGroups", &a.LinkedGroups); err != nil {
		return err
	}
	if len(raw) > 0 {
		a.Extra = raw
	}
	return nil
}

func (a Attribute) MarshalJSON() ([]byte, error) {
	out := map[string]json.RawMessage{}
	mergeExtra(out, a.Extra)
	out["attributeRef"] = mustMarshal(a.AttributeRef)
	out["mustHave"] = mustMarshal(a.MustHave)
	if len(a.LinkedGroups) > 0 {
		out["linkedGroups"] = mustMarshal(a.LinkedGroups)
	}
	return json.Marshal(out)
}

func (g *AttributeGroup) UnmarshalJSON(data []byte) error {
	raw := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if err := unmarshalKnown(raw, "id", &g.ID); err != nil {
		return err
	}
	if err := unmarshalKnown(raw, "name", &g.Name); err != nil {
		return err
	}
	if err := unmarshalKnown(raw, "groupReference", &g.GroupReference); err != nil {
		return err
	}
	if err := unmarshalKnown(raw, "attributes", &g.Attributes); err != nil {
		return err
	}
	if len(raw) > 0 {
		g.Extra = raw
	}
	return nil
}

func (g AttributeGroup) MarshalJSON() ([]byte, error) {
	out := map[string]json.RawMessage{}
	mergeExtra(out, g.Extra)
	out["id"] = mustMarshal(g.ID)
	out["name"] = mustMarshal(g.Name)
	out["groupReference"] = mustMarshal(g.GroupReference)
	out["attributes"] = mustMarshal(g.Attributes)
	return json.Marshal(out)
}

func (d *DataExtraction) UnmarshalJSON(data []byte) error {
	raw := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if err := unmarshalKnown(raw, "attributeGroups", &d.AttributeGroups); err != nil {
		return err
	}
	if len(raw) > 0 {
		d.Extra = raw
	}
	return nil
}

func (d DataExtraction) MarshalJSON() ([]byte, error) {
	out := map[string]json.RawMessage{}
	mergeExtra(out, d.Extra)
	out["attributeGroups"] = mustMarshal(d.AttributeGroups)
	return json.Marshal(out)
}

func (c *CRTDLDocument) UnmarshalJSON(data []byte) error {
	raw := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if err := unmarshalKnown(raw, "display", &c.Display); err != nil {
		return err
	}
	if err := unmarshalKnown(raw, "version", &c.Version); err != nil {
		return err
	}
	if v, ok := raw["cohortDefinition"]; ok {
		c.CohortDefinition = append(c.CohortDefinition[:0], v...)
		delete(raw, "cohortDefinition")
	}
	if err := unmarshalKnown(raw, "dataExtraction", &c.DataExtraction); err != nil {
		return err
	}
	if len(raw) > 0 {
		c.Extra = raw
	}
	return nil
}

func (c CRTDLDocument) MarshalJSON() ([]byte, error) {
	out := map[string]json.RawMessage{}
	mergeExtra(out, c.Extra)
	if c.Display != "" {
		out["display"] = mustMarshal(c.Display)
	}
	if c.Version != "" {
		out["version"] = mustMarshal(c.Version)
	}
	if len(c.CohortDefinition) > 0 {
		out["cohortDefinition"] = append(json.RawMessage(nil), c.CohortDefinition...)
	}
	out["dataExtraction"] = mustMarshal(c.DataExtraction)
	return json.Marshal(out)
}

// FlatteningRequest represents the FHIR Parameters request body sent to fhir-flattener
type FlatteningRequest struct {
	ResourceType string                `json:"resourceType"` // Always "Parameters"
	Parameter    []FlatteningParameter `json:"parameter"`
}

// FlatteningParameter represents a parameter in the FlatteningRequest
type FlatteningParameter struct {
	Name     string `json:"name"`               // "viewDefinition" or "resources"
	Resource any    `json:"resource,omitempty"` // The actual resource (ViewDefinition or FHIR resource)
}

// NewFlatteningRequest creates a new FlatteningRequest with the given ViewDefinition and resources
func NewFlatteningRequest(viewDef ViewDefinition, resources []map[string]any) FlatteningRequest {
	params := make([]FlatteningParameter, 0, len(resources)+1)

	// Add ViewDefinition as first parameter
	params = append(params, FlatteningParameter{
		Name:     "viewDefinition",
		Resource: viewDef,
	})

	// Add each resource as a "resources" parameter
	for _, resource := range resources {
		params = append(params, FlatteningParameter{
			Name:     "resources",
			Resource: resource,
		})
	}

	return FlatteningRequest{
		ResourceType: "Parameters",
		Parameter:    params,
	}
}

// GetFixedIDColumn returns the fixed "id" column definition
func GetFixedIDColumn() ColumnDefinition {
	return ColumnDefinition{
		Name: "id",
		Path: "id",
	}
}

// patientCompartmentPaths maps FHIR R4 resource types in the Patient compartment to their
// patient reference FHIRPath. Only resources with a single, clear patient reference element
// are included. Resources with complex multi-element paths (AuditEvent, Appointment, Group,
// Person, Provenance, Schedule) are intentionally excluded.
// Source: https://hl7.org/fhir/R4/compartmentdefinition-patient.html
var patientCompartmentPaths = map[string]string{
	// Resources using subject.reference
	"Account":                  "subject.reference",
	"AdverseEvent":             "subject.reference",
	"Basic":                    "subject.reference",
	"CarePlan":                 "subject.reference",
	"CareTeam":                 "subject.reference",
	"ChargeItem":               "subject.reference",
	"ClinicalImpression":       "subject.reference",
	"Communication":            "subject.reference",
	"CommunicationRequest":     "subject.reference",
	"Composition":              "subject.reference",
	"Condition":                "subject.reference",
	"DeviceRequest":            "subject.reference",
	"DeviceUseStatement":       "subject.reference",
	"DiagnosticReport":         "subject.reference",
	"DocumentManifest":         "subject.reference",
	"DocumentReference":        "subject.reference",
	"Encounter":                "subject.reference",
	"Flag":                     "subject.reference",
	"Goal":                     "subject.reference",
	"ImagingStudy":             "subject.reference",
	"Invoice":                  "subject.reference",
	"List":                     "subject.reference",
	"MeasureReport":            "subject.reference",
	"Media":                    "subject.reference",
	"MedicationAdministration": "subject.reference",
	"MedicationDispense":       "subject.reference",
	"MedicationRequest":        "subject.reference",
	"MedicationStatement":      "subject.reference",
	"Observation":              "subject.reference",
	"Procedure":                "subject.reference",
	"QuestionnaireResponse":    "subject.reference",
	"RequestGroup":             "subject.reference",
	"RiskAssessment":           "subject.reference",
	"ServiceRequest":           "subject.reference",
	"Specimen":                 "subject.reference",
	"SupplyRequest":            "subject.reference",

	// Resources using patient.reference
	"AllergyIntolerance":          "patient.reference",
	"BodyStructure":               "patient.reference",
	"Claim":                       "patient.reference",
	"ClaimResponse":               "patient.reference",
	"Consent":                     "patient.reference",
	"CoverageEligibilityRequest":  "patient.reference",
	"CoverageEligibilityResponse": "patient.reference",
	"DetectedIssue":               "patient.reference",
	"EpisodeOfCare":               "patient.reference",
	"ExplanationOfBenefit":        "patient.reference",
	"FamilyMemberHistory":         "patient.reference",
	"Immunization":                "patient.reference",
	"ImmunizationEvaluation":      "patient.reference",
	"ImmunizationRecommendation":  "patient.reference",
	"MolecularSequence":           "patient.reference",
	"NutritionOrder":              "patient.reference",
	"RelatedPerson":               "patient.reference",
	"SupplyDelivery":              "patient.reference",
	"VisionPrescription":          "patient.reference",

	// Resources using other reference paths
	"Coverage":          "beneficiary.reference",
	"EnrollmentRequest": "candidate.reference",
	"ResearchSubject":   "individual.reference",
}

// GetPatientReferencePath returns the FHIRPath to the patient reference for a given
// resource type, and whether the resource is in the FHIR R4 Patient compartment.
// Returns ("", false) for Patient itself and for resources not in the compartment.
func GetPatientReferencePath(resourceType string) (string, bool) {
	path, ok := patientCompartmentPaths[resourceType]
	return path, ok
}

// NewBaseViewDefinition creates a base ViewDefinition with required fields
func NewBaseViewDefinition(name string, resourceType string) ViewDefinition {
	return ViewDefinition{
		ResourceType: "https://sql-on-fhir.org/ig/StructureDefinition/ViewDefinition",
		Name:         name,
		Status:       "draft",
		Resource:     resourceType,
		Select:       []SelectClause{},
	}
}
