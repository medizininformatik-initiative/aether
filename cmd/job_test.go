package cmd

import (
	"testing"

	"github.com/mattn/go-runewidth"
	"github.com/stretchr/testify/assert"
)

func TestFormatStatusField_DisplayWidthMatchesHeader(t *testing.T) {
	statuses := []string{"completed", "in_progress", "failed", "pending"}
	for _, status := range statuses {
		t.Run(status, func(t *testing.T) {
			symbol := getJobStatusSymbol(status)
			field := formatStatusField(symbol, status)
			assert.Equal(t, statusFieldWidth, runewidth.StringWidth(field),
				"status field must occupy exactly %d display columns to align with the %%-15s header",
				statusFieldWidth)
		})
	}
}

func TestFormatStatusField_UnknownStatusStillPads(t *testing.T) {
	field := formatStatusField(getJobStatusSymbol("mystery"), "mystery")
	assert.Equal(t, statusFieldWidth, runewidth.StringWidth(field))
}
