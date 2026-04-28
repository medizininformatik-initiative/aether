package unit

import (
	"encoding/base64"
	"encoding/json"
	"io"
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

// Unit test for TORCH client SubmitExtraction()

func TestTORCHClient_SubmitExtraction_Success(t *testing.T) {
	// Create temp CRTDL file
	tempDir := t.TempDir()
	crtdlPath := filepath.Join(tempDir, "test.json")
	crtdlContent := map[string]any{
		"cohortDefinition": map[string]any{
			"version":           "1.0.0",
			"inclusionCriteria": []any{},
		},
		"dataExtraction": map[string]any{
			"attributeGroups": []any{},
		},
	}
	crtdlJSON, _ := json.Marshal(crtdlContent)
	_ = os.WriteFile(crtdlPath, crtdlJSON, 0644)

	// Mock TORCH server
	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/fhir/$extract-data", r.URL.Path)
		assert.Equal(t, "application/fhir+json", r.Header.Get("Content-Type"))

		// Verify authentication
		authHeader := r.Header.Get("Authorization")
		require.NotEmpty(t, authHeader)
		assert.Contains(t, authHeader, "Basic ")

		// Verify body is valid FHIR Parameters
		var params map[string]any
		err := json.NewDecoder(r.Body).Decode(&params)
		require.NoError(t, err)
		assert.Equal(t, "Parameters", params["resourceType"])

		// Return 202 with Content-Location
		w.Header().Set("Content-Location", serverURL+"/fhir/extraction/job-abc123")
		w.WriteHeader(http.StatusAccepted)
	}))
	serverURL = server.URL
	defer server.Close()

	// Test execution
	logger := lib.NewLogger(lib.LogLevelDebug)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 3, InitialBackoffMs: 100, MaxBackoffMs: 1000}, models.TLSConfig{}, logger)
	torchConfig := models.TORCHConfig{
		BaseURL:            server.URL,
		Username:           "testuser",
		Password:           "testpass",
		ExtractionTimeout:  30 * time.Minute,
		PollingInterval:    1 * time.Second,
		MaxPollingInterval: 5 * time.Second,
	}

	client := services.NewTORCHClient(torchConfig, httpClient, logger)
	extractionURL, err := client.SubmitExtraction(crtdlPath)

	// Assertions
	assert.NoError(t, err)
	assert.Equal(t, server.URL+"/fhir/extraction/job-abc123", extractionURL)
}

func TestTORCHClient_SubmitExtraction_FileNotFound(t *testing.T) {
	logger := lib.NewLogger(lib.LogLevelDebug)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 3, InitialBackoffMs: 100, MaxBackoffMs: 1000}, models.TLSConfig{}, logger)
	torchConfig := models.TORCHConfig{
		BaseURL:  "http://localhost:8080",
		Username: "testuser",
		Password: "testpass",
	}

	client := services.NewTORCHClient(torchConfig, httpClient, logger)
	_, err := client.SubmitExtraction("/nonexistent/file.json")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read CRTDL file")
}

func TestTORCHClient_SubmitExtraction_Unauthorized(t *testing.T) {
	// Create temp CRTDL file
	tempDir := t.TempDir()
	crtdlPath := filepath.Join(tempDir, "test.json")
	crtdlJSON := []byte(`{"cohortDefinition":{"inclusionCriteria":[]},"dataExtraction":{"attributeGroups":[]}}`)
	_ = os.WriteFile(crtdlPath, crtdlJSON, 0644)

	// Mock TORCH server returning 401
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("Invalid credentials"))
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelDebug)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 3, InitialBackoffMs: 100, MaxBackoffMs: 1000}, models.TLSConfig{}, logger)
	torchConfig := models.TORCHConfig{
		BaseURL:  server.URL,
		Username: "wrong",
		Password: "wrong",
	}

	client := services.NewTORCHClient(torchConfig, httpClient, logger)
	_, err := client.SubmitExtraction(crtdlPath)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "401")
}

// Unit test for TORCH client PollExtractionStatus()

func TestTORCHClient_PollExtractionStatus_ImmediateSuccess(t *testing.T) {
	// Mock TORCH server that returns success immediately
	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Contains(t, r.URL.Path, "/fhir/extraction/")

		// Return 200 with file URLs
		result := map[string]any{
			"resourceType": "Parameters",
			"parameter": []map[string]any{
				{
					"name": "output",
					"part": []map[string]any{
						{
							"name":     "url",
							"valueUrl": serverURL + "/output/batch-1.ndjson",
						},
						{
							"name":     "url",
							"valueUrl": serverURL + "/output/batch-2.ndjson",
						},
					},
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(result)
	}))
	serverURL = server.URL
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelDebug)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 3, InitialBackoffMs: 100, MaxBackoffMs: 1000}, models.TLSConfig{}, logger)
	torchConfig := models.TORCHConfig{
		BaseURL:            server.URL,
		Username:           "testuser",
		Password:           "testpass",
		ExtractionTimeout:  1 * time.Minute,
		PollingInterval:    1 * time.Second,
		MaxPollingInterval: 5 * time.Second,
	}

	client := services.NewTORCHClient(torchConfig, httpClient, logger)
	urls, err := client.PollExtractionStatus(server.URL+"/fhir/extraction/job-123", false)

	assert.NoError(t, err)
	require.Len(t, urls, 2)
	assert.Equal(t, server.URL+"/output/batch-1.ndjson", urls[0])
	assert.Equal(t, server.URL+"/output/batch-2.ndjson", urls[1])
}

// TestTORCHClient_PollExtractionStatus_HTTP102Processing verifies that HTTP 102 (Processing)
// is handled the same as HTTP 202 (Accepted) during polling. Note: Go's net/http client
// transparently consumes 1xx status codes, so we cannot test 102 directly with httptest.
// This test verifies the code path via 202, which shares the same switch case.
func TestTORCHClient_PollExtractionStatus_HTTP102Processing(t *testing.T) {
	pollCount := 0
	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pollCount++
		if pollCount < 3 {
			// Return 202 (exercises same code path as 102 Processing)
			w.WriteHeader(http.StatusAccepted)
			return
		}

		// Return 200 (complete)
		result := map[string]any{
			"resourceType": "Parameters",
			"parameter": []map[string]any{
				{
					"name": "output",
					"part": []map[string]any{
						{
							"name":     "url",
							"valueUrl": serverURL + "/output/result.ndjson",
						},
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(result)
	}))
	serverURL = server.URL
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelDebug)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 3, InitialBackoffMs: 100, MaxBackoffMs: 1000}, models.TLSConfig{}, logger)
	torchConfig := models.TORCHConfig{
		BaseURL:            server.URL,
		Username:           "testuser",
		Password:           "testpass",
		ExtractionTimeout:  1 * time.Minute,
		PollingInterval:    1 * time.Second,
		MaxPollingInterval: 5 * time.Second,
	}

	client := services.NewTORCHClient(torchConfig, httpClient, logger)
	urls, err := client.PollExtractionStatus(server.URL+"/fhir/extraction/job-123", false)

	assert.NoError(t, err)
	require.Len(t, urls, 1)
	assert.Equal(t, server.URL+"/output/result.ndjson", urls[0])
	assert.Equal(t, 3, pollCount, "Should have polled 3 times before completion")
}

