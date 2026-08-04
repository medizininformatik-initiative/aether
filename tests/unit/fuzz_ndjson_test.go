package unit

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/medizininformatik-initiative/aether/internal/lib"
)

// FuzzReadNDJSON checks the NDJSON stream decoder against arbitrary bytes.
//
// Invariants:
//   - The returned count equals the number of callback calls, on the success
//     path and on the error path.
//   - Resources written back with WriteNDJSONLine decode again to the same
//     count and the same content.
func FuzzReadNDJSON(f *testing.F) {
	seeds := []string{
		"",
		`{"resourceType":"Patient","id":"p1"}`,
		"{\"resourceType\":\"Patient\"}\n{\"resourceType\":\"Condition\"}\n",
		`{"resourceType":"Patient"}{"resourceType":"Patient"}`,
		"{\"a\":1}\n{",
		"[]",
		"null",
		"{\"nested\":{\"deep\":[1,2,3]}}",
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		var collected []lib.FHIRResource
		count, err := lib.ReadNDJSON(bytes.NewReader(data), func(r lib.FHIRResource) error {
			collected = append(collected, r)
			return nil
		})

		if count != len(collected) {
			t.Fatalf("ReadNDJSON reported %d resources but called the callback %d times", count, len(collected))
		}
		if err != nil {
			return
		}

		var buf bytes.Buffer
		for _, r := range collected {
			if writeErr := lib.WriteNDJSONLine(&buf, r); writeErr != nil {
				t.Fatalf("WriteNDJSONLine failed for a resource that ReadNDJSON accepted: %v", writeErr)
			}
		}

		var reread []lib.FHIRResource
		count2, err := lib.ReadNDJSON(&buf, func(r lib.FHIRResource) error {
			reread = append(reread, r)
			return nil
		})
		if err != nil {
			t.Fatalf("re-reading written NDJSON failed: %v", err)
		}
		if count2 != count {
			t.Fatalf("round trip changed the resource count: %d -> %d", count, count2)
		}

		for i := range collected {
			before, _ := json.Marshal(collected[i])
			after, _ := json.Marshal(reread[i])
			if !bytes.Equal(before, after) {
				t.Fatalf("round trip changed resource %d: %s -> %s", i, before, after)
			}
		}
	})
}

// FuzzReadNDJSONCallbackError checks the count on the path where the callback
// fails. The count must name the resource that failed.
func FuzzReadNDJSONCallbackError(f *testing.F) {
	f.Add([]byte(`{"resourceType":"Patient"}{"resourceType":"Condition"}`), 1)
	f.Add([]byte(`{"a":1}`), 0)

	f.Fuzz(func(t *testing.T, data []byte, failAt int) {
		if failAt < 0 {
			return
		}

		calls := 0
		count, err := lib.ReadNDJSON(bytes.NewReader(data), func(r lib.FHIRResource) error {
			calls++
			if calls > failAt {
				return errCallbackStop
			}
			return nil
		})

		if count != calls {
			t.Fatalf("ReadNDJSON reported %d resources but called the callback %d times (err=%v)", count, calls, err)
		}
	})
}

var errCallbackStop = errStop{}

type errStop struct{}

func (errStop) Error() string { return "stop" }
