package unit

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/medizininformatik-initiative/aether/internal/models"
)

// TestPipelineStep_Validate tests validation of PipelineStep struct
func TestPipelineStep_Validate(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name    string
		step    models.PipelineStep
		wantErr bool
		errMsg  string
	}{
		{
			name: "Valid pending step",
			step: models.PipelineStep{
				Name:   models.StepLocalImport,
				Status: models.StepStatusPending,
			},
			wantErr: false,
		},
		{
			name: "Valid in_progress step",
			step: models.PipelineStep{
				Name:      models.StepLocalImport,
				Status:    models.StepStatusInProgress,
				StartedAt: &now,
			},
			wantErr: false,
		},
		{
			name: "Valid completed step",
			step: models.PipelineStep{
				Name:           models.StepLocalImport,
				Status:         models.StepStatusCompleted,
				StartedAt:      &now,
				CompletedAt:    &now,
				FilesProcessed: 10,
				BytesProcessed: 1024,
			},
			wantErr: false,
		},
		{
			name: "Invalid step name",
			step: models.PipelineStep{
				Name:   models.StepName("invalid_step"),
				Status: models.StepStatusPending,
			},
			wantErr: true,
			errMsg:  "invalid step name",
		},
		{
			name: "Invalid step status",
			step: models.PipelineStep{
				Name:   models.StepLocalImport,
				Status: models.StepStatus("invalid_status"),
			},
			wantErr: true,
			errMsg:  "invalid step status",
		},
		{
			name: "Negative retry count",
			step: models.PipelineStep{
				Name:       models.StepLocalImport,
				Status:     models.StepStatusPending,
				RetryCount: -1,
			},
			wantErr: true,
			errMsg:  "retry_count cannot be negative",
		},
		{
			name: "Negative files processed",
			step: models.PipelineStep{
				Name:           models.StepLocalImport,
				Status:         models.StepStatusPending,
				FilesProcessed: -1,
			},
			wantErr: true,
			errMsg:  "files_processed cannot be negative",
		},
		{
			name: "Negative bytes processed",
			step: models.PipelineStep{
				Name:           models.StepLocalImport,
				Status:         models.StepStatusPending,
				BytesProcessed: -1,
			},
			wantErr: true,
			errMsg:  "bytes_processed cannot be negative",
		},
		{
			name: "In progress without started_at",
			step: models.PipelineStep{
				Name:   models.StepLocalImport,
				Status: models.StepStatusInProgress,
			},
			wantErr: true,
			errMsg:  "started_at must be set when step is in_progress or completed",
		},
		{
			name: "Completed without started_at",
			step: models.PipelineStep{
				Name:        models.StepLocalImport,
				Status:      models.StepStatusCompleted,
				CompletedAt: &now,
			},
			wantErr: true,
			errMsg:  "started_at must be set when step is in_progress or completed",
		},
		{
			name: "Completed without completed_at",
			step: models.PipelineStep{
				Name:      models.StepLocalImport,
				Status:    models.StepStatusCompleted,
				StartedAt: &now,
			},
			wantErr: true,
			errMsg:  "completed_at must be set when step is completed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.step.Validate()

			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestFHIRDataFile_Validate tests validation of FHIRDataFile struct
func TestFHIRDataFile_Validate(t *testing.T) {
	tests := []struct {
		name    string
		file    models.FHIRDataFile
		wantErr bool
		errMsg  string
	}{
		{
			name: "Valid NDJSON file",
			file: models.FHIRDataFile{
				FileName:     "Patient.ndjson",
				FilePath:     "safe/path/Patient.ndjson",
				FileSize:     1024,
				LineCount:    100,
				ResourceType: "Patient",
				SourceStep:   models.StepLocalImport,
			},
			wantErr: false,
		},
		{
			name: "Invalid file extension",
			file: models.FHIRDataFile{
				FileName:   "Patient.json",
				FilePath:   "safe/path/Patient.json",
				FileSize:   1024,
				SourceStep: models.StepLocalImport,
			},
			wantErr: true,
			errMsg:  "file_name must end with .ndjson",
		},
		{
			name: "Unsafe file path (path traversal)",
			file: models.FHIRDataFile{
				FileName:   "Patient.ndjson",
				FilePath:   "../../../etc/passwd",
				FileSize:   1024,
				SourceStep: models.StepLocalImport,
			},
			wantErr: true,
			errMsg:  "unsafe file_path detected",
		},
		{
			name: "Zero file size",
			file: models.FHIRDataFile{
				FileName:   "Patient.ndjson",
				FilePath:   "safe/path/Patient.ndjson",
				FileSize:   0,
				SourceStep: models.StepLocalImport,
			},
			wantErr: true,
			errMsg:  "file_size must be greater than 0",
		},
		{
			name: "Negative file size",
			file: models.FHIRDataFile{
				FileName:   "Patient.ndjson",
				FilePath:   "safe/path/Patient.ndjson",
				FileSize:   -1,
				SourceStep: models.StepLocalImport,
			},
			wantErr: true,
			errMsg:  "file_size must be greater than 0",
		},
		{
			name: "Negative line count",
			file: models.FHIRDataFile{
				FileName:   "Patient.ndjson",
				FilePath:   "safe/path/Patient.ndjson",
				FileSize:   1024,
				LineCount:  -1,
				SourceStep: models.StepLocalImport,
			},
			wantErr: true,
			errMsg:  "line_count cannot be negative",
		},
		{
			name: "Invalid source step",
			file: models.FHIRDataFile{
				FileName:   "Patient.ndjson",
				FilePath:   "safe/path/Patient.ndjson",
				FileSize:   1024,
				SourceStep: models.StepName("invalid_step"),
			},
			wantErr: true,
			errMsg:  "invalid source_step",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.file.Validate()

			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