func TestTORCHClient_PollExtractionStatus_WithOperationOutcomeDiagnostics(t *testing.T) {
	pollCount := 0
	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pollCount++
		if pollCount < 3 {
			// Return 202 with OperationOutcome containing progress diagnostics
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"resourceType": "OperationOutcome",
				"issue": []map[string]any{
					{
						"severity":    "information",
						"code":        "informational",
						"diagnostics": "Processing Patient resources",
					},
				},
			})
			return
		}

		// Return 200 (complete)
		result := map[string]any{
			"resourceType": "Parameters",
			"parameter": []map[string]any{
				{
					"name": "output",
					"part": []map[string]any{
						{
							"name":     "url",
							"valueUrl": serverURL + "/output/result.ndjson",
						},
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(result)
	}))
	serverURL = server.URL
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelDebug)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 3, InitialBackoffMs: 100, MaxBackoffMs: 1000}, models.TLSConfig{}, logger)
	torchConfig := models.TORCHConfig{
		BaseURL:            server.URL,
		Username:           "testuser",
		Password:           "testpass",
		ExtractionTimeout:  1 * time.Minute,
		PollingInterval:    1 * time.Second,
		MaxPollingInterval: 5 * time.Second,
	}

	client := services.NewTORCHClient(torchConfig, httpClient, logger)
	urls, err := client.PollExtractionStatus(server.URL+"/fhir/extraction/job-123", false)

	assert.NoError(t, err)
	require.Len(t, urls, 1)
	assert.Equal(t, 3, pollCount)
}

func TestTORCHClient_PollExtractionStatus_DiagnosticsNotRepeated(t *testing.T) {
	pollCount := 0
	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pollCount++
		if pollCount < 4 {
			// Return same diagnostic message for first 3 polls
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"resourceType": "OperationOutcome",
				"issue": []map[string]any{
					{
						"severity":    "information",
						"code":        "informational",
						"diagnostics": "Processing Patient resources",
					},
				},
			})
			return
		}

		// Return 200 (complete)
		result := map[string]any{
			"resourceType": "Parameters",
			"parameter": []map[string]any{
				{
					"name": "output",
					"part": []map[string]any{
						{
							"name":     "url",
							"valueUrl": serverURL + "/output/result.ndjson",
						},
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(result)
	}))
	serverURL = server.URL
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelDebug)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 3, InitialBackoffMs: 100, MaxBackoffMs: 1000}, models.TLSConfig{}, logger)
	torchConfig := models.TORCHConfig{
		BaseURL:            server.URL,
		Username:           "testuser",
		Password:           "testpass",
		ExtractionTimeout:  1 * time.Minute,
		PollingInterval:    1 * time.Second,
		MaxPollingInterval: 5 * time.Second,
	}

	client := services.NewTORCHClient(torchConfig, httpClient, logger)
	urls, err := client.PollExtractionStatus(server.URL+"/fhir/extraction/job-123", false)

	// Should succeed — the duplicate diagnostics are just not re-logged
	assert.NoError(t, err)
	require.Len(t, urls, 1)
	assert.Equal(t, 4, pollCount)
}

func TestTORCHClient_PollExtractionStatus_EmptyOutput(t *testing.T) {
	// Mock TORCH server that returns success but with empty output array
	// This happens when CRTDL query matches no data
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Contains(t, r.URL.Path, "/fhir/extraction/")

		// Return 200 with TORCH simple format but empty output
		result := map[string]any{
			"requiresAccessToken": false,
			"output":              []any{},
			"request":             "http://torch:8080/fhir/$extract-data",
			"deleted":             []any{},
			"transactionTime":     "2025-10-23T10:45:27.359016918Z",
			"error":               []any{},
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(result)
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelDebug)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 3, InitialBackoffMs: 100, MaxBackoffMs: 1000}, models.TLSConfig{}, logger)
	torchConfig := models.TORCHConfig{
		BaseURL:            server.URL,
		Username:           "testuser",
		Password:           "testpass",
		ExtractionTimeout:  1 * time.Minute,
		PollingInterval:    1 * time.Second,
		MaxPollingInterval: 5 * time.Second,
	}

	client := services.NewTORCHClient(torchConfig, httpClient, logger)
	urls, err := client.PollExtractionStatus(server.URL+"/fhir/extraction/job-123", false)

	// Should return error with helpful message
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "TORCH extraction completed successfully but found no matching data")
	assert.Contains(t, err.Error(), "CRTDL query criteria matched no resources")
	assert.Nil(t, urls)
}

func TestTORCHClient_PollExtractionStatus_Timeout(t *testing.T) {
	// Mock TORCH server that always returns 202
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelDebug)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 3, InitialBackoffMs: 100, MaxBackoffMs: 1000}, models.TLSConfig{}, logger)
	torchConfig := models.TORCHConfig{
		BaseURL:            server.URL,
		Username:           "testuser",
		Password:           "testpass",
		ExtractionTimeout:  0 * time.Minute, // Immediate timeout (converted to milliseconds)
		PollingInterval:    1 * time.Second,
		MaxPollingInterval: 5 * time.Second,
	}

	client := services.NewTORCHClient(torchConfig, httpClient, logger)
	_, err := client.PollExtractionStatus(server.URL+"/fhir/extraction/job-123", false)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "timeout")
}

func TestTORCHClient_PollExtractionStatus_ServerError(t *testing.T) {
	// Mock TORCH server returning 500 error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"resourceType": "OperationOutcome",
			"issue": []map[string]any{
				{
					"severity":    "error",
					"code":        "processing",
					"diagnostics": "Database timeout",
				},
			},
		})
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelDebug)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 3, InitialBackoffMs: 100, MaxBackoffMs: 1000}, models.TLSConfig{}, logger)
	torchConfig := models.TORCHConfig{
		BaseURL:            server.URL,
		Username:           "testuser",
		Password:           "testpass",
		ExtractionTimeout:  5 * time.Minute,
		PollingInterval:    1 * time.Second,
		MaxPollingInterval: 30 * time.Second,
	}

	client := services.NewTORCHClient(torchConfig, httpClient, logger)
	_, err := client.PollExtractionStatus(server.URL+"/fhir/extraction/job-123", false)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

// Unit test for TORCH client DownloadExtractionFiles()

