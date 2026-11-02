package unit

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/models"
)

func validJobForValidation() models.PipelineJob {
	return models.PipelineJob{
		JobID:       models.GenerateJobID(),
		InputSource: "/data/in",
		InputType:   models.InputTypeLocal,
		Status:      models.JobStatusPending,
		Steps:       []models.PipelineStep{{Name: models.StepLocalImport}},
		CurrentStep: string(models.StepLocalImport),
	}
}

func TestPipelineJob_Validate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(j *models.PipelineJob)
		wantErr string
	}{
		{"valid job", func(j *models.PipelineJob) {}, ""},
		{"empty job id", func(j *models.PipelineJob) { j.JobID = "" }, "job_id is required"},
		{"empty source", func(j *models.PipelineJob) { j.InputSource = "" }, "input_source is required"},
		{"empty source with crtdl", func(j *models.PipelineJob) {
			j.InputSource = ""
			j.CRTDLPath = "x.json"
		}, ""},
		{"empty source with local_import dir", func(j *models.PipelineJob) {
			j.InputSource = ""
			j.Config.Services.LocalImport.Dir = "/d"
		}, ""},
		{"empty source with csv_import dir", func(j *models.PipelineJob) {
			j.InputSource = ""
			j.Config.Services.CSVImport.Dir = "/d"
		}, ""},
		{"http source without scheme", func(j *models.PipelineJob) {
			j.InputType = models.InputTypeHTTP
			j.InputSource = "ftp://host/x"
		}, "valid HTTP(S) URL"},
		{"http source with bad url", func(j *models.PipelineJob) {
			j.InputType = models.InputTypeHTTP
			j.InputSource = "http://host:bad/x"
		}, "invalid input_source URL"},
		{"http source valid", func(j *models.PipelineJob) {
			j.InputType = models.InputTypeHTTP
			j.InputSource = "https://host/x"
		}, ""},
		{"unknown input type", func(j *models.PipelineJob) { j.InputType = "nope" }, "invalid input_type"},
		{"unknown status", func(j *models.PipelineJob) { j.Status = "nope" }, "invalid status"},
		{"current step not in steps", func(j *models.PipelineJob) { j.CurrentStep = "dimp" }, "not found in job steps"},
		{"negative files", func(j *models.PipelineJob) { j.TotalFiles = -1 }, "total_files cannot be negative"},
		{"negative bytes", func(j *models.PipelineJob) { j.TotalBytes = -1 }, "total_bytes cannot be negative"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := validJobForValidation()
			tt.mutate(&job)

			err := job.Validate()

			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}
