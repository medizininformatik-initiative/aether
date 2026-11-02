package models

// StepPrerequisites defines which steps must complete before a given step can run
// Note: "import" is a placeholder representing any import step
var StepPrerequisites = map[StepName][]StepName{
	StepTorchImport: {},         // No prerequisites - can always run
	StepLocalImport: {},         // No prerequisites - can always run
	StepHttpImport:  {},         // No prerequisites - can always run
	StepCSVImport:   {},         // No prerequisites - can always run
	StepDIMP:        {"import"}, // Requires any import step to complete
	StepValidation:  {"import"}, // Can validate after import (regardless of DIMP)
	StepFlattening:  {"import"}, // Flattening needs imported FHIR input to transform
	StepWait:        {"import"}, // Wait requires at least one step to have run
	StepSend:        {"import"}, // Send requires at least import to have run
}

// ValidateStepPrerequisites checks if all prerequisite steps have completed successfully
// Returns the first missing prerequisite, or empty string if all prerequisites are met
func ValidateStepPrerequisites(job PipelineJob, stepName StepName) (StepName, bool) {
	prerequisites, exists := StepPrerequisites[stepName]
	if !exists {
		// Unknown step - no prerequisites defined
		return "", true
	}

	for _, prerequisite := range prerequisites {
		// "import" means any import step type
		if prerequisite == "import" {
			if !hasCompletedImport(job) {
				return "import", false
			}
			continue
		}

		step, found := GetStepByName(job, prerequisite)
		if !found {
			// Prerequisite step doesn't exist in job's step list
			// This means it's not enabled in config, which is fine
			continue
		}

		if step.Status != StepStatusCompleted {
			return prerequisite, false
		}
	}

	return "", true
}

// hasCompletedImport reports whether any import step of the job has completed.
func hasCompletedImport(job PipelineJob) bool {
	for _, step := range job.Steps {
		if step.Name.IsImport() && step.Status == StepStatusCompleted {
			return true
		}
	}
	return false
}

// CanRunStep checks if a step can be executed based on current job state
// Returns true if the step can run, false otherwise with the blocking prerequisite
func CanRunStep(job PipelineJob, stepName StepName) (bool, StepName) {
	prerequisite, canRun := ValidateStepPrerequisites(job, stepName)
	return canRun, prerequisite
}

// GetStepDependencies returns the list of steps that must complete before the given step
func GetStepDependencies(stepName StepName) []StepName {
	deps, exists := StepPrerequisites[stepName]
	if !exists {
		return []StepName{}
	}
	return deps
}
