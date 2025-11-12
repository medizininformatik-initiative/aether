package unit

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

// TestDownloadFromURL_Success verifies successful download from HTTP URL
func TestDownloadFromURL_Success(t *testing.T) {
	// Create test server that serves FHIR NDJSON content
	testContent := `{"resourceType":"Patient","id":"1"}
{"resourceType":"Patient","id":"2"}
{"resourceType":"Patient","id":"3"}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(testContent))
	}))
	defer server.Close()

	// Setup
	tempDir := t.TempDir()
	destDir := filepath.Join(tempDir, "download")
	logger := lib.NewLogger(lib.LogLevelInfo)
	httpClient := services.DefaultHTTPClient()

	// Execute download
	url := server.URL + "/Patient.ndjson"
	downloadedFiles, err := services.DownloadFromURL(url, destDir, httpClient, logger, false)

	// Verify results
	assert.NoError(t, err, "Download should succeed")
	require.Len(t, downloadedFiles, 1, "Should download 1 file")

	downloaded := downloadedFiles[0]
	assert.Equal(t, "Patient.ndjson", downloaded.FileName, "FileName should be extracted from URL")
	assert.Greater(t, downloaded.FileSize, int64(0), "FileSize should be > 0")
	assert.Equal(t, models.StepHttpImport, downloaded.SourceStep, "SourceStep should be import")
	assert.Equal(t, "Patient", downloaded.ResourceType, "ResourceType should be extracted from filename")
	assert.Equal(t, 3, downloaded.LineCount, "Should count 3 lines/resources")

	// Verify file was saved
	destPath := filepath.Join(destDir, downloaded.FileName)
	assert.FileExists(t, destPath, "Downloaded file should exist")

	// Verify content
	content, err := os.ReadFile(destPath)
	require.NoError(t, err)
	assert.Equal(t, testContent, string(content), "Downloaded content should match")
}

// TestDownloadFromURL_HTTP404 verifies error handling for 404 Not Found
func TestDownloadFromURL_HTTP404(t *testing.T) {
	// Create test server that returns 404
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("Not Found"))
	}))
	defer server.Close()

	// Setup
	tempDir := t.TempDir()
	destDir := filepath.Join(tempDir, "download")
	logger := lib.NewLogger(lib.LogLevelInfo)
	httpClient := services.DefaultHTTPClient()

	// Execute download
	downloadedFiles, err := services.DownloadFromURL(server.URL+"/missing.ndjson", destDir, httpClient, logger, false)

	// Verify error
	assert.Error(t, err, "Should fail with 404")
	assert.Contains(t, err.Error(), "404", "Error should mention HTTP status")
	assert.Nil(t, downloadedFiles, "Should not return files on error")

	// Verify no partial file was created
	files, _ := filepath.Glob(filepath.Join(destDir, "*.ndjson"))
	assert.Empty(t, files, "No partial files should remain after failed download")
}

// TestDownloadFromURL_HTTP500 verifies error handling for 500 Server Error (transient)
func TestDownloadFromURL_HTTP500(t *testing.T) {
	// Create test server that returns 500
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("Internal Server Error"))
	}))
	defer server.Close()

	// Setup
	tempDir := t.TempDir()
	destDir := filepath.Join(tempDir, "download")
	logger := lib.NewLogger(lib.LogLevelInfo)
	httpClient := services.DefaultHTTPClient()

	// Execute download
	downloadedFiles, err := services.DownloadFromURL(server.URL+"/error.ndjson", destDir, httpClient, logger, false)

	// Verify error (should eventually fail after retries)
	assert.Error(t, err, "Should fail with 500 after retries")
	assert.Contains(t, err.Error(), "failed after", "Error should mention failure after retries")
	assert.Nil(t, downloadedFiles, "Should not return files on error")
}

// TestDownloadFromURL_InvalidURL verifies error handling for unreachable URLs
func TestDownloadFromURL_InvalidURL(t *testing.T) {
	// Use unreachable URL
	invalidURL := "http://localhost:99999/unreachable.ndjson"

	// Setup
	tempDir := t.TempDir()
	destDir := filepath.Join(tempDir, "download")
	logger := lib.NewLogger(lib.LogLevelInfo)
	httpClient := services.DefaultHTTPClient()

	// Execute download
	downloadedFiles, err := services.DownloadFromURL(invalidURL, destDir, httpClient, logger, false)

	// Verify error
	assert.Error(t, err, "Should fail for unreachable URL")
	assert.Nil(t, downloadedFiles, "Should not return files on error")
}

// TestDownloadFromURL_FilenameFallback verifies filename extraction from URL
func TestDownloadFromURL_FilenameFallback(t *testing.T) {
	testContent := `{"resourceType":"Bundle","id":"1"}`

	tests := []struct {
		name             string
		urlPath          string
		expectedFilename string
	}{
		{
			name:             "URL with .ndjson extension",
			urlPath:          "/data/Patient.ndjson",
			expectedFilename: "Patient.ndjson",
		},
		{
			name:             "URL without extension",
			urlPath:          "/download",
			expectedFilename: "download.ndjson",
		},
		{
			name:             "URL ending with slash",
			urlPath:          "/data/",
			expectedFilename: "data.ndjson",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test server
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(testContent))
			}))
			defer server.Close()

			// Setup
			tempDir := t.TempDir()
			destDir := filepath.Join(tempDir, "download")
			logger := lib.NewLogger(lib.LogLevelInfo)
			httpClient := services.DefaultHTTPClient()

			// Execute download
			url := server.URL + tt.urlPath
			downloadedFiles, err := services.DownloadFromURL(url, destDir, httpClient, logger, false)

			// Verify filename
			assert.NoError(t, err)
			require.Len(t, downloadedFiles, 1)
			assert.Equal(t, tt.expectedFilename, downloadedFiles[0].FileName,
				"FileName should be: %s", tt.expectedFilename)
		})
	}
}

// TestDownloadFromURL_WithProgress verifies download with progress tracking
func TestDownloadFromURL_WithProgress(t *testing.T) {
	// Create test server with larger content
	testContent := make([]byte, 10000) // 10KB of data
	for i := range testContent {
		testContent[i] = 'x'
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(testContent)
	}))
	defer server.Close()

	// Setup
	tempDir := t.TempDir()
	destDir := filepath.Join(tempDir, "download")
	logger := lib.NewLogger(lib.LogLevelInfo)
	httpClient := services.DefaultHTTPClient()

	// Execute download with progress (using internal function)
	downloadedFiles, err := services.DownloadFromURLWithProgress(server.URL+"/large.ndjson", destDir, httpClient, logger)

	// Verify results
	assert.NoError(t, err, "Download should succeed")
	require.Len(t, downloadedFiles, 1)
	assert.Equal(t, int64(10000), downloadedFiles[0].FileSize, "FileSize should match")
}

// TestDownloadFromURL_ProgressCallback verifies progress callback is invoked
func TestDownloadFromURL_ProgressCallback(t *testing.T) {
	// Create test content
	testContent := make([]byte, 5000)
	for i := range testContent {
		testContent[i] = byte(i % 256)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(testContent)
	}))
	defer server.Close()

	// Setup
	tempDir := t.TempDir()
	destDir := filepath.Join(tempDir, "download")
	destFile := filepath.Join(destDir, "test.ndjson")
	require.NoError(t, os.MkdirAll(destDir, 0755))

	httpClient := services.DefaultHTTPClient()

	// Track progress updates
	var progressUpdates []int64
	progressCallback := func(bytes int64) {
		progressUpdates = append(progressUpdates, bytes)
	}

	// Create destination file
	file, err := os.Create(destFile)
	require.NoError(t, err)
	defer func() { _ = file.Close() }()

	// Download with progress callback
	bytesDownloaded, err := httpClient.DownloadWithProgress(server.URL+"/data.ndjson", file, progressCallback)

	// Verify
	assert.NoError(t, err)
	assert.Equal(t, int64(5000), bytesDownloaded)
	assert.NotEmpty(t, progressUpdates, "Progress callback should be invoked")
	assert.Equal(t, int64(5000), progressUpdates[len(progressUpdates)-1],
		"Final progress should equal total bytes")
}

// TestValidateImportSource_HTTPUrl tests input validation for HTTP URLs
func TestValidateImportSource_HTTPUrl(t *testing.T) {
	tests := []struct {
		name        string
		url         string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "Valid HTTP URL",
			url:         "http://example.com/data.ndjson",
			expectError: false,
		},
		{
			name:        "Valid HTTPS URL",
			url:         "https://example.com/data.ndjson",
			expectError: false,
		},
		{
			name:        "Empty URL",
			url:         "",
			expectError: true,
			errorMsg:    "cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := services.ValidateImportSource(tt.url, models.InputTypeHTTP)

			if tt.expectError {
				assert.Error(t, err, "Should return error")
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg, "Error message should be descriptive")
				}
			} else {
				assert.NoError(t, err, "Should not return error")
			}
		})
	}
}

// TestHTTPClient_Retry verifies retry behavior for transient errors
func TestHTTPClient_Retry(t *testing.T) {
	// Track number of attempts
	attempts := 0

	// Create server that fails twice, then succeeds
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusServiceUnavailable) // 503 is transient
			_, _ = w.Write([]byte("Service Unavailable"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"resourceType":"Patient"}`))
	}))
	defer server.Close()

	// Setup HTTP client with quick retries for testing
	logger := lib.NewLogger(lib.LogLevelInfo)
	httpClient := services.NewHTTPClient(
		5*time.Second,
		models.RetryConfig{
			MaxAttempts:      5,
			InitialBackoffMs: 10, // Short backoff for fast test
			MaxBackoffMs:     100,
		},
		logger,
	)

	// Execute download
	tempDir := t.TempDir()
	destDir := filepath.Join(tempDir, "download")
	downloadedFiles, err := services.DownloadFromURL(server.URL+"/test.ndjson", destDir, httpClient, logger, false)

	// Verify retry succeeded
	assert.NoError(t, err, "Should succeed after retries")
	assert.Len(t, downloadedFiles, 1, "Should download file after retries")
	assert.Equal(t, 3, attempts, "Should make 3 attempts (2 failures + 1 success)")
}

// TestHTTPClient_NoRetryFor4xx verifies that 4xx errors are not retried
func TestHTTPClient_NoRetryFor4xx(t *testing.T) {
	// Track number of attempts
	attempts := 0

	// Create server that always returns 400 Bad Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("Bad Request"))
	}))
	defer server.Close()

	// Setup
	logger := lib.NewLogger(lib.LogLevelInfo)
	httpClient := services.DefaultHTTPClient()
	tempDir := t.TempDir()
	destDir := filepath.Join(tempDir, "download")

	// Execute download
	downloadedFiles, err := services.DownloadFromURL(server.URL+"/bad.ndjson", destDir, httpClient, logger, false)

	// Verify no retry for 4xx
	assert.Error(t, err, "Should fail with 400")
	assert.Nil(t, downloadedFiles)
	assert.Equal(t, 1, attempts, "Should only make 1 attempt (no retries for 4xx)")
}
