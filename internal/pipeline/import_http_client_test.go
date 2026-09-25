package pipeline

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/services"
	"github.com/medizininformatik-initiative/aether/internal/services/servicestest"
)

// runTORCHURLImport runs the torch import step for a TORCH result URL and
// returns the HTTP client that the step gives to the TORCH client.
func runTORCHURLImport(t *testing.T, injected *services.HTTPClient) *services.HTTPClient {
	t.Helper()
	var used *services.HTTPClient
	SetExtractorFactoryForTesting(func(_ models.TORCHConfig, httpClient *services.HTTPClient, _ *lib.Logger) services.Extractor {
		used = httpClient
		return &servicestest.MockExtractor{}
	})
	t.Cleanup(ResetExtractorFactory)

	cfg := models.DefaultConfig()
	cfg.JobsDir = t.TempDir()
	cfg.Retry.InitialBackoffMs = 500
	job := &models.PipelineJob{
		JobID:       "550e8400-e29b-41d4-a716-446655440000",
		InputType:   models.InputTypeTORCHURL,
		InputSource: "http://torch.example/fhir/__status/1",
		Config:      cfg,
	}
	ctx := &StepContext{
		Job:        job,
		Layout:     services.NewJobLayoutForDir(t.TempDir(), nil),
		Logger:     lib.NewLogger(lib.LogLevelError),
		HTTPClient: injected,
	}

	_, err := importStep{name: models.StepTorchImport}.Run(ctx)
	require.NoError(t, err)
	return used
}

func TestImportStep_UsesInjectedHTTPClient(t *testing.T) {
	injected := newTestHTTPClient(lib.NewLogger(lib.LogLevelError))

	assert.Same(t, injected, runTORCHURLImport(t, injected))
}

// Without an injected client the step builds one. Its timeout is ten times the
// initial retry backoff, so that a large download does not time out.
func TestImportStep_BuildsHTTPClientWhenNoneInjected(t *testing.T) {
	used := runTORCHURLImport(t, nil)

	require.NotNil(t, used)
	assert.Equal(t, 5*time.Second, used.Timeout())
}
