package unit

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/send"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

func transferSendConfig(url string) models.SendConfig {
	return models.SendConfig{
		URL:    url,
		SendAs: models.SendModeTransferLoad,
		Transfer: models.TransferConfig{
			ProjectIdentifier:      "TEST-PROJECT",
			OrganizationIdentifier: "test.hospital.de",
		},
	}
}

func writeSendFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func TestSend_TransferLoad_UploadsBinaryPerFileAndOneDocumentReference(t *testing.T) {
	var mu sync.Mutex
	var resources []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var res map[string]any
		_ = json.Unmarshal(body, &res)
		mu.Lock()
		resources = append(resources, res)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	dir := t.TempDir()
	csvPath := filepath.Join(dir, "Patient.csv")
	ndjsonPath := filepath.Join(dir, "sub", "Observation.ndjson")
	writeSendFile(t, csvPath, "id\n1\n")
	writeSendFile(t, ndjsonPath, `{"resourceType":"Observation"}`+"\n")

	result, err := send.Send(context.Background(), send.Request{
		JobID:  "job-1",
		Config: transferSendConfig(server.URL),
		Retry:  FastRetryConfig(),
		Files: []send.File{
			{Path: csvPath, Name: "Patient.csv"},
			{Path: ndjsonPath, Name: filepath.Join("sub", "Observation.ndjson")},
		},
		State: transferState(dir),
	}, lib.NewLogger(lib.LogLevelError))
	require.NoError(t, err)
	assert.Equal(t, 2, result.FilesProcessed)

	var entryNames []string
	docRefs := 0
	for _, res := range resources {
		switch res["resourceType"] {
		case "DocumentReference":
			docRefs++
		case "Binary":
			data, decErr := base64.StdEncoding.DecodeString(res["data"].(string))
			require.NoError(t, decErr)
			zr, zipErr := zip.NewReader(bytes.NewReader(data), int64(len(data)))
			require.NoError(t, zipErr)
			require.Len(t, zr.File, 1)
			entryNames = append(entryNames, zr.File[0].Name)
		}
	}
	assert.Equal(t, 1, docRefs)
	assert.ElementsMatch(t, []string{"Patient.csv", "Observation.ndjson"}, entryNames)
}

func s3SendConfig() models.SendConfig {
	return models.SendConfig{
		SendAs: models.SendModeS3Upload,
		S3: models.S3Config{
			Region:          "eu-central-1",
			Bucket:          "test-bucket",
			AccessKeyID:     "AKIAIOSFODNN7EXAMPLE",
			SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
			Timeout:         30 * time.Minute,
		},
	}
}

func installMockS3(t *testing.T) *services.MockS3Uploader {
	t.Helper()
	mock := &services.MockS3Uploader{Bucket: "test-bucket"}
	send.SetS3UploaderFactoryForTesting(func(models.S3Config, models.AuthConfig, models.TLSConfig, *lib.Logger) (services.S3Uploader, error) {
		return mock, nil
	})
	t.Cleanup(send.ResetS3UploaderFactory)
	return mock
}

func TestSend_S3Upload_UsesJobIDAndNameAsKeyAndWritesManifest(t *testing.T) {
	mock := installMockS3(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "Patient.csv")
	writeSendFile(t, path, "id\n1\n")
	manifestPath := filepath.Join(dir, "manifest.json")

	result, err := send.Send(context.Background(), send.Request{
		JobID:  "job-s3",
		Config: s3SendConfig(),
		Retry:  FastRetryConfig(),
		Files:  []send.File{{Path: path, Name: filepath.Join("sub", "Patient.csv")}},
		State:  send.State{ManifestPath: manifestPath},
	}, lib.NewLogger(lib.LogLevelError))
	require.NoError(t, err)
	assert.Equal(t, 1, result.FilesProcessed)
	assert.Equal(t, []string{"job-s3/sub/Patient.csv"}, mock.UploadedKeys)

	manifest, err := models.LoadUploadManifest(manifestPath)
	require.NoError(t, err)
	assert.True(t, manifest.IsFileUploaded(path))
}

func TestSend_S3Upload_SkipsFilesAlreadyInManifest(t *testing.T) {
	mock := installMockS3(t)
	dir := t.TempDir()
	done := filepath.Join(dir, "done.csv")
	todo := filepath.Join(dir, "todo.csv")
	writeSendFile(t, done, "a\n")
	writeSendFile(t, todo, "b\n")
	manifestPath := filepath.Join(dir, "manifest.json")

	manifest := models.NewUploadManifest("job-resume", "test-bucket")
	manifest.AddUploadedFile(done, "job-resume/done.csv", "\"etag\"", 2)
	require.NoError(t, models.SaveManifest(manifest, manifestPath))

	_, err := send.Send(context.Background(), send.Request{
		JobID:  "job-resume",
		Config: s3SendConfig(),
		Retry:  FastRetryConfig(),
		Files:  []send.File{{Path: done, Name: "done.csv"}, {Path: todo, Name: "todo.csv"}},
		State:  send.State{ManifestPath: manifestPath},
	}, lib.NewLogger(lib.LogLevelError))
	require.NoError(t, err)
	assert.Equal(t, []string{"job-resume/todo.csv"}, mock.UploadedKeys)
}

