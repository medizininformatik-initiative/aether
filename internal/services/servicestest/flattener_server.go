package servicestest

import (
	"net/http"
	"net/http/httptest"
)

// FlattenerCapabilityStatement is the CapabilityStatement that the
// fhir-flattener (0.1.0-alpha.7 to 0.1.0-alpha.9) returns from /fhir/metadata.
const FlattenerCapabilityStatement = `{"resourceType":"CapabilityStatement","status":"active","kind":"instance","fhirVersion":"4.0.1",` +
	`"format":["application/fhir+json"],"rest":[{"mode":"server","resource":[{"type":"ViewDefinition","operation":[` +
	`{"name":"$run","definition":"http://sql-on-fhir.org/OperationDefinition/$run"}]}]}]}`

// NewFlattenerServer starts a test server that answers the flattener metadata
// route with FlattenerCapabilityStatement, so the health check of the
// flattening step passes, and passes every other request to handler. The
// caller closes the server.
func NewFlattenerServer(handler http.Handler) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/fhir/metadata" {
			w.Header().Set("Content-Type", "application/fhir+json")
			_, _ = w.Write([]byte(FlattenerCapabilityStatement))
			return
		}
		handler.ServeHTTP(w, r)
	}))
}
