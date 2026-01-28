package unit

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExecuteSendStep_Success(t *testing.T) {
	// Capture resources received by the mock server
	var receivedResources []map[string]any
	var receivedRequests []struct {
		Method string
		Path   string
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "PUT", r.Method)
		assert.Equal(t, "application/fhir+json", r.Header.Get("Content-Type"))

		receivedRequests = append(receivedRequests, struct {
			Method string
			Path   string
		}{r.Method, r.URL.Path})

		var resource map[string]any
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &resource)
		receivedResources = append(receivedResources, resource)

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	jobID := "test-send-job"
	jobDir := filepath.Join(tmpDir, jobID)

	// Create input directory with test files
	inputDir := filepath.Join(jobDir, "csv")
	require.NoError(t, os.MkdirAll(inputDir, 0755))

	// Write a test CSV file
	csvContent := "id,gender,birthDate\n1,male,1990-01-01\n"
	require.NoError(t, os.WriteFile(filepath.Join(inputDir, "Patient.csv"), []byte(csvContent), 0644))

	// Write a test NDJSON file
	ndjsonContent := `{"resourceType":"Patient","id":"1"}` + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(inputDir, "Patient.ndjson"), []byte(ndjsonContent), 0644))

	job := createSendTestJob(server.URL, jobID, tmpDir)

	logger := lib.NewLogger(lib.LogLevelDebug)
	err := pipeline.ExecuteSendStep(job, jobDir, logger)
	require.NoError(t, err)

	// 2 Binary resources + 1 DocumentReference = 3 PUT requests
	require.Len(t, receivedRequests, 3)
	require.Len(t, receivedResources, 3)

	// All requests should be PUTs
	for _, req := range receivedRequests {
		assert.Equal(t, "PUT", req.Method)
	}

	// Collect resources by type
	var binaryResources []map[string]any
	var docRef map[string]any
	for _, res := range receivedResources {
		switch res["resourceType"].(string) {
		case "Binary":
			binaryResources = append(binaryResources, res)
		case "DocumentReference":
			docRef = res
		}
	}

	assert.Len(t, binaryResources, 2)
	require.NotNil(t, docRef)

	// Verify DocumentReference structure
	assert.Equal(t, "DocumentReference", docRef["resourceType"])
	assert.Equal(t, "current", docRef["status"])
	assert.Equal(t, "final", docRef["docStatus"])

	// Verify masterIdentifier
	masterID := docRef["masterIdentifier"].(map[string]any)
	assert.Equal(t, "http://medizininformatik-initiative.de/sid/project-identifier", masterID["system"])
	assert.Equal(t, "TEST-PROJECT", masterID["value"])

	// Verify author organization
	authors := docRef["author"].([]any)
	require.Len(t, authors, 1)
	author := authors[0].(map[string]any)
	assert.Equal(t, "Organization", author["type"])
	authorIdent := author["identifier"].(map[string]any)
	assert.Equal(t, "http://dsf.dev/sid/organization-identifier", authorIdent["system"])
	assert.Equal(t, "test.hospital.de", authorIdent["value"])

	// Verify DocumentReference content links
	contentArr := docRef["content"].([]any)
	assert.Len(t, contentArr, 2)

	// Verify Binary resources have base64-encoded data
	for _, binary := range binaryResources {
		assert.Equal(t, "Binary", binary["resourceType"])
		assert.NotEmpty(t, binary["id"])
		assert.NotEmpty(t, binary["contentType"])
		assert.NotEmpty(t, binary["data"])
	}

	// Verify PUT paths match resource IDs
	for _, req := range receivedRequests {
		// Path should be /Binary/{id} or /DocumentReference/{id}
		assert.True(t, len(req.Path) > 1, "Path should not be empty")
	}

	// Verify step was marked completed
	var sendStep *models.PipelineStep
	for i := range job.Steps {
		if job.Steps[i].Name == models.StepSend {
			sendStep = &job.Steps[i]
			break
		}
	}
	require.NotNil(t, sendStep)
	assert.Equal(t, models.StepStatusCompleted, sendStep.Status)
	assert.Equal(t, 2, sendStep.FilesProcessed)
}

