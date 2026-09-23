package services

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtractProgressDiagnostics(t *testing.T) {
	cases := map[string]struct {
		body string
		want string
	}{
		"information diagnostic": {
			body: `{"resourceType":"OperationOutcome","issue":[{"severity":"information","diagnostics":"Batch 2 of 5"}]}`,
			want: "Batch 2 of 5",
		},
		"skips issues that are not information": {
			body: `{"resourceType":"OperationOutcome","issue":[{"severity":"warning","diagnostics":"slow"},{"severity":"information","diagnostics":"Batch 3 of 5"}]}`,
			want: "Batch 3 of 5",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, extractProgressDiagnostics([]byte(tc.body)))
		})
	}
}
