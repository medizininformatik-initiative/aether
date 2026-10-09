package unit

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/medizininformatik-initiative/aether/internal/models"
)

func TestIsTorchConsentFile(t *testing.T) {
	tests := map[string]bool{
		"x_consent.ndjson":     true,
		"x_consent.ndjson.zst": true,
		"X_CONSENT.NDJSON":     true,
		"Patient.ndjson":       false,
		"consent.ndjson":       false,
		"x_consent.json":       false,
		"x_consent.ndjson.bak": false,
		"Consent.ndjson":       false,
	}
	for name, want := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, want, models.IsTorchConsentFile(name))
		})
	}
}
