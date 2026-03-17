package models

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// GenerateJobID creates a new job ID in the format YYYYMMDD_HHMM_UUID.
// The timestamp uses UTC for consistency across systems.
func GenerateJobID() string {
	timestamp := time.Now().UTC().Format("20060102_1504")
	return fmt.Sprintf("%s_%s", timestamp, uuid.New().String())
}

// ValidateJobID checks whether id is either a valid UUID (legacy format)
// or a valid YYYYMMDD_HHMM_UUID (new format).
func ValidateJobID(id string) error {
	if id == "" {
		return fmt.Errorf("job_id is required")
	}

	// Try legacy UUID-only format first
	if _, err := uuid.Parse(id); err == nil {
		return nil
	}

	// Try new YYYYMMDD_HHMM_UUID format
	parts := strings.SplitN(id, "_", 3)
	if len(parts) != 3 {
		return fmt.Errorf("invalid job_id: must be a UUID or YYYYMMDD_HHMM_UUID format")
	}

	datePart, timePart, uuidPart := parts[0], parts[1], parts[2]

	// Validate timestamp by parsing and round-trip check.
	// Go's time.Parse silently overflows invalid dates (e.g. Feb 31 → Mar 3),
	// so we re-format and compare to catch those.
	timestampStr := datePart + "_" + timePart
	parsed, err := time.Parse("20060102_1504", timestampStr)
	if err != nil {
		return fmt.Errorf("invalid job_id: bad timestamp prefix: %w", err)
	}
	if parsed.Format("20060102_1504") != timestampStr {
		return fmt.Errorf("invalid job_id: timestamp %s is not a valid calendar date", timestampStr)
	}

	// Validate UUID suffix
	if _, err := uuid.Parse(uuidPart); err != nil {
		return fmt.Errorf("invalid job_id: bad UUID suffix: %w", err)
	}

	return nil
}
