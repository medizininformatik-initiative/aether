package lib

import (
	"fmt"
	"regexp"
	"strconv"
	"time"
)

// iso8601Pattern matches ISO 8601 duration strings like PT30M, PT1H30M5S, P1DT12H
var iso8601Pattern = regexp.MustCompile(`^P(?:(\d+)D)?(?:T(?:(\d+)H)?(?:(\d+)M)?(?:(\d+(?:\.\d+)?)S)?)?$`)

// ParseDuration parses a duration string in either ISO 8601 format (e.g. "PT30M")
// or Go format (e.g. "30m"). ISO 8601 is tried first; if it doesn't match,
// Go's time.ParseDuration is used as fallback.
func ParseDuration(s string) (time.Duration, error) {
	if s == "" {
		return 0, nil
	}

	if d, ok := parseISO8601Duration(s); ok {
		return d, nil
	}

	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q: must be ISO 8601 (e.g. PT30M) or Go format (e.g. 30m)", s)
	}
	return d, nil
}

// parseISO8601Duration attempts to parse an ISO 8601 duration string.
// Returns the duration and true if successful, or zero and false if the string
// is not in ISO 8601 format.
func parseISO8601Duration(s string) (time.Duration, bool) {
	matches := iso8601Pattern.FindStringSubmatch(s)
	if matches == nil {
		return 0, false
	}

	// Reject bare "P" or "PT" with no components
	if matches[1] == "" && matches[2] == "" && matches[3] == "" && matches[4] == "" {
		return 0, false
	}

	var d time.Duration

	if matches[1] != "" {
		days, _ := strconv.Atoi(matches[1])
		d += time.Duration(days) * 24 * time.Hour
	}
	if matches[2] != "" {
		hours, _ := strconv.Atoi(matches[2])
		d += time.Duration(hours) * time.Hour
	}
	if matches[3] != "" {
		minutes, _ := strconv.Atoi(matches[3])
		d += time.Duration(minutes) * time.Minute
	}
	if matches[4] != "" {
		seconds, _ := strconv.ParseFloat(matches[4], 64)
		d += time.Duration(seconds * float64(time.Second))
	}

	return d, true
}
