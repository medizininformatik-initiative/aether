package models

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

// HasForEach tells if the select clause iterates with forEach or forEachOrNull.
func (s SelectClause) HasForEach() bool {
	return s.ForEach != "" || s.ForEachOrNull != ""
}

// ColumnDefinition represents a column in a ViewDefinition select clause
type ColumnDefinition struct {
	Name        string `json:"name"`                  // Column name
	Path        string `json:"path,omitempty"`        // FHIRPath expression
	Type        string `json:"type,omitempty"`        // Column type (string, integer, etc.)
	Collection  bool   `json:"collection,omitempty"`  // Whether this is a collection
	Description string `json:"description,omitempty"` // Column description
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
