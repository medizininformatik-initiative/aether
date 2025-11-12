package contract

import (
	"encoding/base64"
	"encoding/json"
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

// Contract test for TORCH API submission endpoint
// Tests the FHIR $extract-data operation with CRTDL extraction request

func TestTORCHService_SubmitExtraction_Success(t *testing.T) {
	// Mock TORCH server that accepts CRTDL extraction
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request format per TORCH API spec
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/fhir/$extract-data", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		// Verify Basic authentication header
		authHeader := r.Header.Get("Authorization")
		assert.NotEmpty(t, authHeader)
		assert.Contains(t, authHeader, "Basic ")

		// Verify FHIR Parameters resource structure
		var params map[string]any
		err := json.NewDecoder(r.Body).Decode(&params)
		require.NoError(t, err)

		assert.Equal(t, "Parameters", params["resourceType"])

		// Verify parameter array with base64-encoded CRTDL
		paramArray, ok := params["parameter"].([]any)
		require.True(t, ok, "parameter should be array")
		require.Len(t, paramArray, 1, "should have one parameter")

		param := paramArray[0].(map[string]any)
		assert.Equal(t, "crtdl", param["name"])

		// Verify base64 encoding
		base64Value, ok := param["valueBase64Binary"].(string)
		require.True(t, ok, "valueBase64Binary should be string")

		// Decode and verify it's valid JSON
		decoded, err := base64.StdEncoding.DecodeString(base64Value)
		require.NoError(t, err, "should be valid base64")

		var crtdl map[string]any
		err = json.Unmarshal(decoded, &crtdl)
		require.NoError(t, err, "decoded value should be valid JSON")
		assert.Contains(t, crtdl, "cohortDefinition")
		assert.Contains(t, crtdl, "dataExtraction")

		// Return HTTP 202 with Content-Location header
		w.Header().Set("Content-Location", "/fhir/extraction/job-123")
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	// Create temp CRTDL file
	tempDir := t.TempDir()
	crtdlPath := filepath.Join(tempDir, "test.crtdl")
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

	// Test will verify submission flow
	logger := lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1, InitialBackoffMs: 100, MaxBackoffMs: 1000}, logger)
	torchConfig := models.TORCHConfig{
		BaseURL:                   server.URL,
		Username:                  "testuser",
		Password:                  "testpass",
		ExtractionTimeoutMinutes:  30,
		PollingIntervalSeconds:    1,
		MaxPollingIntervalSeconds: 5,
	}
	client := services.NewTORCHClient(torchConfig, httpClient, logger)

	extractionURL, err := client.SubmitExtraction(crtdlPath)
	assert.NoError(t, err)
	assert.Equal(t, server.URL+"/fhir/extraction/job-123", extractionURL)
}

func TestTORCHService_SubmitExtraction_InvalidCRTDL(t *testing.T) {
	// Mock TORCH server returning 400 for invalid CRTDL
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"resourceType": "OperationOutcome",
			"issue": []map[string]any{
				{
					"severity":    "error",
					"code":        "invalid",
					"diagnostics": "CRTDL validation failed: missing inclusionCriteria",
				},
			},
		})
	}))
	defer server.Close()

	// Create temp invalid CRTDL file
	tempDir := t.TempDir()
	crtdlPath := filepath.Join(tempDir, "invalid.crtdl")
	crtdlJSON := []byte(`{"cohortDefinition":{}}`)
	_ = os.WriteFile(crtdlPath, crtdlJSON, 0644)

	// Test will verify error handling
	logger := lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1, InitialBackoffMs: 100, MaxBackoffMs: 1000}, logger)
	torchConfig := models.TORCHConfig{
		BaseURL:  server.URL,
		Username: "testuser",
		Password: "testpass",
	}
	client := services.NewTORCHClient(torchConfig, httpClient, logger)

	_, err := client.SubmitExtraction(crtdlPath)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "400")
	assert.Contains(t, err.Error(), "CRTDL validation failed")
}

func TestTORCHService_SubmitExtraction_Unauthorized(t *testing.T) {
	// Mock TORCH server returning 401 for invalid credentials
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("Authentication required"))
	}))
	defer server.Close()

	// Create temp CRTDL file
	tempDir := t.TempDir()
	crtdlPath := filepath.Join(tempDir, "test.crtdl")
	crtdlJSON := []byte(`{"cohortDefinition":{"inclusionCriteria":[]},"dataExtraction":{"attributeGroups":[]}}`)
	_ = os.WriteFile(crtdlPath, crtdlJSON, 0644)

	// Test will verify authentication error handling
	logger := lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1, InitialBackoffMs: 100, MaxBackoffMs: 1000}, logger)
	torchConfig := models.TORCHConfig{
		BaseURL:  server.URL,
		Username: "wronguser",
		Password: "wrongpass",
	}
	client := services.NewTORCHClient(torchConfig, httpClient, logger)

	_, err := client.SubmitExtraction(crtdlPath)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "401")
}

