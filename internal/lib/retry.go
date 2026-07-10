package lib

import (
	"math"
	"time"

	"github.com/medizininformatik-initiative/aether/internal/models"
)

// CalculateBackoff computes exponential backoff duration
// Formula: min(initialBackoff * 2^attempt, maxBackoff)
func CalculateBackoff(attempt int, initialBackoffMs int64, maxBackoffMs int64) time.Duration {
	if attempt < 0 {
		attempt = 0
	}

	// Exponential backoff: initialBackoff * 2^attempt
	backoffMs := float64(initialBackoffMs) * math.Pow(2, float64(attempt))

	// Cap at maxBackoff
	if backoffMs > float64(maxBackoffMs) {
		backoffMs = float64(maxBackoffMs)
	}

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

// NewRetryConfigFrom models creates RetryConfig from models.RetryConfig.
// A non-positive MaxAttempts is normalized to a single attempt so the retry
// loop always runs at least once instead of failing with a nil-wrapped error.
func NewRetryConfigFromModel(config models.RetryConfig) RetryConfig {
	maxAttempts := config.MaxAttempts
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	return RetryConfig{
		MaxAttempts:      maxAttempts,
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

	errMsg := err.Error()

	// Common network error patterns (case-insensitive matching)
	networkErrors := []string{
		"connection refused",
		"connection reset",
		"no such host",
		"timeout",
		"temporary failure",
		"network is unreachable",
		"deadline exceeded", // Catches "context deadline exceeded"
		"EOF",
	}

	for _, pattern := range networkErrors {
		if containsIgnoreCase(errMsg, pattern) {
			return true
		}
	}

	return false
}

// Helper function to check if string contains substring (case-insensitive)
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// containsIgnoreCase checks if string contains substring (case-insensitive)
func containsIgnoreCase(s, substr string) bool {
	// Convert both strings to lowercase for comparison
	sLower := ""
	substrLower := ""

	for _, r := range s {
		if r >= 'A' && r <= 'Z' {
			sLower += string(r + 32)
		} else {
			sLower += string(r)
		}
	}

	for _, r := range substr {
		if r >= 'A' && r <= 'Z' {
			substrLower += string(r + 32)
		} else {
			substrLower += string(r)
		}
	}

	return contains(sLower, substrLower)
}
