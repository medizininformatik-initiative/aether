package services

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
)

// HTTPClient wraps the standard http.Client with retry logic and configuration
type HTTPClient struct {
	client      *http.Client
	retryConfig lib.RetryConfig
	logger      *lib.Logger
}

// NewHTTPClient creates an HTTP client with timeout and retry configuration
func NewHTTPClient(timeout time.Duration, retryConfig models.RetryConfig, logger *lib.Logger) *HTTPClient {
	return &HTTPClient{
		client: &http.Client{
			Timeout: timeout,
		},
		retryConfig: lib.NewRetryConfigFromModel(retryConfig),
		logger:      logger,
	}
}

// DefaultHTTPClient creates an HTTP client with sensible defaults
func DefaultHTTPClient() *HTTPClient {
	return NewHTTPClient(
		30*time.Second,
		models.RetryConfig{
			MaxAttempts:      5,
			InitialBackoffMs: 1000,
			MaxBackoffMs:     30000,
		},
		lib.DefaultLogger,
	)
}

// Get performs an HTTP GET request with retry logic
func (c *HTTPClient) Get(url string) (*http.Response, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	return c.Do(req)
}

// Post performs an HTTP POST request with retry logic
func (c *HTTPClient) Post(url string, contentType string, body []byte) (*http.Response, error) {
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", contentType)

	return c.Do(req)
}

// PostJSON performs an HTTP POST request with JSON content type
func (c *HTTPClient) PostJSON(url string, jsonBody []byte) (*http.Response, error) {
	return c.Post(url, "application/json", jsonBody)
}

// Do executes an HTTP request with retry logic for transient errors
func (c *HTTPClient) Do(req *http.Request) (*http.Response, error) {
	var resp *http.Response
	var lastErr error

	// Retry logic
	for attempt := 0; attempt < c.retryConfig.MaxAttempts; attempt++ {
		// Clone request body if needed (body can only be read once)
		var bodyBytes []byte
		if req.Body != nil {
			bodyBytes, _ = io.ReadAll(req.Body)
			req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		}

		// Execute request
		startTime := time.Now()
		resp, lastErr = c.client.Do(req)
		duration := time.Since(startTime)

		// Log the request
		lib.LogServiceCall(c.logger, req.URL.Host, req.URL.Path, req.Method)

		// Success
		if lastErr == nil {
			// Log response
			lib.LogServiceResponse(c.logger, req.URL.Host, resp.StatusCode, duration)

			// Check if HTTP status indicates error
			if resp.StatusCode >= 400 {
				// Classify error type
				errorType := lib.ClassifyHTTPError(resp.StatusCode)

				// Create error for HTTP status
				statusErr := fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)

				// For non-transient errors, return immediately
				if errorType == models.ErrorTypeNonTransient {
					return resp, nil // Return response so caller can read error details
				}

				// For transient errors, retry
				if lib.ShouldRetry(errorType, attempt, c.retryConfig.MaxAttempts) {
					lib.LogRetry(c.logger, req.URL.String(), attempt, c.retryConfig.MaxAttempts, statusErr)

					// Store the error in case this is the last attempt
					lastErr = statusErr

					// Close response body before retry
					_ = resp.Body.Close()

					// Wait before retry
					if attempt < c.retryConfig.MaxAttempts-1 {
						backoff := lib.CalculateBackoff(attempt, c.retryConfig.InitialBackoffMs, c.retryConfig.MaxBackoffMs)
						time.Sleep(backoff)
					}

					// Reset request body for retry
					if bodyBytes != nil {
						req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
					}

					continue
				}
			}

			return resp, nil
		}

		// Network error occurred
		// Check if it's a retryable network error
		if lib.IsNetworkError(lastErr) {
			errorType := models.ErrorTypeTransient
			if lib.ShouldRetry(errorType, attempt, c.retryConfig.MaxAttempts) {
				lib.LogRetry(c.logger, req.URL.String(), attempt, c.retryConfig.MaxAttempts, lastErr)

				// Wait before retry
				if attempt < c.retryConfig.MaxAttempts-1 {
					backoff := lib.CalculateBackoff(attempt, c.retryConfig.InitialBackoffMs, c.retryConfig.MaxBackoffMs)
					time.Sleep(backoff)
				}

				// Reset request body for retry
				if bodyBytes != nil {
					req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
				}

				continue
			}
		}

		// Non-retryable error
		return nil, lastErr
	}

	return nil, fmt.Errorf("request failed after %d attempts: %w", c.retryConfig.MaxAttempts, lastErr)
}

// Download downloads a file from a URL and writes it to a writer
// Returns the number of bytes downloaded
func (c *HTTPClient) Download(url string, writer io.Writer) (int64, error) {
	resp, err := c.Get(url)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	// Copy response body to writer
	bytesWritten, err := io.Copy(writer, resp.Body)
	if err != nil {
		return bytesWritten, fmt.Errorf("failed to download: %w", err)
	}

	return bytesWritten, nil
}

// DownloadWithProgress downloads a file with progress callback
// The callback is called periodically with bytes downloaded so far
func (c *HTTPClient) DownloadWithProgress(url string, writer io.Writer, progressCallback func(int64)) (int64, error) {
	resp, err := c.Get(url)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	// Create a progress reader that calls the callback
	reader := &ProgressReader{
		Reader:   resp.Body,
		Callback: progressCallback,
	}

	// Copy response body to writer
	bytesWritten, err := io.Copy(writer, reader)
	if err != nil {
		return bytesWritten, fmt.Errorf("failed to download: %w", err)
	}

	return bytesWritten, nil
}

// ProgressReader wraps an io.Reader and calls a callback with bytes read
type ProgressReader struct {
	Reader   io.Reader
	Callback func(int64)
	total    int64
}

func (r *ProgressReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.total += int64(n)
	if r.Callback != nil && n > 0 {
		r.Callback(r.total)
	}
	return n, err
}
