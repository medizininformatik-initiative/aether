package integration

import (
	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/pipeline"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

// runPipelineStep drives one pipeline step through the exported RunStep seam for
// black-box tests that previously called a per-step Execute*Step wrapper.
func runPipelineStep(name models.StepName, job *models.PipelineJob, jobDir string, logger *lib.Logger) error {
	return pipeline.RunStep(name, &pipeline.StepContext{Job: job, Layout: services.NewJobLayoutForDir(jobDir, job.Config.Pipeline.EnabledSteps), Logger: logger})
}

// runImportStep drives the import step through RunStep, selecting the import
// variant from job.CurrentStep and returning the (in-place mutated) job to match
// the shape black-box import tests expect.
func runImportStep(job *models.PipelineJob, logger *lib.Logger, httpClient *services.HTTPClient, showProgress bool) (*models.PipelineJob, error) {
	err := pipeline.RunStep(models.StepName(job.CurrentStep), &pipeline.StepContext{
		Job:          job,
		Layout:       services.NewJobLayout(job.Config.JobsDir, job.JobID, job.Config.Pipeline.EnabledSteps),
		Logger:       logger,
		HTTPClient:   httpClient,
		ShowProgress: showProgress,
	})
	return job, err
}
