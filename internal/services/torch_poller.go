package services

import (
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/medizininformatik-initiative/aether/internal/lib"
)

// PollConfig holds configuration for extraction polling
type PollConfig struct {
	Timeout         time.Duration
	StartTime       time.Time
	PollInterval    time.Duration
	MaxPollInterval time.Duration
	PollCount       int
}

// NewPollConfig creates polling configuration from client settings
func NewPollConfig(timeoutMinutes, pollIntervalSeconds, maxPollIntervalSeconds int) *PollConfig {
	return &PollConfig{
		Timeout:         time.Duration(timeoutMinutes) * time.Minute,
		StartTime:       time.Now(),
		PollInterval:    time.Duration(pollIntervalSeconds) * time.Second,
		MaxPollInterval: time.Duration(maxPollIntervalSeconds) * time.Second,
		PollCount:       0,
	}
}

// createPollRequest creates an HTTP GET request with authentication for polling
func createPollRequest(extractionURL string, c *TORCHClient) (*http.Request, error) {
	req, err := http.NewRequest("GET", extractionURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create poll request: %w", err)
	}

	req.Header.Set("Authorization", c.buildBasicAuthHeader())
	req.Header.Set("Accept", "application/json")

	return req, nil
}

// handlePollResponse processes a polling response and returns completion status and file URLs
func handlePollResponse(resp *http.Response, c *TORCHClient) (complete bool, fileURLs []string, err error) {
	defer func() { _ = resp.Body.Close() }()

	bodyBytes, _ := io.ReadAll(resp.Body)

	switch resp.StatusCode {
	case http.StatusAccepted:
		// Still in progress
		return false, nil, nil

	case http.StatusOK:
		// Extraction complete - parse result
		c.logger.Info("TORCH extraction completed")
		c.logger.Debug("TORCH extraction response body", "body", string(bodyBytes))
		fileURLs, err := c.parseExtractionResult(bodyBytes)
		if err != nil {
			return false, nil, err
		}
		return true, fileURLs, nil

	default:
		// Error response
		errorType := lib.ClassifyHTTPError(resp.StatusCode)
		c.logger.Error("TORCH extraction failed",
			"status_code", resp.StatusCode,
			"status", resp.Status,
			"error_body", string(bodyBytes))

		return false, nil, &TORCHError{
			Operation:  "poll",
			StatusCode: resp.StatusCode,
			Message:    string(bodyBytes),
			ErrorType:  errorType,
		}
	}
}

// CalculateNextPollInterval calculates exponential backoff for next polling attempt
func CalculateNextPollInterval(current, max time.Duration) time.Duration {
	next := current * 2
	if next > max {
		return max
	}
	return next
}

// CheckTimeout checks if polling has exceeded timeout
func (pc *PollConfig) CheckTimeout() bool {
	return time.Since(pc.StartTime) > pc.Timeout
}

// GetElapsedTime returns time elapsed since polling started
func (pc *PollConfig) GetElapsedTime() time.Duration {
	return time.Since(pc.StartTime)
}

// IncrementPollCount increments the poll attempt counter
func (pc *PollConfig) IncrementPollCount() {
	pc.PollCount++
}

// UpdateInterval updates the poll interval with exponential backoff
func (pc *PollConfig) UpdateInterval() {
	pc.PollInterval = CalculateNextPollInterval(pc.PollInterval, pc.MaxPollInterval)
}
