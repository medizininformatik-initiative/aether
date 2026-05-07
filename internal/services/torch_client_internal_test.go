package services

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/medizininformatik-initiative/aether/internal/lib"
)

// Pure unit table for makeAbsoluteURL — all URL shapes and fallback branches,
// no HTTP server. The integration path stays covered by the
// SchemelessHostPortURL test in tests/unit; this exercises the resolution
// logic directly.
func TestMakeAbsoluteURL(t *testing.T) {
	logger := lib.NewLogger(lib.LogLevelDebug)

	cases := []struct {
		name    string
		baseURL string
		rawURL  string
		want    string
	}{
		{name: "absolute http passed through", baseURL: "http://localhost:8080", rawURL: "http://other:9090/output/x.ndjson", want: "http://other:9090/output/x.ndjson"},
		{name: "absolute https passed through", baseURL: "http://localhost:8080", rawURL: "https://other:9090/x", want: "https://other:9090/x"},
		{name: "path-relative resolved against base", baseURL: "http://localhost:8080", rawURL: "/output/x.ndjson", want: "http://localhost:8080/output/x.ndjson"},
		{name: "path-relative with base trailing slash collapses double slash", baseURL: "http://localhost:8080/", rawURL: "/output/x.ndjson", want: "http://localhost:8080/output/x.ndjson"},
		{name: "scheme-less host-prefixed inherits http", baseURL: "http://localhost:8080", rawURL: "localhost:8080/output/x.ndjson", want: "http://localhost:8080/output/x.ndjson"},
		{name: "scheme-less host-prefixed inherits https", baseURL: "https://torch.example", rawURL: "127.0.0.1:8080/output/x.ndjson", want: "https://127.0.0.1:8080/output/x.ndjson"},
		{name: "invalid base returns raw unchanged", baseURL: "not-a-url", rawURL: "/output/x.ndjson", want: "/output/x.ndjson"},
		{name: "empty base returns raw unchanged", baseURL: "", rawURL: "/output/x.ndjson", want: "/output/x.ndjson"},
		{name: "unparseable path-relative returns raw unchanged", baseURL: "http://localhost:8080", rawURL: "/foo\x7fbar", want: "/foo\x7fbar"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := makeAbsoluteURL(tc.baseURL, tc.rawURL, logger)
			assert.Equal(t, tc.want, got)
		})
	}
}
