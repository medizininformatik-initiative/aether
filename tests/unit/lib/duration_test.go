package lib_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/lib"
)

func TestParseDuration_ISO8601(t *testing.T) {
	tests := []struct {
		input    string
		expected time.Duration
	}{
		{"PT30M", 30 * time.Minute},
		{"PT5S", 5 * time.Second},
		{"PT30S", 30 * time.Second},
		{"PT10S", 10 * time.Second},
		{"PT1H", 1 * time.Hour},
		{"PT72H", 72 * time.Hour},
		{"PT1H30M", 90 * time.Minute},
		{"PT1H30M15S", time.Hour + 30*time.Minute + 15*time.Second},
		{"P1D", 24 * time.Hour},
		{"P3D", 72 * time.Hour},
		{"P1DT12H", 36 * time.Hour},
		{"P1DT2H30M", 26*time.Hour + 30*time.Minute},
		{"PT0S", 0},
		{"PT1.5S", 1500 * time.Millisecond},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			d, err := lib.ParseDuration(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, d)
		})
	}
}

func TestParseDuration_GoFormat(t *testing.T) {
	tests := []struct {
		input    string
		expected time.Duration
	}{
		{"30m", 30 * time.Minute},
		{"5s", 5 * time.Second},
		{"1h", 1 * time.Hour},
		{"1h30m", 90 * time.Minute},
		{"500ms", 500 * time.Millisecond},
		{"1m30s", 90 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			d, err := lib.ParseDuration(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, d)
		})
	}
}

func TestParseDuration_Empty(t *testing.T) {
	d, err := lib.ParseDuration("")
	require.NoError(t, err)
	assert.Equal(t, time.Duration(0), d)
}

func TestParseDuration_Invalid(t *testing.T) {
	invalid := []string{"abc", "30", "PT", "P", "PTXM"}
	for _, s := range invalid {
		t.Run(s, func(t *testing.T) {
			_, err := lib.ParseDuration(s)
			assert.Error(t, err)
		})
	}
}