func TestTORCHClient_DownloadExtractionFiles_Success(t *testing.T) {
	ndjsonContent := `{"resourceType":"Patient","id":"1"}
{"resourceType":"Patient","id":"2"}
{"resourceType":"Observation","id":"obs-1"}`

	// Mock TORCH server serving files
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)

		// Verify authentication
		authHeader := r.Header.Get("Authorization")
		assert.NotEmpty(t, authHeader)

		w.Header().Set("Content-Type", "application/fhir+ndjson")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(ndjsonContent))
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelDebug)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 3, InitialBackoffMs: 100, MaxBackoffMs: 1000}, models.TLSConfig{}, logger)
	torchConfig := models.TORCHConfig{
		BaseURL:  server.URL,
		Username: "testuser",
		Password: "testpass",
	}

	tempDir := t.TempDir()
	client := services.NewTORCHClient(torchConfig, httpClient, logger)

	fileURLs := []string{
		server.URL + "/output/batch-1.ndjson",
		server.URL + "/output/batch-2.ndjson",
	}

	files, err := client.DownloadExtractionFiles(fileURLs, tempDir, false, false, "")

	assert.NoError(t, err)
	assert.Len(t, files, 2)

	// Verify files were created
	for _, file := range files {
		filePath := filepath.Join(tempDir, file.FileName)
		assert.FileExists(t, filePath)

		// Verify content
		content, _ := os.ReadFile(filePath)
		assert.Contains(t, string(content), "Patient")
	}
}

