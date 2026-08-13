// Package crtdl validates CRTDL (Clinical Resource Transfer Definition
// Language) documents: JSON structure via an embedded JSON Schema and
// cross-reference invariants via Go code.
package crtdl

import (
	"bytes"
	"errors"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// Finding is a single defect found in a CRTDL document. Every finding makes
// the document invalid.
type Finding struct {
	Code    string // stable machine-readable identifier, e.g. "linked-group-not-found"
	Group   string // attribute group id, if known
	Message string
}

// Result holds all findings for one validated document.
type Result struct {
	Findings []Finding
}

// Valid reports whether the document has no findings.
func (r *Result) Valid() bool {
	return len(r.Findings) == 0
}

// Validate checks a CRTDL document and returns all findings.
func Validate(data []byte) *Result {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return &Result{Findings: []Finding{{
			Code:    "malformed-json",
			Message: err.Error(),
		}}}
	}
	if err := compiledSchema().Validate(doc); err != nil {
		var validationErr *jsonschema.ValidationError
		if errors.As(err, &validationErr) {
			// Cross-reference checks need a structurally sound document,
			// so schema violations end the validation here.
			return &Result{Findings: schemaFindings(validationErr)}
		}
		return &Result{Findings: []Finding{{
			Code:    "schema-violation",
			Message: err.Error(),
		}}}
	}
	return &Result{Findings: crossRefFindings(data)}
}
