package unit

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/lib/crtdl"
)

// mutateCRTDLBytes loads the valid fixture, applies the mutation, and
// returns the result as JSON bytes.
func mutateCRTDLBytes(t *testing.T, mutate func(doc map[string]any)) []byte {
	t.Helper()
	data, err := os.ReadFile(validCRTDLFixture)
	require.NoError(t, err)

	var doc map[string]any
	require.NoError(t, json.Unmarshal(data, &doc))
	mutate(doc)

	mutated, err := json.Marshal(doc)
	require.NoError(t, err)
	return mutated
}

// findingCodes collects the codes of all findings in a result.
func findingCodes(result *crtdl.Result) []string {
	var codes []string
	for _, f := range result.Findings {
		codes = append(codes, f.Code)
	}
	return codes
}

func TestValidateAcceptsValidDocument(t *testing.T) {
	data, err := os.ReadFile(validCRTDLFixture)
	require.NoError(t, err)

	result := crtdl.Validate(data)

	assert.True(t, result.Valid())
	assert.Empty(t, result.Findings)
}

func TestValidateRejectsMalformedJSON(t *testing.T) {
	result := crtdl.Validate([]byte("{not json"))

	assert.False(t, result.Valid())
	require.Len(t, result.Findings, 1)
	assert.Equal(t, "malformed-json", result.Findings[0].Code)
	assert.NotEmpty(t, result.Findings[0].Message)
}

func TestValidateRejectsDuplicateGroupID(t *testing.T) {
	data := mutateCRTDLBytes(t, func(doc map[string]any) {
		extraction := doc["dataExtraction"].(map[string]any)
		groups := extraction["attributeGroups"].([]any)
		extraction["attributeGroups"] = append(groups, groups[0])
	})

	result := crtdl.Validate(data)

	assert.False(t, result.Valid())
	assert.Contains(t, findingCodes(result), "duplicate-group-id")

	var finding crtdl.Finding
	for _, f := range result.Findings {
		if f.Code == "duplicate-group-id" {
			finding = f
		}
	}
	assert.Equal(t, "1234", finding.Group)
	assert.Contains(t, finding.Message, "1234")
}

func TestValidateRejectsDateFilterEndBeforeStart(t *testing.T) {
	data := mutateCRTDLBytes(t, func(doc map[string]any) {
		groups := doc["dataExtraction"].(map[string]any)["attributeGroups"].([]any)
		filters := groups[0].(map[string]any)["filter"].([]any)
		for _, f := range filters {
			filterMap := f.(map[string]any)
			if filterMap["type"] == "date" {
				filterMap["start"] = "2021-10-09"
				filterMap["end"] = "2021-05-01"
			}
		}
	})

	result := crtdl.Validate(data)

	assert.False(t, result.Valid())
	require.Len(t, result.Findings, 1)
	finding := result.Findings[0]
	assert.Equal(t, "filter-date-order", finding.Code)
	assert.Equal(t, "1234", finding.Group)
	assert.Contains(t, finding.Message, "2021-05-01")
	assert.Contains(t, finding.Message, "2021-10-09")
}
