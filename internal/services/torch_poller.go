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

// PollConfig holds configuration and timing state for extraction polling.
// LastContact anchors the liveness window (reset on every 200/202); PollStart
// is the wall-clock start, kept only for reporting total elapsed time.
type PollConfig struct {
	Timeout         time.Duration
	LastContact     time.Time
	PollStart       time.Time
	PollInterval    time.Duration
	MaxPollInterval time.Duration
	PollCount       int
}

// NewPollConfig creates polling configuration from TORCH client settings
func NewPollConfig(cfg models.TORCHConfig) *PollConfig {
	now := time.Now()
	return &PollConfig{
		Timeout:         cfg.ExtractionTimeout,
		LastContact:     now,
		PollStart:       now,
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

	if err := c.httpClient.ApplyAuth(req, c.authConfig()); err != nil {
		return nil, fmt.Errorf("failed to add auth header: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	return req, nil
}

// pollOutcome is the result of interpreting a single status poll response.
// When err is set, retryable says whether the poll loop should keep polling
// (transient) or give up (terminal).
type pollOutcome struct {
	complete    bool
	fileURLs    []string
	diagnostics string
	retryable   bool
	err         error
}

// handlePollResponse interprets a status poll response: completion + file URLs
// on 200, in-progress diagnostics on 202, a dead-handle signal on 404/410, and
// a retryable/terminal error otherwise.
func (c *TORCHClient) handlePollResponse(resp *http.Response) pollOutcome {
	defer func() { _ = resp.Body.Close() }()

	bodyBytes, _ := io.ReadAll(resp.Body)

	switch resp.StatusCode {
	case http.StatusAccepted, http.StatusProcessing:
		// Still in progress (202 Accepted or 102 Processing)
		return pollOutcome{diagnostics: extractProgressDiagnostics(bodyBytes)}

	case http.StatusOK:
		c.logger.Info("TORCH extraction completed")
		c.logger.Debug("TORCH extraction response body", "body", string(bodyBytes))
		fileURLs, err := c.parseExtractionResult(bodyBytes)
		if err != nil {
			return pollOutcome{err: err}
		}
		return pollOutcome{complete: true, fileURLs: fileURLs}

	case http.StatusNotFound, http.StatusGone:
		// Handle is dead — the job no longer exists on TORCH. Signal the caller
		// to clear the stored handle and re-submit a fresh extraction.
		c.logger.Warn("TORCH job handle is gone", "status_code", resp.StatusCode, "status", resp.Status)
		return pollOutcome{err: ErrHandleDead}

	default:
		// TORCH returns 500 only for a terminal job failure (TEMP_FAILED is
		// surfaced as 202/503), so treat it as non-transient here even though
		// 5xx is retryable for generic HTTP calls. Other 5xx, 408, and 429 stay
		// transient and retryable; 4xx is terminal.
		errorType := lib.ClassifyHTTPError(resp.StatusCode)
		if resp.StatusCode == http.StatusInternalServerError {
			errorType = models.ErrorTypeNonTransient
		}
		retryable := errorType == models.ErrorTypeTransient
		logFields := []any{"status_code", resp.StatusCode, "status", resp.Status, "error_body", string(bodyBytes)}
		if retryable {
			c.logger.Warn("TORCH poll returned transient error, will retry", logFields...)
		} else {
			c.logger.Error("TORCH extraction failed", logFields...)
		}

		return pollOutcome{
			retryable: retryable,
			err: &TORCHError{
				Operation:  "poll",
				StatusCode: resp.StatusCode,
				Message:    string(bodyBytes),
				ErrorType:  errorType,
			},
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

// CheckTimeout reports whether the liveness window has elapsed since the last
// contact with TORCH. extraction_timeout is a no-progress window, not a total
// cap: a healthy job resets it on every 200/202 (see RecordContact).
func (pc *PollConfig) CheckTimeout() bool {
	return time.Since(pc.LastContact) > pc.Timeout
}

// RecordContact resets the liveness window. Call it on every 200/202 response
// so a long but responsive extraction never trips extraction_timeout.
func (pc *PollConfig) RecordContact() {
	pc.LastContact = time.Now()
}

// GetElapsedTime returns the total time elapsed since polling started.
func (pc *PollConfig) GetElapsedTime() time.Duration {
	return time.Since(pc.PollStart)
}

// IncrementPollCount increments the poll attempt counter
func (pc *PollConfig) IncrementPollCount() {
	pc.PollCount++
}

// UpdateInterval updates the poll interval with exponential backoff
func (pc *PollConfig) UpdateInterval() {
	pc.PollInterval = CalculateNextPollInterval(pc.PollInterval, pc.MaxPollInterval)
}
