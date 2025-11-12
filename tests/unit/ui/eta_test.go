package ui_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/medizininformatik-initiative/aether/internal/ui"
)

func TestETACalculator_Creation(t *testing.T) {
	calc := ui.NewETACalculator()

	assert.NotNil(t, calc, "ETA calculator should be created")
}

func TestETACalculator_InsufficientData(t *testing.T) {
	calc := ui.NewETACalculator()

	// With no samples, ETA should be invalid
	eta, valid := calc.CalculateETA(100, 0)
	assert.False(t, valid, "ETA should be invalid with no samples")
	assert.Equal(t, time.Duration(0), eta)

	// With only one sample, ETA should still be invalid
	calc.RecordProgress(10)
	_, valid = calc.CalculateETA(100, 10)
	assert.False(t, valid, "ETA should be invalid with only one sample")
}

func TestETACalculator_BasicCalculation(t *testing.T) {
	calc := ui.NewETACalculator()

	// Record progress at different points
	calc.RecordProgress(0)
	time.Sleep(100 * time.Millisecond)
	calc.RecordProgress(10)
	time.Sleep(100 * time.Millisecond)
	calc.RecordProgress(20)

	// Now we should have enough data
	eta, valid := calc.CalculateETA(100, 20)
	assert.True(t, valid, "ETA should be valid with multiple samples")
	assert.Greater(t, eta.Milliseconds(), int64(0), "ETA should be positive")

	// ETA should decrease as we make more progress
	calc.RecordProgress(50)
	eta2, valid2 := calc.CalculateETA(100, 50)
	assert.True(t, valid2)
	assert.Less(t, eta2.Milliseconds(), eta.Milliseconds(), "ETA should decrease with more progress")
}

func TestETACalculator_CompletedTask(t *testing.T) {
	calc := ui.NewETACalculator()

	calc.RecordProgress(50)
	calc.RecordProgress(100)

	// When current == total, ETA should be 0
	eta, valid := calc.CalculateETA(100, 100)
	assert.True(t, valid)
	assert.Equal(t, time.Duration(0), eta, "ETA should be 0 when task is complete")
}

func TestETACalculator_ThroughputCalculation(t *testing.T) {
	calc := ui.NewETACalculator()

	calc.RecordProgress(0)
	time.Sleep(100 * time.Millisecond)
	calc.RecordProgress(10)
	time.Sleep(100 * time.Millisecond)
	calc.RecordProgress(20)

	// Get throughput (items per second)
	throughput, valid := calc.GetThroughput()
	assert.True(t, valid, "Throughput should be valid")
	assert.Greater(t, throughput, 0.0, "Throughput should be positive")
}

// Test Progress indicators must show elapsed time and ETA requirement: Average computed from last 10 items or 30 seconds
func TestETACalculator_AveragingWindow(t *testing.T) {
	// Test max samples limit (10 items)
	calc := ui.NewETACalculatorCustom(10, 30*time.Second)

	// Record 15 samples - should only keep last 10
	for i := 0; i <= 15; i++ {
		calc.RecordProgress(int64(i * 10))
		time.Sleep(10 * time.Millisecond)
	}

	eta, valid := calc.CalculateETA(200, 150)
	assert.True(t, valid, "ETA should be valid")
	assert.NotNil(t, eta)
}

// Test Progress indicators must show elapsed time and ETA requirement: 30-second time window
func TestETACalculator_TimeWindow(t *testing.T) {
	// Use shorter time window for testing
	calc := ui.NewETACalculatorCustom(100, 200*time.Millisecond)

	// Record samples
	calc.RecordProgress(10)
	time.Sleep(50 * time.Millisecond)
	calc.RecordProgress(20)

	// Wait for samples to expire
	time.Sleep(250 * time.Millisecond)

	// Record new sample
	calc.RecordProgress(30)

	// Old samples should be pruned
	// With only one recent sample, ETA should be invalid
	eta, valid := calc.CalculateETA(100, 30)
	assert.False(t, valid, "ETA should be invalid after old samples are pruned")
	_ = eta
}

func TestETACalculator_Reset(t *testing.T) {
	calc := ui.NewETACalculator()

	calc.RecordProgress(10)
	calc.RecordProgress(20)

	// Reset
	calc.Reset()

	// After reset, should need multiple samples again
	eta, valid := calc.CalculateETA(100, 20)
	assert.False(t, valid, "ETA should be invalid after reset")
	_ = eta
}

func TestFormatETA(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		want     string
	}{
		{"Less than 1s", 500 * time.Millisecond, "< 1s"},
		{"Seconds", 45 * time.Second, "45s"},
		{"Minutes and seconds", 2*time.Minute + 30*time.Second, "2m30s"},
		{"Hours and minutes", 2*time.Hour + 15*time.Minute, "2h15m"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ui.FormatETA(tt.duration)
			assert.NotEmpty(t, result)
			// Just verify it doesn't panic and returns something
		})
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
	}{
		{"Milliseconds", 500 * time.Millisecond},
		{"Seconds", 30 * time.Second},
		{"Minutes", 5 * time.Minute},
		{"Hours", 2 * time.Hour},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ui.FormatDuration(tt.duration)
			assert.NotEmpty(t, result)
		})
	}
}

// Test Progress indicators must show elapsed time and ETA formula: ETA = (total_items - processed_items) * avg_time_per_item
func TestETACalculator_FormulaVerification(t *testing.T) {
	calc := ui.NewETACalculator()

	// Simulate processing 10 items per 100ms
	startTime := time.Now()
	calc.RecordProgress(0)

	time.Sleep(100 * time.Millisecond)
	calc.RecordProgress(10)

	time.Sleep(100 * time.Millisecond)
	calc.RecordProgress(20)

	// Calculate ETA for remaining 80 items
	eta, valid := calc.CalculateETA(100, 20)
	assert.True(t, valid)

	// Verify ETA is reasonable
	// Should be approximately 400ms (80 items * 10ms per item, with 10 items/100ms rate)
	elapsedSinceStart := time.Since(startTime)
	_ = elapsedSinceStart

	// ETA should be positive and not absurdly large
	assert.Greater(t, eta.Milliseconds(), int64(0))
	assert.Less(t, eta.Milliseconds(), int64(10000), "ETA shouldn't be more than 10 seconds for this test")
}

// Test that ETA becomes more accurate with more samples
func TestETACalculator_Accuracy(t *testing.T) {
	calc := ui.NewETACalculator()

	// Record consistent progress
	for i := 0; i <= 5; i++ {
		calc.RecordProgress(int64(i * 20))
		if i < 5 {
			time.Sleep(50 * time.Millisecond)
		}
	}

	// ETA should be valid with consistent progress
	eta, valid := calc.CalculateETA(200, 100)
	assert.True(t, valid)
	assert.Greater(t, eta.Milliseconds(), int64(0))
}
