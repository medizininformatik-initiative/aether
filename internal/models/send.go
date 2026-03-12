package models

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// UploadManifest tracks the progress of an S3 upload job for retry resilience.
// Saved to disk after each successful file upload so that interrupted uploads
// can resume without re-uploading already-completed files.
type UploadManifest struct {
	JobID       string               `json:"job_id"`
	Bucket      string               `json:"bucket"`
	StartedAt   time.Time            `json:"started_at"`
	CompletedAt *time.Time           `json:"completed_at,omitempty"`
	Files       []UploadedFileRecord `json:"files"`
}

// UploadedFileRecord tracks a single file that has been successfully uploaded.
type UploadedFileRecord struct {
	LocalPath  string    `json:"local_path"`
	S3Key      string    `json:"s3_key"`
	ETag       string    `json:"etag"`
	SizeBytes  int64     `json:"size_bytes"`
	UploadedAt time.Time `json:"uploaded_at"`
}

// NewUploadManifest creates a new manifest for tracking uploads.
func NewUploadManifest(jobID, bucket string) *UploadManifest {
	return &UploadManifest{
		JobID:     jobID,
		Bucket:    bucket,
		StartedAt: time.Now(),
		Files:     []UploadedFileRecord{},
	}
}

// IsFileUploaded checks whether a file (by local path) has already been uploaded.
func (m *UploadManifest) IsFileUploaded(localPath string) bool {
	for _, f := range m.Files {
		if f.LocalPath == localPath {
			return true
		}
	}
	return false
}

// AddUploadedFile records a successfully uploaded file.
func (m *UploadManifest) AddUploadedFile(localPath, s3Key, etag string, sizeBytes int64) {
	m.Files = append(m.Files, UploadedFileRecord{
		LocalPath:  localPath,
		S3Key:      s3Key,
		ETag:       etag,
		SizeBytes:  sizeBytes,
		UploadedAt: time.Now(),
	})
}

// MarkCompleted marks the manifest as fully complete.
func (m *UploadManifest) MarkCompleted() {
	now := time.Now()
	m.CompletedAt = &now
}

// SaveManifest writes the manifest to disk using atomic write (temp file + rename).
func SaveManifest(manifest *UploadManifest, manifestPath string) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal manifest: %w", err)
	}

	// Atomic write: write to temp file, then rename
	tmpPath := manifestPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write manifest temp file: %w", err)
	}

	if err := os.Rename(tmpPath, manifestPath); err != nil {
		return fmt.Errorf("failed to rename manifest file: %w", err)
	}

	return nil
}

// LoadUploadManifest loads an existing manifest from disk.
// Returns nil (no error) if the file does not exist.
func LoadUploadManifest(manifestPath string) (*UploadManifest, error) {
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read manifest: %w", err)
	}

	var manifest UploadManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse manifest: %w", err)
	}

	return &manifest, nil
}

// GetUploadManifestPath returns the path to the upload manifest file for a job.
func GetUploadManifestPath(jobDir string) string {
	return filepath.Join(jobDir, "upload_manifest.json")
}
