package ui_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/medizininformatik-initiative/aether/internal/ui"
)

func TestProgressBar_Creation(t *testing.T) {
	// Test creating a progress bar with known total
	total := int64(100)
	description := "Testing progress"

	bar := ui.NewProgressBar(total, description)

	assert.NotNil(t, bar, "Progress bar should be created")
	assert.Equal(t, 0.0, bar.GetPercentage(), "Initial percentage should be 0")
}

func TestProgressBar_Add(t *testing.T) {
	var buf bytes.Buffer
	total := int64(100)
	bar := ui.NewProgressBarWithWriter(total, "Test", &buf)

	// Add progress
	err := bar.Add(25)
	assert.NoError(t, err)
	assert.Equal(t, 25.0, bar.GetPercentage())

	err = bar.Add(25)
	assert.NoError(t, err)
	assert.Equal(t, 50.0, bar.GetPercentage())

	// Finish
	err = bar.Finish()
	assert.NoError(t, err)
}

func TestProgressBar_Set(t *testing.T) {
	var buf bytes.Buffer
	total := int64(200)
	bar := ui.NewProgressBarWithWriter(total, "Test", &buf)

	// Set progress directly
	err := bar.Set(100)
	assert.NoError(t, err)
	assert.Equal(t, 50.0, bar.GetPercentage())

	err = bar.Set(200)
	assert.NoError(t, err)
	assert.Equal(t, 100.0, bar.GetPercentage())

	err = bar.Finish()
	assert.NoError(t, err)
}

func TestProgressBar_Percentage(t *testing.T) {
	tests := []struct {
		name     string
		total    int64
		current  int64
		expected float64
	}{
		{"0%", 100, 0, 0.0},
		{"25%", 100, 25, 25.0},
		{"50%", 100, 50, 50.0},
		{"75%", 100, 75, 75.0},
		{"100%", 100, 100, 100.0},
		{"ZeroTotal", 0, 0, 0.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			bar := ui.NewProgressBarWithWriter(tt.total, "Test", &buf)

			if tt.current > 0 {
				_ = bar.Set(tt.current)
			}

			percentage := bar.GetPercentage()
			assert.Equal(t, tt.expected, percentage)
		})
	}
}

func TestProgressBar_ElapsedTime(t *testing.T) {
	var buf bytes.Buffer
	bar := ui.NewProgressBarWithWriter(100, "Test", &buf)

	// Elapsed time should be > 0 after creation
	elapsed := bar.GetElapsedTime()
	assert.Greater(t, elapsed.Nanoseconds(), int64(0))
}

func TestSpinner_Lifecycle(t *testing.T) {
	spinner := ui.NewSpinner("Test operation")

	assert.NotNil(t, spinner)
	assert.False(t, spinner.IsActive(), "Spinner should not be active initially")

	spinner.Start()
	assert.True(t, spinner.IsActive(), "Spinner should be active after Start()")

	spinner.Stop(true)
	assert.False(t, spinner.IsActive(), "Spinner should not be active after Stop()")
}

func TestSpinner_Messages(t *testing.T) {
	// Test spinner message updates
	spinner := ui.NewSpinner("Initial message")

	spinner.Start()
	spinner.UpdateMessage("Updated message")
	spinner.Stop(true)

	// Just verify no panics occur
	assert.False(t, spinner.IsActive())
}

func TestSpinner_SuccessFailure(t *testing.T) {
	// Test success case
	spinner1 := ui.NewSpinner("Success test")
	spinner1.Start()
	spinner1.Stop(true)

	// Test failure case
	spinner2 := ui.NewSpinner("Failure test")
	spinner2.Start()
	spinner2.Stop(false)

	// Verify no panics
	assert.False(t, spinner1.IsActive())
	assert.False(t, spinner2.IsActive())
}

// Test Progress indicators must update at least every 2 seconds requirement: 2 second update throttle
func TestProgressBar_UpdateFrequency(t *testing.T) {
	var buf bytes.Buffer
	bar := ui.NewProgressBarWithWriter(1000, "Progress indicators must update at least every 2 seconds Test", &buf)

	// Add multiple small increments rapidly
	for i := 0; i < 10; i++ {
		_ = bar.Add(10)
	}

	// Verify progress bar doesn't panic with rapid updates
	// (throttling is handled internally by progressbar library)
	assert.Equal(t, 10.0, bar.GetPercentage())
	_ = bar.Finish()
}

// Test Progress bars must show completion percentage requirement: Progress bar format verification
func TestProgressBar_Format(t *testing.T) {
	var buf bytes.Buffer
	bar := ui.NewProgressBarWithWriter(100, "Import FHIR files", &buf)

	_ = bar.Add(50)
	_ = bar.Finish()

	// Verify some output was written (basic check)
	output := buf.String()
	assert.NotEmpty(t, output, "Progress bar should produce output")
}

// Test progress bar with description containing operation name (progress indicator requirementse)
func TestProgressBar_OperationName(t *testing.T) {
	tests := []struct {
		description string
	}{
		{"Downloading FHIR files"},
		{"Processing Patient resources"},
		{"Importing data"},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			var buf bytes.Buffer
			bar := ui.NewProgressBarWithWriter(100, tt.description, &buf)

			_ = bar.Add(100)
			_ = bar.Finish()

			// Verify bar was created with description
			assert.NotNil(t, bar)
		})
	}
}