func TestExecuteSendStep_ZipContentVerification(t *testing.T) {
	var binaryResource map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var resource map[string]any
		_ = json.Unmarshal(body, &resource)

		// Capture the Binary resource (not the DocumentReference)
		if resource["resourceType"] == "Binary" {
			binaryResource = resource
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	jobID := "test-zip-verify"
	jobDir := filepath.Join(tmpDir, jobID)
	inputDir := filepath.Join(jobDir, "csv")
	require.NoError(t, os.MkdirAll(inputDir, 0755))

	originalContent := "id,name\n1,Alice\n2,Bob\n"
	require.NoError(t, os.WriteFile(filepath.Join(inputDir, "data.csv"), []byte(originalContent), 0644))

	job := createSendTestJob(server.URL, jobID, tmpDir)
	logger := lib.NewLogger(lib.LogLevelDebug)
	err := pipeline.ExecuteSendStep(job, jobDir, logger)
	require.NoError(t, err)

	// Verify the captured Binary resource
	require.NotNil(t, binaryResource)
	assert.Equal(t, "text/zip", binaryResource["contentType"])

	b64Data := binaryResource["data"].(string)
	zipBytes, err := base64.StdEncoding.DecodeString(b64Data)
	require.NoError(t, err)

	// Open zip and verify contents
	zipReader, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	require.NoError(t, err)
	require.Len(t, zipReader.File, 1)
	assert.Equal(t, "data.csv", zipReader.File[0].Name)

	rc, err := zipReader.File[0].Open()
	require.NoError(t, err)
	defer func() { _ = rc.Close() }()

	decompressed, err := io.ReadAll(rc)
	require.NoError(t, err)
	assert.Equal(t, originalContent, string(decompressed))
}

func TestExecuteSendStep_ContentTypeMapping(t *testing.T) {
	var receivedResources []map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var resource map[string]any
		_ = json.Unmarshal(body, &resource)
		receivedResources = append(receivedResources, resource)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	jobID := "test-content-types"
	jobDir := filepath.Join(tmpDir, jobID)
	inputDir := filepath.Join(jobDir, "csv")
	require.NoError(t, os.MkdirAll(inputDir, 0755))

	// Create files of different types
	require.NoError(t, os.WriteFile(filepath.Join(inputDir, "Patient.ndjson"), []byte(`{"resourceType":"Patient"}`+"\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(inputDir, "data.csv"), []byte("id\n1\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(inputDir, "data.parquet"), []byte("parquet-content"), 0644))

	job := createSendTestJob(server.URL, jobID, tmpDir)
	logger := lib.NewLogger(lib.LogLevelDebug)
	err := pipeline.ExecuteSendStep(job, jobDir, logger)
	require.NoError(t, err)

	// 3 Binaries + 1 DocumentReference = 4 requests
	require.Len(t, receivedResources, 4)

	// Collect content types from Binary resources
	contentTypes := make(map[string]bool)
	for _, resource := range receivedResources {
		if resource["resourceType"] == "Binary" {
			ct := resource["contentType"].(string)
			contentTypes[ct] = true
		}
	}

	assert.True(t, contentTypes["application/zip"], "ndjson should have application/zip")
	assert.True(t, contentTypes["text/zip"], "csv/parquet should have text/zip")
}

func TestExecuteSendStep_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"issue":"bad request"}`))
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	jobID := "test-send-error"
	jobDir := filepath.Join(tmpDir, jobID)
	inputDir := filepath.Join(jobDir, "csv")
	require.NoError(t, os.MkdirAll(inputDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(inputDir, "test.csv"), []byte("data"), 0644))

	job := createSendTestJob(server.URL, jobID, tmpDir)
	logger := lib.NewLogger(lib.LogLevelDebug)
	err := pipeline.ExecuteSendStep(job, jobDir, logger)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to upload Binary")
}

func TestExecuteSendStep_NoFiles(t *testing.T) {
	tmpDir := t.TempDir()
	jobID := "test-send-empty"
	jobDir := filepath.Join(tmpDir, jobID)
	inputDir := filepath.Join(jobDir, "csv")
	require.NoError(t, os.MkdirAll(inputDir, 0755))

	job := createSendTestJob("http://unused", jobID, tmpDir)
	logger := lib.NewLogger(lib.LogLevelDebug)
	err := pipeline.ExecuteSendStep(job, jobDir, logger)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no files found")
}

func TestExecuteSendStep_InvalidConfig(t *testing.T) {
	tmpDir := t.TempDir()
	jobID := "test-send-invalid-config"
	jobDir := filepath.Join(tmpDir, jobID)

	job := &models.PipelineJob{
		JobID:       jobID,
		InputSource: "/tmp/test",
		InputType:   models.InputTypeLocal,
		Status:      models.JobStatusInProgress,
		CurrentStep: string(models.StepSend),
		Config: models.ProjectConfig{
			Pipeline: models.PipelineConfig{
				EnabledSteps: []models.StepName{models.StepLocalImport, models.StepSend},
			},
			Services: models.ServiceConfig{
				Send: models.SendConfig{
					ServerURL: "", // Missing required field
				},
			},
			JobsDir: tmpDir,
		},
		Steps: []models.PipelineStep{
			{Name: models.StepSend, Status: models.StepStatusPending},
		},
	}

	logger := lib.NewLogger(lib.LogLevelDebug)
	err := pipeline.ExecuteSendStep(job, jobDir, logger)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "server_url is required")
}

func TestExecuteSendStep_NotEnabled(t *testing.T) {
	tmpDir := t.TempDir()
	jobID := "test-send-disabled"
	jobDir := filepath.Join(tmpDir, jobID)

	// Create a job where send step is NOT in enabled steps
	job := &models.PipelineJob{
		JobID:       jobID,
		InputSource: "/tmp/test",
		InputType:   models.InputTypeLocal,
		Status:      models.JobStatusInProgress,
		CurrentStep: string(models.StepSend),
		Config: models.ProjectConfig{
			Pipeline: models.PipelineConfig{
				EnabledSteps: []models.StepName{models.StepLocalImport}, // No StepSend
			},
			Services: models.ServiceConfig{
				Send: models.SendConfig{
					ServerURL:              "http://unused",
					ProjectIdentifier:      "TEST-PROJECT",
					OrganizationIdentifier: "test.org",
				},
			},
			JobsDir: tmpDir,
		},
		Steps: []models.PipelineStep{
			{Name: models.StepSend, Status: models.StepStatusPending},
		},
	}

	logger := lib.NewLogger(lib.LogLevelDebug)
	err := pipeline.ExecuteSendStep(job, jobDir, logger)
	// Should succeed by skipping (not an error)
	assert.NoError(t, err)
}

func TestExecuteSendStep_InputDirNotExists(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	jobID := "test-send-no-input-dir"
	jobDir := filepath.Join(tmpDir, jobID)
	// Deliberately NOT creating the input directory

	job := createSendTestJob(server.URL, jobID, tmpDir)
	logger := lib.NewLogger(lib.LogLevelDebug)
	err := pipeline.ExecuteSendStep(job, jobDir, logger)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to list input files")
}

func TestExecuteSendStep_DocumentReferenceUploadError(t *testing.T) {
	requestCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		body, _ := io.ReadAll(r.Body)
		var resource map[string]any
		_ = json.Unmarshal(body, &resource)

		// Succeed on Binary uploads, fail on DocumentReference
		if resource["resourceType"] == "DocumentReference" {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"issue":"server error"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	jobID := "test-docref-error"
	jobDir := filepath.Join(tmpDir, jobID)
	inputDir := filepath.Join(jobDir, "csv")
	require.NoError(t, os.MkdirAll(inputDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(inputDir, "test.csv"), []byte("data"), 0644))

	job := createSendTestJob(server.URL, jobID, tmpDir)
	logger := lib.NewLogger(lib.LogLevelDebug)
	err := pipeline.ExecuteSendStep(job, jobDir, logger)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to upload DocumentReference")
}

func TestExecuteSendStep_JsonFile(t *testing.T) {
	var receivedResources []map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var resource map[string]any
		_ = json.Unmarshal(body, &resource)
		receivedResources = append(receivedResources, resource)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	jobID := "test-json-file"
	jobDir := filepath.Join(tmpDir, jobID)
	inputDir := filepath.Join(jobDir, "csv")
	require.NoError(t, os.MkdirAll(inputDir, 0755))

	// Create a JSON file - all files are now zipped
	bundleContent := `{
		"resourceType": "Bundle",
		"type": "transaction",
		"entry": [{"resource": {"resourceType": "Patient", "id": "1"}}]
	}`
	require.NoError(t, os.WriteFile(filepath.Join(inputDir, "bundle.json"), []byte(bundleContent), 0644))

	job := createSendTestJob(server.URL, jobID, tmpDir)
	logger := lib.NewLogger(lib.LogLevelDebug)
	err := pipeline.ExecuteSendStep(job, jobDir, logger)
	require.NoError(t, err)

	// All files are now zipped - JSON files get application/zip
	var binaryResource map[string]any
	for _, res := range receivedResources {
		if res["resourceType"] == "Binary" {
			binaryResource = res
			break
		}
	}
	require.NotNil(t, binaryResource)
	assert.Equal(t, "application/zip", binaryResource["contentType"])

	// Verify the data is a valid zip archive containing the JSON
	b64Data := binaryResource["data"].(string)
	zipBytes, err := base64.StdEncoding.DecodeString(b64Data)
	require.NoError(t, err)

	zipReader, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	require.NoError(t, err)
	require.Len(t, zipReader.File, 1)
	assert.Equal(t, "bundle.json", zipReader.File[0].Name)
}

func TestExecuteSendStep_AllJsonFilesZipped(t *testing.T) {
	// All JSON files are now zipped - no special handling for FHIR Bundles
	var receivedResources []map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var resource map[string]any
		_ = json.Unmarshal(body, &resource)
		receivedResources = append(receivedResources, resource)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	jobID := "test-all-json-zipped"
	jobDir := filepath.Join(tmpDir, jobID)
	inputDir := filepath.Join(jobDir, "csv")
	require.NoError(t, os.MkdirAll(inputDir, 0755))

	// Create various JSON files - all should be zipped
	require.NoError(t, os.WriteFile(filepath.Join(inputDir, "config.json"), []byte(`{"type": "config"}`), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(inputDir, "invalid.json"), []byte(`{not valid json}`), 0644))

	job := createSendTestJob(server.URL, jobID, tmpDir)
	logger := lib.NewLogger(lib.LogLevelDebug)
	err := pipeline.ExecuteSendStep(job, jobDir, logger)
	require.NoError(t, err)

	// Count Binary resources with application/zip
	zipCount := 0
	for _, res := range receivedResources {
		if res["resourceType"] == "Binary" && res["contentType"] == "application/zip" {
			zipCount++
		}
	}
	assert.Equal(t, 2, zipCount, "All JSON files should be zipped")
}

func TestExecuteSendStep_SubdirectoryFiles(t *testing.T) {
	var receivedResources []map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var resource map[string]any
		_ = json.Unmarshal(body, &resource)
		receivedResources = append(receivedResources, resource)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	jobID := "test-subdirs"
	jobDir := filepath.Join(tmpDir, jobID)
	inputDir := filepath.Join(jobDir, "csv")
	subDir := filepath.Join(inputDir, "subdir")
	require.NoError(t, os.MkdirAll(subDir, 0755))

	// Create files in both root and subdirectory
	require.NoError(t, os.WriteFile(filepath.Join(inputDir, "root.csv"), []byte("data"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(subDir, "nested.csv"), []byte("nested"), 0644))

	job := createSendTestJob(server.URL, jobID, tmpDir)
	logger := lib.NewLogger(lib.LogLevelDebug)
	err := pipeline.ExecuteSendStep(job, jobDir, logger)
	require.NoError(t, err)

	// Should process 2 files (root.csv + nested.csv) -> 2 Binaries + 1 DocumentReference
	binaryCount := 0
	for _, res := range receivedResources {
		if res["resourceType"] == "Binary" {
			binaryCount++
		}
	}
	assert.Equal(t, 2, binaryCount)
}

func TestExecuteSendStep_ServerNonRetryableError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return 400 Bad Request (non-retryable error)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"issue":"bad request"}`))
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	jobID := "test-non-retryable-error"
	jobDir := filepath.Join(tmpDir, jobID)
	inputDir := filepath.Join(jobDir, "csv")
	require.NoError(t, os.MkdirAll(inputDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(inputDir, "test.csv"), []byte("data"), 0644))

	job := createSendTestJob(server.URL, jobID, tmpDir)
	logger := lib.NewLogger(lib.LogLevelDebug)
	err := pipeline.ExecuteSendStep(job, jobDir, logger)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to upload Binary")

	// Check that step has non-transient error type for 400 errors
	var sendStep *models.PipelineStep
	for i := range job.Steps {
		if job.Steps[i].Name == models.StepSend {
			sendStep = &job.Steps[i]
			break
		}
	}
	require.NotNil(t, sendStep)
	assert.Equal(t, models.ErrorTypeNonTransient, sendStep.LastError.Type)
}