// Contract test for TORCH API polling endpoint

func TestTORCHService_PollStatus_InProgress(t *testing.T) {
	// Mock TORCH server returning HTTP 202 (still processing)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/fhir/extraction/job-123", r.URL.Path)

		// Verify Basic authentication
		authHeader := r.Header.Get("Authorization")
		assert.NotEmpty(t, authHeader)
		assert.Contains(t, authHeader, "Basic ")

		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	// Test will verify polling continues on 202 (will timeout since server always returns 202)
	logger := lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1, InitialBackoffMs: 100, MaxBackoffMs: 1000}, logger)
	torchConfig := models.TORCHConfig{
		BaseURL:                   server.URL,
		Username:                  "testuser",
		Password:                  "testpass",
		ExtractionTimeoutMinutes:  0, // Immediate timeout
		PollingIntervalSeconds:    1,
		MaxPollingIntervalSeconds: 1,
	}
	client := services.NewTORCHClient(torchConfig, httpClient, logger)

	_, err := client.PollExtractionStatus(server.URL+"/fhir/extraction/job-123", false)
	assert.Error(t, err)
	assert.Equal(t, services.ErrExtractionTimeout, err)
}

func TestTORCHService_PollStatus_Complete(t *testing.T) {
	// Mock TORCH server returning HTTP 200 with extraction results
	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/fhir/extraction/job-123", r.URL.Path)

		// Return FHIR Parameters with output URLs
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

	// Test will verify result parsing
	logger := lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1, InitialBackoffMs: 100, MaxBackoffMs: 1000}, logger)
	torchConfig := models.TORCHConfig{
		BaseURL:                   server.URL,
		Username:                  "testuser",
		Password:                  "testpass",
		ExtractionTimeoutMinutes:  30,
		PollingIntervalSeconds:    1,
		MaxPollingIntervalSeconds: 5,
	}
	client := services.NewTORCHClient(torchConfig, httpClient, logger)

	urls, err := client.PollExtractionStatus(server.URL+"/fhir/extraction/job-123", false)
	assert.NoError(t, err)
	require.Len(t, urls, 2)
	assert.Equal(t, server.URL+"/output/batch-1.ndjson", urls[0])
	assert.Equal(t, server.URL+"/output/batch-2.ndjson", urls[1])
}

