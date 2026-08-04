package lib

import (
	"fmt"
	"math"
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

	d, ok, err := parseISO8601Duration(s)
	if err != nil {
		return 0, err
	}
	if ok {
		return d, nil
	}

	d, err = time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q: must be ISO 8601 (e.g. PT30M) or Go format (e.g. 30m)", s)
	}
	return d, nil
}

// parseISO8601Duration attempts to parse an ISO 8601 duration string.
// Returns ok=false if the string is not in ISO 8601 format, or an error if it
// matches the format but the value does not fit in a time.Duration.
func parseISO8601Duration(s string) (time.Duration, bool, error) {
	matches := iso8601Pattern.FindStringSubmatch(s)
	if matches == nil {
		return 0, false, nil
	}

	// Reject bare "P" or "PT" with no components
	if matches[1] == "" && matches[2] == "" && matches[3] == "" && matches[4] == "" {
		return 0, false, nil
	}

	errOutOfRange := fmt.Errorf("invalid duration %q: value out of range", s)

	var d time.Duration

	components := []struct {
		value string
		unit  time.Duration
	}{
		{matches[1], 24 * time.Hour},
		{matches[2], time.Hour},
		{matches[3], time.Minute},
	}
	for _, c := range components {
		if c.value == "" {
			continue
		}
		n, err := strconv.ParseInt(c.value, 10, 64)
		if err != nil || n > int64(math.MaxInt64/c.unit) {
			return 0, false, errOutOfRange
		}
		scaled := time.Duration(n) * c.unit
		if d > math.MaxInt64-scaled {
			return 0, false, errOutOfRange
		}
		d += scaled
	}

	if matches[4] != "" {
		seconds, err := strconv.ParseFloat(matches[4], 64)
		// Reject before converting to time.Duration: float-to-integer
		// conversion is implementation-defined when the value overflows.
		if err != nil || seconds > float64(math.MaxInt64/time.Second) {
			return 0, false, errOutOfRange
		}
		scaled := time.Duration(seconds * float64(time.Second))
		if d > math.MaxInt64-scaled {
			return 0, false, errOutOfRange
		}
		d += scaled
	}

	return d, true, nil
}
