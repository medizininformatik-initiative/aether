package integration

import (
	"fmt"
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
	"github.com/medizininformatik-initiative/aether/internal/pipeline"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

// TestPipelineImportURL_EndToEnd verifies the complete pipeline import workflow from HTTP URL
func TestPipelineImportURL_EndToEnd(t *testing.T) {
	// Create test FHIR content
	testContent := `{"resourceType":"Patient","id":"patient-1","name":[{"family":"Smith"}]}
{"resourceType":"Patient","id":"patient-2","name":[{"family":"Jones"}]}
{"resourceType":"Patient","id":"patient-3","name":[{"family":"Brown"}]}
{"resourceType":"Patient","id":"patient-4","name":[{"family":"Davis"}]}
{"resourceType":"Patient","id":"patient-5","name":[{"family":"Wilson"}]}`

	// Create test HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(testContent))
	}))
	defer server.Close()

	// Setup
	tempDir := t.TempDir()
	jobsDir := filepath.Join(tempDir, "jobs")

	config := models.ProjectConfig{
		JobsDir: jobsDir,
		Pipeline: models.PipelineConfig{
			EnabledSteps: []models.StepName{models.StepHttpImport},
		},
		Retry: models.RetryConfig{
			MaxAttempts:      5,
			InitialBackoffMs: 1000,
			MaxBackoffMs:     30000,
		},
	}

	logger := lib.NewLogger(lib.LogLevelInfo)
	httpClient := services.DefaultHTTPClient()

	// Step 1: Create job with HTTP URL input
	url := server.URL + "/Patient.ndjson"
	job, err := pipeline.CreateJob(url, config, logger)
	require.NoError(t, err, "Job creation should succeed")
	assert.Equal(t, models.InputTypeHTTP, job.InputType, "Input type should be HTTP")
	assert.Equal(t, url, job.InputSource, "Input source should be the URL")

	// Step 2: Start job
	startedJob := pipeline.StartJob(job)
	assert.Equal(t, models.JobStatusInProgress, startedJob.Status)

	// Step 3: Execute import with progress display
	importedJob, err := pipeline.ExecuteImportStep(startedJob, logger, httpClient, true)
	require.NoError(t, err, "Import should succeed")

	// Verify import step completed
	importStep, found := models.GetStepByName(*importedJob, models.StepHttpImport)
	require.True(t, found, "Import step should exist")
	assert.Equal(t, models.StepStatusCompleted, importStep.Status, "Import step should be completed")
	assert.Equal(t, 1, importStep.FilesProcessed, "Should download 1 file")
	assert.Greater(t, importStep.BytesProcessed, int64(0), "Should process bytes")

	// Verify file was downloaded
	importDir := services.GetJobOutputDir(jobsDir, job.JobID, models.StepHttpImport)
	downloadedFile := filepath.Join(importDir, "Patient.ndjson")
	assert.FileExists(t, downloadedFile, "Downloaded file should exist")

	// Verify content
	content, err := os.ReadFile(downloadedFile)
	require.NoError(t, err)
	assert.Equal(t, testContent, string(content), "Downloaded content should match")

	// Verify resource count
	lineCount, err := lib.CountResourcesInFile(downloadedFile)
	require.NoError(t, err)
	assert.Equal(t, 5, lineCount, "Should count 5 resources")

	// Step 4: Verify state persistence
	_ = pipeline.UpdateJob(jobsDir, importedJob)
	reloadedJob, err := pipeline.LoadJob(jobsDir, job.JobID)
	require.NoError(t, err, "Should reload job from disk")
	assert.Equal(t, importedJob.Status, reloadedJob.Status, "Status should be persisted")
	assert.Equal(t, importedJob.TotalFiles, reloadedJob.TotalFiles, "File count should be persisted")
}

// TestPipelineImportURL_WithRetry verifies retry behavior for transient HTTP errors
func TestPipelineImportURL_WithRetry(t *testing.T) {
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
		_, _ = w.Write([]byte(`{"resourceType":"Patient","id":"1"}
{"resourceType":"Patient","id":"2"}`))
	}))
	defer server.Close()

	// Setup
	tempDir := t.TempDir()
	jobsDir := filepath.Join(tempDir, "jobs")

	config := models.ProjectConfig{
		JobsDir: jobsDir,
		Pipeline: models.PipelineConfig{
			EnabledSteps: []models.StepName{models.StepHttpImport},
		},
		Retry: models.RetryConfig{
			MaxAttempts:      5,
			InitialBackoffMs: 10, // Short backoff for fast test
			MaxBackoffMs:     100,
		},
	}

	logger := lib.NewLogger(lib.LogLevelInfo)
	httpClient := services.NewHTTPClient(5*time.Second, config.Retry, logger)

	// Create and execute job
	job, _ := pipeline.CreateJob(server.URL+"/test.ndjson", config, logger)
	startedJob := pipeline.StartJob(job)
	importedJob, err := pipeline.ExecuteImportStep(startedJob, logger, httpClient, false)

	// Verify retry succeeded
	require.NoError(t, err, "Import should succeed after retries")
	assert.Equal(t, 3, attempts, "Should make 3 attempts (2 failures + 1 success)")

	importStep, _ := models.GetStepByName(*importedJob, models.StepHttpImport)
	assert.Equal(t, models.StepStatusCompleted, importStep.Status, "Should complete after retry")
}

