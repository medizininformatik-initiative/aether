package unit

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/services"
)

// validCRTDLFixture is a CRTDL document that the schema and the
// cross-reference checks accept without findings.
const validCRTDLFixture = "../../internal/lib/crtdl/testdata/diagnosis_linked_with_encounter_corrected.json"

func TestVerifyCRTDLFileAcceptsValidFile(t *testing.T) {
	require.NoError(t, services.VerifyCRTDLFile(validCRTDLFixture))
}

// mutateCRTDLFixture loads the valid fixture, applies the mutation, and
// writes the result to a file in a temporary directory.
func mutateCRTDLFixture(t *testing.T, mutate func(doc map[string]any)) string {
	t.Helper()
	mutated := mutateCRTDLBytes(t, mutate)
	path := filepath.Join(t.TempDir(), "crtdl.json")
	require.NoError(t, os.WriteFile(path, mutated, 0o644))
	return path
}

func TestVerifyCRTDLFileRejectsUnresolvedLinkedGroup(t *testing.T) {
	path := mutateCRTDLFixture(t, func(doc map[string]any) {
		groups := doc["dataExtraction"].(map[string]any)["attributeGroups"].([]any)
		attrs := groups[0].(map[string]any)["attributes"].([]any)
		attrs[1].(map[string]any)["linkedGroups"] = []any{"no-such-group"}
	})

	err := services.VerifyCRTDLFile(path)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "linked-group-not-found")
	assert.Contains(t, err.Error(), "no-such-group")
}

func TestVerifyCRTDLFileRejectsSchemaViolation(t *testing.T) {
	path := mutateCRTDLFixture(t, func(doc map[string]any) {
		groups := doc["dataExtraction"].(map[string]any)["attributeGroups"].([]any)
		delete(groups[0].(map[string]any), "groupReference")
	})

	err := services.VerifyCRTDLFile(path)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "schema-violation")
	assert.Contains(t, err.Error(), "groupReference")
}

func TestVerifyCRTDLFileRejectsDuplicateGroupNames(t *testing.T) {
	path := mutateCRTDLFixture(t, func(doc map[string]any) {
		groups := doc["dataExtraction"].(map[string]any)["attributeGroups"].([]any)
		groups[1].(map[string]any)["name"] = groups[0].(map[string]any)["name"]
	})

	err := services.VerifyCRTDLFile(path)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "TyphusDiagnosis")
	assert.Contains(t, err.Error(), "unique name")
}

func TestVerifyCRTDLFileRejectsGroupWithoutName(t *testing.T) {
	path := mutateCRTDLFixture(t, func(doc map[string]any) {
		groups := doc["dataExtraction"].(map[string]any)["attributeGroups"].([]any)
		delete(groups[0].(map[string]any), "name")
	})

	err := services.VerifyCRTDLFile(path)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing 'name' field")
}

func TestVerifyCRTDLFileRejectsGroupWithoutAttributes(t *testing.T) {
	path := mutateCRTDLFixture(t, func(doc map[string]any) {
		groups := doc["dataExtraction"].(map[string]any)["attributeGroups"].([]any)
		groups[1].(map[string]any)["attributes"] = []any{}
	})

	err := services.VerifyCRTDLFile(path)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one attribute")
}

func TestVerifyCRTDLFileRejectsEmptyAttributeGroups(t *testing.T) {
	path := mutateCRTDLFixture(t, func(doc map[string]any) {
		doc["dataExtraction"].(map[string]any)["attributeGroups"] = []any{}
	})

	err := services.VerifyCRTDLFile(path)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one attributeGroup")
}

func TestVerifyCRTDLFileRejectsGroupNamesThatMapToOneCSVFile(t *testing.T) {
	path := mutateCRTDLFixture(t, func(doc map[string]any) {
		groups := doc["dataExtraction"].(map[string]any)["attributeGroups"].([]any)
		groups[0].(map[string]any)["name"] = "Fall/Kontakt"
		groups[1].(map[string]any)["name"] = "Fall_Kontakt"
	})

	err := services.VerifyCRTDLFile(path)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "Fall_Kontakt.csv")
}

func TestVerifyCRTDLFileReportsAllAetherRuleViolations(t *testing.T) {
	path := mutateCRTDLFixture(t, func(doc map[string]any) {
		groups := doc["dataExtraction"].(map[string]any)["attributeGroups"].([]any)
		delete(groups[0].(map[string]any), "name")
		groups[1].(map[string]any)["attributes"] = []any{}
	})

	err := services.VerifyCRTDLFile(path)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing 'name' field")
	assert.Contains(t, err.Error(), "at least one attribute")
}

func TestVerifyCRTDLFileRejectsEmptyID(t *testing.T) {
	path := mutateCRTDLFixture(t, func(doc map[string]any) {
		groups := doc["dataExtraction"].(map[string]any)["attributeGroups"].([]any)
		groups[0].(map[string]any)["id"] = ""
	})

	err := services.VerifyCRTDLFile(path)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing 'id' field")
}

func TestVerifyCRTDLFileRejectsEmptyGroupReference(t *testing.T) {
	path := mutateCRTDLFixture(t, func(doc map[string]any) {
		groups := doc["dataExtraction"].(map[string]any)["attributeGroups"].([]any)
		groups[0].(map[string]any)["groupReference"] = ""
	})

	err := services.VerifyCRTDLFile(path)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "groupReference")
}

func TestVerifyCRTDLFileRejectsEmptyAttributeRef(t *testing.T) {
	path := mutateCRTDLFixture(t, func(doc map[string]any) {
		groups := doc["dataExtraction"].(map[string]any)["attributeGroups"].([]any)
		attrs := groups[0].(map[string]any)["attributes"].([]any)
		attrs[0].(map[string]any)["attributeRef"] = ""
	})

	err := services.VerifyCRTDLFile(path)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing 'attributeRef' field")
}

func TestVerifyCRTDLFileRejectsMissingFile(t *testing.T) {
	require.Error(t, services.VerifyCRTDLFile(filepath.Join(t.TempDir(), "absent.json")))
}

func TestParseCRTDLRejectsUnresolvedLinkedGroup(t *testing.T) {
	path := mutateCRTDLFixture(t, func(doc map[string]any) {
		groups := doc["dataExtraction"].(map[string]any)["attributeGroups"].([]any)
		attrs := groups[0].(map[string]any)["attributes"].([]any)
		attrs[1].(map[string]any)["linkedGroups"] = []any{"no-such-group"}
	})

	_, err := services.ParseCRTDL(path)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "linked-group-not-found")
}

func TestVerifyCRTDLBytesAcceptsValidDocument(t *testing.T) {
	data, err := os.ReadFile(validCRTDLFixture)
	require.NoError(t, err)

	require.NoError(t, services.VerifyCRTDLBytes(data))
}

func TestVerifyCRTDLBytesRejectsSchemaViolation(t *testing.T) {
	err := services.VerifyCRTDLBytes([]byte(`{"dataExtraction":{"attributeGroups":[]}}`))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "schema-violation")
}
