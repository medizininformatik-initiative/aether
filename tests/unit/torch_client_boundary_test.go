package unit

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

// newBoundaryTestClient builds a TORCH client against the given server with the
// given file-availability settings.
func newBoundaryTestClient(serverURL string, retries int, interval time.Duration) *services.TORCHClient {
	return newBoundaryTestClientWithLog(serverURL, retries, interval,
		models.RetryConfig{MaxAttempts: 1, InitialBackoffMs: 10, MaxBackoffMs: 100}, io.Discard)
}

// newBoundaryTestClientWithLog is newBoundaryTestClient with an explicit retry
// policy for the download loop and the log output directed to logTo.
func newBoundaryTestClientWithLog(
	serverURL string, retries int, interval time.Duration,
	retryCfg models.RetryConfig, logTo io.Writer,
) *services.TORCHClient {
	logger := lib.NewLoggerWithWriter(lib.LogLevelDebug, logTo)
	httpClient := services.NewHTTPClient(5*time.Second, retryCfg, models.TLSConfig{}, logger)
	cfg := models.TORCHConfig{
		BaseURL:           serverURL,
		Auth:              models.AuthConfig{Username: "testuser", Password: "testpass"},
		FileReadyRetries:  retries,
		FileReadyInterval: interval,
	}
	return services.NewTORCHClient(cfg, httpClient, logger)
}

// Status 400 is the lowest status that counts as an error. A client that treats
// only statuses above 400 as errors reads a rejected submission as valid and
// reports the missing Content-Location header instead of the server's reason.
func TestTORCHClient_SubmitExtractionWithContent_Status400IsAnError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("malformed CRTDL"))
	}))
	defer server.Close()

	client := newBoundaryTestClient(server.URL, 1, time.Millisecond)

	_, err := client.SubmitExtractionWithContent([]byte(`{"cohortDefinition":{}}`))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "malformed CRTDL")
	assert.Contains(t, err.Error(), "400")
}

// A download that answers with status 400 must fail. A client that treats only
// statuses above 400 as errors writes the error body to the output file and
// reports success.
func TestTORCHClient_DownloadExtractionFiles_Status400IsAnError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", "36")
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("bad range request"))
	}))
	defer server.Close()

	client := newBoundaryTestClient(server.URL, 1, time.Millisecond)
	destDir := t.TempDir()

	_, err := client.DownloadExtractionFiles(
		[]string{server.URL + "/output/Patient.ndjson"}, destDir, false, false, "")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "400")
	assert.NoFileExists(t, filepath.Join(destDir, "Patient.ndjson"))
}

// A HEAD that answers 200 with a content length of zero describes a file that
// nginx created but did not fill yet. The file is not ready for download.
func TestTORCHClient_WaitForFileAvailability_ZeroLengthIsNotAvailable(t *testing.T) {
	headCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			headCount++
			w.Header().Set("Content-Length", "0")
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := newBoundaryTestClient(server.URL, 2, time.Millisecond)

	_, err := client.DownloadExtractionFiles(
		[]string{server.URL + "/output/Patient.ndjson"}, t.TempDir(), false, false, "")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "file not available")
	assert.Equal(t, 2, headCount, "every retry must re-check the empty file")
}

// The spinner counts the files from one, not from zero, and marks a completed
// download with the success marker.
func TestTORCHClient_DownloadExtractionFiles_ProgressCountsFilesFromOne(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", "36")
			w.WriteHeader(http.StatusOK)
			return
		}
		_, _ = w.Write([]byte(`{"resourceType":"Patient","id":"1"}`))
	}))
	defer server.Close()

	client := newBoundaryTestClient(server.URL, 1, time.Millisecond)
	fileURLs := []string{server.URL + "/output/a.ndjson", server.URL + "/output/b.ndjson"}

	stop := captureStdoutForTest(t)
	files, err := client.DownloadExtractionFiles(fileURLs, t.TempDir(), true, false, "")
	out := stop()

	require.NoError(t, err)
	require.Len(t, files, 2)
	assert.Contains(t, out, "Downloading file 1/2: a.ndjson")
	assert.Contains(t, out, "Downloading file 2/2: b.ndjson")
	assert.Contains(t, out, "✓ Downloading file 1/2: a.ndjson")
}

// A download that fails must stop the spinner with the failure marker.
func TestTORCHClient_DownloadExtractionFiles_ProgressShowsFailureMarker(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", "36")
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := newBoundaryTestClient(server.URL, 1, time.Millisecond)

	stop := captureStdoutForTest(t)
	_, err := client.DownloadExtractionFiles(
		[]string{server.URL + "/output/a.ndjson"}, t.TempDir(), true, false, "")
	out := stop()

	require.Error(t, err)
	assert.Contains(t, out, "✗ Downloading file 1/1: a.ndjson")
	assert.NotContains(t, out, "✓ Downloading file 1/1")
}

// The download loop makes exactly MaxAttempts attempts. It waits between them,
// but not after the last one. The server recovers on the third attempt, which
// the client must never reach.
func TestTORCHClient_DownloadFile_StopsAfterMaxAttempts(t *testing.T) {
	var gets int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", "36")
			w.WriteHeader(http.StatusOK)
			return
		}
		if atomic.AddInt32(&gets, 1) > 2 {
			_, _ = w.Write([]byte(`{"resourceType":"Patient","id":"1"}`))
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := newBoundaryTestClientWithLog(server.URL, 1, time.Millisecond,
		models.RetryConfig{MaxAttempts: 2, InitialBackoffMs: 250, MaxBackoffMs: 5000}, io.Discard)

	start := time.Now()
	_, err := client.DownloadExtractionFiles(
		[]string{server.URL + "/output/a.ndjson"}, t.TempDir(), false, false, "")
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "503")
	assert.Equal(t, int32(2), atomic.LoadInt32(&gets), "two attempts, so one wait of 250ms")
	assert.Less(t, elapsed, 500*time.Millisecond,
		"a second wait after the last attempt would add 500ms")
}