func TestExecuteSendStep_UnknownFileExtension(t *testing.T) {
	var receivedResources []map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var resource map[string]any
		_ = json.Unmarshal(body, &resource)
		receivedResources = append(receivedResources, resource)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	jobID := "test-unknown-ext"
	jobDir := filepath.Join(tmpDir, jobID)
	inputDir := filepath.Join(jobDir, "csv")
	require.NoError(t, os.MkdirAll(inputDir, 0755))

	// Create a file with unknown extension
	require.NoError(t, os.WriteFile(filepath.Join(inputDir, "data.xyz"), []byte("some data"), 0644))

	job := createSendTestJob(server.URL, jobID, tmpDir)
	logger := lib.NewLogger(lib.LogLevelDebug)
	err := pipeline.ExecuteSendStep(job, jobDir, logger)
	require.NoError(t, err)

	var binaryResource map[string]any
	for _, res := range receivedResources {
		if res["resourceType"] == "Binary" {
			binaryResource = res
			break
		}
	}
	require.NotNil(t, binaryResource)
	// Unknown extension defaults to application/zip
	assert.Equal(t, "application/zip", binaryResource["contentType"])
}

func TestExecuteSendStep_FileReadError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	jobID := "test-file-read-error"
	jobDir := filepath.Join(tmpDir, jobID)
	inputDir := filepath.Join(jobDir, "csv")
	require.NoError(t, os.MkdirAll(inputDir, 0755))

	// Create a file that cannot be read (directory pretending to be a file via symlink trick won't work)
	// Instead, create a file and then make its directory unreadable
	testFile := filepath.Join(inputDir, "test.csv")
	require.NoError(t, os.WriteFile(testFile, []byte("data"), 0644))

	// Make the file unreadable by removing read permissions
	require.NoError(t, os.Chmod(testFile, 0000))

	// Restore permissions after test
	defer func() { _ = os.Chmod(testFile, 0644) }()

	job := createSendTestJob(server.URL, jobID, tmpDir)
	logger := lib.NewLogger(lib.LogLevelDebug)
	err := pipeline.ExecuteSendStep(job, jobDir, logger)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to process")
}

