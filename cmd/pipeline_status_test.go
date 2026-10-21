package cmd

import (
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

func TestFormatStepLine_CompletedStepShowsFilesAndBytes(t *testing.T) {
	step := models.PipelineStep{
		Name: models.StepLocalImport, Status: models.StepStatusCompleted,
		FilesProcessed: 3, BytesProcessed: 2048,
	}

	line := formatStepLine(step)

	assert.Contains(t, line, "(3 files, "+formatBytes(2048)+")")
}

func TestFormatStepLine_ZeroBytesOmitsSize(t *testing.T) {
	step := models.PipelineStep{Name: models.StepDIMP, Status: models.StepStatusCompleted, FilesProcessed: 1}

	assert.Contains(t, formatStepLine(step), "(1 files)")
}

func TestFormatStepLine_PendingStepShowsNoCounts(t *testing.T) {
	step := models.PipelineStep{Name: models.StepSend, Status: models.StepStatusPending}

	assert.NotContains(t, formatStepLine(step), "files")
}

func TestFormatStepLine_FailedStepShowsError(t *testing.T) {
	step := models.PipelineStep{
		Name: models.StepSend, Status: models.StepStatusFailed,
		LastError: &models.StepError{Message: "boom"},
	}

	assert.Contains(t, formatStepLine(step), "\n    Error: boom")
}

func TestRunPipelineStatus_ShowsFixedDurationOfCompletedJob(t *testing.T) {
	configPath, jobsDir := writeStartConfig(t, "pipeline:\n  enabled_steps: [local_import]\n")
	updated := time.Now().Add(-2 * time.Hour)
	job := &models.PipelineJob{
		JobID: "6f1c2d3e-1111-4222-8333-444455556666", Status: models.JobStatusCompleted, InputSource: "/data", InputType: models.InputTypeLocal, CurrentStep: "local_import",
		CreatedAt: updated.Add(-90 * time.Minute), UpdatedAt: updated,
		Steps: []models.PipelineStep{{Name: models.StepLocalImport, Status: models.StepStatusCompleted}},
	}
	require.NoError(t, services.SaveJobState(jobsDir, job))

	var runErr error
	out := captureStdout(t, func() {
		runErr = runPipelineStatus(nil, []string{configPath, "6f1c2d3e-1111-4222-8333-444455556666"})
	})

	require.NoError(t, runErr)
	assert.Contains(t, out, "Duration: 1h30m0s")
	assert.Contains(t, out, "Steps:")
}

// The persisted status of a job whose process died stays in_progress. The
// duration must use the effective status, else it grows to about 10h.
func TestRunPipelineStatus_DeadProcessShowsFixedDuration(t *testing.T) {
	configPath, jobsDir := writeStartConfig(t, "pipeline:\n  enabled_steps: [local_import]\n")
	created := time.Now().Add(-10 * time.Hour)
	job := &models.PipelineJob{
		JobID: "6f1c2d3e-1111-4222-8333-444455556667", Status: models.JobStatusInProgress,
		InputSource: "/data", InputType: models.InputTypeLocal, CurrentStep: "local_import",
		CreatedAt: created, UpdatedAt: created.Add(90 * time.Minute),
		Steps: []models.PipelineStep{{Name: models.StepLocalImport, Status: models.StepStatusInProgress}},
	}
	require.NoError(t, services.SaveJobState(jobsDir, job))

	var runErr error
	out := captureStdout(t, func() {
		runErr = runPipelineStatus(nil, []string{configPath, job.JobID})
	})

	require.NoError(t, runErr)
	assert.Contains(t, out, "stopped (process died)")
	assert.Contains(t, out, "Duration: 1h30m0s")
}

func TestRunPipelineStatus_UnknownJobFails(t *testing.T) {
	configPath, _ := writeStartConfig(t, "pipeline:\n  enabled_steps: [local_import]\n")

	err := runPipelineStatus(nil, []string{configPath, "missing"})

	assert.ErrorContains(t, err, "failed to load job")
}

func TestRunPipelineStatus_MissingConfigFails(t *testing.T) {
	err := runPipelineStatus(nil, []string{filepath.Join(t.TempDir(), "none.yaml"), "x"})

	assert.ErrorContains(t, err, "failed to load configuration")
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { _ = r.Close() })

	// Read while fn writes, so that output larger than the pipe buffer does
	// not block fn.
	done := make(chan []byte)
	go func() {
		data, _ := io.ReadAll(r)
		done <- data
	}()

	os.Stdout = w
	func() {
		defer func() {
			os.Stdout = old
			_ = w.Close()
		}()
		fn()
	}()
	return string(<-done)
}
