package unit

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/models"
)

func csvImportConfig(steps []models.StepName, sendAs models.SendMode) models.ProjectConfig {
	config := models.DefaultConfig()
	config.Pipeline.EnabledSteps = steps
	config.Services.Send = models.SendConfig{
		URL:    "http://dsf.example.com/fhir",
		SendAs: sendAs,
		Auth:   models.AuthConfig{Username: "user", Password: "pass"},
		Transfer: models.TransferConfig{
			ProjectIdentifier:      "Test_Project",
			OrganizationIdentifier: "test.org",
		},
		S3: models.S3Config{
			Bucket:          "bucket",
			Region:          "eu-central-1",
			AccessKeyID:     "key",
			SecretAccessKey: "secret",
			Timeout:         time.Minute,
		},
	}
	return config
}

func TestProjectConfigValidate_CSVImport(t *testing.T) {
	csv, send, wait := models.StepCSVImport, models.StepSend, models.StepWait

	t.Run("csv_import then send with transfer_load is valid", func(t *testing.T) {
		config := csvImportConfig([]models.StepName{csv, send}, models.SendModeTransferLoad)
		assert.NoError(t, config.Validate())
	})

	t.Run("csv_import then send with s3_upload is valid", func(t *testing.T) {
		config := csvImportConfig([]models.StepName{csv, send}, models.SendModeS3Upload)
		assert.NoError(t, config.Validate())
	})

	t.Run("csv_import then wait then send is valid", func(t *testing.T) {
		config := csvImportConfig([]models.StepName{csv, wait, send}, models.SendModeTransferLoad)
		assert.NoError(t, config.Validate())
	})
}

func TestProjectConfigValidate_CSVImportRestrictions(t *testing.T) {
	csv, send := models.StepCSVImport, models.StepSend

	cases := []struct {
		name   string
		steps  []models.StepName
		sendAs models.SendMode
		errMsg string
	}{
		{"direct_resource_load uploads FHIR only", []models.StepName{csv, send}, models.SendModeDirectResourceLoad, "csv_import requires send_as"},
		{"flattening needs FHIR data", []models.StepName{csv, models.StepFlattening, send}, models.SendModeTransferLoad, "after csv_import"},
		{"dimp needs FHIR data", []models.StepName{csv, models.StepDIMP}, models.SendModeTransferLoad, "after csv_import"},
		{"validation needs FHIR data", []models.StepName{csv, models.StepValidation, send}, models.SendModeTransferLoad, "after csv_import"},
		{"no other import step", []models.StepName{models.StepLocalImport, csv, send}, models.SendModeTransferLoad, "first enabled step"},
		{"no second import step after csv_import", []models.StepName{csv, models.StepHttpImport, send}, models.SendModeTransferLoad, "after csv_import"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			config := csvImportConfig(c.steps, c.sendAs)
			err := config.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), c.errMsg)
		})
	}
}

func TestProjectConfigValidate_FHIRPipelinesStayValid(t *testing.T) {
	config := csvImportConfig([]models.StepName{models.StepLocalImport, models.StepSend}, models.SendModeDirectResourceLoad)
	assert.NoError(t, config.Validate())
}
