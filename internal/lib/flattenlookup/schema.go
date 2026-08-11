package flattenlookup

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

// SchemaJSON is the embedded JSON Schema for the flatten-lookup format.
// It is exported so consumers can ship or serve the schema document itself.
//
//go:embed schema/flatten-lookup.schema.json
var SchemaJSON []byte

var compiledSchema = sync.OnceValue(func() *jsonschema.Schema {
	const name = "flatten-lookup.schema.json"
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(SchemaJSON))
	if err != nil {
		panic(fmt.Sprintf("embedded schema is not valid JSON: %v", err))
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource(name, doc); err != nil {
		panic(fmt.Sprintf("embedded schema rejected: %v", err))
	}
	schema, err := compiler.Compile(name)
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
			Severity: SeverityError,
			Code:     "schema-violation",
			Message:  fmt.Sprintf("at %s: %s", location, err.ErrorKind.LocalizedString(errorPrinter)),
		}}
	}
	var findings []Finding
	for _, cause := range err.Causes {
		findings = append(findings, schemaFindings(cause)...)
	}
	return findings
}
