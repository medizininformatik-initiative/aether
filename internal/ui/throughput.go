package ui

import (
	"fmt"
	"time"
)

// ThroughputCalculator tracks and calculates data processing rates
// Displays throughput as items/sec and MB/sec for user feedback
type ThroughputCalculator struct {
	startTime        time.Time
	totalItems       int64
	totalBytes       int64
	lastUpdateTime   time.Time
	lastUpdateItems  int64
	lastUpdateBytes  int64
	instantItemsRate float64
	instantBytesRate float64
}

// NewThroughputCalculator creates a new throughput calculator that starts now
func NewThroughputCalculator() *ThroughputCalculator {
	return NewThroughputCalculatorAt(time.Now())
}

// NewThroughputCalculatorAt creates a throughput calculator that starts at the given time
func NewThroughputCalculatorAt(now time.Time) *ThroughputCalculator {
	return &ThroughputCalculator{
		startTime:        now,
		totalItems:       0,
		totalBytes:       0,
		lastUpdateTime:   now,
		lastUpdateItems:  0,
		lastUpdateBytes:  0,
		instantItemsRate: 0,
		instantBytesRate: 0,
	}
}

// Update records progress now and recalculates throughput rates
func (t *ThroughputCalculator) Update(items int64, bytes int64) {
	t.UpdateAt(items, bytes, time.Now())
}

// UpdateAt records progress at the given time and recalculates throughput rates
func (t *ThroughputCalculator) UpdateAt(items int64, bytes int64, now time.Time) {
	// Calculate instantaneous rates (since last update)
	timeSinceLastUpdate := now.Sub(t.lastUpdateTime).Seconds()
	if timeSinceLastUpdate > 0 {
		itemsDelta := items - t.lastUpdateItems
		bytesDelta := bytes - t.lastUpdateBytes

		t.instantItemsRate = float64(itemsDelta) / timeSinceLastUpdate
		t.instantBytesRate = float64(bytesDelta) / timeSinceLastUpdate
	}

	// Update totals
	t.totalItems = items
	t.totalBytes = bytes
	t.lastUpdateTime = now
	t.lastUpdateItems = items
	t.lastUpdateBytes = bytes
}

// GetAverageItemsPerSecond returns overall average items per second up to now
func (t *ThroughputCalculator) GetAverageItemsPerSecond() float64 {
	return t.GetAverageItemsPerSecondAt(time.Now())
}

// GetAverageItemsPerSecondAt returns overall average items per second up to the given time
func (t *ThroughputCalculator) GetAverageItemsPerSecondAt(now time.Time) float64 {
	elapsed := now.Sub(t.startTime).Seconds()
	if elapsed <= 0 {
		return 0
	}
	return float64(t.totalItems) / elapsed
}

// GetAverageBytesPerSecond returns overall average bytes per second up to now
func (t *ThroughputCalculator) GetAverageBytesPerSecond() float64 {
	return t.GetAverageBytesPerSecondAt(time.Now())
}

// GetAverageBytesPerSecondAt returns overall average bytes per second up to the given time
func (t *ThroughputCalculator) GetAverageBytesPerSecondAt(now time.Time) float64 {
	elapsed := now.Sub(t.startTime).Seconds()
	if elapsed <= 0 {
		return 0
	}
	return float64(t.totalBytes) / elapsed
}

// GetInstantItemsPerSecond returns current items per second (since last update)
func (t *ThroughputCalculator) GetInstantItemsPerSecond() float64 {
	return t.instantItemsRate
}

// GetInstantBytesPerSecond returns current bytes per second (since last update)
func (t *ThroughputCalculator) GetInstantBytesPerSecond() float64 {
	return t.instantBytesRate
}

// FormatItemsPerSecond formats items/sec rate as human-readable string
// Example: "2.3 items/sec"
func FormatItemsPerSecond(itemsPerSec float64) string {
	if itemsPerSec < 0.01 {
		return "< 0.01 items/sec"
	}
	return fmt.Sprintf("%.2f items/sec", itemsPerSec)
}

// FormatBytesPerSecond formats bytes/sec rate as human-readable string
// Example: "5.2 MB/sec"
func FormatBytesPerSecond(bytesPerSec float64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
	)

	if bytesPerSec >= GB {
		return fmt.Sprintf("%.2f GB/sec", bytesPerSec/GB)
	} else if bytesPerSec >= MB {
		return fmt.Sprintf("%.2f MB/sec", bytesPerSec/MB)
	} else if bytesPerSec >= KB {
		return fmt.Sprintf("%.2f KB/sec", bytesPerSec/KB)
	}
	return fmt.Sprintf("%.0f B/sec", bytesPerSec)
}

// FormatBytes formats bytes as human-readable size
func FormatBytes(bytes int64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
		TB = 1024 * GB
	)

	fbytes := float64(bytes)

	if bytes >= TB {
		return fmt.Sprintf("%.2f TB", fbytes/TB)
	} else if bytes >= GB {
		return fmt.Sprintf("%.2f GB", fbytes/GB)
	} else if bytes >= MB {
		return fmt.Sprintf("%.2f MB", fbytes/MB)
	} else if bytes >= KB {
		return fmt.Sprintf("%.2f KB", fbytes/KB)
	}
	return fmt.Sprintf("%d B", bytes)
}

// Reset resets the throughput calculator
func (t *ThroughputCalculator) Reset() {
	now := time.Now()
	t.startTime = now
	t.totalItems = 0
	t.totalBytes = 0
	t.lastUpdateTime = now
	t.lastUpdateItems = 0
	t.lastUpdateBytes = 0
	t.instantItemsRate = 0
	t.instantBytesRate = 0
}

// GetElapsedTime returns time since the calculator was created or reset
func (t *ThroughputCalculator) GetElapsedTime() time.Duration {
	return time.Since(t.startTime)
}

// Summary returns a formatted summary of throughput metrics
func (t *ThroughputCalculator) Summary() string {
	avgItemsRate := t.GetAverageItemsPerSecond()
	avgBytesRate := t.GetAverageBytesPerSecond()
	elapsed := t.GetElapsedTime()

	return fmt.Sprintf(
		"%d items (%s) in %s | Avg: %s, %s",
		t.totalItems,
		FormatBytes(t.totalBytes),
		FormatDuration(elapsed),
		FormatItemsPerSecond(avgItemsRate),
		FormatBytesPerSecond(avgBytesRate),
	)
}