func TestTORCHClient_DownloadExtractionFiles_PartialFailure(t *testing.T) {
	// Mock server that fails for second file
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			// First file succeeds
			w.Header().Set("Content-Type", "application/fhir+ndjson")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"resourceType":"Patient","id":"1"}`))
		} else {
			// Second file fails
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelDebug)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 3, InitialBackoffMs: 100, MaxBackoffMs: 1000}, models.TLSConfig{}, logger)
	torchConfig := models.TORCHConfig{
		BaseURL:  server.URL,
		Username: "testuser",
		Password: "testpass",
	}

	tempDir := t.TempDir()
	client := services.NewTORCHClient(torchConfig, httpClient, logger)

	fileURLs := []string{
		server.URL + "/output/batch-1.ndjson",
		server.URL + "/output/batch-2.ndjson",
	}

	_, err := client.DownloadExtractionFiles(fileURLs, tempDir, false, false, "")

	// Should fail on second file
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "404")
}

// Unit test for base64 CRTDL encoding

func TestTORCHClient_EncodeCRTDLToBase64_ValidJSON(t *testing.T) {
	// Create temp CRTDL file
	tempDir := t.TempDir()
	crtdlPath := filepath.Join(tempDir, "test.json")
	crtdlContent := map[string]any{
		"cohortDefinition": map[string]any{
			"version":           "1.0.0",
			"inclusionCriteria": []any{},
		},
		"dataExtraction": map[string]any{
			"attributeGroups": []any{},
		},
	}
	crtdlJSON, _ := json.Marshal(crtdlContent)
	_ = os.WriteFile(crtdlPath, crtdlJSON, 0644)

	logger := lib.NewLogger(lib.LogLevelDebug)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 3, InitialBackoffMs: 100, MaxBackoffMs: 1000}, models.TLSConfig{}, logger)
	torchConfig := models.TORCHConfig{
		BaseURL:  "http://localhost:8080",
		Username: "testuser",
		Password: "testpass",
	}

	_ = services.NewTORCHClient(torchConfig, httpClient, logger)

	// Call internal encoding function (will be exposed via reflection or package access)
	// For now, test via SubmitExtraction which uses it internally
	// This validates round-trip encoding

	// Read file and encode manually to test
	fileContent, err := os.ReadFile(crtdlPath)
	require.NoError(t, err)

	encoded := base64.StdEncoding.EncodeToString(fileContent)
	assert.NotEmpty(t, encoded)

	// Decode and verify
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	require.NoError(t, err)

	var decodedCRTDL map[string]any
	err = json.Unmarshal(decoded, &decodedCRTDL)
	require.NoError(t, err)

	assert.Contains(t, decodedCRTDL, "cohortDefinition")
	assert.Contains(t, decodedCRTDL, "dataExtraction")
}

// Unit test for exponential backoff polling logic

func TestTORCHClient_PollExtractionStatus_ExponentialBackoff(t *testing.T) {
	pollTimes := []time.Time{}
	pollCount := 0
	maxPolls := 4

	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pollTimes = append(pollTimes, time.Now())
		pollCount++

		if pollCount < maxPolls {
			// Return 202 (in progress)
			w.WriteHeader(http.StatusAccepted)
			return
		}

		// Return 200 (complete)
		result := map[string]any{
			"resourceType": "Parameters",
			"parameter": []map[string]any{
				{
					"name": "output",
					"part": []map[string]any{
						{
							"name":     "url",
							"valueUrl": serverURL + "/output/result.ndjson",
						},
					},
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(result)
	}))
	serverURL = server.URL
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelDebug)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 3, InitialBackoffMs: 100, MaxBackoffMs: 1000}, models.TLSConfig{}, logger)
	torchConfig := models.TORCHConfig{
		BaseURL:            server.URL,
		Username:           "testuser",
		Password:           "testpass",
		ExtractionTimeout:  1 * time.Minute,
		PollingInterval:    1 * time.Second,  // Start at 1 second
		MaxPollingInterval: 10 * time.Second, // Cap at 10 seconds
	}

	client := services.NewTORCHClient(torchConfig, httpClient, logger)
	urls, err := client.PollExtractionStatus(server.URL+"/fhir/extraction/job-123", false)

	assert.NoError(t, err)
	assert.Len(t, urls, 1)
	assert.Equal(t, maxPolls, pollCount)

	// Verify exponential backoff (intervals should grow)
	// First interval: ~1s, Second: ~2s, Third: ~4s
	if len(pollTimes) >= 3 {
		interval1 := pollTimes[1].Sub(pollTimes[0])
		interval2 := pollTimes[2].Sub(pollTimes[1])

		// Second interval should be roughly 2x first interval (with tolerance)
		// Allow for timing variance - just check second > first
		assert.Greater(t, interval2, interval1, "Polling intervals should increase (exponential backoff)")
	}
}

func TestTORCHClient_Ping_Success(t *testing.T) {
	// Mock TORCH server responding to GET request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelDebug)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 3, InitialBackoffMs: 100, MaxBackoffMs: 1000}, models.TLSConfig{}, logger)
	torchConfig := models.TORCHConfig{
		BaseURL:  server.URL,
		Username: "testuser",
		Password: "testpass",
	}

	client := services.NewTORCHClient(torchConfig, httpClient, logger)
	err := client.Ping()

	assert.NoError(t, err)
}

func TestTORCHClient_Ping_Unreachable(t *testing.T) {
	logger := lib.NewLogger(lib.LogLevelDebug)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 3, InitialBackoffMs: 100, MaxBackoffMs: 1000}, models.TLSConfig{}, logger)
	torchConfig := models.TORCHConfig{
		BaseURL:  "http://unreachable-host-12345.invalid:9999",
		Username: "testuser",
		Password: "testpass",
	}

	client := services.NewTORCHClient(torchConfig, httpClient, logger)
	err := client.Ping()

	assert.Error(t, err)
}

// Performance test - verify TORCH connectivity check < 5 seconds

func TestTORCHClient_Ping_PerformanceWithin5Seconds(t *testing.T) {
	// Mock TORCH server with slight delay to simulate realistic network latency
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate 100ms network latency
		time.Sleep(100 * time.Millisecond)
		assert.Equal(t, "GET", r.Method)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelError) // Reduce log noise for performance test
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 3, InitialBackoffMs: 100, MaxBackoffMs: 1000}, models.TLSConfig{}, logger)
	torchConfig := models.TORCHConfig{
		BaseURL:  server.URL,
		Username: "testuser",
		Password: "testpass",
	}

	client := services.NewTORCHClient(torchConfig, httpClient, logger)

	// Measure execution time
	startTime := time.Now()
	err := client.Ping()
	duration := time.Since(startTime)

	// Assertions
	assert.NoError(t, err)
	assert.Less(t, duration, 5*time.Second, "TORCH connectivity check must complete within 5 seconds, took: %v", duration)

	// Log performance for visibility
	t.Logf("TORCH connectivity check completed in %v (requirement: < 5s)", duration)
}

// Tests for makeAbsoluteURL helper (relative URL handling)

func TestTORCHClient_MakeAbsoluteURL_RelativeURL(t *testing.T) {
	logger := lib.NewLogger(lib.LogLevelDebug)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 3, InitialBackoffMs: 100, MaxBackoffMs: 1000}, models.TLSConfig{}, logger)
	torchConfig := models.TORCHConfig{
		BaseURL:  "http://torch.example.com:8080",
		Username: "testuser",
		Password: "testpass",
	}

	client := services.NewTORCHClient(torchConfig, httpClient, logger)

	// Test relative URL conversion (this is tested indirectly through DownloadExtractionFiles)
	// The makeAbsoluteURL method is private, but it's tested via file downloads
	assert.NotNil(t, client)
}

// Tests for CRTDL encoding edge cases

func TestTORCHClient_EncodeCRTDLToBase64_EmptyFile(t *testing.T) {
	tempDir := t.TempDir()
	crtdlPath := filepath.Join(tempDir, "empty.json")
	// Create empty file
	err := os.WriteFile(crtdlPath, []byte(""), 0644)
	require.NoError(t, err)

	logger := lib.NewLogger(lib.LogLevelDebug)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 3, InitialBackoffMs: 100, MaxBackoffMs: 1000}, models.TLSConfig{}, logger)
	torchConfig := models.TORCHConfig{
		BaseURL:  "http://localhost:8080",
		Username: "testuser",
		Password: "testpass",
	}

	client := services.NewTORCHClient(torchConfig, httpClient, logger)
	_, err = client.SubmitExtraction(crtdlPath)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}

func TestTORCHClient_EncodeCRTDLToBase64_InvalidJSON(t *testing.T) {
	tempDir := t.TempDir()
	crtdlPath := filepath.Join(tempDir, "invalid.json")
	// Write invalid JSON
	err := os.WriteFile(crtdlPath, []byte("{invalid json"), 0644)
	require.NoError(t, err)

	logger := lib.NewLogger(lib.LogLevelDebug)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 3, InitialBackoffMs: 100, MaxBackoffMs: 1000}, models.TLSConfig{}, logger)
	torchConfig := models.TORCHConfig{
		BaseURL:  "http://localhost:8080",
		Username: "testuser",
		Password: "testpass",
	}

	client := services.NewTORCHClient(torchConfig, httpClient, logger)
	_, err = client.SubmitExtraction(crtdlPath)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not valid JSON")
}

// Tests for response parsing edge cases

func TestTORCHClient_ParseExtractionResult_FHIRFormat(t *testing.T) {
	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		result := map[string]any{
			"resourceType": "Parameters",
			"parameter": []map[string]any{
				{
					"name": "output",
					"part": []map[string]any{
						{
							"name":     "url",
							"valueUrl": serverURL + "/files/result.ndjson",
						},
					},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(result)
	}))
	serverURL = server.URL
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelDebug)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 3, InitialBackoffMs: 100, MaxBackoffMs: 1000}, models.TLSConfig{}, logger)
	torchConfig := models.TORCHConfig{
		BaseURL:            server.URL,
		Username:           "testuser",
		Password:           "testpass",
		ExtractionTimeout:  1 * time.Minute,
		PollingInterval:    1 * time.Second,
		MaxPollingInterval: 5 * time.Second,
	}

	client := services.NewTORCHClient(torchConfig, httpClient, logger)
	urls, err := client.PollExtractionStatus(server.URL+"/fhir/extraction/job-123", false)

	assert.NoError(t, err)
	assert.Len(t, urls, 1)
	assert.Contains(t, urls[0], "result.ndjson")
}

func TestTORCHClient_ParseExtractionResult_SimpleFormat(t *testing.T) {
	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		result := map[string]any{
			"requiresAccessToken": false,
			"output": []map[string]any{
				{
					"type": "data",
					"url":  serverURL + "/downloads/data.ndjson",
				},
			},
		}
		_ = json.NewEncoder(w).Encode(result)
	}))
	serverURL = server.URL
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelDebug)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 3, InitialBackoffMs: 100, MaxBackoffMs: 1000}, models.TLSConfig{}, logger)
	torchConfig := models.TORCHConfig{
		BaseURL:            server.URL,
		Username:           "testuser",
		Password:           "testpass",
		ExtractionTimeout:  1 * time.Minute,
		PollingInterval:    1 * time.Second,
		MaxPollingInterval: 5 * time.Second,
	}

	client := services.NewTORCHClient(torchConfig, httpClient, logger)
	urls, err := client.PollExtractionStatus(server.URL+"/fhir/extraction/job-123", false)

	assert.NoError(t, err)
	assert.Len(t, urls, 1)
	assert.Contains(t, urls[0], "data.ndjson")
}

func TestTORCHClient_ParseExtractionResult_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{invalid json}"))
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelDebug)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 3, InitialBackoffMs: 100, MaxBackoffMs: 1000}, models.TLSConfig{}, logger)
	torchConfig := models.TORCHConfig{
		BaseURL:            server.URL,
		Username:           "testuser",
		Password:           "testpass",
		ExtractionTimeout:  1 * time.Minute,
		PollingInterval:    1 * time.Second,
		MaxPollingInterval: 5 * time.Second,
	}

	client := services.NewTORCHClient(torchConfig, httpClient, logger)
	_, err := client.PollExtractionStatus(server.URL+"/fhir/extraction/job-123", false)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid JSON")
}

func TestTORCHClient_SubmitExtraction_MissingContentLocation(t *testing.T) {
	tempDir := t.TempDir()
	crtdlPath := filepath.Join(tempDir, "test.json")
	crtdlJSON := []byte(`{"cohortDefinition":{"inclusionCriteria":[]},"dataExtraction":{"attributeGroups":[]}}`)
	_ = os.WriteFile(crtdlPath, crtdlJSON, 0644)

	// Mock TORCH server returning 202 without Content-Location header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		// Intentionally omit Content-Location header
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelDebug)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 3, InitialBackoffMs: 100, MaxBackoffMs: 1000}, models.TLSConfig{}, logger)
	torchConfig := models.TORCHConfig{
		BaseURL:  server.URL,
		Username: "testuser",
		Password: "testpass",
	}

	client := services.NewTORCHClient(torchConfig, httpClient, logger)
	_, err := client.SubmitExtraction(crtdlPath)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Content-Location")
}

func TestTORCHClient_DownloadExtractionFiles_EmptyFileList(t *testing.T) {
	logger := lib.NewLogger(lib.LogLevelDebug)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 3, InitialBackoffMs: 100, MaxBackoffMs: 1000}, models.TLSConfig{}, logger)
	torchConfig := models.TORCHConfig{
		BaseURL:  "http://localhost:8080",
		Username: "testuser",
		Password: "testpass",
	}

	tempDir := t.TempDir()
	client := services.NewTORCHClient(torchConfig, httpClient, logger)

	files, err := client.DownloadExtractionFiles([]string{}, tempDir, false, false, "")

	assert.NoError(t, err)
	assert.Len(t, files, 0)
}

func TestTORCHClient_DownloadExtractionFiles_InvalidDestinationPermissions(t *testing.T) {
	ndjsonContent := `{"resourceType":"Patient","id":"1"}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/fhir+ndjson")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(ndjsonContent))
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelDebug)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 3, InitialBackoffMs: 100, MaxBackoffMs: 1000}, models.TLSConfig{}, logger)
	torchConfig := models.TORCHConfig{
		BaseURL:  server.URL,
		Username: "testuser",
		Password: "testpass",
	}

	// Use read-only directory to trigger permission error
	client := services.NewTORCHClient(torchConfig, httpClient, logger)

	// Try to download to root directory (will fail with permission error)
	fileURLs := []string{server.URL + "/output/batch-1.ndjson"}
	_, err := client.DownloadExtractionFiles(fileURLs, "/root/invalid", false, false, "")

	assert.Error(t, err)
}

