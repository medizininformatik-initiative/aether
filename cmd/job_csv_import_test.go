package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/models"
)

func TestValidateStepName_AcceptsCSVImport(t *testing.T) {
	step, err := validateStepName("csv_import")

	require.NoError(t, err)
	assert.Equal(t, models.StepCSVImport, step)
}

func TestIsImportStep_CSVImport(t *testing.T) {
	assert.True(t, isImportStep(models.StepCSVImport))
}

func TestValidateStepName_RejectsUnknownName(t *testing.T) {
	_, err := validateStepName("no_such_step")

	require.Error(t, err)
}
