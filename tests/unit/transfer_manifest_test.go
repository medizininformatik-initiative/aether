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
	m := models.NewTransferManifest("job-1")

	assert.Equal(t, "job-1", m.JobID)
	assert.NotZero(t, m.StartedAt)
	assert.Nil(t, m.CompletedAt)
	assert.False(t, m.IsUploaded("a.csv"))

	m.MarkUploaded("a.csv")
	m.MarkUploaded("a.csv")

	assert.True(t, m.IsUploaded("a.csv"))
	assert.False(t, m.IsUploaded("b.csv"))
	assert.Len(t, m.Files, 1)

	m.MarkCompleted()
	assert.NotNil(t, m.CompletedAt)
}

func TestTransferManifest_SaveAndLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transfer_manifest.json")

	m := models.NewTransferManifest("job-1")
	m.MarkUploaded("a.csv")
	m.MarkUploaded("sub/b.csv")
	m.MarkCompleted()

	require.NoError(t, models.SaveTransferManifest(m, path))
	assert.NoFileExists(t, path+".tmp")

	loaded, err := models.LoadTransferManifest(path)
	require.NoError(t, err)
	require.NotNil(t, loaded)

	assert.Equal(t, "job-1", loaded.JobID)
	assert.NotNil(t, loaded.CompletedAt)
	assert.True(t, loaded.IsUploaded("a.csv"))
	assert.True(t, loaded.IsUploaded("sub/b.csv"))
	assert.False(t, loaded.IsUploaded("c.csv"))
}

func TestGetTransferManifestPath(t *testing.T) {
	assert.Equal(t, "/jobs/job-123/transfer_manifest.json", models.GetTransferManifestPath("/jobs/job-123"))
}
