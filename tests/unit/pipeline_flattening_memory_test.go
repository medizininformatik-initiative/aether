package unit

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/services/servicestest"
)

// memoryTestPatient returns a Patient with many small nested values, like a
// real resource. The decoded form of such a resource is several times larger
// than its JSON text.
func memoryTestPatient(i int) map[string]any {
	names := make([]any, 3)
	for n := range names {
		names[n] = map[string]any{
			"use":    "official",
			"family": fmt.Sprintf("Family%d-%d", i, n),
			"given":  []any{"Anna", "Maria", "Sophie"},
		}
	}
	extensions := make([]any, 12)
	for n := range extensions {
		extensions[n] = map[string]any{
			"url":         fmt.Sprintf("https://example.com/extension/%d", n),
			"valueString": fmt.Sprintf("value-%d-%d", i, n),
		}
	}
	addresses := make([]any, 3)
	for n := range addresses {
		addresses[n] = map[string]any{
			"use":        "home",
			"line":       []any{"Musterstrasse 1"},
			"city":       "Musterstadt",
			"postalCode": "12345",
			"country":    "DE",
		}
	}
	p := batchingPatient(fmt.Sprintf("p%d", i))
	p["name"] = names
	p["extension"] = extensions
	p["address"] = addresses
	p["birthDate"] = "1970-01-01"
	p["gender"] = "female"
	return p
}

// writeMemoryTestInput writes count Patients to inputDir, in Bundles of 100
// Patients with their Provenance.
func writeMemoryTestInput(t *testing.T, inputDir string, count int) {
	t.Helper()
	f, err := os.Create(filepath.Join(inputDir, "bundles.ndjson"))
	require.NoError(t, err)
	defer func() { require.NoError(t, f.Close()) }()
	w := bufio.NewWriter(f)
	enc := json.NewEncoder(w)
	patients := make([]map[string]any, 0, 100)
	for i := range count {
		patients = append(patients, memoryTestPatient(i))
		if len(patients) == cap(patients) || i == count-1 {
			require.NoError(t, enc.Encode(linkedBundle(patients...)))
			patients = patients[:0]
		}
	}
	require.NoError(t, w.Flush())
}

// A pending batch holds the JSON text of its resources, so the heap that the
// batch needs stays near the size of that text.
func TestFlatteningStep_BatchHeapStaysNearItsJSONSize(t *testing.T) {
	const (
		patientCount = 30_000
		batchMB      = 20
	)
	job, jobDir, inputDir := newBatchingJob(t)
	job.Config.Services.Flattening.BatchSizeMB = batchMB
	writeMemoryTestInput(t, inputDir, patientCount)

	var (
		baseline   uint64
		measured   bool
		heapGrowth int64
		batchBytes int
		calls      int
	)
	fake := &servicestest.MockFlattener{
		FlattenFunc: func(_ models.ViewDefinition, resources []json.RawMessage) ([][]string, error) {
			calls++
			if measured {
				return nil, nil
			}
			measured = true
			for _, r := range resources {
				batchBytes += len(r)
			}
			var m runtime.MemStats
			runtime.GC()
			runtime.ReadMemStats(&m)
			heapGrowth = int64(m.HeapAlloc) - int64(baseline)
			return nil, nil
		},
	}
	installFakeFlattener(t, fake)

	var m runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m)
	baseline = m.HeapAlloc

	stopCapture := captureStdoutForTest(t)
	err := runPipelineStep(models.StepFlattening, job, jobDir, lib.NewLogger(lib.LogLevelError))
	stopCapture()
	require.NoError(t, err)

	require.GreaterOrEqual(t, calls, 2, "the input must be larger than one batch")
	require.GreaterOrEqual(t, batchBytes, batchMB*oneMiB, "a batch flushes when it reaches the limit")
	require.Less(t, batchBytes, batchMB*oneMiB+4096, "a batch flushes at the first resource that reaches the limit")
	ratio := float64(heapGrowth) / float64(batchBytes)
	t.Logf("batch bytes %d, heap growth %d, ratio %.2f", batchBytes, heapGrowth, ratio)
	assert.Greater(t, ratio, 0.5, "the measurement must see the batch")
	assert.Less(t, ratio, 2.0)
}