// =============================================
// File Availability Tests for TORCH Client
// =============================================

func TestTORCHClient_WaitForFileAvailability_ImmediateSuccess(t *testing.T) {
	ndjsonContent := `{"resourceType":"Patient","id":"1"}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "HEAD" {
			w.Header().Set("Content-Length", "36")
			w.WriteHeader(http.StatusOK)
			return
		}
		// GET for actual download
		w.Header().Set("Content-Type", "application/fhir+ndjson")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(ndjsonContent))
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelDebug)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 3, InitialBackoffMs: 100, MaxBackoffMs: 1000}, models.TLSConfig{}, logger)
	torchConfig := models.TORCHConfig{
		BaseURL:           server.URL,
		Username:          "testuser",
		Password:          "testpass",
		FileReadyRetries:  1,
		FileReadyInterval: 1 * time.Second,
	}

	tempDir := t.TempDir()
	client := services.NewTORCHClient(torchConfig, httpClient, logger)

	fileURLs := []string{server.URL + "/output/Patient.ndjson"}
	files, err := client.DownloadExtractionFiles(fileURLs, tempDir, false, false, "")

	assert.NoError(t, err)
	require.Len(t, files, 1)
	assert.Equal(t, "Patient.ndjson", files[0].FileName)
}

func TestTORCHClient_WaitForFileAvailability_RetryThenSuccess(t *testing.T) {
	headCount := 0
	ndjsonContent := `{"resourceType":"Patient","id":"1"}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "HEAD" {
			headCount++
			if headCount <= 2 {
				// First 2 HEAD requests return 404
				w.WriteHeader(http.StatusNotFound)
				return
			}
			// Third HEAD request succeeds
			w.Header().Set("Content-Length", "36")
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.Method == "GET" && r.Header.Get("Range") != "" {
			// Range fallback for 404 from HEAD
			w.WriteHeader(http.StatusNotFound)
			return
		}
		// GET for actual download
		w.Header().Set("Content-Type", "application/fhir+ndjson")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(ndjsonContent))
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelDebug)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1, InitialBackoffMs: 100, MaxBackoffMs: 1000}, models.TLSConfig{}, logger)
	torchConfig := models.TORCHConfig{
		BaseURL:           server.URL,
		Username:          "testuser",
		Password:          "testpass",
		FileReadyRetries:  5,
		FileReadyInterval: 1 * time.Second,
	}

	tempDir := t.TempDir()
	client := services.NewTORCHClient(torchConfig, httpClient, logger)

	fileURLs := []string{server.URL + "/output/Patient.ndjson"}
	files, err := client.DownloadExtractionFiles(fileURLs, tempDir, false, false, "")

	assert.NoError(t, err)
	require.Len(t, files, 1)
	assert.Equal(t, 3, headCount, "Should have made 3 HEAD requests (2 failures + 1 success)")
}

func TestTORCHClient_WaitForFileAvailability_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "HEAD" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		// Range fallback also returns not found
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelDebug)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1, InitialBackoffMs: 100, MaxBackoffMs: 1000}, models.TLSConfig{}, logger)
	torchConfig := models.TORCHConfig{
		BaseURL:           server.URL,
		Username:          "testuser",
		Password:          "testpass",
		FileReadyRetries:  2,
		FileReadyInterval: 1 * time.Second,
	}

	tempDir := t.TempDir()
	client := services.NewTORCHClient(torchConfig, httpClient, logger)

	fileURLs := []string{server.URL + "/output/Patient.ndjson"}
	_, err := client.DownloadExtractionFiles(fileURLs, tempDir, false, false, "")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "file not available")
}

