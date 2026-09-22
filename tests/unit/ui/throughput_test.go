package ui_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/medizininformatik-initiative/aether/internal/ui"
)

func TestFormatItemsPerSecond(t *testing.T) {
	tests := []struct {
		name string
		rate float64
		want string
	}{
		{"below floor rounds to placeholder", 0.005, "< 0.01 items/sec"},
		{"zero rounds to placeholder", 0, "< 0.01 items/sec"},
		{"at floor is printed", 0.01, "0.01 items/sec"},
		{"typical rate", 2.345, "2.35 items/sec"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ui.FormatItemsPerSecond(tt.rate))
		})
	}
}

func TestFormatBytesPerSecond(t *testing.T) {
	const (
		kb = 1024
		mb = 1024 * kb
		gb = 1024 * mb
	)
	tests := []struct {
		name string
		rate float64
		want string
	}{
		{"bytes", 512, "512 B/sec"},
		{"below the kilobyte boundary", kb - 1, "1023 B/sec"},
		{"at the kilobyte boundary", kb, "1.00 KB/sec"},
		{"kilobytes", 2 * kb, "2.00 KB/sec"},
		{"below the megabyte boundary", mb - 1, "1024.00 KB/sec"},
		{"at the megabyte boundary", mb, "1.00 MB/sec"},
		{"megabytes", 5 * mb, "5.00 MB/sec"},
		{"below the gigabyte boundary", gb - 1, "1024.00 MB/sec"},
		{"at the gigabyte boundary", gb, "1.00 GB/sec"},
		{"gigabytes", 3 * gb, "3.00 GB/sec"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ui.FormatBytesPerSecond(tt.rate))
		})
	}
}

func TestFormatBytes(t *testing.T) {
	const (
		kb = 1024
		mb = 1024 * kb
		gb = 1024 * mb
		tb = 1024 * gb
	)
	tests := []struct {
		name  string
		bytes int64
		want  string
	}{
		{"bytes", 512, "512 B"},
		{"below the kilobyte boundary", kb - 1, "1023 B"},
		{"at the kilobyte boundary", kb, "1.00 KB"},
		{"kilobytes", 2 * kb, "2.00 KB"},
		{"below the megabyte boundary", mb - 1, "1024.00 KB"},
		{"at the megabyte boundary", mb, "1.00 MB"},
		{"megabytes", 3 * mb, "3.00 MB"},
		{"below the gigabyte boundary", gb - 1, "1024.00 MB"},
		{"at the gigabyte boundary", gb, "1.00 GB"},
		{"gigabytes", 4 * gb, "4.00 GB"},
		{"below the terabyte boundary", tb - 1, "1024.00 GB"},
		{"at the terabyte boundary", tb, "1.00 TB"},
		{"terabytes", 5 * tb, "5.00 TB"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ui.FormatBytes(tt.bytes))
		})
	}
}

func TestThroughputCalculator_UpdateComputesRates(t *testing.T) {
	calc := ui.NewThroughputCalculator()
	time.Sleep(5 * time.Millisecond)
	calc.Update(100, 4096)

	assert.Positive(t, calc.GetInstantItemsPerSecond(), "instant items rate is computed after update")
	assert.Positive(t, calc.GetInstantBytesPerSecond(), "instant bytes rate is computed after update")
	assert.Positive(t, calc.GetAverageItemsPerSecond(), "average items rate is computed after update")
	assert.Positive(t, calc.GetAverageBytesPerSecond(), "average bytes rate is computed after update")
	assert.Positive(t, calc.GetElapsedTime(), "elapsed time advances after update")
}

func TestThroughputCalculator_UpdateAtComputesInstantRatesFromDeltas(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	calc := ui.NewThroughputCalculatorAt(start)

	calc.UpdateAt(10, 1024, start.Add(500*time.Millisecond))
	assert.InDelta(t, 20.0, calc.GetInstantItemsPerSecond(), 1e-9, "10 items in 0.5s is 20 items/sec")
	assert.InDelta(t, 2048.0, calc.GetInstantBytesPerSecond(), 1e-9, "1024 bytes in 0.5s is 2048 bytes/sec")

	calc.UpdateAt(30, 4096, start.Add(time.Second))
	assert.InDelta(t, 40.0, calc.GetInstantItemsPerSecond(), 1e-9, "the rate uses the delta of 20 items, not the total")
	assert.InDelta(t, 6144.0, calc.GetInstantBytesPerSecond(), 1e-9, "the rate uses the delta of 3072 bytes, not the total")
}

func TestThroughputCalculator_UpdateAtKeepsRatesFiniteWhenNoTimePassed(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	calc := ui.NewThroughputCalculatorAt(start)

	calc.UpdateAt(10, 1024, start)

	assert.Zero(t, calc.GetInstantItemsPerSecond(), "an update at the start time gives no items rate")
	assert.Zero(t, calc.GetInstantBytesPerSecond(), "an update at the start time gives no bytes rate")
}

func TestThroughputCalculator_AverageAtDividesTotalsByElapsedTime(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	calc := ui.NewThroughputCalculatorAt(start)
	half := start.Add(500 * time.Millisecond)
	calc.UpdateAt(100, 8192, half)

	assert.InDelta(t, 200.0, calc.GetAverageItemsPerSecondAt(half), 1e-9, "100 items in 0.5s is 200 items/sec")
	assert.InDelta(t, 16384.0, calc.GetAverageBytesPerSecondAt(half), 1e-9, "8192 bytes in 0.5s is 16384 bytes/sec")
}

func TestThroughputCalculator_AverageAtIsZeroWhenNoTimePassed(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	calc := ui.NewThroughputCalculatorAt(start)
	calc.UpdateAt(100, 8192, start)

	assert.Zero(t, calc.GetAverageItemsPerSecondAt(start), "no elapsed time gives no average items rate")
	assert.Zero(t, calc.GetAverageBytesPerSecondAt(start), "no elapsed time gives no average bytes rate")
}

func TestThroughputCalculator_ResetClearsState(t *testing.T) {
	calc := ui.NewThroughputCalculator()
	time.Sleep(5 * time.Millisecond)
	calc.Update(100, 4096)
	calc.Reset()

	assert.Zero(t, calc.GetInstantItemsPerSecond(), "reset clears instant items rate")
	assert.Zero(t, calc.GetInstantBytesPerSecond(), "reset clears instant bytes rate")
	assert.Zero(t, calc.GetAverageItemsPerSecond(), "reset clears totals so average is zero")
	assert.Zero(t, calc.GetAverageBytesPerSecond(), "reset clears totals so average is zero")
}

func TestThroughputCalculator_Summary(t *testing.T) {
	calc := ui.NewThroughputCalculator()
	time.Sleep(5 * time.Millisecond)
	calc.Update(42, 1000)

	summary := calc.Summary()
	assert.Contains(t, summary, "42 items", "summary reports item count")
	assert.Contains(t, summary, "1000 B", "summary reports byte total")
	assert.Contains(t, summary, "Avg:", "summary reports averages")
}
