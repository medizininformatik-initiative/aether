package ui

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/schollz/progressbar/v3"
)

// ProgressBar wraps the progressbar library to provide progress visualization
// with percentage, ETA, and throughput for user feedback
type ProgressBar struct {
	bar         *progressbar.ProgressBar
	description string
	total       int64
	current     int64
	startTime   time.Time
}

// NewProgressBar creates a progress bar for operations with known total size
// Progress bars provide visual feedback with completion percentage and throughput
// Updates every 500ms to provide timely feedback to users
func NewProgressBar(total int64, description string) *ProgressBar {
	bar := progressbar.NewOptions64(
		total,
		progressbar.OptionSetDescription(description),
		progressbar.OptionShowCount(),
		progressbar.OptionSetWidth(40),
		progressbar.OptionThrottle(500*time.Millisecond), // Update every 500ms
		progressbar.OptionShowIts(),                      // Show items per second (throughput)
		progressbar.OptionSetWriter(os.Stderr),           // Write to stderr (unbuffered)
		progressbar.OptionSetRenderBlankState(true),
		progressbar.OptionEnableColorCodes(false), // Disable colors for better compatibility
	)

	return &ProgressBar{
		bar:         bar,
		description: description,
		total:       total,
		current:     0,
		startTime:   time.Now(),
	}
}

// NewProgressBarWithWriter creates a progress bar that writes to a specific writer
// Useful for testing with mock writers
func NewProgressBarWithWriter(total int64, description string, writer io.Writer) *ProgressBar {
	bar := progressbar.NewOptions64(
		total,
		progressbar.OptionSetDescription(description),
		progressbar.OptionShowCount(),
		progressbar.OptionSetWidth(40),
		progressbar.OptionThrottle(500*time.Millisecond),
		progressbar.OptionShowIts(),
		progressbar.OptionSetWriter(writer),
		progressbar.OptionEnableColorCodes(false),
	)

	return &ProgressBar{
		bar:         bar,
		description: description,
		total:       total,
		current:     0,
		startTime:   time.Now(),
	}
}

// Add increments the progress bar by the given amount
// Throughput (items/sec) is calculated and displayed automatically
func (p *ProgressBar) Add(amount int64) error {
	p.current += amount
	return p.bar.Add64(amount)
}

// Set sets the progress bar to a specific value
func (p *ProgressBar) Set(value int64) error {
	p.current = value
	return p.bar.Set64(value)
}

// Finish completes the progress bar
func (p *ProgressBar) Finish() error {
	return p.bar.Finish()
}

// Clear clears the progress bar from the terminal
func (p *ProgressBar) Clear() error {
	return p.bar.Clear()
}

// GetPercentage returns current completion percentage (0-100)
func (p *ProgressBar) GetPercentage() float64 {
	if p.total == 0 {
		return 0
	}
	return (float64(p.current) / float64(p.total)) * 100
}

// GetElapsedTime returns time elapsed since progress bar was created
func (p *ProgressBar) GetElapsedTime() time.Duration {
	return time.Since(p.startTime)
}

// Spinner provides visual feedback for operations with unknown duration
// Used when total size or duration cannot be determined in advance
type Spinner struct {
	description string
	startTime   time.Time
	active      bool
}

// NewSpinner creates a spinner for unknown-duration operations
func NewSpinner(description string) *Spinner {
	return &Spinner{
		description: description,
		startTime:   time.Now(),
		active:      false,
	}
}

// Start begins the spinner animation
func (s *Spinner) Start() {
	s.active = true
	s.startTime = time.Now()
	fmt.Printf("%s...\n", s.description)
}

// Stop ends the spinner animation
func (s *Spinner) Stop(success bool) {
	s.active = false
	elapsed := time.Since(s.startTime)

	if success {
		fmt.Printf("✓ %s (completed in %v)\n", s.description, elapsed.Round(time.Millisecond))
	} else {
		fmt.Printf("✗ %s (failed after %v)\n", s.description, elapsed.Round(time.Millisecond))
	}
}

// UpdateMessage updates the spinner's description while it's running
func (s *Spinner) UpdateMessage(message string) {
	s.description = message
	if s.active {
		fmt.Printf("\r%s... (%v elapsed)", message, time.Since(s.startTime).Round(time.Second))
	}
}

// IsActive returns whether the spinner is currently running
func (s *Spinner) IsActive() bool {
	return s.active
}

// RenderBar returns a textual progress bar of the given width for a fraction
// in [0, 1]. Out-of-range fractions are clamped.
func RenderBar(fraction float64, width int) string {
	if fraction < 0 {
		fraction = 0
	}
	if fraction > 1 {
		fraction = 1
	}
	filled := int(fraction * float64(width))
	return "[" + strings.Repeat("#", filled) + strings.Repeat(".", width-filled) + "]"
}