func TestTORCHService_PollStatus_Failed(t *testing.T) {
	// Mock TORCH server returning extraction failure
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"resourceType": "OperationOutcome",
			"issue": []map[string]any{
				{
					"severity":    "error",
					"code":        "processing",
					"diagnostics": "Extraction failed: database timeout",
				},
			},
		})
	}))
	defer server.Close()

	// Test will verify error handling
	logger := lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1, InitialBackoffMs: 100, MaxBackoffMs: 1000}, logger)
	torchConfig := models.TORCHConfig{
		BaseURL:                   server.URL,
		Username:                  "testuser",
		Password:                  "testpass",
		ExtractionTimeoutMinutes:  30,
		PollingIntervalSeconds:    1,
		MaxPollingIntervalSeconds: 5,
	}
	client := services.NewTORCHClient(torchConfig, httpClient, logger)

	_, err := client.PollExtractionStatus(server.URL+"/fhir/extraction/job-123", false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

// Contract test for TORCH API file download

func TestTORCHService_DownloadFile_Success(t *testing.T) {
	// Mock TORCH server serving NDJSON file
	ndjsonContent := `{"resourceType":"Patient","id":"1"}
{"resourceType":"Patient","id":"2"}
{"resourceType":"Observation","id":"obs-1"}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/output/batch-1.ndjson", r.URL.Path)

		// Verify authentication
		authHeader := r.Header.Get("Authorization")
		assert.NotEmpty(t, authHeader)

		w.Header().Set("Content-Type", "application/fhir+ndjson")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(ndjsonContent))
	}))
	defer server.Close()

	// Test will verify file download
	logger := lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1, InitialBackoffMs: 100, MaxBackoffMs: 1000}, logger)
	torchConfig := models.TORCHConfig{
		BaseURL:  server.URL,
		Username: "testuser",
		Password: "testpass",
	}
	client := services.NewTORCHClient(torchConfig, httpClient, logger)

	// Create temp destination directory
	tempDir := t.TempDir()

	files, err := client.DownloadExtractionFiles([]string{server.URL + "/output/batch-1.ndjson"}, tempDir, false)
	assert.NoError(t, err)
	require.Len(t, files, 1)

	// Read downloaded file and verify contents
	content, _ := os.ReadFile(filepath.Join(tempDir, files[0].FileName))
	assert.Contains(t, string(content), "Patient")
	assert.Contains(t, string(content), "Observation")
}

func TestTORCHService_DownloadFile_NotFound(t *testing.T) {
	// Mock TORCH server returning 404 for missing file
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("File not found"))
	}))
	defer server.Close()

	// Test will verify 404 error handling
	logger := lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1, InitialBackoffMs: 100, MaxBackoffMs: 1000}, logger)
	torchConfig := models.TORCHConfig{
		BaseURL:  server.URL,
		Username: "testuser",
		Password: "testpass",
	}
	client := services.NewTORCHClient(torchConfig, httpClient, logger)

	// Create temp destination directory
	tempDir := t.TempDir()

	_, err := client.DownloadExtractionFiles([]string{server.URL + "/output/missing.ndjson"}, tempDir, false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "404")
}

func TestTORCHService_DownloadFile_ServerError(t *testing.T) {
	// Mock TORCH server returning 500 during download (retryable)
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("Database error"))
	}))
	defer server.Close()

	// Test will verify error handling on 500
	// Note: Current implementation doesn't retry downloads (uses client.Do instead of httpClient.Do)
	logger := lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1, InitialBackoffMs: 100, MaxBackoffMs: 1000}, logger)
	torchConfig := models.TORCHConfig{
		BaseURL:  server.URL,
		Username: "testuser",
		Password: "testpass",
	}
	client := services.NewTORCHClient(torchConfig, httpClient, logger)

	// Create temp destination directory
	tempDir := t.TempDir()

	_, err := client.DownloadExtractionFiles([]string{server.URL + "/output/batch-1.ndjson"}, tempDir, false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "500")
	assert.Equal(t, 1, callCount, "Should call once (no retry in current implementation)")
}

func TestTORCHService_EndToEnd_SubmitPollDownload(t *testing.T) {
	// Integration test verifying complete TORCH workflow
	// This test ensures all three operations work together correctly

	extractionJobPath := "/fhir/extraction/job-xyz"
	pollCount := 0
	maxPolls := 3

	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Handle extraction submission
		if r.Method == "POST" && r.URL.Path == "/fhir/$extract-data" {
			w.Header().Set("Content-Location", extractionJobPath)
			w.WriteHeader(http.StatusAccepted)
			return
		}

		// Handle polling - return 202 for first few polls, then 200
		if r.Method == "GET" && r.URL.Path == extractionJobPath {
			pollCount++
			if pollCount < maxPolls {
				w.WriteHeader(http.StatusAccepted)
				return
			}

			// Return result with file URLs
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
			return
		}

		// Handle file download
		if r.Method == "GET" && r.URL.Path == "/output/result.ndjson" {
			w.Header().Set("Content-Type", "application/fhir+ndjson")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"resourceType":"Patient","id":"test-patient"}`))
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	serverURL = server.URL
	defer server.Close()

	// Create temp CRTDL file
	tempDir := t.TempDir()
	crtdlPath := filepath.Join(tempDir, "test.crtdl")
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

	// Test will verify complete workflow
	logger := lib.NewLogger(lib.LogLevelError)
	httpClient := services.NewHTTPClient(5*time.Second, models.RetryConfig{MaxAttempts: 1, InitialBackoffMs: 100, MaxBackoffMs: 1000}, logger)
	torchConfig := models.TORCHConfig{
		BaseURL:                   server.URL,
		Username:                  "testuser",
		Password:                  "testpass",
		ExtractionTimeoutMinutes:  30,
		PollingIntervalSeconds:    1,
		MaxPollingIntervalSeconds: 5,
	}
	client := services.NewTORCHClient(torchConfig, httpClient, logger)

	// Submit extraction
	extractionURL, err := client.SubmitExtraction(crtdlPath)
	require.NoError(t, err)
	assert.Equal(t, server.URL+extractionJobPath, extractionURL)

	// Poll until complete
	urls, err := client.PollExtractionStatus(extractionURL, false)
	require.NoError(t, err)
	require.Len(t, urls, 1)

	// Download files
	downloadDir := filepath.Join(tempDir, "downloads")
	files, err := client.DownloadExtractionFiles(urls, downloadDir, false)
	require.NoError(t, err)
	require.Len(t, files, 1)

	// Read and verify file contents
	content, _ := os.ReadFile(filepath.Join(downloadDir, files[0].FileName))
	assert.Contains(t, string(content), "test-patient")

	assert.GreaterOrEqual(t, pollCount, maxPolls, "Should have polled until completion")
}
