package unit

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

type urlDownload func(url, destDir string, httpClient *services.HTTPClient, logger *lib.Logger, compress bool) ([]models.FHIRDataFile, error)

var urlDownloads = map[string]urlDownload{
	"DownloadFromURL": func(url, destDir string, httpClient *services.HTTPClient, logger *lib.Logger, compress bool) ([]models.FHIRDataFile, error) {
		return services.DownloadFromURL(url, destDir, httpClient, logger, true, compress, "default")
	},
	"DownloadFromURLWithProgress": func(url, destDir string, httpClient *services.HTTPClient, logger *lib.Logger, compress bool) ([]models.FHIRDataFile, error) {
		return services.DownloadFromURLWithProgress(url, destDir, httpClient, logger, compress, "default")
	},
}

func serveBody(t *testing.T, body string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server
}

// The compressed writer holds a small body in a buffer and writes it to the
// file only when it closes. /dev/full accepts the open and fails every write
// with ENOSPC, so the write fails at close time. The download must report it
// and must not show or log success.
func TestURLDownload_ReportsWriteErrorFromClose(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("/dev/full is a Linux device")
	}
	server := serveBody(t, `{"resourceType":"Patient","id":"1"}`)

	for name, download := range urlDownloads {
		t.Run(name, func(t *testing.T) {
			destDir := t.TempDir()
			destPath := filepath.Join(destDir, "Patient.ndjson"+lib.CompressedFileExtension)
			require.NoError(t, os.Symlink("/dev/full", destPath))
			var logs strings.Builder
			logger := lib.NewLoggerWithWriter(lib.LogLevelInfo, &logs)
			stop := captureStdoutForTest(t)

			files, err := download(server.URL+"/Patient.ndjson", destDir, services.DefaultHTTPClient(), logger, true)

			stdout := stop()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "no space left on device")
			assert.Nil(t, files)
			assert.Contains(t, stdout, "✗ Connecting to ")
			assert.NotContains(t, stdout, "✓ Connecting to ")
			assert.NotContains(t, logs.String(), "Download completed")
		})
	}
}

func TestURLDownload_SpinnerShowsResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/missing.ndjson" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"resourceType":"Patient","id":"1"}`))
	}))
	defer server.Close()

	cases := map[string]struct {
		path string
		mark string
	}{
		"success": {path: "/Patient.ndjson", mark: "✓ Connecting to "},
		"failure": {path: "/missing.ndjson", mark: "✗ Connecting to "},
	}

	for name, download := range urlDownloads {
		for caseName, tc := range cases {
			t.Run(name+"/"+caseName, func(t *testing.T) {
				stop := captureStdoutForTest(t)
				_, _ = download(server.URL+tc.path, t.TempDir(), services.DefaultHTTPClient(), lib.NewLogger(lib.LogLevelError), false)
				assert.Contains(t, stop(), tc.mark)
			})
		}
	}
}

// With compression, the received byte count differs from the size of the file.
// FileSize must give the size of the file on disk.
func TestURLDownload_FileSizeIsCompressedSize(t *testing.T) {
	server := serveBody(t, strings.Repeat(`{"resourceType":"Patient","id":"1"}`+"\n", 100))

	for name, download := range urlDownloads {
		t.Run(name, func(t *testing.T) {
			destDir := t.TempDir()
			stop := captureStdoutForTest(t)

			files, err := download(server.URL+"/Patient.ndjson", destDir, services.DefaultHTTPClient(), lib.NewLogger(lib.LogLevelError), true)

			stop()
			require.NoError(t, err)
			require.Len(t, files, 1)
			info, err := os.Stat(filepath.Join(destDir, files[0].FilePath))
			require.NoError(t, err)
			assert.Equal(t, info.Size(), files[0].FileSize)
		})
	}
}

// With progress shown, DownloadFromURL logs "Download completed" only when the
// download succeeds and received at least one byte.
func TestDownloadFromURL_LogsCompletionOnlyForReceivedBytes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/missing.ndjson":
			w.WriteHeader(http.StatusNotFound)
		case "/Patient.ndjson":
			_, _ = w.Write([]byte(`{"resourceType":"Patient","id":"1"}`))
		}
	}))
	defer server.Close()

	cases := map[string]struct {
		path   string
		logged bool
	}{
		"body":       {path: "/Patient.ndjson", logged: true},
		"empty body": {path: "/empty.ndjson", logged: false},
		"failure":    {path: "/missing.ndjson", logged: false},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var logs strings.Builder
			logger := lib.NewLoggerWithWriter(lib.LogLevelInfo, &logs)
			stop := captureStdoutForTest(t)

			_, _ = services.DownloadFromURL(server.URL+tc.path, t.TempDir(), services.DefaultHTTPClient(), logger, true, false, "")

			stop()
			if tc.logged {
				assert.Contains(t, logs.String(), "Download completed")
			} else {
				assert.NotContains(t, logs.String(), "Download completed")
			}
		})
	}
}
