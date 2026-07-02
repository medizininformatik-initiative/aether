package models

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/models"
)

func TestValidateStepPrerequisites_ImportHasNone(t *testing.T) {
	job := createTestJob([]models.StepName{models.StepLocalImport})

	prerequisite, canRun := models.ValidateStepPrerequisites(job, models.StepLocalImport)

	assert.True(t, canRun)
	assert.Equal(t, models.StepName(""), prerequisite)
}

func TestValidateStepPrerequisites_DIMPRequiresImport(t *testing.T) {
	job := createTestJob([]models.StepName{models.StepLocalImport, models.StepDIMP})

	// Import not completed yet
	prerequisite, canRun := models.ValidateStepPrerequisites(job, models.StepDIMP)

	assert.False(t, canRun)
	assert.Equal(t, models.StepName("import"), prerequisite) // Returns "import" placeholder
}

func TestValidateStepPrerequisites_DIMPAllowedAfterImport(t *testing.T) {
	job := createTestJob([]models.StepName{models.StepLocalImport, models.StepDIMP})

	// Mark import as completed
	importStep, _ := models.GetStepByName(job, models.StepLocalImport)
	importStep = models.CompleteStep(importStep, 10, 1000)
	job = models.ReplaceStep(job, importStep)

	// Now DIMP should be allowed
	prerequisite, canRun := models.ValidateStepPrerequisites(job, models.StepDIMP)

	assert.True(t, canRun)
	assert.Equal(t, models.StepName(""), prerequisite)
}

func TestValidateStepPrerequisites_ValidationRequiresImport(t *testing.T) {
	job := createTestJob([]models.StepName{models.StepLocalImport, models.StepValidation})

	// Import not completed
	prerequisite, canRun := models.ValidateStepPrerequisites(job, models.StepValidation)

	assert.False(t, canRun)
	assert.Equal(t, models.StepName("import"), prerequisite) // Returns "import" placeholder
}

func TestValidateStepPrerequisites_PrerequisiteNotEnabled(t *testing.T) {
	// Job without import step (unusual but possible)
	job := createTestJob([]models.StepName{models.StepDIMP})

	// DIMP requires import, but import is not in the step list
	// Validation should fail since no import step is completed
	prerequisite, canRun := models.ValidateStepPrerequisites(job, models.StepDIMP)

	assert.False(t, canRun) // Import prerequisite not met
	assert.Equal(t, models.StepName("import"), prerequisite)
}

func TestValidateStepPrerequisites_UnknownStep(t *testing.T) {
	job := createTestJob([]models.StepName{models.StepLocalImport})

	// Unknown step name
	prerequisite, canRun := models.ValidateStepPrerequisites(job, models.StepName("unknown"))

	assert.True(t, canRun) // Unknown step has no prerequisites defined
	assert.Equal(t, models.StepName(""), prerequisite)
}

func TestCanRunStep_Wrapper(t *testing.T) {
	job := createTestJob([]models.StepName{models.StepLocalImport, models.StepDIMP})

	// Import not completed
	canRun, prerequisite := models.CanRunStep(job, models.StepDIMP)

	assert.False(t, canRun)
	assert.Equal(t, models.StepName("import"), prerequisite) // Returns "import" placeholder
}

func TestGetStepDependencies_Import(t *testing.T) {
	deps := models.GetStepDependencies(models.StepLocalImport)
	assert.Empty(t, deps)
}

func TestGetStepDependencies_DIMP(t *testing.T) {
	deps := models.GetStepDependencies(models.StepDIMP)
	require.Len(t, deps, 1)
	assert.Equal(t, models.StepName("import"), deps[0]) // Returns "import" placeholder
}

func TestGetStepDependencies_UnknownStep(t *testing.T) {
	deps := models.GetStepDependencies(models.StepName("nonexistent"))
	assert.Empty(t, deps)
}

