package unit

import (
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/models"
)

// malformedURL makes url.Parse fail.
var malformedURL = "http://[invalid"

func TestServiceURLValidation_ErrorMessages(t *testing.T) {
	flattening := func(u string) error {
		return (&models.FlatteningConfig{
			ServiceURL: u,
			LookupPath: "/path/to/lookup.json",
			Formats:    []string{"csv"},
			Timeout:    30 * time.Minute,
		}).Validate()
	}
	send := func(u string) error {
		return (&models.SendConfig{
			URL:       u,
			SendAs:    models.SendModeDirectResourceLoad,
			BatchSize: 100,
		}).Validate()
	}
	s3 := func(u string) error {
		return (&models.SendConfig{
			SendAs: models.SendModeS3Upload,
			S3: models.S3Config{
				Endpoint:        u,
				Region:          "eu-central-1",
				Bucket:          "my-bucket",
				AccessKeyID:     "AKIAIOSFODNN7EXAMPLE",
				SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
				Timeout:         30 * time.Minute,
			},
		}).Validate()
	}
	torch := func(u string) error {
		return (&models.TORCHConfig{
			BaseURL:            u,
			Auth:               models.AuthConfig{Username: "user", Password: "pass"},
			ExtractionTimeout:  30 * time.Minute,
			PollingInterval:    5 * time.Second,
			MaxPollingInterval: 30 * time.Second,
		}).Validate()
	}

	tests := []struct {
		name     string
		validate func(string) error
		label    string
	}{
		{"flattening service_url", flattening, "flattening service_url"},
		{"send url", send, "send url"},
		{"s3 endpoint", s3, "s3 endpoint"},
		{"torch base_url", torch, "TORCH base_url"},
	}

	t.Run("s3 endpoint is optional", func(t *testing.T) {
		assert.NoError(t, s3(""))
	})

	for _, tt := range tests {
		t.Run(tt.name+" accepts http and https", func(t *testing.T) {
			assert.NoError(t, tt.validate("http://localhost:8080"))
			assert.NoError(t, tt.validate("https://localhost:8443/path"))
		})

		t.Run(tt.name+" rejects a wrong scheme", func(t *testing.T) {
			err := tt.validate("ftp://localhost:8080")
			require.Error(t, err)
			assert.Equal(t, "invalid "+tt.label+": must use http or https scheme, got 'ftp'", err.Error())
		})

		t.Run(tt.name+" rejects a URL without a scheme", func(t *testing.T) {
			err := tt.validate("localhost/path")
			require.Error(t, err)
			assert.Equal(t, "invalid "+tt.label+": must use http or https scheme, got ''", err.Error())
		})

		t.Run(tt.name+" wraps the parse error", func(t *testing.T) {
			_, parseErr := url.Parse(malformedURL)
			require.Error(t, parseErr)

			err := tt.validate(malformedURL)
			require.Error(t, err)
			assert.Equal(t, "invalid "+tt.label+": "+parseErr.Error(), err.Error())
			var urlErr *url.Error
			assert.True(t, errors.As(err, &urlErr), "error must wrap *url.Error")
		})
	}
}
