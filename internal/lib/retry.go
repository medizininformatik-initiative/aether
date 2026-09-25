package lib

import (
	"math"
	"strings"
	"time"

	"github.com/medizininformatik-initiative/aether/internal/models"
)

// CalculateBackoff computes exponential backoff duration
// Formula: min(initialBackoff * 2^attempt, maxBackoff)
func CalculateBackoff(attempt int, initialBackoffMs int64, maxBackoffMs int64) time.Duration {
	attempt = max(attempt, 0)
	backoffMs := float64(initialBackoffMs) * math.Pow(2, float64(attempt))
	backoffMs = min(backoffMs, float64(maxBackoffMs))

	return time.Duration(backoffMs) * time.Millisecond
}

// ShouldRetry determines if an operation should be retried based on error type and retry count
func ShouldRetry(errorType models.ErrorType, currentRetries int, maxRetries int) bool {
	// Only retry transient errors
	if errorType != models.ErrorTypeTransient {
		return false
	}

	// Check if we haven't exceeded max retries
	return currentRetries < maxRetries
}

// ClassifyHTTPError determines if an HTTP error is transient or non-transient
func ClassifyHTTPError(statusCode int) models.ErrorType {
	if models.IsTransientHTTPStatus(statusCode) {
		return models.ErrorTypeTransient
	}
	return models.ErrorTypeNonTransient
}

// RetryConfig holds retry strategy parameters
type RetryConfig struct {
	MaxAttempts      int
	InitialBackoffMs int64
	MaxBackoffMs     int64
}

// NewRetryConfigFromModel creates RetryConfig from models.RetryConfig.
// A non-positive MaxAttempts is normalized to a single attempt so the retry
// loop always runs at least once instead of failing with a nil-wrapped error.
func NewRetryConfigFromModel(config models.RetryConfig) RetryConfig {
	return RetryConfig{
		MaxAttempts:      max(config.MaxAttempts, 1),
		InitialBackoffMs: config.InitialBackoffMs,
		MaxBackoffMs:     config.MaxBackoffMs,
	}
}

// IsNetworkError checks if an error is likely a network-related issue
// These are typically transient and should be retried
func IsNetworkError(err error) bool {
	if err == nil {
		return false
	}

	errMsg := strings.ToLower(err.Error())

	networkErrors := []string{
		"connection refused",
		"connection reset",
		"no such host",
		"timeout",
		"temporary failure",
		"network is unreachable",
		"deadline exceeded", // Catches "context deadline exceeded"
		"eof",
	}

	for _, pattern := range networkErrors {
		if strings.Contains(errMsg, pattern) {
			return true
		}
	}

	return false
}