// Integration test: Full pipeline flow
func TestValidation_PipelineSequence(t *testing.T) {
	// Full pipeline: Import -> DIMP -> Flattening
	job := createTestJob([]models.StepName{
		models.StepLocalImport,
		models.StepDIMP,
		models.StepFlattening,
	})

	// Initially, only import can run
	canRun, _ := models.CanRunStep(job, models.StepLocalImport)
	assert.True(t, canRun)

	canRun, prereq := models.CanRunStep(job, models.StepDIMP)
	assert.False(t, canRun)
	assert.Equal(t, models.StepName("import"), prereq) // Returns "import" placeholder

	// Complete import
	importStep, _ := models.GetStepByName(job, models.StepLocalImport)
	importStep = models.CompleteStep(importStep, 10, 1000)
	job = models.ReplaceStep(job, importStep)

	// Now DIMP can run
	canRun, _ = models.CanRunStep(job, models.StepDIMP)
	assert.True(t, canRun)

}

// withConcretePrerequisite temporarily wires a concrete (non-"import") prerequisite
// into the global StepPrerequisites map so the explicit-step branch of
// ValidateStepPrerequisites can be exercised, then restores the original state.
func withConcretePrerequisite(t *testing.T, step models.StepName, prereqs []models.StepName) {
	t.Helper()
	original, existed := models.StepPrerequisites[step]
	models.StepPrerequisites[step] = prereqs
	t.Cleanup(func() {
		if existed {
			models.StepPrerequisites[step] = original
		} else {
			delete(models.StepPrerequisites, step)
		}
	})
}

func TestValidateStepPrerequisites_ConcretePrereqNotEnabled(t *testing.T) {
	withConcretePrerequisite(t, models.StepFlattening, []models.StepName{models.StepDIMP})

	// Job has Flattening but not the concrete DIMP prerequisite step
	job := createTestJob([]models.StepName{models.StepFlattening})

	prerequisite, canRun := models.ValidateStepPrerequisites(job, models.StepFlattening)

	// Prerequisite step not present in job -> treated as not enabled, allowed
	assert.True(t, canRun)
	assert.Equal(t, models.StepName(""), prerequisite)
}

func TestValidateStepPrerequisites_ConcretePrereqNotCompleted(t *testing.T) {
	withConcretePrerequisite(t, models.StepFlattening, []models.StepName{models.StepDIMP})

	// DIMP present but not completed
	job := createTestJob([]models.StepName{models.StepDIMP, models.StepFlattening})

	prerequisite, canRun := models.ValidateStepPrerequisites(job, models.StepFlattening)

	assert.False(t, canRun)
	assert.Equal(t, models.StepDIMP, prerequisite)
}

func TestValidateStepPrerequisites_ConcretePrereqCompleted(t *testing.T) {
	withConcretePrerequisite(t, models.StepFlattening, []models.StepName{models.StepDIMP})

	job := createTestJob([]models.StepName{models.StepDIMP, models.StepFlattening})

	// Complete the concrete prerequisite
	dimpStep, _ := models.GetStepByName(job, models.StepDIMP)
	dimpStep = models.CompleteStep(dimpStep, 5, 500)
	job = models.ReplaceStep(job, dimpStep)

	prerequisite, canRun := models.ValidateStepPrerequisites(job, models.StepFlattening)

	assert.True(t, canRun)
	assert.Equal(t, models.StepName(""), prerequisite)
}

// Helper: Create a test job with given steps
func createTestJob(steps []models.StepName) models.PipelineJob {
	now := time.Now()
	return models.PipelineJob{
		JobID:       "test-job-123",
		CreatedAt:   now,
		UpdatedAt:   now,
		InputSource: "/test/input",
		InputType:   models.InputTypeLocal,
		CurrentStep: string(steps[0]),
		Status:      models.JobStatusInProgress,
		Steps:       models.InitializeSteps(steps),
		Config: models.ProjectConfig{
			Pipeline: models.PipelineConfig{
				EnabledSteps: steps,
			},
		},
	}
}