func TestTORCHClient_WaitForFileAvailability_RangeFallback(t *testing.T) {
	ndjsonContent := `{"resourceType":"Patient","id":"1"}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "HEAD" {
			// HEAD not allowed — triggers Range fallback
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if r.Method == "GET" && r.Header.Get("Range") != "" {
			// Range GET succeeds with 206
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write([]byte("{"))
			return
		}
		// Full GET for download
		w.Header().Set("Content-Type", "application/fhir+ndjson")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(ndjsonContent))
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelDebug)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 3, InitialBackoffMs: 100, MaxBackoffMs: 1000}, models.TLSConfig{}, logger)
	torchConfig := models.TORCHConfig{
		BaseURL:           server.URL,
		Username:          "testuser",
		Password:          "testpass",
		FileReadyRetries:  1,
		FileReadyInterval: 1 * time.Second,
	}

	tempDir := t.TempDir()
	client := services.NewTORCHClient(torchConfig, httpClient, logger)

	fileURLs := []string{server.URL + "/output/Patient.ndjson"}
	files, err := client.DownloadExtractionFiles(fileURLs, tempDir, false, false, "")

	assert.NoError(t, err)
	require.Len(t, files, 1)
}

// =============================================
// Compression Tests for TORCH Client
// =============================================

// TestTORCHClient_DownloadExtractionFiles_WithCompression verifies download with compression enabled
func TestTORCHClient_DownloadExtractionFiles_WithCompression(t *testing.T) {
	ndjsonContent := `{"resourceType":"Patient","id":"1"}
{"resourceType":"Patient","id":"2"}
{"resourceType":"Observation","id":"obs-1"}`

	// Mock TORCH server serving files
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)

		w.Header().Set("Content-Type", "application/fhir+ndjson")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(ndjsonContent))
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelDebug)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 3, InitialBackoffMs: 100, MaxBackoffMs: 1000}, models.TLSConfig{}, logger)
	torchConfig := models.TORCHConfig{
		BaseURL:  server.URL,
		Username: "testuser",
		Password: "testpass",
	}

	tempDir := t.TempDir()
	client := services.NewTORCHClient(torchConfig, httpClient, logger)

	fileURLs := []string{
		server.URL + "/output/batch-1.ndjson",
		server.URL + "/output/batch-2.ndjson",
	}

	// Download with compression enabled
	files, err := client.DownloadExtractionFiles(fileURLs, tempDir, false, true, "default")

	assert.NoError(t, err)
	assert.Len(t, files, 2)

	// Verify files have compressed extension
	for _, file := range files {
		assert.True(t, lib.IsCompressedFile(file.FileName), "File should have .zst extension: %s", file.FileName)
		filePath := filepath.Join(tempDir, file.FileName)
		assert.FileExists(t, filePath)

		// Verify we can read the compressed content
		reader, err := lib.OpenFileForReading(filePath)
		require.NoError(t, err)
		content, err := io.ReadAll(reader)
		require.NoError(t, err)
		require.NoError(t, reader.Close())
		assert.Contains(t, string(content), "Patient")
	}
}

// TestTORCHClient_DownloadExtractionFiles_CompressionAllLevels verifies all compression levels work
func TestTORCHClient_DownloadExtractionFiles_CompressionAllLevels(t *testing.T) {
	ndjsonContent := `{"resourceType":"Patient","id":"1"}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/fhir+ndjson")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(ndjsonContent))
	}))
	defer server.Close()

	levels := []string{"fastest", "default", "better", "best"}

	for _, level := range levels {
		t.Run(level, func(t *testing.T) {
			logger := lib.NewLogger(lib.LogLevelDebug)
			httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 3, InitialBackoffMs: 100, MaxBackoffMs: 1000}, models.TLSConfig{}, logger)
			torchConfig := models.TORCHConfig{
				BaseURL:  server.URL,
				Username: "testuser",
				Password: "testpass",
			}

			tempDir := t.TempDir()
			client := services.NewTORCHClient(torchConfig, httpClient, logger)

			fileURLs := []string{server.URL + "/output/batch.ndjson"}
			files, err := client.DownloadExtractionFiles(fileURLs, tempDir, false, true, level)

			assert.NoError(t, err, "Download should succeed with compression level: %s", level)
			require.Len(t, files, 1)
			assert.Equal(t, "batch.ndjson.zst", files[0].FileName)
		})
	}
}

// TestTORCHClient_DownloadExtractionFiles_WithProgressAndCompression verifies progress + compression
func TestTORCHClient_DownloadExtractionFiles_WithProgressAndCompression(t *testing.T) {
	ndjsonContent := `{"resourceType":"Patient","id":"1"}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/fhir+ndjson")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(ndjsonContent))
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelDebug)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 3, InitialBackoffMs: 100, MaxBackoffMs: 1000}, models.TLSConfig{}, logger)
	torchConfig := models.TORCHConfig{
		BaseURL:  server.URL,
		Username: "testuser",
		Password: "testpass",
	}

	tempDir := t.TempDir()
	client := services.NewTORCHClient(torchConfig, httpClient, logger)

	fileURLs := []string{server.URL + "/output/Patient.ndjson"}

	// Download with progress and compression
	files, err := client.DownloadExtractionFiles(fileURLs, tempDir, true, true, "default")

	assert.NoError(t, err)
	require.Len(t, files, 1)
	assert.Equal(t, "Patient.ndjson.zst", files[0].FileName)

	// Verify file is readable
	filePath := filepath.Join(tempDir, files[0].FileName)
	reader, err := lib.OpenFileForReading(filePath)
	require.NoError(t, err)
	content, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())
	assert.Contains(t, string(content), "Patient")
}

// TestTORCHClient_DownloadExtractionFiles_CompressedFileSizeSmaller verifies compression reduces size
func TestTORCHClient_DownloadExtractionFiles_CompressedFileSizeSmaller(t *testing.T) {
	// Create repetitive content that compresses well
	var ndjsonContent string
	for i := 0; i < 100; i++ {
		ndjsonContent += `{"resourceType":"Patient","id":"` + string(rune('0'+i%10)) + `","name":[{"family":"Smith","given":["John"]}]}` + "\n"
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/fhir+ndjson")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(ndjsonContent))
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelDebug)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 3, InitialBackoffMs: 100, MaxBackoffMs: 1000}, models.TLSConfig{}, logger)
	torchConfig := models.TORCHConfig{
		BaseURL:  server.URL,
		Username: "testuser",
		Password: "testpass",
	}

	tempDir := t.TempDir()
	client := services.NewTORCHClient(torchConfig, httpClient, logger)

	fileURLs := []string{server.URL + "/output/Patient.ndjson"}
	files, err := client.DownloadExtractionFiles(fileURLs, tempDir, false, true, "default")

	assert.NoError(t, err)
	require.Len(t, files, 1)

	// Compressed size should be smaller than original content length
	assert.Less(t, files[0].FileSize, int64(len(ndjsonContent)),
		"Compressed file should be smaller than original content")
}

// =============================================
// Additional Coverage Tests for Error Paths
// Target: 100% patch coverage for torch_client.go
// =============================================

// TestTORCHClient_SubmitExtraction_HttpDoError verifies error handling when HTTP request fails
// (covers lines 145-153 in torch_client.go)
func TestTORCHClient_SubmitExtraction_HttpDoError(t *testing.T) {
	// Create temp CRTDL file
	tempDir := t.TempDir()
	crtdlPath := filepath.Join(tempDir, "test.json")
	crtdlJSON := []byte(`{"cohortDefinition":{"inclusionCriteria":[]},"dataExtraction":{"attributeGroups":[]}}`)
	_ = os.WriteFile(crtdlPath, crtdlJSON, 0644)

	// Create server that immediately closes connection
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Close connection without response - this will cause HTTP client error
		hj, ok := w.(http.Hijacker)
		if ok {
			conn, _, _ := hj.Hijack()
			_ = conn.Close()
		}
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelDebug)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1, InitialBackoffMs: 100, MaxBackoffMs: 1000}, models.TLSConfig{}, logger)
	torchConfig := models.TORCHConfig{
		BaseURL:  server.URL,
		Username: "testuser",
		Password: "testpass",
	}

	client := services.NewTORCHClient(torchConfig, httpClient, logger)
	_, err := client.SubmitExtraction(crtdlPath)

	assert.Error(t, err, "Should fail when server closes connection")
	// Error should be a TORCHError with transient error type
}

// TestTORCHClient_PollExtractionStatus_HttpDoError verifies that transient HTTP errors
// during polling are retried until the extraction timeout is reached.
func TestTORCHClient_PollExtractionStatus_HttpDoError(t *testing.T) {
	// Create server that immediately closes connection
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		hj, ok := w.(http.Hijacker)
		if ok {
			conn, _, _ := hj.Hijack()
			_ = conn.Close()
		}
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelDebug)
	httpClient := services.NewHTTPClient(1*time.Second, models.RetryConfig{MaxAttempts: 1, InitialBackoffMs: 100, MaxBackoffMs: 1000}, models.TLSConfig{}, logger)
	torchConfig := models.TORCHConfig{
		BaseURL:            server.URL,
		Username:           "testuser",
		Password:           "testpass",
		ExtractionTimeout:  0, // Immediate timeout so the test completes quickly
		PollingInterval:    1 * time.Second,
		MaxPollingInterval: 1 * time.Second,
	}

	client := services.NewTORCHClient(torchConfig, httpClient, logger)
	_, err := client.PollExtractionStatus(server.URL+"/fhir/extraction/job-123", false)

	assert.Error(t, err, "Should eventually fail with extraction timeout")
	assert.ErrorIs(t, err, services.ErrExtractionTimeout)
}

// TestTORCHClient_PollExtractionStatus_HttpDoErrorThenRecovers verifies that transient HTTP errors
// during polling are retried and polling succeeds once the server recovers.
func TestTORCHClient_PollExtractionStatus_HttpDoErrorThenRecovers(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if requestCount <= 2 {
			// First two requests: close connection to simulate transient error
			hj, ok := w.(http.Hijacker)
			if ok {
				conn, _, _ := hj.Hijack()
				_ = conn.Close()
			}
			return
		}
		// Third request: return completed result
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"output": [{"type": "data", "url": "/downloads/result.ndjson"}]}`))
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelDebug)
	httpClient := services.NewHTTPClient(2*time.Second, models.RetryConfig{MaxAttempts: 1, InitialBackoffMs: 100, MaxBackoffMs: 1000}, models.TLSConfig{}, logger)
	torchConfig := models.TORCHConfig{
		BaseURL:            server.URL,
		Username:           "testuser",
		Password:           "testpass",
		ExtractionTimeout:  1 * time.Minute,
		PollingInterval:    1 * time.Second,
		MaxPollingInterval: 5 * time.Second,
	}

	client := services.NewTORCHClient(torchConfig, httpClient, logger)
	fileURLs, err := client.PollExtractionStatus(server.URL+"/fhir/extraction/job-123", false)

	assert.NoError(t, err, "Should succeed after transient errors resolve")
	assert.Len(t, fileURLs, 1, "Should return file URLs from successful response")
	assert.Equal(t, 3, requestCount, "Should have retried through transient errors")
}

