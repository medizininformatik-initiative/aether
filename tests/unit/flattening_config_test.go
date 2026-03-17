package unit

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/models"
)

func TestFlatteningConfigValidation(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		config := models.FlatteningConfig{
			ServiceURL: "http://localhost:8080",
			LookupPath: "/path/to/lookup.json",
			Formats:    []string{"csv"},
			Timeout:    30 * time.Minute,
		}
		err := config.Validate()
		assert.NoError(t, err)
	})

	t.Run("valid config with https", func(t *testing.T) {
		config := models.FlatteningConfig{
			ServiceURL: "https://flattener.example.com",
			LookupPath: "/path/to/lookup.json",
			Formats:    []string{"csv"},
			Timeout:    30 * time.Minute,
		}
		err := config.Validate()
		assert.NoError(t, err)
	})

	t.Run("missing service URL", func(t *testing.T) {
		config := models.FlatteningConfig{
			ServiceURL: "",
			LookupPath: "/path/to/lookup.json",
			Formats:    []string{"csv"},
			Timeout:    30 * time.Minute,
		}
		err := config.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "service_url is required")
	})

	t.Run("invalid URL format", func(t *testing.T) {
		config := models.FlatteningConfig{
			ServiceURL: "://invalid-url",
			LookupPath: "/path/to/lookup.json",
			Formats:    []string{"csv"},
			Timeout:    30 * time.Minute,
		}
		err := config.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid flattening service_url")
	})

	t.Run("invalid URL scheme", func(t *testing.T) {
		config := models.FlatteningConfig{
			ServiceURL: "ftp://localhost:8080",
			LookupPath: "/path/to/lookup.json",
			Formats:    []string{"csv"},
			Timeout:    30 * time.Minute,
		}
		err := config.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must use http or https")
	})

	t.Run("missing lookup path", func(t *testing.T) {
		config := models.FlatteningConfig{
			ServiceURL: "http://localhost:8080",
			LookupPath: "",
			Formats:    []string{"csv"},
			Timeout:    30 * time.Minute,
		}
		err := config.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "lookup_path is required")
	})

	t.Run("empty formats", func(t *testing.T) {
		config := models.FlatteningConfig{
			ServiceURL: "http://localhost:8080",
			LookupPath: "/path/to/lookup.json",
			Formats:    []string{},
			Timeout:    30 * time.Minute,
		}
		err := config.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "formats must contain at least one format")
	})

	t.Run("invalid format", func(t *testing.T) {
		config := models.FlatteningConfig{
			ServiceURL: "http://localhost:8080",
			LookupPath: "/path/to/lookup.json",
			Formats:    []string{"parquet"},
			Timeout:    30 * time.Minute,
		}
		err := config.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid flattening format")
	})

	t.Run("zero timeout", func(t *testing.T) {
		config := models.FlatteningConfig{
			ServiceURL: "http://localhost:8080",
			LookupPath: "/path/to/lookup.json",
			Formats:    []string{"csv"},
			Timeout:    0,
		}
		err := config.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "timeout must be > 0")
	})

	t.Run("negative timeout", func(t *testing.T) {
		config := models.FlatteningConfig{
			ServiceURL: "http://localhost:8080",
			LookupPath: "/path/to/lookup.json",
			Formats:    []string{"csv"},
			Timeout:    -1 * time.Second,
		}
		err := config.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "timeout must be > 0")
	})
}

func TestDefaultFlatteningConfig(t *testing.T) {
	config := models.DefaultFlatteningConfig()

	assert.Empty(t, config.ServiceURL)
	assert.Empty(t, config.LookupPath)
	assert.Equal(t, []string{"csv"}, config.Formats)
	assert.Equal(t, 30*time.Minute, config.Timeout)
	assert.Equal(t, 500, config.BatchSizeMB)
}

func TestGetBatchSizeBytes(t *testing.T) {
	t.Run("default when zero", func(t *testing.T) {
		config := models.FlatteningConfig{BatchSizeMB: 0}
		assert.Equal(t, 500*1024*1024, config.GetBatchSizeBytes())
	})

	t.Run("default when negative", func(t *testing.T) {
		config := models.FlatteningConfig{BatchSizeMB: -5}
		assert.Equal(t, 500*1024*1024, config.GetBatchSizeBytes())
	})

	t.Run("custom value", func(t *testing.T) {
		config := models.FlatteningConfig{BatchSizeMB: 20}
		assert.Equal(t, 20*1024*1024, config.GetBatchSizeBytes())
	})

	t.Run("small value", func(t *testing.T) {
		config := models.FlatteningConfig{BatchSizeMB: 1}
		assert.Equal(t, 1*1024*1024, config.GetBatchSizeBytes())
	})
}

