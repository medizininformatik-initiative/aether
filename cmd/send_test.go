package cmd

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/send"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

// runSendCommand runs `aether send` through the root command.
func runSendCommand(t *testing.T, args ...string) error {
	t.Helper()
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	rootCmd.SetArgs(append([]string{"send"}, args...))
	t.Cleanup(func() {
		rootCmd.SetArgs(nil)
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
		sendJobID = ""
	})
	return rootCmd.Execute()
}

func transferConfig(t *testing.T, serverURL string) (configPath, jobsDir string) {
	t.Helper()
	return writeStartConfig(t, fmt.Sprintf(`
services:
  send:
    send_as: transfer_load
    url: %q
    transfer:
      project_identifier: TEST-PROJECT
      organization_identifier: test.hospital.de
retry:
  max_attempts: 1
`, serverURL))
}

// recordingServer records the Binary and DocumentReference resources it gets.
type recordingServer struct {
	*httptest.Server
	mu        sync.Mutex
	resources []map[string]any
}

func newRecordingServer(t *testing.T) *recordingServer {
	t.Helper()
	rs := &recordingServer{}
	rs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var res map[string]any
		_ = json.Unmarshal(body, &res)
		rs.mu.Lock()
		rs.resources = append(rs.resources, res)
		rs.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(rs.Close)
	return rs
}

func (rs *recordingServer) count() int {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return len(rs.resources)
}

// zipEntries returns the entry name and content of each sent Binary.
func (rs *recordingServer) zipEntries(t *testing.T) map[string]string {
	t.Helper()
	rs.mu.Lock()
	defer rs.mu.Unlock()
	entries := map[string]string{}
	for _, res := range rs.resources {
		if res["resourceType"] != "Binary" {
			continue
		}
		data, err := base64.StdEncoding.DecodeString(res["data"].(string))
		require.NoError(t, err)
		zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		require.NoError(t, err)
		require.Len(t, zr.File, 1)
		rc, err := zr.File[0].Open()
		require.NoError(t, err)
		content, err := io.ReadAll(rc)
		require.NoError(t, err)
		require.NoError(t, rc.Close())
		entries[zr.File[0].Name] = string(content)
	}
	return entries
}

func writeCSVTree(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range map[string]string{
		"a.csv":     "id\n1\n",
		"sub/b.CSV": "id\n2\n",
		"notes.txt": "not sent",
	} {
		path := filepath.Join(dir, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
	return dir
}

// treeSnapshot maps each relative path below dir to its content.
func treeSnapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	snap := map[string]string{}
	require.NoError(t, filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		data, readErr := os.ReadFile(path)
		snap[rel] = string(data)
		return readErr
	}))
	return snap
}

func onlyJobID(t *testing.T, jobsDir string) string {
	t.Helper()
	entries, err := os.ReadDir(jobsDir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	return entries[0].Name()
}

func TestSendCommand_WrongArgCountIsUsageError(t *testing.T) {
	for _, args := range [][]string{{}, {"aether.yaml"}, {"a", "b", "c"}} {
		err := runSendCommand(t, args...)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "usage: aether send <config> <dir>")
	}
}

func TestSendCommand_RejectsDirectResourceLoadBeforeCreatingJob(t *testing.T) {
	configPath, jobsDir := writeStartConfig(t, `
services:
  send:
    send_as: direct_resource_load
    url: "http://localhost:1"
`)

	err := runSendCommand(t, configPath, writeCSVTree(t))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "direct_resource_load")
	assertNoJobs(t, jobsDir)
}

func TestSendCommand_RejectsInvalidSendConfig(t *testing.T) {
	configPath, jobsDir := writeStartConfig(t, `
services:
  send:
    send_as: transfer_load
`)

	err := runSendCommand(t, configPath, writeCSVTree(t))

	require.Error(t, err)
	assertNoJobs(t, jobsDir)
}

func TestSendCommand_RejectsMissingDirectory(t *testing.T) {
	server := newRecordingServer(t)
	configPath, jobsDir := transferConfig(t, server.URL)

	err := runSendCommand(t, configPath, filepath.Join(t.TempDir(), "absent"))

	require.Error(t, err)
	assertNoJobs(t, jobsDir)
	assert.Zero(t, server.count())
}

func TestSendCommand_TransferLoadSendsOnlyCSVFilesAndLeavesDirUntouched(t *testing.T) {
	server := newRecordingServer(t)
	configPath, jobsDir := transferConfig(t, server.URL)
	dir := writeCSVTree(t)
	before := treeSnapshot(t, dir)

	require.NoError(t, runSendCommand(t, configPath, dir))

	assert.Equal(t, map[string]string{"a.csv": "id\n1\n", "sub/b.CSV": "id\n2\n"}, server.zipEntries(t))
	assert.Equal(t, before, treeSnapshot(t, dir))

	jobID := onlyJobID(t, jobsDir)
	stored, err := send.LoadSource(filepath.Join(services.GetJobDir(jobsDir, jobID), "source.json"))
	require.NoError(t, err)
	assert.True(t, stored.Completed)
	assert.Len(t, stored.Files, 2)
	assert.NoFileExists(t, services.GetStateFilePath(jobsDir, jobID))
}

