package unit

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

// TestJobLayout_OutputDir verifies each step resolves to its canonical output directory.
func TestJobLayout_OutputDir(t *testing.T) {
	layout := services.NewJobLayout("/tmp/jobs", "job-1", nil)

	cases := []struct {
		step models.StepName
		want string
	}{
		{models.StepTorchImport, "/tmp/jobs/job-1/import"},
		{models.StepLocalImport, "/tmp/jobs/job-1/import"},
		{models.StepHttpImport, "/tmp/jobs/job-1/import"},
		{models.StepDIMP, "/tmp/jobs/job-1/dimp"},
		{models.StepValidation, "/tmp/jobs/job-1/validation"},
		{models.StepFlattening, "/tmp/jobs/job-1/csv"},
		{models.StepSend, "/tmp/jobs/job-1/send"},
		{models.StepWait, "/tmp/jobs/job-1"},
	}
	for _, c := range cases {
		t.Run(string(c.step), func(t *testing.T) {
			assert.Equal(t, c.want, layout.OutputDir(c.step))
		})
	}
}

// TestJobLayout_InputDir_NoWait verifies input resolution skips transparent steps
// (validation) and falls back to the job dir when no producer precedes the step.
func TestJobLayout_InputDir_NoWait(t *testing.T) {
	cases := []struct {
		name  string
		steps []models.StepName
		step  models.StepName
		want  string
	}{
		{"first step reads job dir", []models.StepName{models.StepLocalImport, models.StepDIMP}, models.StepLocalImport, "/tmp/jobs/job-1"},
		{"dimp reads import", []models.StepName{models.StepLocalImport, models.StepDIMP}, models.StepDIMP, "/tmp/jobs/job-1/import"},
		{"skips validation", []models.StepName{models.StepLocalImport, models.StepValidation, models.StepDIMP}, models.StepDIMP, "/tmp/jobs/job-1/import"},
		{"no producer falls back to job dir", []models.StepName{models.StepValidation, models.StepDIMP}, models.StepDIMP, "/tmp/jobs/job-1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			layout := services.NewJobLayout("/tmp/jobs", "job-1", c.steps)
			assert.Equal(t, c.want, layout.InputDir(c.step))
		})
	}
}

// TestJobLayout_ViewDefinitionsDir verifies the flattening view-definitions dir.
func TestJobLayout_ViewDefinitionsDir(t *testing.T) {
	layout := services.NewJobLayout("/tmp/jobs", "job-1", nil)
	assert.Equal(t, "/tmp/jobs/job-1/viewdefinitions", layout.ViewDefinitionsDir())
}

// TestJobLayout_WaitDir verifies the wait override dir for a wait step at a given
// index, and that an invalid placement (first step / no producer) errors.
func TestJobLayout_WaitDir(t *testing.T) {
	t.Run("wait after import", func(t *testing.T) {
		layout := services.NewJobLayout("/tmp/jobs", "job-1", []models.StepName{models.StepLocalImport, models.StepWait, models.StepDIMP})
		dir, err := layout.WaitDir(1)
		assert.NoError(t, err)
		assert.Equal(t, "/tmp/jobs/job-1/import_wait", dir)
	})
	t.Run("wait after validation", func(t *testing.T) {
		layout := services.NewJobLayout("/tmp/jobs", "job-1", []models.StepName{models.StepLocalImport, models.StepValidation, models.StepWait, models.StepDIMP})
		dir, err := layout.WaitDir(2)
		assert.NoError(t, err)
		assert.Equal(t, "/tmp/jobs/job-1/validation_wait", dir)
	})
	t.Run("wait as first step errors", func(t *testing.T) {
		layout := services.NewJobLayout("/tmp/jobs", "job-1", []models.StepName{models.StepWait, models.StepDIMP})
		_, err := layout.WaitDir(0)
		assert.Error(t, err)
	})
}

// TestJobLayout_InputDir_Wait verifies a wait override is named after the step it
// immediately follows, so the reader matches the path the paused pipeline tells
// the user to populate — including when that step is transparent (validation).
func TestJobLayout_InputDir_Wait(t *testing.T) {
	cases := []struct {
		name  string
		steps []models.StepName
		step  models.StepName
		want  string
	}{
		{"wait after import", []models.StepName{models.StepLocalImport, models.StepWait, models.StepDIMP}, models.StepDIMP, "/tmp/jobs/job-1/import_wait"},
		{"wait after validation", []models.StepName{models.StepLocalImport, models.StepValidation, models.StepWait, models.StepDIMP}, models.StepDIMP, "/tmp/jobs/job-1/validation_wait"},
		{"transparent step after wait", []models.StepName{models.StepLocalImport, models.StepWait, models.StepValidation, models.StepFlattening}, models.StepFlattening, "/tmp/jobs/job-1/import_wait"},
		{"degenerate leading waits fall back", []models.StepName{models.StepWait, models.StepWait, models.StepDIMP}, models.StepDIMP, "/tmp/jobs/job-1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			layout := services.NewJobLayout("/tmp/jobs", "job-1", c.steps)
			assert.Equal(t, c.want, layout.InputDir(c.step))
		})
	}
}
