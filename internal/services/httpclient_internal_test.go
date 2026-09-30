package services

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
)

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

// A real dial timeout needs an address that drops packets, which makes a
// test slow and flaky. These errors have the same shape as the errors that
// http.Client returns.
func TestIsClientTimeout(t *testing.T) {
	tests := map[string]struct {
		err      error
		expected bool
	}{
		"response timeout": {
			&url.Error{Op: "Post", URL: "http://x", Err: timeoutErr{}}, true,
		},
		"dial timeout": {
			&url.Error{Op: "Post", URL: "http://x", Err: &net.OpError{Op: "dial", Net: "tcp", Err: timeoutErr{}}}, false,
		},
		"read timeout after the connection": {
			&url.Error{Op: "Post", URL: "http://x", Err: &net.OpError{Op: "read", Net: "tcp", Err: timeoutErr{}}}, true,
		},
		"connection refused": {
			&url.Error{Op: "Post", URL: "http://x", Err: &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}}, false,
		},
		"body read timeout": {
			fmt.Errorf("failed to parse NDJSON: %w", timeoutErr{}), true,
		},
		"error without timeout": {errors.New("unexpected EOF"), false},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.expected, isClientTimeout(tc.err))
		})
	}
}
