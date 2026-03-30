package services

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
)

// PollConfig holds configuration for extraction polling
type PollConfig struct {
	Timeout         time.Duration
	StartTime       time.Time
	PollInterval    time.Duration
	MaxPollInterval time.Duration
	PollCount       int
}

// NewPollConfig creates polling configuration from TORCH client settings
func NewPollConfig(cfg models.TORCHConfig) *PollConfig {
	return &PollConfig{
		Timeout:         cfg.ExtractionTimeout,
		StartTime:       time.Now(),
		PollInterval:    cfg.PollingInterval,
		MaxPollInterval: cfg.MaxPollingInterval,
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

// handlePollResponse processes a polling response and returns completion status, file URLs, and progress diagnostics
func handlePollResponse(resp *http.Response, c *TORCHClient) (complete bool, fileURLs []string, diagnostics string, err error) {
	defer func() { _ = resp.Body.Close() }()

	bodyBytes, _ := io.ReadAll(resp.Body)

	switch resp.StatusCode {
	case http.StatusAccepted, http.StatusProcessing:
		// Still in progress (202 Accepted or 102 Processing)
		diagnostics = extractProgressDiagnostics(bodyBytes)
		return false, nil, diagnostics, nil

	case http.StatusOK:
		// Extraction complete - parse result
		c.logger.Info("TORCH extraction completed")
		c.logger.Debug("TORCH extraction response body", "body", string(bodyBytes))
		fileURLs, err := c.parseExtractionResult(bodyBytes)
		if err != nil {
			return false, nil, "", err
		}
		return true, fileURLs, "", nil

	default:
		// Error response
		errorType := lib.ClassifyHTTPError(resp.StatusCode)
		c.logger.Error("TORCH extraction failed",
			"status_code", resp.StatusCode,
			"status", resp.Status,
			"error_body", string(bodyBytes))

		return false, nil, "", &TORCHError{
			Operation:  "poll",
			StatusCode: resp.StatusCode,
			Message:    string(bodyBytes),
			ErrorType:  errorType,
		}
	}
}

// extractProgressDiagnostics parses an OperationOutcome response body and returns
// the first informational diagnostic message, if any.
func extractProgressDiagnostics(body []byte) string {
	if len(body) == 0 {
		return ""
	}

	var outcome OperationOutcome
	if err := json.Unmarshal(body, &outcome); err != nil {
		return ""
	}

	if outcome.ResourceType != "OperationOutcome" {
		return ""
	}

	for _, issue := range outcome.Issue {
		if issue.Severity == "information" && issue.Diagnostics != "" {
			return issue.Diagnostics
		}
	}

	return ""
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