func TestSend_DumpsFailedRequestsToStateDir(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	dir := t.TempDir()
	path := filepath.Join(dir, "Patient.csv")
	writeSendFile(t, path, "id\n1\n")
	dumpState := transferState(dir)
	dumpState.FailedRequestsDir = filepath.Join(dir, "dumps")

	cfg := transferSendConfig(server.URL)
	cfg.DumpFailedRequests = true

	_, err := send.Send(context.Background(), send.Request{
		JobID:  "job-dump",
		Config: cfg,
		Retry:  FastRetryConfig(),
		Files:  []send.File{{Path: path, Name: "Patient.csv"}},
		State:  dumpState,
	}, lib.NewLogger(lib.LogLevelError))
	require.Error(t, err)

	dumps, globErr := filepath.Glob(filepath.Join(dumpState.FailedRequestsDir, "*.request.json"))
	require.NoError(t, globErr)
	assert.NotEmpty(t, dumps)
}

func TestSend_InvalidConfigFailsBeforeAnyRequest(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	dir := t.TempDir()
	path := filepath.Join(dir, "Patient.csv")
	writeSendFile(t, path, "id\n1\n")

	cfg := transferSendConfig(server.URL)
	cfg.Transfer.ProjectIdentifier = ""
	wantErr := cfg.Validate()
	require.Error(t, wantErr)

	_, err := send.Send(context.Background(), send.Request{
		JobID:  "job-invalid",
		Config: cfg,
		Retry:  FastRetryConfig(),
		Files:  []send.File{{Path: path, Name: "Patient.csv"}},
	}, lib.NewLogger(lib.LogLevelError))
	require.Error(t, err)
	assert.Equal(t, wantErr.Error(), err.Error())
	assert.Zero(t, requests.Load())
}

func TestSend_DirectResourceLoad_UploadsCoreFilesFirst(t *testing.T) {
	var mu sync.Mutex
	var ids []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		body, _ := io.ReadAll(r.Body)
		var bundle map[string]any
		_ = json.Unmarshal(body, &bundle)
		if entries, ok := bundle["entry"].([]any); ok && len(entries) > 0 {
			if res, ok := entries[0].(map[string]any)["resource"].(map[string]any); ok {
				ids = append(ids, res["id"].(string))
			}
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	dir := t.TempDir()
	patient := filepath.Join(dir, "Patient.ndjson")
	core := filepath.Join(dir, "core.ndjson")
	writeSendFile(t, patient, `{"resourceType":"Patient","id":"patient-file"}`+"\n")
	writeSendFile(t, core, `{"resourceType":"Organization","id":"core-file"}`+"\n")

	_, err := send.Send(context.Background(), send.Request{
		JobID: "job-direct",
		Config: models.SendConfig{
			URL:    server.URL,
			SendAs: models.SendModeDirectResourceLoad,
		},
		Retry: FastRetryConfig(),
		Files: []send.File{{Path: patient, Name: "Patient.ndjson"}, {Path: core, Name: "core.ndjson"}},
	}, lib.NewLogger(lib.LogLevelError))
	require.NoError(t, err)
	assert.Equal(t, []string{"core-file", "patient-file"}, ids)
}

func TestSend_RejectsBrokenRequestContract(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Patient.csv")
	writeSendFile(t, path, "id\n1\n")
	files := []send.File{{Path: path, Name: "Patient.csv"}}
	dumpConfig := transferSendConfig("http://127.0.0.1:1")
	dumpConfig.DumpFailedRequests = true

	tests := []struct {
		name    string
		req     send.Request
		wantErr string
	}{
		{
			name:    "no files",
			req:     send.Request{JobID: "job", Config: transferSendConfig("http://127.0.0.1:1")},
			wantErr: "no files to send",
		},
		{
			name:    "empty job ID",
			req:     send.Request{Config: transferSendConfig("http://127.0.0.1:1"), Files: files},
			wantErr: "job ID",
		},
		{
			name:    "s3 without manifest path",
			req:     send.Request{JobID: "job", Config: s3SendConfig(), Files: files},
			wantErr: "manifest path",
		},
		{
			name:    "transfer without manifest path",
			req:     send.Request{JobID: "job", Config: transferSendConfig("http://127.0.0.1:1"), Files: files},
			wantErr: "transfer manifest path",
		},
		{
			name:    "dump without directory",
			req:     send.Request{JobID: "job", Config: dumpConfig, Files: files},
			wantErr: "failed requests directory",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := installMockS3(t)
			_, err := send.Send(context.Background(), tt.req, lib.NewLogger(lib.LogLevelError))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
			assert.Empty(t, mock.UploadedKeys)
		})
	}
}

