package services

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
)

// The streaming download client drops the whole-request deadline, so the
// connect and TLS-handshake phases (not covered by ResponseHeaderTimeout or the
// body stall watchdog) must still be bounded — otherwise a black-holed TCP
// connect on a custom-TLS transport would hang forever. Custom-TLS transports
// (BuildTLSTransport) ship without a dialer, so this guards that path.
func TestNewDownloadClient_BoundsConnectAndHandshake(t *testing.T) {
	logger := lib.NewLogger(lib.LogLevelError)

	cases := map[string]models.TLSConfig{
		"default transport":    {},
		"custom TLS transport": {InsecureSkipVerify: true},
	}

	for name, tlsCfg := range cases {
		t.Run(name, func(t *testing.T) {
			shared := NewHTTPClient(30, models.RetryConfig{MaxAttempts: 1}, tlsCfg, logger)

			dl := shared.newDownloadClient(60)

			assert.Zero(t, dl.Timeout, "download client must not carry a whole-request deadline")
			transport, ok := dl.Transport.(*http.Transport)
			require.True(t, ok)
			assert.NotNil(t, transport.DialContext, "connect phase must be bounded")
			assert.Positive(t, transport.TLSHandshakeTimeout, "TLS handshake must be bounded")
			assert.Equal(t, 60*1, int(transport.ResponseHeaderTimeout))
		})
	}
}

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

// A body that delivers headers plus a partial payload then goes silent must be
// aborted by the stall watchdog with the stall-specific error, the half-written
// file must be removed, and the stall must not be retried (retrying would just
// repeat the stall). A short stall timeout keeps the test fast.
func TestDownloadFile_MidBodyStallAborts(t *testing.T) {
	logger := lib.NewLogger(lib.LogLevelError)

	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/fhir+ndjson")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"resourceType":"Patient","id":"1"}` + "\n"))
		w.(http.Flusher).Flush()
		// Go silent past the stall window; release when the client aborts the
		// request so the handler goroutine does not linger.
		<-r.Context().Done()
	}))
	defer server.Close()

	stallTimeout := 150 * time.Millisecond
	httpClient := NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 3, InitialBackoffMs: 10, MaxBackoffMs: 50}, models.TLSConfig{}, logger)
	torchConfig := models.TORCHConfig{
		BaseURL:              server.URL,
		DownloadStallTimeout: stallTimeout,
	}
	client := NewTORCHClient(torchConfig, httpClient, logger)

	destPath := filepath.Join(t.TempDir(), "stalled.ndjson")

	start := time.Now()
	_, err := client.downloadFile(server.URL+"/output/stalled.ndjson", destPath, false, "")
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.ErrorIs(t, err, errDownloadStalled, "abort must carry the stall-specific cause, not a generic context error")
	assert.NoFileExists(t, destPath, "partial output file must be cleaned up after a stalled download")
	assert.Equal(t, int32(1), hits.Load(), "a stalled download must not be retried")
	assert.Less(t, elapsed, 2*time.Second, "stall detection must stay well under the whole-request timeout")
}

func TestJobIDFromStatusURL(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "status url", in: "http://torch:8080/fhir/__status/abc-123", want: "abc-123"},
		{name: "trailing slash", in: "http://torch:8080/fhir/__status/abc-123/", want: "abc-123"},
		{name: "query string ignored", in: "http://torch:8080/fhir/__status/abc-123?x=1", want: "abc-123"},
		{name: "https", in: "https://torch.example/fhir/__status/uuid", want: "uuid"},
		{name: "bare id", in: "abc-123", want: "abc-123"},
		{name: "empty", in: "", want: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, JobIDFromStatusURL(tc.in))
		})
	}
}
