package unit

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

func TestSendConfigLoading_S3(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	jobsDir := filepath.Join(tmpDir, "jobs")
	require.NoError(t, os.MkdirAll(jobsDir, 0755))

	configContent := `
services:
  send:
    send_as: "s3_upload"
    s3:
      endpoint: "http://seaweedfs:8333"
      region: "us-west-2"
      bucket: "pipeline-data"
      access_key_id: "TESTKEY"
      secret_access_key: "TESTSECRET"
      use_path_style: true
      timeout: 45m

pipeline:
  enabled_steps:
    - local_import
    - send

retry:
  max_attempts: 3
  initial_backoff_ms: 500
  max_backoff_ms: 5000

jobs_dir: "` + jobsDir + `"
`
	require.NoError(t, os.WriteFile(configFile, []byte(configContent), 0644))

	config, err := services.LoadConfig(configFile)
	require.NoError(t, err)

	assert.Equal(t, models.SendModeS3Upload, config.Services.Send.SendAs)
	assert.Equal(t, "http://seaweedfs:8333", config.Services.Send.S3.Endpoint)
	assert.Equal(t, "us-west-2", config.Services.Send.S3.Region)
	assert.Equal(t, "pipeline-data", config.Services.Send.S3.Bucket)
	assert.Equal(t, "TESTKEY", config.Services.Send.S3.AccessKeyID)
	assert.Equal(t, "TESTSECRET", config.Services.Send.S3.SecretAccessKey)
	assert.True(t, config.Services.Send.S3.UsePathStyle)
	assert.Equal(t, 45*time.Minute, config.Services.Send.S3.Timeout)
}

func TestSendConfigLoading_S3_Defaults(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	jobsDir := filepath.Join(tmpDir, "jobs")
	require.NoError(t, os.MkdirAll(jobsDir, 0755))

	// Omit region and timeout to test defaults
	configContent := `
services:
  send:
    send_as: "s3_upload"
    s3:
      bucket: "my-bucket"
      access_key_id: "KEY"
      secret_access_key: "SECRET"

pipeline:
  enabled_steps:
    - local_import
    - send

retry:
  max_attempts: 3
  initial_backoff_ms: 500
  max_backoff_ms: 5000

jobs_dir: "` + jobsDir + `"
`
	require.NoError(t, os.WriteFile(configFile, []byte(configContent), 0644))

	config, err := services.LoadConfig(configFile)
	require.NoError(t, err)

	// Defaults should be applied
	assert.Equal(t, "eu-central-1", config.Services.Send.S3.Region)
	assert.Equal(t, 30*time.Minute, config.Services.Send.S3.Timeout)
	assert.False(t, config.Services.Send.S3.UsePathStyle)
}

func TestSendConfigLoading_S3_EnvVars(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	jobsDir := filepath.Join(tmpDir, "jobs")
	require.NoError(t, os.MkdirAll(jobsDir, 0755))

	// Set env vars
	t.Setenv("TEST_S3_BUCKET", "env-bucket")
	t.Setenv("TEST_S3_KEY", "env-key-id")
	t.Setenv("TEST_S3_SECRET", "env-secret-key")

	configContent := `
services:
  send:
    send_as: "s3_upload"
    s3:
      bucket: "${TEST_S3_BUCKET}"
      access_key_id: "${TEST_S3_KEY}"
      secret_access_key: "${TEST_S3_SECRET}"

pipeline:
  enabled_steps:
    - local_import
    - send

retry:
  max_attempts: 3
  initial_backoff_ms: 500
  max_backoff_ms: 5000

jobs_dir: "` + jobsDir + `"
`
	require.NoError(t, os.WriteFile(configFile, []byte(configContent), 0644))

	config, err := services.LoadConfig(configFile)
	require.NoError(t, err)

	assert.Equal(t, "env-bucket", config.Services.Send.S3.Bucket)
	assert.Equal(t, "env-key-id", config.Services.Send.S3.AccessKeyID)
	assert.Equal(t, "env-secret-key", config.Services.Send.S3.SecretAccessKey)
}
