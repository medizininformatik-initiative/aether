package lib

import (
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
)

func TestCalculateBackoff(t *testing.T) {
	const (
		initialMs = int64(100)
		maxMs     = int64(30000)
	)

	tests := []struct {
		name     string
		attempt  int
		initial  int64
		max      int64
		expected time.Duration
	}{
		{
			name:     "negative attempt clamps to zero base delay",
			attempt:  -5,
			initial:  initialMs,
			max:      maxMs,
			expected: 100 * time.Millisecond,
		},
		{
			name:     "attempt zero yields base delay",
			attempt:  0,
			initial:  initialMs,
			max:      maxMs,
			expected: 100 * time.Millisecond,
		},
		{
			name:     "exponential growth",
			attempt:  3,
			initial:  initialMs,
			max:      maxMs,
			expected: 800 * time.Millisecond,
		},
		{
			name:     "capped at max backoff",
			attempt:  10,
			initial:  initialMs,
			max:      maxMs,
			expected: 30000 * time.Millisecond,
		},
		{
			name:     "very large attempt does not overflow and is capped",
			attempt:  1024,
			initial:  initialMs,
			max:      maxMs,
			expected: 30000 * time.Millisecond,
		},
		{
			name:     "initial larger than max is capped to max",
			attempt:  0,
			initial:  50000,
			max:      maxMs,
			expected: 30000 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := lib.CalculateBackoff(tt.attempt, tt.initial, tt.max)
			assert.Equal(t, tt.expected, got)

			// Result must always stay within [0, maxBackoff].
			assert.GreaterOrEqual(t, got, time.Duration(0))
			assert.LessOrEqual(t, got, time.Duration(tt.max)*time.Millisecond)
		})
	}
}

func TestIsNetworkError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{name: "nil error", err: nil, expected: false},
		{name: "connection refused", err: errors.New("dial tcp: connection refused"), expected: true},
		{name: "connection reset", err: errors.New("read: connection reset by peer"), expected: true},
		{name: "no such host", err: errors.New("lookup foo: no such host"), expected: true},
		{name: "timeout", err: errors.New("i/o timeout"), expected: true},
		{name: "temporary failure", err: errors.New("temporary failure in name resolution"), expected: true},
		{name: "network is unreachable", err: errors.New("connect: network is unreachable"), expected: true},
		{name: "context deadline exceeded", err: errors.New("context deadline exceeded"), expected: true},
		{name: "plain EOF", err: io.EOF, expected: true},
		{name: "mixed case connection refused", err: errors.New("Connection Refused"), expected: true},
		{name: "upper case connection reset", err: errors.New("CONNECTION RESET"), expected: true},
		{name: "wrapped connection refused", err: fmt.Errorf("request failed: %w", errors.New("connection refused")), expected: true},
		{name: "wrapped EOF", err: fmt.Errorf("read body: %w", io.EOF), expected: true},
		{name: "non-network error", err: errors.New("invalid json payload"), expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, lib.IsNetworkError(tt.err))
		})
	}
}

func TestShouldRetry(t *testing.T) {
	tests := []struct {
		name           string
		errorType      models.ErrorType
		currentRetries int
		maxRetries     int
		expected       bool
	}{
		{
			name:           "transient below max retries",
			errorType:      models.ErrorTypeTransient,
			currentRetries: 1,
			maxRetries:     3,
			expected:       true,
		},
		{
			name:           "transient at max retries boundary",
			errorType:      models.ErrorTypeTransient,
			currentRetries: 3,
			maxRetries:     3,
			expected:       false,
		},
		{
			name:           "transient one below max retries boundary",
			errorType:      models.ErrorTypeTransient,
			currentRetries: 2,
			maxRetries:     3,
			expected:       true,
		},
		{
			name:           "non-transient never retries",
			errorType:      models.ErrorTypeNonTransient,
			currentRetries: 0,
			maxRetries:     3,
			expected:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, lib.ShouldRetry(tt.errorType, tt.currentRetries, tt.maxRetries))
		})
	}
}
