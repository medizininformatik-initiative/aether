package unit

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/lib/flattenlookup"
)

// lookupDoc wraps a JSON elements object into a one-table lookup document
// that satisfies the flatten-lookup schema.
func lookupDoc(elements string) []byte {
	return []byte(`[
		{
			"url": "https://example.com/StructureDefinition/TestProfile",
			"resourceType": "Patient",
			"elements": ` + elements + `
		}
	]`)
}

const viewDef = `{"column": [{"name": "value", "path": "value", "type": "string"}]}`

func TestFlattenlookupValidFileIsValidWithoutFindings(t *testing.T) {
	result := flattenlookup.Validate(lookupDoc(`{
		"Patient.name": {"viewDefinition": ` + viewDef + `, "children": ["Patient.name.family"]},
		"Patient.name.family": {"viewDefinition": ` + viewDef + `}
	}`))

	assert.True(t, result.Valid())
	assert.Empty(t, result.Findings)
}

func TestFlattenlookupErrorFindingMakesResultInvalid(t *testing.T) {
	// Two elements claim "Patient.name.family" as a child, so the
	// multiple-parents error fires.
	result := flattenlookup.Validate(lookupDoc(`{
		"Patient.name": {"viewDefinition": ` + viewDef + `, "children": ["Patient.name.family"]},
		"Patient.other": {"viewDefinition": ` + viewDef + `, "children": ["Patient.name.family"]},
		"Patient.name.family": {"viewDefinition": ` + viewDef + `}
	}`))

	assert.False(t, result.Valid())
	require.NotEmpty(t, result.Findings)
	finding := result.Findings[0]
	assert.Equal(t, flattenlookup.SeverityError, finding.Severity)
	assert.Equal(t, "multiple-parents", finding.Code)
	assert.Equal(t, "https://example.com/StructureDefinition/TestProfile", finding.Table)
	assert.Equal(t, "Patient.name.family", finding.Element)
}

func TestFlattenlookupWarningFindingKeepsResultValid(t *testing.T) {
	// "Patient.other" does not extend "Patient.name", so the parent-not-prefix
	// warning fires; a warning alone does not invalidate the file.
	result := flattenlookup.Validate(lookupDoc(`{
		"Patient.name": {"viewDefinition": ` + viewDef + `, "children": ["Patient.other"]},
		"Patient.other": {"viewDefinition": ` + viewDef + `}
	}`))

	assert.True(t, result.Valid())
	require.Len(t, result.Findings, 1)
	assert.Equal(t, flattenlookup.SeverityWarning, result.Findings[0].Severity)
	assert.Equal(t, "parent-not-prefix", result.Findings[0].Code)
}

func TestFlattenlookupVerboseReportsDeprecatedParent(t *testing.T) {
	// The stored parent agrees with the derived link, so the file has no
	// defect. Only verbose mode reports the deprecated field.
	doc := lookupDoc(`{
		"Patient.name": {"viewDefinition": ` + viewDef + `, "children": ["Patient.name.family"]},
		"Patient.name.family": {"viewDefinition": ` + viewDef + `, "parent": "Patient.name"}
	}`)

	quiet := flattenlookup.Validate(doc)
	assert.True(t, quiet.Valid())
	assert.Empty(t, quiet.Findings)

	verbose := flattenlookup.Validate(doc, flattenlookup.Verbose())
	assert.True(t, verbose.Valid())
	require.Len(t, verbose.Findings, 1)
	finding := verbose.Findings[0]
	assert.Equal(t, flattenlookup.SeverityWarning, finding.Severity)
	assert.Equal(t, "deprecated-parent", finding.Code)
	assert.Equal(t, "Patient.name.family", finding.Element)
}

func TestFlattenlookupStoredParentMismatchIsAWarning(t *testing.T) {
	// The stored parent names an element that does not list this element in
	// its children, so the derived link disagrees with the stored one.
	result := flattenlookup.Validate(lookupDoc(`{
		"Patient.name": {"viewDefinition": ` + viewDef + `, "children": ["Patient.name.family"]},
		"Patient.name.family": {"viewDefinition": ` + viewDef + `},
		"Patient.name.given": {"viewDefinition": ` + viewDef + `, "parent": "Patient.name"}
	}`))

	assert.True(t, result.Valid())
	require.Len(t, result.Findings, 1)
	finding := result.Findings[0]
	assert.Equal(t, flattenlookup.SeverityWarning, finding.Severity)
	assert.Equal(t, "parent-mismatch", finding.Code)
	assert.Equal(t, "Patient.name.given", finding.Element)
	assert.Contains(t, finding.Message, "no element lists this element")
}

func TestFlattenlookupAmbiguousChildReportsOnlyMultipleParents(t *testing.T) {
	// Two elements claim the same child, which is one defect. The stored
	// parent on the child must not add a second finding for it.
	result := flattenlookup.Validate(lookupDoc(`{
		"Patient.name": {"viewDefinition": ` + viewDef + `, "children": ["Patient.name.family"]},
		"Patient.contact": {"viewDefinition": ` + viewDef + `, "children": ["Patient.name.family"]},
		"Patient.name.family": {"viewDefinition": ` + viewDef + `, "parent": "Patient.name"}
	}`))

	assert.False(t, result.Valid())
	require.Len(t, result.Findings, 1)
	finding := result.Findings[0]
	assert.Equal(t, "multiple-parents", finding.Code)
	assert.Equal(t, "Patient.name.family", finding.Element)
}