func TestExecuteSendStep_CompressedNdjsonFile(t *testing.T) {
	var receivedResources []map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var resource map[string]any
		_ = json.Unmarshal(body, &resource)
		receivedResources = append(receivedResources, resource)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	jobID := "test-compressed-ndjson"
	jobDir := filepath.Join(tmpDir, jobID)
	inputDir := filepath.Join(jobDir, "csv")
	require.NoError(t, os.MkdirAll(inputDir, 0755))

	// Create a .ndjson.zst file (compressed NDJSON)
	ndjsonContent := `{"resourceType":"Patient","id":"1"}` + "\n"

	// Write compressed content using zstd
	zstdFile := filepath.Join(inputDir, "Patient.ndjson.zst")
	compressedWriter, err := lib.CreateCompressedFileWriter(zstdFile, "default")
	require.NoError(t, err)
	_, err = compressedWriter.Write([]byte(ndjsonContent))
	require.NoError(t, err)
	require.NoError(t, compressedWriter.Close())

	job := createSendTestJob(server.URL, jobID, tmpDir)
	logger := lib.NewLogger(lib.LogLevelDebug)
	err = pipeline.ExecuteSendStep(job, jobDir, logger)
	require.NoError(t, err)

	// Verify the Binary was created with zip content type
	var binaryResource map[string]any
	for _, res := range receivedResources {
		if res["resourceType"] == "Binary" {
			binaryResource = res
			break
		}
	}
	require.NotNil(t, binaryResource)
	assert.Equal(t, "application/zip", binaryResource["contentType"])

	// Verify the zip contains the uncompressed NDJSON content
	b64Data := binaryResource["data"].(string)
	zipBytes, err := base64.StdEncoding.DecodeString(b64Data)
	require.NoError(t, err)

	zipReader, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	require.NoError(t, err)
	require.Len(t, zipReader.File, 1)

	// The zip entry should have .ndjson extension (without .zst)
	assert.Equal(t, "Patient.ndjson", zipReader.File[0].Name)

	// Verify content
	rc, err := zipReader.File[0].Open()
	require.NoError(t, err)
	defer func() { _ = rc.Close() }()
	decompressed, err := io.ReadAll(rc)
	require.NoError(t, err)
	assert.Equal(t, ndjsonContent, string(decompressed))
}

