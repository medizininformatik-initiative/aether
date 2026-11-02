package models

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/medizininformatik-initiative/aether/internal/models"
)

func TestValidateStepPrerequisites_CSVImportHasNone(t *testing.T) {
	job := createTestJob([]models.StepName{models.StepCSVImport, models.StepSend})

	prerequisite, canRun := models.ValidateStepPrerequisites(job, models.StepCSVImport)

	assert.True(t, canRun)
	assert.Equal(t, models.StepName(""), prerequisite)
}

func TestValidateStepPrerequisites_SendRequiresCSVImport(t *testing.T) {
	job := createTestJob([]models.StepName{models.StepCSVImport, models.StepSend})

	_, canRun := models.ValidateStepPrerequisites(job, models.StepSend)
	assert.False(t, canRun)

	importStep, _ := models.GetStepByName(job, models.StepCSVImport)
	job = models.ReplaceStep(job, models.CompleteStep(importStep, 10, 1000))

	prerequisite, canRun := models.ValidateStepPrerequisites(job, models.StepSend)
	assert.True(t, canRun)
	assert.Equal(t, models.StepName(""), prerequisite)
}