func TestSend_TransferLoad_ZipEntryNameIsEntryName(t *testing.T) {
	var mu sync.Mutex
	var binaries []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var res map[string]any
		_ = json.Unmarshal(body, &res)
		if res["resourceType"] == "Binary" {
			mu.Lock()
			binaries = append(binaries, res)
			mu.Unlock()
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "b.csv")
	writeSendFile(t, path, "id\n1\n")

	_, err := send.Send(context.Background(), send.Request{
		JobID:  "job-name",
		Config: transferSendConfig(server.URL),
		Retry:  FastRetryConfig(),
		Files:  []send.File{{Path: path, Name: "sub/b.csv", EntryName: "sub/b.csv"}},
		State:  transferState(dir),
	}, lib.NewLogger(lib.LogLevelError))
	require.NoError(t, err)

	require.Len(t, binaries, 1)
	data, err := base64.StdEncoding.DecodeString(binaries[0]["data"].(string))
	require.NoError(t, err)
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	require.NoError(t, err)
	require.Len(t, zr.File, 1)
	assert.Equal(t, "sub/b.csv", zr.File[0].Name)
}

func TestSend_TransferLoad_ErrorNamesEntryName(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	dir := t.TempDir()

	_, err := send.Send(context.Background(), send.Request{
		JobID:  "job-name",
		Config: transferSendConfig(server.URL),
		Retry:  FastRetryConfig(),
		Files:  []send.File{{Path: filepath.Join(dir, "2024", "a.csv"), Name: "2024/a.csv", EntryName: "2024/a.csv"}},
		State:  transferState(dir),
	}, lib.NewLogger(lib.LogLevelError))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to process 2024/a.csv")
}

func TestSend_TransferLoad_UploadsEachBinaryBeforeReadingNextFile(t *testing.T) {
	dir := t.TempDir()
	firstPath := filepath.Join(dir, "first.csv")
	secondPath := filepath.Join(dir, "second.csv")
	writeSendFile(t, firstPath, "id\n1\n")

	var mu sync.Mutex
	var binaryData []string
	var writeErr error
	created := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var res map[string]any
		_ = json.Unmarshal(body, &res)
		mu.Lock()
		defer mu.Unlock()
		if res["resourceType"] == "Binary" {
			data, _ := res["data"].(string)
			binaryData = append(binaryData, data)
			if !created {
				created = true
				writeErr = os.WriteFile(secondPath, []byte("id\nwritten-by-server\n"), 0o644)
			}
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	_, err := send.Send(context.Background(), send.Request{
		JobID:  "job-lazy",
		Config: transferSendConfig(server.URL),
		Retry:  FastRetryConfig(),
		Files: []send.File{
			{Path: firstPath, Name: "first.csv"},
			{Path: secondPath, Name: "second.csv"},
		},
		State: transferState(dir),
	}, lib.NewLogger(lib.LogLevelError))
	require.NoError(t, writeErr)
	require.NoError(t, err)

	require.Len(t, binaryData, 2)
	data, decErr := base64.StdEncoding.DecodeString(binaryData[1])
	require.NoError(t, decErr)
	zr, zipErr := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	require.NoError(t, zipErr)
	require.Len(t, zr.File, 1)
	rc, openErr := zr.File[0].Open()
	require.NoError(t, openErr)
	content, readErr := io.ReadAll(rc)
	require.NoError(t, readErr)
	require.NoError(t, rc.Close())
	assert.Equal(t, "id\nwritten-by-server\n", string(content))
}

func TestSend_TransferLoad_DocumentReferenceIsLastAndLinksBinariesInFileOrder(t *testing.T) {
	server := newTransferServer(t)
	req := transferRequest(t, server.URL, "a.csv", "b.csv", "c.csv")

	_, err := send.Send(context.Background(), req, lib.NewLogger(lib.LogLevelError))
	require.NoError(t, err)

	assert.Equal(t, []string{"Binary", "Binary", "Binary", "DocumentReference"}, server.requestKinds())
	binaryIDs := server.binaryIDs()
	require.Len(t, binaryIDs, 3)
	urls := server.documentReferenceAttachmentURLs(t)
	require.Len(t, urls, len(binaryIDs))
	for i, url := range urls {
		assert.Equal(t, "Binary/"+binaryIDs[i], url)
	}
	for _, c := range server.documentReferences[0]["content"].([]any) {
		attachment := c.(map[string]any)["attachment"].(map[string]any)
		assert.Equal(t, "application/zip", attachment["contentType"])
	}
}
