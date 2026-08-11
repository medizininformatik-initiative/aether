// Package flattenlookup validates flatten-lookup.json files: JSON structure
// via an embedded JSON Schema and cross-reference invariants (child resolution,
// parent uniqueness, cycles, naming convention) via Go code. A lookup file
// stores only the children direction; the parent link is derived from it.
// The stored parent field is deprecated but still accepted.
package flattenlookup

import (
	"bytes"
	"errors"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// Severity classifies a finding. Errors make the file invalid; warnings do not.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

// Finding is a single defect found in a lookup file.
type Finding struct {
	Severity Severity
	Code     string // stable machine-readable identifier, e.g. "unresolved-child"
	Table    string // profile URL of the table, if known
	Element  string // element ID, if known
	Message  string
}

// Result holds all findings for one validated file.
type Result struct {
	Findings []Finding
}

// Valid reports whether the file has no error-severity findings.
// Warnings do not make a file invalid.
func (r *Result) Valid() bool {
	for _, f := range r.Findings {
		if f.Severity == SeverityError {
			return false
		}
	}
	return true
}

// Option changes what Validate reports.
type Option func(*options)

// options collects the settings an Option sets.
type options struct {
	verbose bool
}

// Verbose adds the findings that only report on style and deprecation, not on
// defects: today the deprecated-parent warning.
func Verbose() Option {
	return func(o *options) { o.verbose = true }
}

// Validate checks a flatten-lookup.json document and returns all findings.
func Validate(data []byte, opts ...Option) *Result {
	var cfg options
	for _, opt := range opts {
		opt(&cfg)
	}

	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return &Result{Findings: []Finding{{
			Severity: SeverityError,
			Code:     "malformed-json",
			Message:  err.Error(),
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
			Severity: SeverityError,
			Code:     "schema-violation",
			Message:  err.Error(),
		}}}
	}
	return &Result{Findings: crossRefFindings(data, cfg)}
}
