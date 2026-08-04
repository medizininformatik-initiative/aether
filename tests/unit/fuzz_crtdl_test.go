package unit

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/medizininformatik-initiative/aether/internal/models"
)

// FuzzCRTDLRoundTrip checks the custom JSON methods of the CRTDL document.
// These methods keep unknown fields so that a document survives preprocessing.
//
// Invariants:
//   - Marshaling never panics. mustMarshal panics on a marshal error.
//   - The encoding reaches a fixed point after one pass. The first pass is not
//     an identity, because absent optional fields gain a default.
//   - Every top level field of the input is still present after the round trip.
func FuzzCRTDLRoundTrip(f *testing.F) {
	seeds := []string{
		`{}`,
		`{"display":"d","version":"1"}`,
		`{"cohortDefinition":{"inclusionCriteria":[[{"termCodes":[]}]]}}`,
		`{"dataExtraction":{"attributeGroups":[{"id":"g1","name":"n","groupReference":"http://x","attributes":[{"attributeRef":"Patient.id","mustHave":true}]}]}}`,
		`{"unknownTopLevel":{"keep":"me"},"display":"d"}`,
		`{"dataExtraction":{"attributeGroups":[{"id":"g1","includeReferenceOnly":true}],"extraSibling":1}}`,
		`{"display":null,"version":null}`,
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<16 {
			return
		}

		var doc models.CRTDLDocument
		if err := json.Unmarshal(data, &doc); err != nil {
			return
		}

		first, err := json.Marshal(doc)
		if err != nil {
			t.Fatalf("a document that unmarshaled cleanly does not marshal: %v", err)
		}

		var again models.CRTDLDocument
		if err := json.Unmarshal(first, &again); err != nil {
			t.Fatalf("the output of MarshalJSON does not unmarshal: %v\noutput: %s", err, first)
		}
		second, err := json.Marshal(again)
		if err != nil {
			t.Fatalf("the second marshal failed: %v", err)
		}

		if !bytes.Equal(first, second) {
			t.Fatalf("the encoding has no fixed point:\npass 1: %s\npass 2: %s", first, second)
		}

		// Every top level key of the input must survive.
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(data, &raw); err != nil {
			return
		}
		var out map[string]json.RawMessage
		if err := json.Unmarshal(first, &out); err != nil {
			t.Fatalf("the output is not a JSON object: %s", first)
		}
		// A known field with an empty value is dropped on purpose, so check only
		// the unknown fields. Keeping those is the contract of issue #356.
		known := map[string]bool{
			"display": true, "version": true, "cohortDefinition": true, "dataExtraction": true,
		}
		for key, value := range raw {
			if known[key] || string(value) == "null" {
				continue
			}
			kept, ok := out[key]
			if !ok {
				t.Fatalf("the round trip dropped the unknown top level field %q\ninput:  %s\noutput: %s", key, data, first)
			}
			// Compare the meaning, not the bytes: json.Marshal escapes "&" and
			// "<" as & and <, which does not change the value.
			var want, got any
			if json.Unmarshal(value, &want) != nil || json.Unmarshal(kept, &got) != nil {
				continue
			}
			if !reflect.DeepEqual(want, got) {
				t.Fatalf("the round trip changed the unknown top level field %q: %s -> %s", key, value, kept)
			}
		}
	})
}
