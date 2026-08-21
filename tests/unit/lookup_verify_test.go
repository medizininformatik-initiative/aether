package unit

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/services"
)

func writeLookupFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "flatten-lookup.json")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

// minimalViewDefinition is the smallest viewDefinition the lookup schema accepts.
const minimalViewDefinition = `{"select": [{"column": [{"name": "family", "path": "name.family", "type": "string"}]}]}`

func TestVerifyLookupFileAcceptsValidFile(t *testing.T) {
	path := writeLookupFile(t, `[
		{
			"url": "https://example.com/StructureDefinition/TestProfile",
			"resourceType": "Patient",
			"elements": {
				"Patient.name": {"viewDefinition": `+minimalViewDefinition+`, "children": ["Patient.name.family"]},
				"Patient.name.family": {"viewDefinition": `+minimalViewDefinition+`}
			}
		}
	]`)

	warnings, err := services.VerifyLookupFile(path)

	require.NoError(t, err)
	assert.Empty(t, warnings)
}

func TestVerifyLookupFileAggregatesUnresolvedChildrenIntoOneWarning(t *testing.T) {
	path := writeLookupFile(t, `[
		{
			"url": "https://example.com/StructureDefinition/TestProfile",
			"resourceType": "Patient",
			"elements": {
				"Patient.name": {"viewDefinition": `+minimalViewDefinition+`, "children": ["Patient.name.missing", "Patient.name.gone"]}
			}
		}
	]`)

	warnings, err := services.VerifyLookupFile(path)

	require.NoError(t, err)
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], "unresolved-child")
	assert.Contains(t, warnings[0], "Patient.name.missing")
	assert.Contains(t, warnings[0], "Patient.name.gone")
}

func TestVerifyLookupFileMergesOneWarningCodeAcrossTables(t *testing.T) {
	path := writeLookupFile(t, `[
		{
			"url": "https://example.com/StructureDefinition/ProfileA",
			"resourceType": "Patient",
			"elements": {
				"Patient.name": {"viewDefinition": `+minimalViewDefinition+`, "children": ["Patient.name.missing"]}
			}
		},
		{
			"url": "https://example.com/StructureDefinition/ProfileB",
			"resourceType": "Patient",
			"elements": {
				"Patient.contact": {"viewDefinition": `+minimalViewDefinition+`, "children": ["Patient.contact.missing"]}
			}
		}
	]`)

	warnings, err := services.VerifyLookupFile(path)

	require.NoError(t, err)
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], "ProfileA")
	assert.Contains(t, warnings[0], "ProfileB")
}

func TestVerifyLookupFileKeepsDistinctWarningCodesSeparate(t *testing.T) {
	// "Patient.name" has an unresolved child, and "Patient.other" does not
	// extend "Patient.name", so two different warning codes fire.
	path := writeLookupFile(t, `[
		{
			"url": "https://example.com/StructureDefinition/TestProfile",
			"resourceType": "Patient",
			"elements": {
				"Patient.name": {"viewDefinition": `+minimalViewDefinition+`, "children": ["Patient.name.missing", "Patient.other"]},
				"Patient.other": {"viewDefinition": `+minimalViewDefinition+`}
			}
		}
	]`)

	warnings, err := services.VerifyLookupFile(path)

	require.NoError(t, err)
	require.Len(t, warnings, 2)
	assert.Contains(t, warnings[0], "unresolved-child")
	assert.Contains(t, warnings[1], "parent-not-prefix")
}

func TestVerifyLookupFileReturnsWarningsWithoutError(t *testing.T) {
	// "Patient.other" does not extend "Patient.name", so the parent-not-prefix
	// warning fires; the file stays valid.
	path := writeLookupFile(t, `[
		{
			"url": "https://example.com/StructureDefinition/TestProfile",
			"resourceType": "Patient",
			"elements": {
				"Patient.name": {"viewDefinition": `+minimalViewDefinition+`, "children": ["Patient.other"]},
				"Patient.other": {"viewDefinition": `+minimalViewDefinition+`}
			}
		}
	]`)

	warnings, err := services.VerifyLookupFile(path)

	require.NoError(t, err)
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], "parent-not-prefix")
}

func TestVerifyLookupFileRejectsDuplicateURL(t *testing.T) {
	path := writeLookupFile(t, `[
		{
			"url": "https://example.com/StructureDefinition/TestProfile",
			"resourceType": "Patient",
			"elements": {"Patient.name": {"viewDefinition": `+minimalViewDefinition+`}}
		},
		{
			"url": "https://example.com/StructureDefinition/TestProfile",
			"resourceType": "Patient",
			"elements": {"Patient.name": {"viewDefinition": `+minimalViewDefinition+`}}
		}
	]`)

	_, err := services.VerifyLookupFile(path)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate profile URL")
}

func TestVerifyLookupFileRejectsMissingFile(t *testing.T) {
	_, err := services.VerifyLookupFile(filepath.Join(t.TempDir(), "absent.json"))
	require.Error(t, err)
}

// TestLoadLookupTablesRejectsSchemaViolation shows that LoadLookupTables uses
// the flattenlookup library as its validator: an element without a
// viewDefinition violates the schema, which the old field checks did not see.
func TestLoadLookupTablesRejectsSchemaViolation(t *testing.T) {
	path := writeLookupFile(t, `[
		{
			"url": "https://example.com/StructureDefinition/TestProfile",
			"resourceType": "Patient",
			"elements": {
				"Patient.name": {}
			}
		}
	]`)

	_, err := services.LoadLookupTables(path)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "schema-violation")
}