func TestSendCommand_S3UploadUsesJobIDAsKeyPrefix(t *testing.T) {
	mock := &services.MockS3Uploader{Bucket: "test-bucket"}
	send.SetS3UploaderFactoryForTesting(func(models.S3Config, models.AuthConfig, models.TLSConfig, *lib.Logger) (services.S3Uploader, error) {
		return mock, nil
	})
	t.Cleanup(send.ResetS3UploaderFactory)
	configPath, jobsDir := writeStartConfig(t, `
services:
  send:
    send_as: s3_upload
    s3:
      region: eu-central-1
      bucket: test-bucket
      access_key_id: AKIAIOSFODNN7EXAMPLE
      secret_access_key: wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY
      timeout: 30m
`)

	require.NoError(t, runSendCommand(t, configPath, writeCSVTree(t)))

	jobID := onlyJobID(t, jobsDir)
	assert.Equal(t, []string{jobID + "/a.csv", jobID + "/sub/b.CSV"}, mock.UploadedKeys)
	assert.FileExists(t, models.GetUploadManifestPath(services.GetJobDir(jobsDir, jobID)))
}

func TestSendCommand_ResumeAfterSourceChangeFailsAndSendsNothing(t *testing.T) {
	server := newRecordingServer(t)
	configPath, jobsDir := transferConfig(t, server.URL)
	dir := writeCSVTree(t)
	require.NoError(t, runSendCommand(t, configPath, dir))
	jobID := onlyJobID(t, jobsDir)

	// Mark the job as not completed, as after a failed send.
	sourcePath := filepath.Join(services.GetJobDir(jobsDir, jobID), "source.json")
	stored, err := send.LoadSource(sourcePath)
	require.NoError(t, err)
	stored.Completed = false
	require.NoError(t, send.SaveSource(sourcePath, stored))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.csv"), []byte("id\n9\n"), 0o644))
	sent := server.count()

	err = runSendCommand(t, configPath, dir, "--job", jobID)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "source directory changed since job "+jobID+" started")
	assert.Contains(t, err.Error(), "a.csv")
	assert.Equal(t, sent, server.count())
}

func TestSendCommand_ResumeCompletedJobSendsNothing(t *testing.T) {
	server := newRecordingServer(t)
	configPath, jobsDir := transferConfig(t, server.URL)
	dir := writeCSVTree(t)
	require.NoError(t, runSendCommand(t, configPath, dir))
	jobID := onlyJobID(t, jobsDir)
	sent := server.count()

	require.NoError(t, runSendCommand(t, configPath, dir, "--job", jobID))

	assert.Equal(t, sent, server.count())
}

func TestSendCommand_ResumeUnfinishedJobSendsAgainUnderSameJob(t *testing.T) {
	server := newRecordingServer(t)
	configPath, jobsDir := transferConfig(t, server.URL)
	dir := writeCSVTree(t)
	require.NoError(t, runSendCommand(t, configPath, dir))
	jobID := onlyJobID(t, jobsDir)
	sourcePath := filepath.Join(services.GetJobDir(jobsDir, jobID), "source.json")
	stored, err := send.LoadSource(sourcePath)
	require.NoError(t, err)
	stored.Completed = false
	require.NoError(t, send.SaveSource(sourcePath, stored))
	sent := server.count()

	require.NoError(t, runSendCommand(t, configPath, dir, "--job", jobID))

	assert.Greater(t, server.count(), sent)
	reloaded, err := send.LoadSource(sourcePath)
	require.NoError(t, err)
	assert.True(t, reloaded.Completed)
	assert.Equal(t, jobID, onlyJobID(t, jobsDir))
}

func TestSendCommand_ResumeUnknownJobIsError(t *testing.T) {
	server := newRecordingServer(t)
	configPath, _ := transferConfig(t, server.URL)

	err := runSendCommand(t, configPath, writeCSVTree(t), "--job", "20250101_1200_3f2a9c1e-7b4d-4e8a-9c6f-1d2e3f4a5b6c")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot resume job 20250101_1200_3f2a9c1e-7b4d-4e8a-9c6f-1d2e3f4a5b6c")
	assert.Zero(t, server.count())
}

func TestSendCommand_ServerFailureReportsResumeCommand(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)
	configPath, jobsDir := transferConfig(t, server.URL)

	err := runSendCommand(t, configPath, writeCSVTree(t))

	require.Error(t, err)
	jobID := onlyJobID(t, jobsDir)
	assert.Contains(t, err.Error(), "send failed")
	assert.Contains(t, err.Error(), "--job "+jobID)
	stored, loadErr := send.LoadSource(filepath.Join(services.GetJobDir(jobsDir, jobID), "source.json"))
	require.NoError(t, loadErr)
	assert.False(t, stored.Completed)
}

