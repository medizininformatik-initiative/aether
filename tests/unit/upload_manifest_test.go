package unit

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/models"
)

func TestUploadManifest_NewAndTrack(t *testing.T) {
	m := models.NewUploadManifest("job-123", "my-bucket")

	assert.Equal(t, "job-123", m.JobID)
	assert.Equal(t, "my-bucket", m.Bucket)
	assert.NotZero(t, m.StartedAt)
	assert.Nil(t, m.CompletedAt)
	assert.Empty(t, m.Files)

	// File should not be uploaded yet
	assert.False(t, m.IsFileUploaded("/path/to/file.ndjson"))

	// Add a file
	m.AddUploadedFile("/path/to/file.ndjson", "job-123/file.ndjson", "\"abc123\"", 1024)
	assert.True(t, m.IsFileUploaded("/path/to/file.ndjson"))
	assert.False(t, m.IsFileUploaded("/path/to/other.ndjson"))
	assert.Len(t, m.Files, 1)
	assert.Equal(t, "\"abc123\"", m.Files[0].ETag)
	assert.Equal(t, int64(1024), m.Files[0].SizeBytes)

	// Mark completed
	m.MarkCompleted()
	assert.NotNil(t, m.CompletedAt)
}

func TestUploadManifest_SaveAndLoad(t *testing.T) {
	tmpDir := t.TempDir()
	manifestPath := filepath.Join(tmpDir, "upload_manifest.json")

	m := models.NewUploadManifest("job-456", "test-bucket")
	m.AddUploadedFile("/data/Patient.ndjson", "job-456/Patient.ndjson", "\"etag1\"", 2048)
	m.AddUploadedFile("/data/Observation.ndjson", "job-456/Observation.ndjson", "\"etag2\"", 4096)

	// Save
	err := models.SaveManifest(m, manifestPath)
	require.NoError(t, err)

	// Verify file exists
	_, err = os.Stat(manifestPath)
	require.NoError(t, err)

	// Load
	loaded, err := models.LoadUploadManifest(manifestPath)
	require.NoError(t, err)
	require.NotNil(t, loaded)

	assert.Equal(t, "job-456", loaded.JobID)
	assert.Equal(t, "test-bucket", loaded.Bucket)
	assert.Len(t, loaded.Files, 2)
	assert.True(t, loaded.IsFileUploaded("/data/Patient.ndjson"))
	assert.True(t, loaded.IsFileUploaded("/data/Observation.ndjson"))
}

func TestUploadManifest_LoadNonExistent(t *testing.T) {
	tmpDir := t.TempDir()
	manifestPath := filepath.Join(tmpDir, "nonexistent.json")

	loaded, err := models.LoadUploadManifest(manifestPath)
	assert.NoError(t, err)
	assert.Nil(t, loaded)
}

func TestUploadManifest_LoadInvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	manifestPath := filepath.Join(tmpDir, "bad.json")
	require.NoError(t, os.WriteFile(manifestPath, []byte("{invalid"), 0644))

	loaded, err := models.LoadUploadManifest(manifestPath)
	assert.Error(t, err)
	assert.Nil(t, loaded)
	assert.Contains(t, err.Error(), "failed to parse manifest")
}

func TestGetUploadManifestPath(t *testing.T) {
	path := models.GetUploadManifestPath("/jobs/job-123")
	assert.Equal(t, "/jobs/job-123/upload_manifest.json", path)
}

func TestUploadManifest_AtomicWrite(t *testing.T) {
	tmpDir := t.TempDir()
	manifestPath := filepath.Join(tmpDir, "upload_manifest.json")

	m := models.NewUploadManifest("job-atomic", "bucket")
	m.AddUploadedFile("/data/file.ndjson", "job-atomic/file.ndjson", "\"e1\"", 100)

	require.NoError(t, models.SaveManifest(m, manifestPath))

	// Temp file should not remain
	_, err := os.Stat(manifestPath + ".tmp")
	assert.True(t, os.IsNotExist(err), "temp file should be cleaned up after rename")

	// Manifest should be loadable
	loaded, err := models.LoadUploadManifest(manifestPath)
	require.NoError(t, err)
	require.NotNil(t, loaded)
	assert.Equal(t, "job-atomic", loaded.JobID)
}
