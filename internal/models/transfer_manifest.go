package models

import (
	"path/filepath"
	"time"
)

// TransferManifest records which files of a transfer_load send are on the
// server. Files maps the name that a Binary id derives from to its record.
type TransferManifest struct {
	JobID       string                        `json:"job_id"`
	Target      TransferTarget                `json:"target"`
	StartedAt   time.Time                     `json:"started_at"`
	CompletedAt *time.Time                    `json:"completed_at,omitempty"`
	Files       map[string]TransferFileRecord `json:"files"`
}

// TransferTarget identifies where the Binaries of a transfer_load send go.
type TransferTarget struct {
	URL               string `json:"url"`
	ProjectIdentifier string `json:"project_identifier"`
}

// TransferFileRecord is one file whose Binary is on the server. SHA256 is the
// hash of the file content at upload.
type TransferFileRecord struct {
	SHA256     string    `json:"sha256"`
	UploadedAt time.Time `json:"uploaded_at"`
}

// NewTransferManifest creates an empty manifest for a send of the job to the
// target.
func NewTransferManifest(jobID string, target TransferTarget) *TransferManifest {
	return &TransferManifest{
		JobID:     jobID,
		Target:    target,
		StartedAt: time.Now(),
		Files:     map[string]TransferFileRecord{},
	}
}

// IsFor reports whether the manifest records a send of the job to the target.
// The records of another job or target say nothing about the Binaries there.
func (m *TransferManifest) IsFor(jobID string, target TransferTarget) bool {
	return m.JobID == jobID && m.Target == target
}

// HasRecord reports whether the manifest records an upload of the file.
func (m *TransferManifest) HasRecord(name string) bool {
	_, ok := m.Files[name]
	return ok
}

// IsUploaded reports whether the Binary of the file is on the server with
// the content that has this hash.
func (m *TransferManifest) IsUploaded(name, sha256 string) bool {
	record, ok := m.Files[name]
	return ok && record.SHA256 == sha256
}

// MarkUploaded records that the Binary of the file is on the server with the
// content that has this hash.
func (m *TransferManifest) MarkUploaded(name, sha256 string) {
	if m.Files == nil {
		m.Files = map[string]TransferFileRecord{}
	}
	m.Files[name] = TransferFileRecord{SHA256: sha256, UploadedAt: time.Now()}
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