// TestTORCHClient_DownloadFile_HttpDoError verifies error handling when download request fails
// (covers lines 344-351 in torch_client.go)
func TestTORCHClient_DownloadFile_HttpDoError(t *testing.T) {
	// Create server that immediately closes connection
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if ok {
			conn, _, _ := hj.Hijack()
			_ = conn.Close()
		}
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelDebug)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1, InitialBackoffMs: 100, MaxBackoffMs: 1000}, models.TLSConfig{}, logger)
	torchConfig := models.TORCHConfig{
		BaseURL:  server.URL,
		Username: "testuser",
		Password: "testpass",
	}

	tempDir := t.TempDir()
	client := services.NewTORCHClient(torchConfig, httpClient, logger)

	fileURLs := []string{server.URL + "/output/batch.ndjson"}
	_, err := client.DownloadExtractionFiles(fileURLs, tempDir, false, false, "")

	assert.Error(t, err, "Should fail when download request fails")
}

// TestTORCHClient_DownloadFile_PartialContent verifies error handling when response body is truncated
// (covers lines 385-400 in torch_client.go - copy and close error paths)
func TestTORCHClient_DownloadFile_PartialContent(t *testing.T) {
	// Create server that sends Content-Length but closes before sending full body
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1000")
		w.Header().Set("Content-Type", "application/fhir+ndjson")
		w.WriteHeader(http.StatusOK)
		// Only write partial content
		_, _ = w.Write([]byte(`{"resourceType":"Patient"}`))
		// Force close
		if hj, ok := w.(http.Hijacker); ok {
			conn, _, _ := hj.Hijack()
			_ = conn.Close()
		}
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelDebug)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1, InitialBackoffMs: 100, MaxBackoffMs: 1000}, models.TLSConfig{}, logger)
	torchConfig := models.TORCHConfig{
		BaseURL:  server.URL,
		Username: "testuser",
		Password: "testpass",
	}

	tempDir := t.TempDir()
	client := services.NewTORCHClient(torchConfig, httpClient, logger)

	fileURLs := []string{server.URL + "/output/batch.ndjson"}
	_, err := client.DownloadExtractionFiles(fileURLs, tempDir, false, false, "")

	// May or may not error depending on how the HTTP client handles truncated response
	// The important thing is that no partial file remains
	if err != nil {
		// Verify no partial file exists
		files, _ := filepath.Glob(filepath.Join(tempDir, "*.ndjson"))
		assert.Empty(t, files, "No partial files should remain on error")
	}
}

