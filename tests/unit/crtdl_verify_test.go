package unit

import (
	"encoding/json"
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
	data, err := os.ReadFile(validCRTDLFixture)
	require.NoError(t, err)

	var doc map[string]any
	require.NoError(t, json.Unmarshal(data, &doc))
	mutate(doc)

	mutated, err := json.Marshal(doc)
	require.NoError(t, err)

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

func TestVerifyCRTDLFileRejectsMissingFile(t *testing.T) {
	require.Error(t, services.VerifyCRTDLFile(filepath.Join(t.TempDir(), "absent.json")))
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
