package models

import (
	"path/filepath"
	"time"
)

// TransferManifest records which files of a transfer_load send are on the server.
type TransferManifest struct {
	JobID       string               `json:"job_id"`
	StartedAt   time.Time            `json:"started_at"`
	CompletedAt *time.Time           `json:"completed_at,omitempty"`
	Files       []TransferFileRecord `json:"files"`
}

// TransferFileRecord is one file whose Binary is on the server. Name is the
// name that the Binary id derives from.
type TransferFileRecord struct {
	Name       string    `json:"name"`
	UploadedAt time.Time `json:"uploaded_at"`
}

// NewTransferManifest creates an empty manifest for a job.
func NewTransferManifest(jobID string) *TransferManifest {
	return &TransferManifest{
		JobID:     jobID,
		StartedAt: time.Now(),
		Files:     []TransferFileRecord{},
	}
}

func (m *TransferManifest) index(name string) int {
	for i, f := range m.Files {
		if f.Name == name {
			return i
		}
	}
	return -1
}

// IsUploaded reports whether the Binary of the file is on the server.
func (m *TransferManifest) IsUploaded(name string) bool {
	return m.index(name) >= 0
}

// MarkUploaded records that the Binary of the file is on the server.
func (m *TransferManifest) MarkUploaded(name string) {
	if m.IsUploaded(name) {
		return
	}
	m.Files = append(m.Files, TransferFileRecord{Name: name, UploadedAt: time.Now()})
}

// MarkCompleted records that the send is complete.
func (m *TransferManifest) MarkCompleted() {
	now := time.Now()
	m.CompletedAt = &now
}

// SaveTransferManifest writes the manifest to disk with an atomic write.
func SaveTransferManifest(manifest *TransferManifest, path string) error {
	return writeJSONAtomic(manifest, path)
}

// LoadTransferManifest loads a manifest from disk.
// Returns nil (no error) if the file does not exist.
func LoadTransferManifest(path string) (*TransferManifest, error) {
	return readJSONIfExists[TransferManifest](path)
}

// GetTransferManifestPath returns the path to the transfer manifest file for a job.
func GetTransferManifestPath(jobDir string) string {
	return filepath.Join(jobDir, "transfer_manifest.json")
}