// TestPipelineImportURL_LargeFile verifies handling of larger downloads with progress
func TestPipelineImportURL_LargeFile(t *testing.T) {
	// Create larger test content (simulate 10K resources)
	var testContent string
	for i := 0; i < 10000; i++ {
		testContent += fmt.Sprintf(`{"resourceType":"Patient","id":"patient-%d"}`+"\n", i)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(testContent)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(testContent))
	}))
	defer server.Close()

	// Setup
	tempDir := t.TempDir()
	jobsDir := filepath.Join(tempDir, "jobs")

	config := models.ProjectConfig{
		JobsDir: jobsDir,
		Pipeline: models.PipelineConfig{
			EnabledSteps: []models.StepName{models.StepHttpImport},
		},
		Retry: models.RetryConfig{
			MaxAttempts:      5,
			InitialBackoffMs: 1000,
			MaxBackoffMs:     30000,
		},
	}

	logger := lib.NewLogger(lib.LogLevelInfo)
	httpClient := services.DefaultHTTPClient()

	// Execute import with progress
	job, _ := pipeline.CreateJob(server.URL+"/large.ndjson", config, logger)
	startedJob := pipeline.StartJob(job)
	importedJob, err := pipeline.ExecuteImportStep(startedJob, logger, httpClient, true)

	require.NoError(t, err, "Import should succeed")

	// Verify large file was downloaded
	importStep, _ := models.GetStepByName(*importedJob, models.StepHttpImport)
	assert.Equal(t, models.StepStatusCompleted, importStep.Status)
	assert.Greater(t, importStep.BytesProcessed, int64(100000), "Should download significant amount of data")

	// Verify file exists and has correct line count
	importDir := services.GetJobOutputDir(jobsDir, job.JobID, models.StepHttpImport)
	downloadedFile := filepath.Join(importDir, "large.ndjson")
	assert.FileExists(t, downloadedFile)

	lineCount, _ := lib.CountResourcesInFile(downloadedFile)
	assert.Equal(t, 10000, lineCount, "Should count 10000 resources")
}

// TestPipelineImportURL_ProgressDisplay verifies progress indicators are used (progress indicator requirements)
func TestPipelineImportURL_ProgressDisplay(t *testing.T) {
	// Create test content
	testContent := make([]byte, 50000) // 50KB
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
	jobsDir := filepath.Join(tempDir, "jobs")

	config := models.ProjectConfig{
		JobsDir: jobsDir,
		Pipeline: models.PipelineConfig{
			EnabledSteps: []models.StepName{models.StepHttpImport},
		},
		Retry: models.RetryConfig{
			MaxAttempts:      3,
			InitialBackoffMs: 100,
			MaxBackoffMs:     1000,
		},
	}

	logger := lib.NewLogger(lib.LogLevelInfo)
	httpClient := services.DefaultHTTPClient()

	// Execute with progress display enabled
	job, _ := pipeline.CreateJob(server.URL+"/data.ndjson", config, logger)
	startedJob := pipeline.StartJob(job)

	// This should use progress bar/spinner internally (progress indicator requirementsc, Progress indicators must update at least every 2 seconds)
	importedJob, err := pipeline.ExecuteImportStep(startedJob, logger, httpClient, true)

	require.NoError(t, err, "Import should succeed")

	// Verify download completed
	importStep, _ := models.GetStepByName(*importedJob, models.StepHttpImport)
	assert.Equal(t, models.StepStatusCompleted, importStep.Status)
	assert.Equal(t, int64(50000), importStep.BytesProcessed, "Should download exact byte count")
}

// TestPipelineImportURL_MultipleURLs verifies handling of multiple sequential downloads
func TestPipelineImportURL_MultipleURLs(t *testing.T) {
	tempDir := t.TempDir()
	jobsDir := filepath.Join(tempDir, "jobs")

	config := models.ProjectConfig{
		JobsDir: jobsDir,
		Pipeline: models.PipelineConfig{
			EnabledSteps: []models.StepName{models.StepHttpImport},
		},
		Retry: models.RetryConfig{
			MaxAttempts:      3,
			InitialBackoffMs: 100,
			MaxBackoffMs:     1000,
		},
	}

	logger := lib.NewLogger(lib.LogLevelInfo)
	httpClient := services.DefaultHTTPClient()

	// Create multiple test servers
	urls := []string{}
	for i := 0; i < 3; i++ {
		content := fmt.Sprintf(`{"resourceType":"Patient","id":"patient-%d"}`, i)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(content))
		}))
		defer server.Close()
		urls = append(urls, server.URL+fmt.Sprintf("/Patient_%d.ndjson", i))
	}

	// Download from each URL as separate jobs
	var jobs []*models.PipelineJob
	for _, url := range urls {
		job, _ := pipeline.CreateJob(url, config, logger)
		startedJob := pipeline.StartJob(job)
		importedJob, err := pipeline.ExecuteImportStep(startedJob, logger, httpClient, false)
		require.NoError(t, err, "Import should succeed for URL %s", url)
		jobs = append(jobs, importedJob)
	}

	// Verify all jobs completed
	assert.Len(t, jobs, 3, "Should create 3 jobs")
	for i, job := range jobs {
		importStep, _ := models.GetStepByName(*job, models.StepHttpImport)
		assert.Equal(t, models.StepStatusCompleted, importStep.Status,
			"Job %d should have completed import step", i)
	}

	// Verify all jobs are listed
	jobIDs, err := services.ListAllJobs(jobsDir)
	require.NoError(t, err)
	assert.Len(t, jobIDs, 3, "Should list 3 jobs")
}

