package unit

import (
	"testing"
	"time"

	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestGetFixedColumns(t *testing.T) {
	t.Run("id column", func(t *testing.T) {
		col := models.GetFixedIDColumn()
		assert.Equal(t, "id", col.Name)
		assert.Equal(t, "id", col.Path)
	})

	t.Run("patient column", func(t *testing.T) {
		col := models.GetFixedPatientColumn()
		assert.Equal(t, "patient", col.Name)
		assert.Equal(t, "subject.reference", col.Path)
	})
}