// The availability loop waits between checks, but not after the last one.
func TestTORCHClient_WaitForFileAvailability_DoesNotWaitAfterLastCheck(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "0")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := newBoundaryTestClient(server.URL, 2, 300*time.Millisecond)

	start := time.Now()
	_, err := client.DownloadExtractionFiles(
		[]string{server.URL + "/output/a.ndjson"}, t.TempDir(), false, false, "")
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.Less(t, elapsed, 500*time.Millisecond,
		"two checks give one wait of 300ms; a wait after the last check would add 300ms")
}

// The wait between checks must come before the next check. Without it every
// check runs at once and a file that needs time to appear is never found.
func TestTORCHClient_WaitForFileAvailability_WaitsBeforeTheNextCheck(t *testing.T) {
	start := time.Now()
	var heads int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			atomic.AddInt32(&heads, 1)
			if time.Since(start) < 300*time.Millisecond {
				w.Header().Set("Content-Length", "0")
			} else {
				w.Header().Set("Content-Length", "36")
			}
			w.WriteHeader(http.StatusOK)
			return
		}
		_, _ = w.Write([]byte(`{"resourceType":"Patient","id":"1"}`))
	}))
	defer server.Close()

	client := newBoundaryTestClient(server.URL, 3, 200*time.Millisecond)

	files, err := client.DownloadExtractionFiles(
		[]string{server.URL + "/output/a.ndjson"}, t.TempDir(), false, false, "")

	require.NoError(t, err)
	require.Len(t, files, 1)
	assert.Equal(t, int32(3), atomic.LoadInt32(&heads),
		"the checks fall at 0ms, 200ms and 400ms, and the file appears at 300ms")
}

// The compressed writer holds the data in a buffer and writes it when it
// closes. A write failure therefore shows up only at close time, and the
// download must report it. /dev/full accepts the open and fails every write
// with ENOSPC, which makes the failure happen at close.
func TestTORCHClient_DownloadFile_ReportsWriteErrorFromClose(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("/dev/full is a Linux device")
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", "36")
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	destDir := t.TempDir()
	destPath := filepath.Join(destDir, "a.ndjson"+lib.CompressedFileExtension)
	require.NoError(t, os.Symlink("/dev/full", destPath))

	client := newBoundaryTestClient(server.URL, 1, time.Millisecond)

	files, err := client.DownloadExtractionFiles(
		[]string{server.URL + "/output/a.ndjson"}, destDir, false, true, "fastest")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no space left on device")
	assert.Empty(t, files)
}

// The download log counts the files from one, so that an operator who watches a
// long import sees the same numbers as the progress output.
func TestTORCHClient_DownloadExtractionFiles_LogCountsFilesFromOne(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", "36")
			w.WriteHeader(http.StatusOK)
			return
		}
		_, _ = w.Write([]byte(`{"resourceType":"Patient","id":"1"}`))
	}))
	defer server.Close()

	var logs bytes.Buffer
	client := newBoundaryTestClientWithLog(server.URL, 1, time.Millisecond,
		models.RetryConfig{MaxAttempts: 1, InitialBackoffMs: 10, MaxBackoffMs: 100}, &logs)
	fileURLs := []string{server.URL + "/output/a.ndjson", server.URL + "/output/b.ndjson"}

	files, err := client.DownloadExtractionFiles(fileURLs, t.TempDir(), false, false, "")

	require.NoError(t, err)
	require.Len(t, files, 2)
	assert.Contains(t, logs.String(), "index 1 total 2")
	assert.Contains(t, logs.String(), "index 2 total 2")
}

// The retry log counts the attempts from one. Attempt zero is not a number an
// operator can match to the configured attempt count.
func TestTORCHClient_DownloadFile_LogCountsAttemptsFromOne(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", "36")
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	var logs bytes.Buffer
	client := newBoundaryTestClientWithLog(server.URL, 1, time.Millisecond,
		models.RetryConfig{MaxAttempts: 2, InitialBackoffMs: 10, MaxBackoffMs: 100}, &logs)

	_, err := client.DownloadExtractionFiles(
		[]string{server.URL + "/output/a.ndjson"}, t.TempDir(), false, false, "")

	require.Error(t, err)
	assert.Regexp(t, `TORCH download attempt failed, retrying \| \[url \S+ attempt 1 `, logs.String())
}

// A failed availability check must reach the log. Without it an operator who
// debugs a stuck import sees only the final timeout, not its cause.
func TestTORCHClient_WaitForFileAvailability_LogsTheCheckError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	var logs bytes.Buffer
	client := newBoundaryTestClientWithLog(server.URL, 1, time.Millisecond,
		models.RetryConfig{MaxAttempts: 1, InitialBackoffMs: 10, MaxBackoffMs: 100}, &logs)

	_, err := client.DownloadExtractionFiles(
		[]string{server.URL + "/output/a.ndjson"}, t.TempDir(), false, false, "")

	require.Error(t, err)
	assert.Contains(t, logs.String(), "File availability check error")
	assert.Contains(t, logs.String(), "unexpected status 500")
}
