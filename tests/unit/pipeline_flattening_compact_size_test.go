package unit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/services/servicestest"
)

// A Bundle entry with whitespace in the input counts, and goes to the
// flattener, as compact JSON.
func TestFlatteningStep_CountsCompactSizeOfIndentedResource(t *testing.T) {
	job, jobDir, inputDir := newBatchingJob(t)
	indented, err := json.MarshalIndent(linkedBundle(paddedPatient(t, "1", 5000)), "      ", "    ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(inputDir, "bundle.ndjson"), append(indented, '\n'), 0o644))

	var received int
	fake := &servicestest.MockFlattener{
		FlattenFunc: func(_ models.ViewDefinition, resources []json.RawMessage) ([][]string, error) {
			for _, r := range resources {
				received += len(r)
			}
			return nil, nil
		},
	}
	installFakeFlattener(t, fake)

	var logBuf strings.Builder
	stopCapture := captureStdoutForTest(t)
	err = runPipelineStep(models.StepFlattening, job, jobDir, lib.NewLoggerWithWriter(lib.LogLevelInfo, &logBuf))
	stopCapture()
	require.NoError(t, err)

	assert.Equal(t, 5000, received)
	assert.Contains(t, logBuf.String(), "total_bytes 5000 ")
}