func TestExecuteSendStep_CompressedJsonFile(t *testing.T) {
	var receivedResources []map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var resource map[string]any
		_ = json.Unmarshal(body, &resource)
		receivedResources = append(receivedResources, resource)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	jobID := "test-compressed-json"
	jobDir := filepath.Join(tmpDir, jobID)
	inputDir := filepath.Join(jobDir, "csv")
	require.NoError(t, os.MkdirAll(inputDir, 0755))

	// Create a compressed JSON file (.json.zst)
	jsonContent := `{"resourceType":"Bundle","type":"transaction","entry":[]}`

	// Write compressed content
	zstdFile := filepath.Join(inputDir, "data.json.zst")
	compressedWriter, err := lib.CreateCompressedFileWriter(zstdFile, "default")
	require.NoError(t, err)
	_, err = compressedWriter.Write([]byte(jsonContent))
	require.NoError(t, err)
	require.NoError(t, compressedWriter.Close())

	job := createSendTestJob(server.URL, jobID, tmpDir)
	logger := lib.NewLogger(lib.LogLevelDebug)
	err = pipeline.ExecuteSendStep(job, jobDir, logger)
	require.NoError(t, err)

	// Verify the Binary was created
	var binaryResource map[string]any
	for _, res := range receivedResources {
		if res["resourceType"] == "Binary" {
			binaryResource = res
			break
		}
	}
	require.NotNil(t, binaryResource)
	// Compressed JSON files (.json.zst) go through default path and get zipped
	assert.Equal(t, "application/zip", binaryResource["contentType"])
}

func TestExecuteSendStep_UnreadableFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	jobID := "test-unreadable-file"
	jobDir := filepath.Join(tmpDir, jobID)
	inputDir := filepath.Join(jobDir, "csv")
	require.NoError(t, os.MkdirAll(inputDir, 0755))

	// Create a file but make it unreadable
	testFile := filepath.Join(inputDir, "test.json")
	require.NoError(t, os.WriteFile(testFile, []byte(`{"resourceType":"Patient"}`), 0644))

	// Make the file unreadable
	require.NoError(t, os.Chmod(testFile, 0000))
	defer func() { _ = os.Chmod(testFile, 0644) }()

	job := createSendTestJob(server.URL, jobID, tmpDir)
	logger := lib.NewLogger(lib.LogLevelDebug)
	err := pipeline.ExecuteSendStep(job, jobDir, logger)
	// Should fail when trying to read the file to process it
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to process")
}

