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
		{"kilobytes", 2 * kb, "2.00 KB/sec"},
		{"megabytes", 5 * mb, "5.00 MB/sec"},
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
		{"kilobytes", 2 * kb, "2.00 KB"},
		{"megabytes", 3 * mb, "3.00 MB"},
		{"gigabytes", 4 * gb, "4.00 GB"},
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