func TestSendCommand_ResumeCommandUsesQuotedAbsolutePaths(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)
	configPath, jobsDir := transferConfig(t, server.URL)
	base := t.TempDir()
	writeSendFileAt(t, filepath.Join(base, "my csv", "a.csv"))
	require.NoError(t, os.WriteFile(filepath.Join(base, "aether.yaml"), mustRead(t, configPath), 0o644))
	t.Chdir(base)

	err := runSendCommand(t, "aether.yaml", "my csv")

	require.Error(t, err)
	jobID := onlyJobID(t, jobsDir)
	want := fmt.Sprintf("aether send '%s' '%s' --job %s",
		filepath.Join(base, "aether.yaml"), filepath.Join(base, "my csv"), jobID)
	assert.Contains(t, err.Error(), want)
}

func writeSendFileAt(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("id\n1\n"), 0o644))
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return data
}

func TestSendCommand_ResumeRejectsJobIDWithPathSeparator(t *testing.T) {
	server := newRecordingServer(t)
	configPath, _ := transferConfig(t, server.URL)

	err := runSendCommand(t, configPath, writeCSVTree(t), "--job", "../escape")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid job ID")
	assert.Zero(t, server.count())
}

func TestSendCommand_ResumeRejectsDotJobIDs(t *testing.T) {
	for _, jobID := range []string{"..", "."} {
		t.Run(jobID, func(t *testing.T) {
			server := newRecordingServer(t)
			configPath, _ := transferConfig(t, server.URL)

			err := runSendCommand(t, configPath, writeCSVTree(t), "--job", jobID)

			require.Error(t, err)
			assert.Contains(t, err.Error(), "invalid job ID")
			assert.Zero(t, server.count())
		})
	}
}

func TestSendCommand_ResumeChecksCompletionAfterLock(t *testing.T) {
	server := newRecordingServer(t)
	configPath, jobsDir := transferConfig(t, server.URL)
	dir := writeCSVTree(t)
	files, err := send.FindCSVFiles(dir)
	require.NoError(t, err)
	snapshot, err := send.SnapshotSource(dir, files)
	require.NoError(t, err)
	jobID := models.GenerateJobID()
	sourcePath := filepath.Join(services.GetJobDir(jobsDir, jobID), "source.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(sourcePath), 0o755))
	require.NoError(t, send.SaveSource(sourcePath, snapshot))

	lock, err := services.AcquireJobLock(jobsDir, jobID, lib.NewLogger(lib.LogLevelError))
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- runSendCommand(t, configPath, dir, "--job", jobID) }()
	time.Sleep(30 * time.Millisecond)
	snapshot.Completed = true
	require.NoError(t, send.SaveSource(sourcePath, snapshot))
	require.NoError(t, lock.Release())

	require.NoError(t, <-done)
	assert.Zero(t, server.count())
}

func TestSendCommand_InvalidRetryIsConfigError(t *testing.T) {
	server := newRecordingServer(t)
	configPath, jobsDir := writeStartConfig(t, fmt.Sprintf(`
services:
  send:
    send_as: transfer_load
    url: %q
    transfer:
      project_identifier: TEST-PROJECT
      organization_identifier: test.hospital.de
retry:
  max_attempts: 0
`, server.URL))

	err := runSendCommand(t, configPath, writeCSVTree(t))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "max_attempts must be between 1 and 10")
	assertNoJobs(t, jobsDir)
	assert.Zero(t, server.count())
}

func TestSendCommand_UnusableJobsDirIsConfigError(t *testing.T) {
	server := newRecordingServer(t)
	blocker := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o644))
	configPath := filepath.Join(t.TempDir(), "aether.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(fmt.Sprintf(`
jobs_dir: %q
services:
  send:
    send_as: transfer_load
    url: %q
    transfer:
      project_identifier: TEST-PROJECT
      organization_identifier: test.hospital.de
`, filepath.Join(blocker, "jobs"), server.URL)), 0o644))

	err := runSendCommand(t, configPath, writeCSVTree(t))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "jobs directory")
	assert.Zero(t, server.count())
}

func TestSendCommand_MissingConfigIsError(t *testing.T) {
	err := runSendCommand(t, filepath.Join(t.TempDir(), "absent.yaml"), writeCSVTree(t))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load configuration")
}

func assertNoJobs(t *testing.T, jobsDir string) {
	t.Helper()
	entries, err := os.ReadDir(jobsDir)
	if os.IsNotExist(err) {
		return
	}
	require.NoError(t, err)
	assert.Empty(t, entries)
}