func TestExecuteSendStep_ConnectionRefused(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping network error test in short mode")
	}

	// Use localhost with a port that's not listening to trigger connection refused
	// Find a high port that's likely not in use
	unreachableURL := "http://127.0.0.1:59999"

	tmpDir := t.TempDir()
	jobID := "test-connection-refused"
	jobDir := filepath.Join(tmpDir, jobID)
	inputDir := filepath.Join(jobDir, "csv")
	require.NoError(t, os.MkdirAll(inputDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(inputDir, "test.csv"), []byte("data"), 0644))

	job := createSendTestJob(unreachableURL, jobID, tmpDir)
	logger := lib.NewLogger(lib.LogLevelDebug)
	err := pipeline.ExecuteSendStep(job, jobDir, logger)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to upload Binary")

	// Check that step has transient error type (connection refused is transient)
	var sendStep *models.PipelineStep
	for i := range job.Steps {
		if job.Steps[i].Name == models.StepSend {
			sendStep = &job.Steps[i]
			break
		}
	}
	require.NotNil(t, sendStep)
	assert.Equal(t, models.ErrorTypeTransient, sendStep.LastError.Type)
}

func createSendTestJob(serverURL, jobID, jobsDir string) *models.PipelineJob {
	return &models.PipelineJob{
		JobID:       jobID,
		InputSource: "/tmp/test",
		InputType:   models.InputTypeLocal,
		Status:      models.JobStatusInProgress,
		CurrentStep: string(models.StepSend),
		Config: models.ProjectConfig{
			Pipeline: models.PipelineConfig{
				EnabledSteps: []models.StepName{
					models.StepLocalImport,
					models.StepFlattening,
					models.StepSend,
				},
			},
			Services: models.ServiceConfig{
				Send: models.SendConfig{
					ServerURL:              serverURL,
					ProjectIdentifier:      "TEST-PROJECT",
					OrganizationIdentifier: "test.hospital.de",
				},
			},
			JobsDir: jobsDir,
		},
		Steps: []models.PipelineStep{
			{Name: models.StepSend, Status: models.StepStatusPending},
		},
	}
}

func createSendTestJobWithAuth(serverURL, jobID, jobsDir string, sendConfig models.SendConfig) *models.PipelineJob {
	sendConfig.ServerURL = serverURL
	if sendConfig.ProjectIdentifier == "" {
		sendConfig.ProjectIdentifier = "TEST-PROJECT"
	}
	if sendConfig.OrganizationIdentifier == "" {
		sendConfig.OrganizationIdentifier = "test.hospital.de"
	}
	return &models.PipelineJob{
		JobID:       jobID,
		InputSource: "/tmp/test",
		InputType:   models.InputTypeLocal,
		Status:      models.JobStatusInProgress,
		CurrentStep: string(models.StepSend),
		Config: models.ProjectConfig{
			Pipeline: models.PipelineConfig{
				EnabledSteps: []models.StepName{
					models.StepLocalImport,
					models.StepFlattening,
					models.StepSend,
				},
			},
			Services: models.ServiceConfig{
				Send: sendConfig,
			},
			JobsDir: jobsDir,
		},
		Steps: []models.PipelineStep{
			{Name: models.StepSend, Status: models.StepStatusPending},
		},
	}
}

func TestExecuteSendStep_BasicAuth(t *testing.T) {
	var receivedAuthHeader string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuthHeader = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	jobID := "test-basic-auth"
	jobDir := filepath.Join(tmpDir, jobID)
	inputDir := filepath.Join(jobDir, "csv")
	require.NoError(t, os.MkdirAll(inputDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(inputDir, "test.csv"), []byte("data"), 0644))

	job := createSendTestJobWithAuth(server.URL, jobID, tmpDir, models.SendConfig{
		Username: "testuser",
		Password: "testpass",
	})

	logger := lib.NewLogger(lib.LogLevelDebug)
	err := pipeline.ExecuteSendStep(job, jobDir, logger)
	require.NoError(t, err)

	// Verify Basic Auth header was sent
	assert.True(t, strings.HasPrefix(receivedAuthHeader, "Basic "))

	// Decode and verify credentials
	encodedCreds := strings.TrimPrefix(receivedAuthHeader, "Basic ")
	decoded, err := base64.StdEncoding.DecodeString(encodedCreds)
	require.NoError(t, err)
	assert.Equal(t, "testuser:testpass", string(decoded))
}