func TestNewFlatteningRequest(t *testing.T) {
	viewDef := models.ViewDefinition{
		ResourceType: "https://sql-on-fhir.org/ig/StructureDefinition/ViewDefinition",
		Name:         "TestView",
		Status:       "draft",
		Resource:     "Patient",
		Select:       []models.SelectClause{},
	}

	resources := []map[string]any{
		{"resourceType": "Patient", "id": "1"},
		{"resourceType": "Patient", "id": "2"},
	}

	request := models.NewFlatteningRequest(viewDef, resources)

	assert.Equal(t, "Parameters", request.ResourceType)
	assert.Len(t, request.Parameter, 3) // 1 viewDefinition + 2 resources

	// First parameter should be viewDefinition
	assert.Equal(t, "viewDefinition", request.Parameter[0].Name)

	// Remaining parameters should be resources
	assert.Equal(t, "resources", request.Parameter[1].Name)
	assert.Equal(t, "resources", request.Parameter[2].Name)
}

func TestNewBaseViewDefinition(t *testing.T) {
	viewDef := models.NewBaseViewDefinition("TestName", "Patient")

	assert.Equal(t, "https://sql-on-fhir.org/ig/StructureDefinition/ViewDefinition", viewDef.ResourceType)
	assert.Equal(t, "TestName", viewDef.Name)
	assert.Equal(t, "draft", viewDef.Status)
	assert.Equal(t, "Patient", viewDef.Resource)
	assert.Empty(t, viewDef.Select)
}

func TestGetFixedIDColumn(t *testing.T) {
	col := models.GetFixedIDColumn()
	assert.Equal(t, "id", col.Name)
	assert.Equal(t, "id", col.Path)
}

func TestGetPatientReferencePath(t *testing.T) {
	t.Run("resources with subject.reference", func(t *testing.T) {
		subjectResources := []string{
			"Observation", "Condition", "DiagnosticReport", "MedicationRequest",
			"Procedure", "Encounter", "Specimen", "ServiceRequest",
		}
		for _, rt := range subjectResources {
			path, ok := models.GetPatientReferencePath(rt)
			assert.True(t, ok, "%s should be in patient compartment", rt)
			assert.Equal(t, "subject.reference", path, "%s should use subject.reference", rt)
		}
	})

	t.Run("resources with patient.reference", func(t *testing.T) {
		patientResources := []string{
			"AllergyIntolerance", "Consent", "Immunization", "EpisodeOfCare",
			"FamilyMemberHistory", "NutritionOrder", "VisionPrescription",
		}
		for _, rt := range patientResources {
			path, ok := models.GetPatientReferencePath(rt)
			assert.True(t, ok, "%s should be in patient compartment", rt)
			assert.Equal(t, "patient.reference", path, "%s should use patient.reference", rt)
		}
	})

	t.Run("resources with other reference paths", func(t *testing.T) {
		path, ok := models.GetPatientReferencePath("Coverage")
		assert.True(t, ok)
		assert.Equal(t, "beneficiary.reference", path)

		path, ok = models.GetPatientReferencePath("ResearchSubject")
		assert.True(t, ok)
		assert.Equal(t, "individual.reference", path)

		path, ok = models.GetPatientReferencePath("EnrollmentRequest")
		assert.True(t, ok)
		assert.Equal(t, "candidate.reference", path)
	})

	t.Run("Patient resource is not in compartment map", func(t *testing.T) {
		_, ok := models.GetPatientReferencePath("Patient")
		assert.False(t, ok, "Patient should not be in compartment map")
	})

	t.Run("non-compartment resources return false", func(t *testing.T) {
		nonCompartmentResources := []string{
			"Organization", "Practitioner", "Medication", "Location",
			"Device", "Substance", "HealthcareService", "PractitionerRole",
		}
		for _, rt := range nonCompartmentResources {
			_, ok := models.GetPatientReferencePath(rt)
			assert.False(t, ok, "%s should not be in patient compartment", rt)
		}
	})
}
