package unit

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/models"
)

func TestTransferManifest_LoadMissingFileReturnsNil(t *testing.T) {
	loaded, err := models.LoadTransferManifest(filepath.Join(t.TempDir(), "missing.json"))
	require.NoError(t, err)
	assert.Nil(t, loaded)
}

func TestTransferManifest_LoadCorruptFileReturnsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transfer_manifest.json")
	require.NoError(t, os.WriteFile(path, []byte("{not json"), 0o644))

	loaded, err := models.LoadTransferManifest(path)
	require.Error(t, err)
	assert.Nil(t, loaded)
}

func TestTransferManifest_TracksUploadedFiles(t *testing.T) {
	m := models.NewTransferManifest("job-1", models.TransferTarget{URL: "https://dsf.example"})

	assert.Equal(t, "job-1", m.JobID)
	assert.NotZero(t, m.StartedAt)
	assert.Nil(t, m.CompletedAt)
	assert.False(t, m.IsUploaded("a.csv", "h1"))

	m.MarkUploaded("a.csv", "h1")
	assert.True(t, m.IsUploaded("a.csv", "h1"))
	assert.False(t, m.IsUploaded("a.csv", "h2"), "other content is not on the server")
	assert.False(t, m.IsUploaded("b.csv", "h1"))

	m.MarkUploaded("a.csv", "h2")
	assert.True(t, m.IsUploaded("a.csv", "h2"))
	assert.False(t, m.IsUploaded("a.csv", "h1"), "the new upload replaces the old content")
	assert.Len(t, m.Files, 1)

	m.MarkCompleted()
	assert.NotNil(t, m.CompletedAt)
}

func TestTransferManifest_SaveAndLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transfer_manifest.json")

	m := models.NewTransferManifest("job-1", models.TransferTarget{URL: "https://dsf.example"})
	m.MarkUploaded("a.csv", "h1")
	m.MarkUploaded("sub/b.csv", "h2")
	m.MarkCompleted()

	require.NoError(t, models.SaveTransferManifest(m, path))
	assert.NoFileExists(t, path+".tmp")

	loaded, err := models.LoadTransferManifest(path)
	require.NoError(t, err)
	require.NotNil(t, loaded)

	assert.Equal(t, "job-1", loaded.JobID)
	assert.True(t, loaded.IsFor("job-1", models.TransferTarget{URL: "https://dsf.example"}))
	assert.NotNil(t, loaded.CompletedAt)
	assert.True(t, loaded.IsUploaded("a.csv", "h1"))
	assert.True(t, loaded.IsUploaded("sub/b.csv", "h2"))
	assert.False(t, loaded.IsUploaded("c.csv", "h1"))
}

func TestTransferManifest_IsForComparesJobAndTarget(t *testing.T) {
	target := models.TransferTarget{URL: "https://dsf.example", ProjectIdentifier: "p1"}
	m := models.NewTransferManifest("job-1", target)

	assert.True(t, m.IsFor("job-1", target))
	assert.False(t, m.IsFor("job-2", target))
	assert.False(t, m.IsFor("job-1", models.TransferTarget{URL: "https://other.example", ProjectIdentifier: "p1"}))
	assert.False(t, m.IsFor("job-1", models.TransferTarget{URL: "https://dsf.example", ProjectIdentifier: "p2"}))
}

func TestTransferManifest_LoadedWithoutFilesRecordsUploads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transfer_manifest.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"job_id":"job-1","files":null}`), 0o644))

	loaded, err := models.LoadTransferManifest(path)
	require.NoError(t, err)
	loaded.MarkUploaded("a.csv", "h1")

	assert.True(t, loaded.IsUploaded("a.csv", "h1"))
}

func TestGetTransferManifestPath(t *testing.T) {
	assert.Equal(t, "/jobs/job-123/transfer_manifest.json", models.GetTransferManifestPath("/jobs/job-123"))
}