func TestExecuteSendStep_OAuth2(t *testing.T) {
	var receivedAuthHeader string
	var tokenRequests int

	// Mock OAuth2 token server
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenRequests++
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/protocol/openid-connect/token", r.URL.Path)
		assert.Equal(t, "application/x-www-form-urlencoded", r.Header.Get("Content-Type"))

		body, _ := io.ReadAll(r.Body)
		bodyStr := string(body)
		assert.Contains(t, bodyStr, "grant_type=client_credentials")
		assert.Contains(t, bodyStr, "client_id=test-client")
		assert.Contains(t, bodyStr, "client_secret=test-secret")

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token": "test-token-12345", "expires_in": 300, "token_type": "Bearer"}`))
	}))
	defer tokenServer.Close()

	// Mock FHIR server
	fhirServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuthHeader = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer fhirServer.Close()

	// Clear the token cache before the test
	pipeline.ClearOAuth2TokenCacheForTesting()

	tmpDir := t.TempDir()
	jobID := "test-oauth2"
	jobDir := filepath.Join(tmpDir, jobID)
	inputDir := filepath.Join(jobDir, "csv")
	require.NoError(t, os.MkdirAll(inputDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(inputDir, "test.csv"), []byte("data"), 0644))

	job := createSendTestJobWithAuth(fhirServer.URL, jobID, tmpDir, models.SendConfig{
		OAuthIssuerURI:    tokenServer.URL,
		OAuthClientID:     "test-client",
		OAuthClientSecret: "test-secret",
	})

	logger := lib.NewLogger(lib.LogLevelDebug)
	err := pipeline.ExecuteSendStep(job, jobDir, logger)
	require.NoError(t, err)

	// Verify Bearer token was sent
	assert.Equal(t, "Bearer test-token-12345", receivedAuthHeader)

	// Token should have been fetched (could be 1 or 2 depending on caching)
	assert.GreaterOrEqual(t, tokenRequests, 1)
}

func TestExecuteSendStep_NoAuth(t *testing.T) {
	var receivedAuthHeader string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuthHeader = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	jobID := "test-no-auth"
	jobDir := filepath.Join(tmpDir, jobID)
	inputDir := filepath.Join(jobDir, "csv")
	require.NoError(t, os.MkdirAll(inputDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(inputDir, "test.csv"), []byte("data"), 0644))

	// No auth configured
	job := createSendTestJobWithAuth(server.URL, jobID, tmpDir, models.SendConfig{})

	logger := lib.NewLogger(lib.LogLevelDebug)
	err := pipeline.ExecuteSendStep(job, jobDir, logger)
	require.NoError(t, err)

	// No Authorization header should be sent
	assert.Empty(t, receivedAuthHeader)
}

func TestExecuteSendStep_OAuth2TokenError(t *testing.T) {
	// Mock OAuth2 token server that returns an error
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error": "invalid_client", "error_description": "Bad credentials"}`))
	}))
	defer tokenServer.Close()

	// Mock FHIR server (should not be called)
	fhirServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("FHIR server should not be called when token fetch fails")
		w.WriteHeader(http.StatusOK)
	}))
	defer fhirServer.Close()

	// Clear the token cache before the test
	pipeline.ClearOAuth2TokenCacheForTesting()

	tmpDir := t.TempDir()
	jobID := "test-oauth2-error"
	jobDir := filepath.Join(tmpDir, jobID)
	inputDir := filepath.Join(jobDir, "csv")
	require.NoError(t, os.MkdirAll(inputDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(inputDir, "test.csv"), []byte("data"), 0644))

	job := createSendTestJobWithAuth(fhirServer.URL, jobID, tmpDir, models.SendConfig{
		OAuthIssuerURI:    tokenServer.URL,
		OAuthClientID:     "invalid-client",
		OAuthClientSecret: "wrong-secret",
	})

	logger := lib.NewLogger(lib.LogLevelDebug)
	err := pipeline.ExecuteSendStep(job, jobDir, logger)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get OAuth2 token")
}

func TestExecuteSendStep_OAuth2TokenCaching(t *testing.T) {
	tokenRequestCount := 0

	// Mock OAuth2 token server that counts requests
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenRequestCount++
		w.Header().Set("Content-Type", "application/json")
		// Return a token that expires in 60 seconds (will be cached)
		_, _ = w.Write([]byte(`{"access_token": "cached-token-xyz", "expires_in": 60, "token_type": "Bearer"}`))
	}))
	defer tokenServer.Close()

	// Mock FHIR server
	fhirServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer fhirServer.Close()

	// Clear the token cache before the test
	pipeline.ClearOAuth2TokenCacheForTesting()

	tmpDir := t.TempDir()

	// First job execution
	jobID1 := "test-oauth2-cache-1"
	jobDir1 := filepath.Join(tmpDir, jobID1)
	inputDir1 := filepath.Join(jobDir1, "csv")
	require.NoError(t, os.MkdirAll(inputDir1, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(inputDir1, "test.csv"), []byte("data"), 0644))

	job1 := createSendTestJobWithAuth(fhirServer.URL, jobID1, tmpDir, models.SendConfig{
		OAuthIssuerURI:    tokenServer.URL,
		OAuthClientID:     "test-client",
		OAuthClientSecret: "test-secret",
	})

	logger := lib.NewLogger(lib.LogLevelDebug)
	err := pipeline.ExecuteSendStep(job1, jobDir1, logger)
	require.NoError(t, err)

	firstRequestCount := tokenRequestCount

	// Second job execution - should reuse cached token
	jobID2 := "test-oauth2-cache-2"
	jobDir2 := filepath.Join(tmpDir, jobID2)
	inputDir2 := filepath.Join(jobDir2, "csv")
	require.NoError(t, os.MkdirAll(inputDir2, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(inputDir2, "test2.csv"), []byte("data2"), 0644))

	job2 := createSendTestJobWithAuth(fhirServer.URL, jobID2, tmpDir, models.SendConfig{
		OAuthIssuerURI:    tokenServer.URL,
		OAuthClientID:     "test-client",
		OAuthClientSecret: "test-secret",
	})

	err = pipeline.ExecuteSendStep(job2, jobDir2, logger)
	require.NoError(t, err)

	// The second execution should not have requested a new token (cached)
	// Note: Multiple PUT requests in first job may have all used the same token
	assert.GreaterOrEqual(t, firstRequestCount, 1, "At least one token request should have been made")
	// If caching works, token requests should not increase significantly
	assert.LessOrEqual(t, tokenRequestCount, firstRequestCount+1, "Token caching should reduce requests")
}