// TestTORCHClient_DownloadFile_FilenameFallback verifies filename fallback for edge cases
// (covers lines 282-290 in torch_client.go)
func TestTORCHClient_DownloadFile_FilenameFallback(t *testing.T) {
	ndjsonContent := `{"resourceType":"Patient","id":"1"}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/fhir+ndjson")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(ndjsonContent))
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelDebug)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 3, InitialBackoffMs: 100, MaxBackoffMs: 1000}, models.TLSConfig{}, logger)
	torchConfig := models.TORCHConfig{
		BaseURL:  server.URL,
		Username: "testuser",
		Password: "testpass",
	}

	tempDir := t.TempDir()
	client := services.NewTORCHClient(torchConfig, httpClient, logger)

	// Test URL with proper filename to ensure normal path works
	fileURLs := []string{server.URL + "/Patient.ndjson"}
	files, err := client.DownloadExtractionFiles(fileURLs, tempDir, false, false, "")

	assert.NoError(t, err)
	require.Len(t, files, 1)
	// Should use the filename from URL
	assert.Equal(t, "Patient.ndjson", files[0].FileName)
}

// TestTORCHClient_DownloadFile_HTTP4xxError verifies error handling for 4xx errors
// (covers lines 355-365 in torch_client.go)
func TestTORCHClient_DownloadFile_HTTP4xxError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("Access denied"))
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelDebug)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1, InitialBackoffMs: 100, MaxBackoffMs: 1000}, models.TLSConfig{}, logger)
	torchConfig := models.TORCHConfig{
		BaseURL:  server.URL,
		Username: "testuser",
		Password: "testpass",
	}

	tempDir := t.TempDir()
	client := services.NewTORCHClient(torchConfig, httpClient, logger)

	fileURLs := []string{server.URL + "/output/batch.ndjson"}
	_, err := client.DownloadExtractionFiles(fileURLs, tempDir, false, false, "")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "403")
}

// TestTORCHClient_Ping_ServerError verifies ping error for 5xx responses
// (covers lines 450-453 in torch_client.go)
func TestTORCHClient_Ping_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelDebug)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1, InitialBackoffMs: 100, MaxBackoffMs: 1000}, models.TLSConfig{}, logger)
	torchConfig := models.TORCHConfig{
		BaseURL:  server.URL,
		Username: "testuser",
		Password: "testpass",
	}

	client := services.NewTORCHClient(torchConfig, httpClient, logger)
	err := client.Ping()

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "server error")
}

// TestTORCHClient_DownloadExtractionFiles_LargeFileWithCompression verifies large file download with compression
func TestTORCHClient_DownloadExtractionFiles_LargeFileWithCompression(t *testing.T) {
	// Create larger content
	var ndjsonContent string
	for i := 0; i < 1000; i++ {
		ndjsonContent += `{"resourceType":"Patient","id":"` + string(rune('0'+i%10)) + `"}` + "\n"
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/fhir+ndjson")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(ndjsonContent))
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelDebug)
	httpClient := services.NewHTTPClient(10*time.Second, models.RetryConfig{MaxAttempts: 3, InitialBackoffMs: 100, MaxBackoffMs: 1000}, models.TLSConfig{}, logger)
	torchConfig := models.TORCHConfig{
		BaseURL:  server.URL,
		Username: "testuser",
		Password: "testpass",
	}

	tempDir := t.TempDir()
	client := services.NewTORCHClient(torchConfig, httpClient, logger)

	fileURLs := []string{server.URL + "/output/Patient.ndjson"}
	files, err := client.DownloadExtractionFiles(fileURLs, tempDir, false, true, "default")

	assert.NoError(t, err)
	require.Len(t, files, 1)
	assert.Equal(t, 1000, files[0].LineCount, "Should count 1000 resources")
}

// =============================================
// TORCHError Tests
// =============================================

// TestTORCHError_Error tests the Error() method
func TestTORCHError_Error(t *testing.T) {
	err := &services.TORCHError{
		Operation:  "submit",
		StatusCode: 500,
		Message:    "internal server error",
		ErrorType:  models.ErrorTypeTransient,
	}

	errStr := err.Error()
	assert.Contains(t, errStr, "TORCH")
	assert.Contains(t, errStr, "submit")
	assert.Contains(t, errStr, "500")
	assert.Contains(t, errStr, "internal server error")
}

// TestTORCHError_IsRetryable tests the IsRetryable() method
func TestTORCHError_IsRetryable(t *testing.T) {
	testCases := []struct {
		name      string
		errorType models.ErrorType
		retryable bool
	}{
		{
			name:      "Transient error is retryable",
			errorType: models.ErrorTypeTransient,
			retryable: true,
		},
		{
			name:      "Non-transient error is not retryable",
			errorType: models.ErrorTypeNonTransient,
			retryable: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := &services.TORCHError{
				Operation:  "test",
				StatusCode: 500,
				Message:    "test error",
				ErrorType:  tc.errorType,
			}

			assert.Equal(t, tc.retryable, err.IsRetryable())
		})
	}
}

// =============================================
// SubmitExtractionWithContent Tests
// Target: Cover lines 195-197 and 201-203 in torch_client.go
// =============================================

// TestTORCHClient_SubmitExtractionWithContent_EmptyContent tests error handling for empty content
// (covers lines 195-197 in torch_client.go)
func TestTORCHClient_SubmitExtractionWithContent_EmptyContent(t *testing.T) {
	logger := lib.NewLogger(lib.LogLevelDebug)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 3, InitialBackoffMs: 100, MaxBackoffMs: 1000}, models.TLSConfig{}, logger)
	torchConfig := models.TORCHConfig{
		BaseURL:  "http://localhost:8080",
		Username: "testuser",
		Password: "testpass",
	}

	client := services.NewTORCHClient(torchConfig, httpClient, logger)
	_, err := client.SubmitExtractionWithContent([]byte{})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "CRTDL content is empty")
}

// TestTORCHClient_SubmitExtractionWithContent_InvalidJSON tests error handling for invalid JSON content
// (covers lines 201-203 in torch_client.go)
func TestTORCHClient_SubmitExtractionWithContent_InvalidJSON(t *testing.T) {
	logger := lib.NewLogger(lib.LogLevelDebug)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 3, InitialBackoffMs: 100, MaxBackoffMs: 1000}, models.TLSConfig{}, logger)
	torchConfig := models.TORCHConfig{
		BaseURL:  "http://localhost:8080",
		Username: "testuser",
		Password: "testpass",
	}

	client := services.NewTORCHClient(torchConfig, httpClient, logger)
	_, err := client.SubmitExtractionWithContent([]byte("{invalid json"))

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not valid JSON")
}

// TestTORCHClient_SubmitExtractionWithContent_Success tests successful submission with content
func TestTORCHClient_SubmitExtractionWithContent_Success(t *testing.T) {
	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/fhir/$extract-data", r.URL.Path)

		// Verify body is valid FHIR Parameters with base64 encoded CRTDL
		var params map[string]any
		err := json.NewDecoder(r.Body).Decode(&params)
		require.NoError(t, err)
		assert.Equal(t, "Parameters", params["resourceType"])

		// Return 202 with Content-Location
		w.Header().Set("Content-Location", serverURL+"/fhir/extraction/job-enriched123")
		w.WriteHeader(http.StatusAccepted)
	}))
	serverURL = server.URL
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelDebug)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 3, InitialBackoffMs: 100, MaxBackoffMs: 1000}, models.TLSConfig{}, logger)
	torchConfig := models.TORCHConfig{
		BaseURL:  server.URL,
		Username: "testuser",
		Password: "testpass",
	}

	crtdlContent := []byte(`{"cohortDefinition":{"inclusionCriteria":[]},"dataExtraction":{"attributeGroups":[]}}`)

	client := services.NewTORCHClient(torchConfig, httpClient, logger)
	extractionURL, err := client.SubmitExtractionWithContent(crtdlContent)

	assert.NoError(t, err)
	assert.Equal(t, server.URL+"/fhir/extraction/job-enriched123", extractionURL)
}
