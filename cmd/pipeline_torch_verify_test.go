package cmd

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
)

func TestVerifyTORCHServer_RejectsWrongServerWhenTORCHEnabled(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(server.Close)

	config := models.DefaultConfig()
	config.Pipeline.EnabledSteps = []models.StepName{models.StepTorchImport}
	config.Services.TORCH.BaseURL = server.URL

	err := verifyTORCHServer(&config, lib.DefaultLogger)
	require.Error(t, err)
	assert.Contains(t, err.Error(), server.URL+"/fhir/metadata")
	assert.Contains(t, err.Error(), "HTTP 404")
	assert.Contains(t, err.Error(), "services.torch.base_url")
}

func TestVerifyTORCHServer_AcceptsTORCH(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"resourceType":"CapabilityStatement","software":{"name":"Torch"}}`))
	}))
	t.Cleanup(server.Close)

	config := models.DefaultConfig()
	config.Pipeline.EnabledSteps = []models.StepName{models.StepTorchImport}
	config.Services.TORCH.BaseURL = server.URL

	assert.NoError(t, verifyTORCHServer(&config, lib.DefaultLogger))
}

func TestVerifyTORCHServer_SkipsCheckWhenTORCHDisabled(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(server.Close)

	config := models.DefaultConfig()
	config.Pipeline.EnabledSteps = []models.StepName{models.StepLocalImport}
	config.Services.TORCH.BaseURL = server.URL

	assert.NoError(t, verifyTORCHServer(&config, lib.DefaultLogger))
}