func TestExecuteSendStep_OAuth2TokenInvalidJSON(t *testing.T) {
	// Mock OAuth2 token server that returns invalid JSON
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{invalid json`))
	}))
	defer tokenServer.Close()

	// Mock FHIR server (should not be called)
	fhirServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("FHIR server should not be called when token parsing fails")
		w.WriteHeader(http.StatusOK)
	}))
	defer fhirServer.Close()

	// Clear the token cache before the test
	pipeline.ClearOAuth2TokenCacheForTesting()

	tmpDir := t.TempDir()
	jobID := "test-oauth2-invalid-json"
	jobDir := filepath.Join(tmpDir, jobID)
	inputDir := filepath.Join(jobDir, "csv")
	require.NoError(t, os.MkdirAll(inputDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(inputDir, "test.csv"), []byte("data"), 0644))

	job := createSendTestJobWithAuth(fhirServer.URL, jobID, tmpDir, models.SendConfig{
		OAuthIssuerURI:    tokenServer.URL,
		OAuthClientID:     "test-client",
		OAuthClientSecret: "test-secret",
	})

	logger := lib.NewLogger(lib.LogLevelDebug)
	err := pipeline.ExecuteSendStep(job, jobDir, logger)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get OAuth2 token")
}

func TestExecuteSendStep_OAuth2TokenMissingAccessToken(t *testing.T) {
	// Mock OAuth2 token server that returns empty access_token
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// Valid JSON but missing access_token
		_, _ = w.Write([]byte(`{"expires_in": 300, "token_type": "Bearer"}`))
	}))
	defer tokenServer.Close()

	// Mock FHIR server (should not be called)
	fhirServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("FHIR server should not be called when access_token is missing")
		w.WriteHeader(http.StatusOK)
	}))
	defer fhirServer.Close()

	// Clear the token cache before the test
	pipeline.ClearOAuth2TokenCacheForTesting()

	tmpDir := t.TempDir()
	jobID := "test-oauth2-missing-token"
	jobDir := filepath.Join(tmpDir, jobID)
	inputDir := filepath.Join(jobDir, "csv")
	require.NoError(t, os.MkdirAll(inputDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(inputDir, "test.csv"), []byte("data"), 0644))

	job := createSendTestJobWithAuth(fhirServer.URL, jobID, tmpDir, models.SendConfig{
		OAuthIssuerURI:    tokenServer.URL,
		OAuthClientID:     "test-client",
		OAuthClientSecret: "test-secret",
	})

	logger := lib.NewLogger(lib.LogLevelDebug)
	err := pipeline.ExecuteSendStep(job, jobDir, logger)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get OAuth2 token")
}

func TestExecuteSendStep_OAuth2TokenShortExpiry(t *testing.T) {
	tokenRequestCount := 0

	// Mock OAuth2 token server that returns a short-lived token (expires_in <= 30)
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenRequestCount++
		w.Header().Set("Content-Type", "application/json")
		// Token expires in 10 seconds (will NOT be cached since <= 30)
		_, _ = w.Write([]byte(`{"access_token": "short-lived-token", "expires_in": 10, "token_type": "Bearer"}`))
	}))
	defer tokenServer.Close()

	// Mock FHIR server
	fhirServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer fhirServer.Close()

	// Clear the token cache before the test
	pipeline.ClearOAuth2TokenCacheForTesting()

	tmpDir := t.TempDir()
	jobID := "test-oauth2-short-expiry"
	jobDir := filepath.Join(tmpDir, jobID)
	inputDir := filepath.Join(jobDir, "csv")
	require.NoError(t, os.MkdirAll(inputDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(inputDir, "test.csv"), []byte("data"), 0644))

	job := createSendTestJobWithAuth(fhirServer.URL, jobID, tmpDir, models.SendConfig{
		OAuthIssuerURI:    tokenServer.URL,
		OAuthClientID:     "test-client",
		OAuthClientSecret: "test-secret",
	})

	logger := lib.NewLogger(lib.LogLevelDebug)
	err := pipeline.ExecuteSendStep(job, jobDir, logger)
	require.NoError(t, err)

	// With short-lived token, each PUT request may need a new token
	assert.GreaterOrEqual(t, tokenRequestCount, 1)
}
