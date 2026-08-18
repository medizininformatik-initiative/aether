package crtdl

import (
	"bytes"
	_ "embed"
	"fmt"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

// SchemaJSON and CCDLSchemaJSON are unmodified copies of the JSON Schemas
// that the dataportal-backend enforces (crtdl-schema.json and
// ccdl-schema.json). They are exported so consumers can ship or serve the
// schema documents themselves. The CRTDL schema references the CCDL schema
// by its $id URL; both are registered locally, so validation needs no
// network access.
//
// schema/SOURCE.md names the upstream files, and schema/upstream.json pins
// them. Do not edit the schema files.
//
//go:embed schema/crtdl-schema.json
var SchemaJSON []byte

//go:embed schema/ccdl-schema.json
var CCDLSchemaJSON []byte

var compiledSchema = sync.OnceValue(func() *jsonschema.Schema {
	compiler := jsonschema.NewCompiler()
	compiler.AssertFormat()
	var crtdlID string
	for _, raw := range [][]byte{SchemaJSON, CCDLSchemaJSON} {
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
		if err != nil {
			panic(fmt.Sprintf("embedded schema is not valid JSON: %v", err))
		}
		id, ok := doc.(map[string]any)["$id"].(string)
		if !ok {
			panic("embedded schema has no $id")
		}
		if err := compiler.AddResource(id, doc); err != nil {
			panic(fmt.Sprintf("embedded schema rejected: %v", err))
		}
		if crtdlID == "" {
			crtdlID = id
		}
	}
	schema, err := compiler.Compile(crtdlID)
	if err != nil {
		panic(fmt.Sprintf("embedded schema does not compile: %v", err))
	}
	return schema
})

var errorPrinter = message.NewPrinter(language.English)

// schemaFindings converts a schema validation error tree into one finding
// per leaf cause.
func schemaFindings(err *jsonschema.ValidationError) []Finding {
	if len(err.Causes) == 0 {
		location := "/" + strings.Join(err.InstanceLocation, "/")
		return []Finding{{
			Code:    "schema-violation",
			Message: fmt.Sprintf("at %s: %s", location, err.ErrorKind.LocalizedString(errorPrinter)),
		}}
	}
	var findings []Finding
	for _, cause := range err.Causes {
		findings = append(findings, schemaFindings(cause)...)
	}
	return findings
}