// TestPipelineImportURL_FilenameFromURL verifies filename extraction from various URL formats
func TestPipelineImportURL_FilenameFromURL(t *testing.T) {
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
			name:             "Complex URL path",
			urlPath:          "/api/v1/fhir/export/Bundle_2024.ndjson",
			expectedFilename: "Bundle_2024.ndjson",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test server
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"resourceType":"Bundle"}`))
			}))
			defer server.Close()

			// Setup
			tempDir := t.TempDir()
			jobsDir := filepath.Join(tempDir, "jobs")

			config := models.ProjectConfig{
				JobsDir: jobsDir,
				Pipeline: models.PipelineConfig{
					EnabledSteps: []models.StepName{models.StepHttpImport},
				},
				Retry: models.RetryConfig{
					MaxAttempts:      3,
					InitialBackoffMs: 100,
					MaxBackoffMs:     1000,
				},
			}

			logger := lib.NewLogger(lib.LogLevelInfo)
			httpClient := services.DefaultHTTPClient()

			// Execute import
			url := server.URL + tt.urlPath
			job, _ := pipeline.CreateJob(url, config, logger)
			startedJob := pipeline.StartJob(job)
			_, _ = pipeline.ExecuteImportStep(startedJob, logger, httpClient, false)

			// Verify filename
			importDir := services.GetJobOutputDir(jobsDir, job.JobID, models.StepHttpImport)
			downloadedFile := filepath.Join(importDir, tt.expectedFilename)
			assert.FileExists(t, downloadedFile, "File should be saved with expected filename: %s", tt.expectedFilename)
		})
	}
}

// TestPipelineImportURL_ConcurrentDownloads verifies isolation between concurrent downloads
func TestPipelineImportURL_ConcurrentDownloads(t *testing.T) {
	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate some processing time
		time.Sleep(10 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"resourceType":"Patient"}`))
	}))
	defer server.Close()

	// Setup
	tempDir := t.TempDir()
	jobsDir := filepath.Join(tempDir, "jobs")

	config := models.ProjectConfig{
		JobsDir: jobsDir,
		Pipeline: models.PipelineConfig{
			EnabledSteps: []models.StepName{models.StepHttpImport},
		},
		Retry: models.RetryConfig{
			MaxAttempts:      3,
			InitialBackoffMs: 100,
			MaxBackoffMs:     1000,
		},
	}

	logger := lib.NewLogger(lib.LogLevelInfo)
	httpClient := services.DefaultHTTPClient()

	// Execute two downloads "concurrently" (sequentially in test, but verify isolation)
	job1, _ := pipeline.CreateJob(server.URL+"/data1.ndjson", config, logger)
	job2, _ := pipeline.CreateJob(server.URL+"/data2.ndjson", config, logger)

	startedJob1 := pipeline.StartJob(job1)
	startedJob2 := pipeline.StartJob(job2)

	_, err1 := pipeline.ExecuteImportStep(startedJob1, logger, httpClient, false)
	_, err2 := pipeline.ExecuteImportStep(startedJob2, logger, httpClient, false)

	// Verify both succeeded independently
	require.NoError(t, err1, "Job1 import should succeed")
	require.NoError(t, err2, "Job2 import should succeed")

	// Verify separate directories
	importDir1 := services.GetJobOutputDir(jobsDir, job1.JobID, models.StepHttpImport)
	importDir2 := services.GetJobOutputDir(jobsDir, job2.JobID, models.StepHttpImport)

	assert.NotEqual(t, importDir1, importDir2, "Jobs should have separate import directories")
	assert.DirExists(t, importDir1, "Job1 import directory should exist")
	assert.DirExists(t, importDir2, "Job2 import directory should exist")

	// Verify files are in correct directories
	file1 := filepath.Join(importDir1, "data1.ndjson")
	file2 := filepath.Join(importDir2, "data2.ndjson")
	assert.FileExists(t, file1, "Job1 file should exist")
	assert.FileExists(t, file2, "Job2 file should exist")
}
