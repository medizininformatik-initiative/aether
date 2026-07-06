package pipeline

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/models"
)

func TestStepRegistry_ResolvesEveryStep(t *testing.T) {
	names := []models.StepName{
		models.StepTorchImport, models.StepLocalImport, models.StepHttpImport,
		models.StepDIMP, models.StepValidation, models.StepFlattening,
		models.StepSend, models.StepWait,
	}
	for _, n := range names {
		s, ok := StepFor(n)
		require.True(t, ok, "expected registry entry for %s", n)
		assert.Equal(t, n, s.Name(), "registered step should report its own name")
	}
}

func TestStepRegistry_UnknownStep(t *testing.T) {
	_, ok := StepFor(models.StepName("bogus"))
	assert.False(t, ok)
}

func TestStepRegistry_OverrideForTesting(t *testing.T) {
	fake := &fakeStep{name: models.StepDIMP}
	SetStepRegistryForTesting(func(models.StepName) (Step, bool) { return fake, true })
	defer ResetStepRegistry()

	s, ok := StepFor(models.StepDIMP)
	require.True(t, ok)
	assert.Same(t, fake, s)
}
