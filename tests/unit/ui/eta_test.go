package ui_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/ui"
)

// sampleBase is a fixed start time for the tests that record samples at known moments.
var sampleBase = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func TestETACalculator_ExactETAFromRecordedRate(t *testing.T) {
	calc := ui.NewETACalculatorCustom(10, time.Hour)

	// 10 items in 10 seconds gives 1 second per item.
	calc.RecordProgressAt(5, sampleBase)
	calc.RecordProgressAt(15, sampleBase.Add(10*time.Second))

	// 105 - 15 = 90 items remain, so the ETA is 90 seconds.
	eta, valid := calc.CalculateETA(105, 15)
	require.True(t, valid)
	assert.Equal(t, 90*time.Second, eta)
}

func TestETACalculator_DefaultTimeWindowIs30Seconds(t *testing.T) {
	t.Run("keeps a sample 29 seconds old", func(t *testing.T) {
		calc := ui.NewETACalculator()

		calc.RecordProgressAt(5, sampleBase)
		calc.RecordProgressAt(15, sampleBase.Add(29*time.Second))

		// 10 items in 29 seconds, 90 items remain.
		eta, valid := calc.CalculateETA(105, 15)
		require.True(t, valid, "a sample inside the 30 second window stays")
		assert.Equal(t, 90*2900*time.Millisecond, eta)
	})

	t.Run("drops a sample 31 seconds old", func(t *testing.T) {
		calc := ui.NewETACalculator()

		calc.RecordProgressAt(5, sampleBase)
		calc.RecordProgressAt(15, sampleBase.Add(31*time.Second))

		_, valid := calc.CalculateETA(105, 15)
		assert.False(t, valid, "only one sample stays inside the 30 second window")
	})
}

// Test Progress indicators must show elapsed time and ETA requirement: Average computed from last 10 items or 30 seconds
func TestETACalculator_KeepsExactlyMaxSamples(t *testing.T) {
	calc := ui.NewETACalculatorCustom(3, time.Hour)

	calc.RecordProgressAt(10, sampleBase)
	calc.RecordProgressAt(20, sampleBase.Add(time.Second))
	calc.RecordProgressAt(30, sampleBase.Add(20*time.Second))

	// All three samples stay. The rate is 20 items in 20 seconds.
	// A drop of the oldest sample gives a different rate: 10 items in 19 seconds.
	eta, valid := calc.CalculateETA(130, 30)
	require.True(t, valid)
	assert.Equal(t, 100*time.Second, eta)
}

func TestETACalculator_NonPositiveTimeWindowKeepsNoSample(t *testing.T) {
	calc := ui.NewETACalculatorCustom(10, 0)

	calc.RecordProgressAt(10, sampleBase)
	calc.RecordProgressAt(20, sampleBase.Add(time.Second))

	_, valid := calc.CalculateETA(100, 20)
	assert.False(t, valid, "a window of zero accepts no sample")
}

func TestETACalculator_CompleteWithoutProgress(t *testing.T) {
	calc := ui.NewETACalculatorCustom(10, time.Hour)

	// Two samples with the same item count give no rate.
	calc.RecordProgressAt(100, sampleBase)
	calc.RecordProgressAt(100, sampleBase.Add(time.Second))

	eta, valid := calc.CalculateETA(100, 100)
	assert.True(t, valid, "a complete task reports an ETA of zero even without a rate")
	assert.Equal(t, time.Duration(0), eta)
}

func TestETACalculator_RejectsSamplesWithoutDelta(t *testing.T) {
	t.Run("no item delta", func(t *testing.T) {
		calc := ui.NewETACalculatorCustom(10, time.Hour)

		calc.RecordProgressAt(10, sampleBase)
		calc.RecordProgressAt(10, sampleBase.Add(time.Second))

		throughput, valid := calc.GetThroughput()
		assert.False(t, valid, "equal item counts give no throughput")
		assert.Equal(t, 0.0, throughput)
	})

	t.Run("no time delta", func(t *testing.T) {
		calc := ui.NewETACalculatorCustom(10, time.Hour)

		calc.RecordProgressAt(10, sampleBase)
		calc.RecordProgressAt(20, sampleBase)

		throughput, valid := calc.GetThroughput()
		assert.False(t, valid, "equal timestamps give no throughput")
		assert.Equal(t, 0.0, throughput)
	})
}

func TestETACalculator_ExactThroughput(t *testing.T) {
	calc := ui.NewETACalculatorCustom(10, time.Hour)

	// 5 items in 10 seconds gives 2 seconds per item, so 0.5 items per second.
	calc.RecordProgressAt(5, sampleBase)
	calc.RecordProgressAt(10, sampleBase.Add(10*time.Second))

	throughput, valid := calc.GetThroughput()
	require.True(t, valid)
	assert.Equal(t, 0.5, throughput)
}

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
		{"Just below 1s", 999 * time.Millisecond, "< 1s"},
		{"Exactly 1s", time.Second, "1s"},
		{"Seconds", 45 * time.Second, "45s"},
		{"Just below 1m", 59 * time.Second, "59s"},
		{"Exactly 1m", time.Minute, "1m0s"},
		{"Minutes and seconds", 2*time.Minute + 30*time.Second, "2m30s"},
		{"Just below 1h", 59*time.Minute + 59*time.Second, "59m59s"},
		{"Exactly 1h", time.Hour, "1h0m"},
		{"Hours and minutes", 2*time.Hour + 15*time.Minute, "2h15m"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ui.FormatETA(tt.duration))
		})
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		want     string
	}{
		{"Milliseconds", 500 * time.Millisecond, "500ms"},
		{"Just below 1s", 999 * time.Millisecond, "999ms"},
		{"Exactly 1s", time.Second, "1s"},
		{"Seconds", 30 * time.Second, "30s"},
		{"Minutes", 5 * time.Minute, "5m0s"},
		{"Hours", 2 * time.Hour, "2h0m0s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ui.FormatDuration(tt.duration))
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
