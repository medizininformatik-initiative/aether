package cmd

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/services/servicestest"
)

const preflightLookup = `[
	{
		"url": "https://example.com/StructureDefinition/TestProfile",
		"resourceType": "Patient",
		"elements": {
			"Patient.name": {"viewDefinition": {"select": [{"column": [{"name": "family", "path": "name.family", "type": "string"}]}]}}
		}
	}
]`

// preflightConfig returns a config that enables the given steps and needs no
// service other than the flattener.
func preflightConfig(t *testing.T, flattenerURL string, steps ...models.StepName) *models.ProjectConfig {
	t.Helper()
	lookupPath := filepath.Join(t.TempDir(), "flatten-lookup.json")
	require.NoError(t, os.WriteFile(lookupPath, []byte(preflightLookup), 0o644))

	config := models.DefaultConfig()
	config.Pipeline.EnabledSteps = steps
	config.Services.TORCH.BaseURL = ""
	config.Services.Flattening.LookupPath = lookupPath
	config.Services.Flattening.ServiceURL = flattenerURL
	config.Retry = models.RetryConfig{MaxAttempts: 1, InitialBackoffMs: 1, MaxBackoffMs: 1}
	return &config
}

func metadataServer(t *testing.T, status int, requests *atomic.Int32) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/fhir/metadata" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		requests.Add(1)
		w.WriteHeader(status)
		if status == http.StatusOK {
			_, _ = w.Write([]byte(servicestest.FlattenerCapabilityStatement))
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func TestPreflightStart_FailsWhenFlattenerDoesNotAnswer(t *testing.T) {
	var requests atomic.Int32
	server := metadataServer(t, http.StatusInternalServerError, &requests)
	config := preflightConfig(t, server.URL, models.StepLocalImport, models.StepFlattening)

	_, err := preflightStart(config, startCRTDLFixture, lib.NewLogger(lib.LogLevelError))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "flattener check failed")
	assert.Contains(t, err.Error(), "services.flattening.service_url ("+server.URL+")")
	assert.Equal(t, int32(1), requests.Load())
}

func TestPreflightStart_PassesWhenFlattenerAnswers(t *testing.T) {
	var requests atomic.Int32
	server := metadataServer(t, http.StatusOK, &requests)
	config := preflightConfig(t, server.URL, models.StepLocalImport, models.StepFlattening)

	_, err := preflightStart(config, startCRTDLFixture, lib.NewLogger(lib.LogLevelError))
	require.NoError(t, err)
	assert.Equal(t, int32(1), requests.Load())
}

func TestPreflightStart_FailsWhenServiceIsNotAFlattener(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"resourceType":"CapabilityStatement","rest":[{"mode":"server","resource":[{"type":"Patient"}]}]}`))
	}))
	t.Cleanup(server.Close)
	config := preflightConfig(t, server.URL, models.StepLocalImport, models.StepFlattening)

	_, err := preflightStart(config, startCRTDLFixture, lib.NewLogger(lib.LogLevelError))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "flattener check failed")
	assert.Contains(t, err.Error(), "is not a flattener")
}

func TestPreflightStart_DoesNotProbeFlattenerWhenStepDisabled(t *testing.T) {
	var requests atomic.Int32
	server := metadataServer(t, http.StatusInternalServerError, &requests)
	config := preflightConfig(t, server.URL, models.StepLocalImport)

	_, err := preflightStart(config, startCRTDLFixture, lib.NewLogger(lib.LogLevelError))
	require.NoError(t, err)
	assert.Equal(t, int32(0), requests.Load())
}

func TestPreflightStart_FailsWhenFlattenerRefusesConnection(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	closedURL := server.URL
	server.Close()
	config := preflightConfig(t, closedURL, models.StepLocalImport, models.StepFlattening)

	_, err := preflightStart(config, startCRTDLFixture, lib.NewLogger(lib.LogLevelError))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "flattener check failed")
	assert.Contains(t, err.Error(), "does not answer at")
}

func TestPreflightStart_FailsWhenFlattenerURLIsMissing(t *testing.T) {
	config := preflightConfig(t, "", models.StepLocalImport, models.StepFlattening)

	_, err := preflightStart(config, startCRTDLFixture, lib.NewLogger(lib.LogLevelError))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "flattener check failed")
	assert.Contains(t, err.Error(), "service_url")
}
