package unit

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/models"
)

func TestGenerateJobID_Format(t *testing.T) {
	id := models.GenerateJobID()

	// Pattern: YYYYMMDD_HHMM_<UUID>
	pattern := `^\d{8}_\d{4}_[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`
	matched, err := regexp.MatchString(pattern, id)
	require.NoError(t, err)
	assert.True(t, matched, "Job ID %q should match pattern YYYYMMDD_HHMM_UUID", id)
}

func TestGenerateJobID_Uniqueness(t *testing.T) {
	ids := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id := models.GenerateJobID()
		assert.False(t, ids[id], "Job ID should be unique, got duplicate: %s", id)
		ids[id] = true
	}
}

func TestGenerateJobID_Length(t *testing.T) {
	id := models.GenerateJobID()

	// YYYYMMDD (8) + _ (1) + HHMM (4) + _ (1) + UUID (36) = 50
	assert.Len(t, id, 50, "Job ID should be 50 characters")
}

func TestValidateJobID_NewFormat(t *testing.T) {
	id := models.GenerateJobID()
	err := models.ValidateJobID(id)
	assert.NoError(t, err, "Generated job ID should be valid")
}

func TestValidateJobID_OldUUIDFormat(t *testing.T) {
	err := models.ValidateJobID("550e8400-e29b-41d4-a716-446655440000")
	assert.NoError(t, err, "Old UUID-only format should still be accepted")
}

func TestValidateJobID_InvalidFormats(t *testing.T) {
	tests := []struct {
		name string
		id   string
	}{
		{"empty", ""},
		{"random string", "not-a-valid-id"},
		{"bad timestamp prefix", "20261300_9999_550e8400-e29b-41d4-a716-446655440000"},
		{"missing UUID suffix", "20260317_1430_"},
		{"bad UUID suffix", "20260317_1430_not-a-uuid"},
		{"wrong separator", "20260317-1430-550e8400-e29b-41d4-a716-446655440000"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := models.ValidateJobID(tt.id)
			assert.Error(t, err, "Job ID %q should be invalid", tt.id)
		})
	}
}

func TestValidateJobID_TimestampPrefix(t *testing.T) {
	// Valid timestamp with valid UUID
	validUUID := "550e8400-e29b-41d4-a716-446655440000"

	tests := []struct {
		name    string
		prefix  string
		wantErr bool
	}{
		{"valid date and time", "20260317_1430", false},
		{"midnight", "20260101_0000", false},
		{"end of day", "20261231_2359", false},
		{"feb 31 overflow", "20260231_1430", true},
		{"invalid month 13", "20261317_1430", true},
		{"invalid hour 25", "20260317_2530", true},
		{"invalid minute 60", "20260317_1460", true},
		{"month 00", "20260017_1430", true},
		{"day 00", "20260300_1430", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id := tt.prefix + "_" + validUUID
			err := models.ValidateJobID(id)
			if tt.wantErr {
				assert.Error(t, err, "Job ID with prefix %q should be invalid", tt.prefix)
			} else {
				assert.NoError(t, err, "Job ID with prefix %q should be valid", tt.prefix)
			}
		})
	}
}

func TestGenerateJobID_UsesUTC(t *testing.T) {
	id := models.GenerateJobID()

	// Extract the timestamp part (first 13 chars: YYYYMMDD_HHMM)
	parts := strings.SplitN(id, "_", 3)
	require.Len(t, parts, 3, "Job ID should have 3 parts separated by underscores")

	// We can't assert exact UTC time, but we can verify the date portion is
	// a plausible date (not zero, not far future)
	datePart := parts[0]
	assert.Len(t, datePart, 8)
	assert.True(t, datePart >= "20200101", "Date should be after 2020")
	assert.True(t, datePart <= "20991231", "Date should be before 2100")
}
