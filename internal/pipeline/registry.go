package pipeline

import "github.com/medizininformatik-initiative/aether/internal/models"

// stepRegistry maps every pipeline step name to its Step implementation. The
// three import names share importStep, each carrying its own name; send's three
// sub-modes live inside sendStep.Run.
var stepRegistry = map[models.StepName]Step{
	models.StepTorchImport: importStep{name: models.StepTorchImport},
	models.StepLocalImport: importStep{name: models.StepLocalImport},
	models.StepHttpImport:  importStep{name: models.StepHttpImport},
	models.StepDIMP:        dimpStep{},
	models.StepValidation:  validationStep{},
	models.StepFlattening:  flatteningStep{},
	models.StepSend:        sendStep{},
	models.StepWait:        waitStep{},
}

func defaultStepLookup(name models.StepName) (Step, bool) {
	s, ok := stepRegistry[name]
	return s, ok
}

// stepLookup resolves a step name to its Step. Overridable in tests so the
// orchestration loop can run against fake steps.
var stepLookup = defaultStepLookup

// StepFor returns the Step registered for name. It is the exported registry
// accessor used by the black-box tests; production dispatch (RunLoop) goes through
// the unexported stepLookup directly, so this has no production caller.
// To be reconsidered alongside the pipeline.RunStep unification — see issue #516.
func StepFor(name models.StepName) (Step, bool) { return stepLookup(name) }

// SetStepRegistryForTesting replaces the step lookup for tests.
func SetStepRegistryForTesting(lookup func(models.StepName) (Step, bool)) { stepLookup = lookup }

// ResetStepRegistry restores the default step lookup.
func ResetStepRegistry() { stepLookup = defaultStepLookup }
