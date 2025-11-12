package lib

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
)

func TestValidateStepPrerequisites_ImportHasNone(t *testing.T) {
	job := createTestJob([]models.StepName{models.StepLocalImport})

	prerequisite, canRun := lib.ValidateStepPrerequisites(job, models.StepLocalImport)

	assert.True(t, canRun)
	assert.Equal(t, models.StepName(""), prerequisite)
}

func TestValidateStepPrerequisites_DIMPRequiresImport(t *testing.T) {
	job := createTestJob([]models.StepName{models.StepLocalImport, models.StepDIMP})

	// Import not completed yet
	prerequisite, canRun := lib.ValidateStepPrerequisites(job, models.StepDIMP)

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
	prerequisite, canRun := lib.ValidateStepPrerequisites(job, models.StepDIMP)

	assert.True(t, canRun)
	assert.Equal(t, models.StepName(""), prerequisite)
}

func TestValidateStepPrerequisites_CSVRequiresImport(t *testing.T) {
	job := createTestJob([]models.StepName{models.StepLocalImport, models.StepCSVConversion})

	// Import not completed
	prerequisite, canRun := lib.ValidateStepPrerequisites(job, models.StepCSVConversion)

	assert.False(t, canRun)
	assert.Equal(t, models.StepName("import"), prerequisite) // Returns "import" placeholder
}

func TestValidateStepPrerequisites_CSVAllowedAfterImport(t *testing.T) {
	job := createTestJob([]models.StepName{models.StepLocalImport, models.StepCSVConversion})

	// Mark import as completed
	importStep, _ := models.GetStepByName(job, models.StepLocalImport)
	importStep = models.CompleteStep(importStep, 5, 500)
	job = models.ReplaceStep(job, importStep)

	// CSV conversion should be allowed
	prerequisite, canRun := lib.ValidateStepPrerequisites(job, models.StepCSVConversion)

	assert.True(t, canRun)
	assert.Equal(t, models.StepName(""), prerequisite)
}

func TestValidateStepPrerequisites_ValidationRequiresImport(t *testing.T) {
	job := createTestJob([]models.StepName{models.StepLocalImport, models.StepValidation})

	// Import not completed
	prerequisite, canRun := lib.ValidateStepPrerequisites(job, models.StepValidation)

	assert.False(t, canRun)
	assert.Equal(t, models.StepName("import"), prerequisite) // Returns "import" placeholder
}

func TestValidateStepPrerequisites_PrerequisiteNotEnabled(t *testing.T) {
	// Job without import step (unusual but possible)
	job := createTestJob([]models.StepName{models.StepDIMP})

	// DIMP requires import, but import is not in the step list
	// Validation should fail since no import step is completed
	prerequisite, canRun := lib.ValidateStepPrerequisites(job, models.StepDIMP)

	assert.False(t, canRun) // Import prerequisite not met
	assert.Equal(t, models.StepName("import"), prerequisite)
}

func TestValidateStepPrerequisites_UnknownStep(t *testing.T) {
	job := createTestJob([]models.StepName{models.StepLocalImport})

	// Unknown step name
	prerequisite, canRun := lib.ValidateStepPrerequisites(job, models.StepName("unknown"))

	assert.True(t, canRun) // Unknown step has no prerequisites defined
	assert.Equal(t, models.StepName(""), prerequisite)
}

func TestCanRunStep_Wrapper(t *testing.T) {
	job := createTestJob([]models.StepName{models.StepLocalImport, models.StepDIMP})

	// Import not completed
	canRun, prerequisite := lib.CanRunStep(job, models.StepDIMP)

	assert.False(t, canRun)
	assert.Equal(t, models.StepName("import"), prerequisite) // Returns "import" placeholder
}

func TestGetStepDependencies_Import(t *testing.T) {
	deps := lib.GetStepDependencies(models.StepLocalImport)
	assert.Empty(t, deps)
}

func TestGetStepDependencies_DIMP(t *testing.T) {
	deps := lib.GetStepDependencies(models.StepDIMP)
	require.Len(t, deps, 1)
	assert.Equal(t, models.StepName("import"), deps[0]) // Returns "import" placeholder
}

func TestGetStepDependencies_CSV(t *testing.T) {
	deps := lib.GetStepDependencies(models.StepCSVConversion)
	require.Len(t, deps, 1)
	assert.Equal(t, models.StepName("import"), deps[0]) // Returns "import" placeholder
}

func TestGetStepDependencies_Parquet(t *testing.T) {
	deps := lib.GetStepDependencies(models.StepParquetConversion)
	require.Len(t, deps, 1)
	assert.Equal(t, models.StepName("import"), deps[0]) // Returns "import" placeholder
}

func TestGetStepDependencies_UnknownStep(t *testing.T) {
	deps := lib.GetStepDependencies(models.StepName("nonexistent"))
	assert.Empty(t, deps)
}

// Integration test: Full pipeline flow
func TestValidation_PipelineSequence(t *testing.T) {
	// Full pipeline: Import -> DIMP -> CSV -> Parquet
	job := createTestJob([]models.StepName{
		models.StepLocalImport,
		models.StepDIMP,
		models.StepCSVConversion,
		models.StepParquetConversion,
	})

	// Initially, only import can run
	canRun, _ := lib.CanRunStep(job, models.StepLocalImport)
	assert.True(t, canRun)

	canRun, prereq := lib.CanRunStep(job, models.StepDIMP)
	assert.False(t, canRun)
	assert.Equal(t, models.StepName("import"), prereq) // Returns "import" placeholder

	canRun, prereq = lib.CanRunStep(job, models.StepCSVConversion)
	assert.False(t, canRun)
	assert.Equal(t, models.StepName("import"), prereq) // Returns "import" placeholder

	// Complete import
	importStep, _ := models.GetStepByName(job, models.StepLocalImport)
	importStep = models.CompleteStep(importStep, 10, 1000)
	job = models.ReplaceStep(job, importStep)

	// Now DIMP and CSV can run
	canRun, _ = lib.CanRunStep(job, models.StepDIMP)
	assert.True(t, canRun)

	canRun, _ = lib.CanRunStep(job, models.StepCSVConversion)
	assert.True(t, canRun)

	canRun, _ = lib.CanRunStep(job, models.StepParquetConversion)
	assert.True(t, canRun)

	// Complete DIMP (CSV and Parquet should still be allowed)
	dimpStep, _ := models.GetStepByName(job, models.StepDIMP)
	dimpStep = models.CompleteStep(dimpStep, 10, 900)
	job = models.ReplaceStep(job, dimpStep)

	canRun, _ = lib.CanRunStep(job, models.StepCSVConversion)
	assert.True(t, canRun) // Still allowed (doesn't depend on DIMP)

	canRun, _ = lib.CanRunStep(job, models.StepParquetConversion)
	assert.True(t, canRun)
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
