// Package compat tests aether against real release artifacts of upstream
// projects. These tests skip unless the artifact path env var is set; CI
// downloads a pinned release and runs them.
package compat

import (
	"maps"
	"os"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

// lookupPathEnv points at a flatteningLookup.json extracted from the
// flattening.zip asset of a fhir-ontology-generator release.
const lookupPathEnv = "COMPAT_FLATTENING_LOOKUP"

// TestReleasedFlatteningLookup verifies that a released lookup file loads and
// that every profile in it yields a valid ViewDefinition when all of its
// elements are requested. This is the layer where generator output breaks
// aether, so no FHIR data or flattener service is needed.
func TestReleasedFlatteningLookup(t *testing.T) {
	path := os.Getenv(lookupPathEnv)
	if path == "" {
		t.Skipf("%s not set; set it to a flatteningLookup.json from a fhir-ontology-generator release", lookupPathEnv)
	}

	tables, err := services.LoadLookupTables(path)
	require.NoError(t, err, "released lookup file must load")
	require.NotEmpty(t, tables, "released lookup file must contain profiles")

	builder := services.NewViewDefinitionBuilder(tables)
	for _, table := range tables {
		t.Run(table.URL, func(t *testing.T) {
			group := models.AttributeGroup{
				ID:             table.URL,
				Name:           "compat",
				GroupReference: table.URL,
				Attributes:     allElementsAsAttributes(table),
			}

			viewDef, err := builder.BuildViewDefinition(group)
			require.NoError(t, err)
			require.NoError(t, services.ValidateViewDefinition(viewDef))

			// The builder prepends an id column (plus a patient column for
			// patient-compartment resources), so the profile's own elements
			// must contribute at least one more.
			fixedColumns := 1
			if _, ok := models.GetPatientReferencePath(table.ResourceType); ok {
				fixedColumns++
			}
			names := services.ExtractColumnNames(*viewDef)
			require.Greater(t, len(names), fixedColumns, "profile elements must yield columns")
		})
	}
}

// allElementsAsAttributes requests every element of a profile, sorted for
// deterministic runs, so the builder walks all lookup entries.
func allElementsAsAttributes(table models.LookupTable) []models.Attribute {
	attrs := make([]models.Attribute, 0, len(table.Elements))
	for _, id := range slices.Sorted(maps.Keys(table.Elements)) {
		attrs = append(attrs, models.Attribute{AttributeRef: id})
	}
	return attrs
}
