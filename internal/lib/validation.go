package lib

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/medizininformatik-initiative/aether/internal/models"
)

// StepPrerequisites defines which steps must complete before a given step can run
// Note: "import" is a placeholder representing any of the three import step types
var StepPrerequisites = map[models.StepName][]models.StepName{
	models.StepTorchImport:       {},         // No prerequisites - can always run
	models.StepLocalImport:       {},         // No prerequisites - can always run
	models.StepHttpImport:        {},         // No prerequisites - can always run
	models.StepDIMP:              {"import"}, // Requires any import step to complete
	models.StepValidation:        {"import"}, // Can validate after import (regardless of DIMP)
	models.StepCSVConversion:     {"import"}, // Can convert original or pseudonymized data
	models.StepParquetConversion: {"import"}, // Can convert original or pseudonymized data
}

// ValidateStepPrerequisites checks if all prerequisite steps have completed successfully
// Returns the first missing prerequisite, or empty string if all prerequisites are met
func ValidateStepPrerequisites(job models.PipelineJob, stepName models.StepName) (models.StepName, bool) {
	prerequisites, exists := StepPrerequisites[stepName]
	if !exists {
		// Unknown step - no prerequisites defined
		return "", true
	}

	// Check each prerequisite
	for _, prerequisite := range prerequisites {
		// Special case: "import" means any import step type
		if prerequisite == "import" {
			// Check if any import step is completed
			importCompleted := false
			for _, importStep := range []models.StepName{models.StepTorchImport, models.StepLocalImport, models.StepHttpImport} {
				step, found := models.GetStepByName(job, importStep)
				if found && step.Status == models.StepStatusCompleted {
					importCompleted = true
					break
				}
			}
			if !importCompleted {
				return "import", false
			}
			continue
		}

		step, found := models.GetStepByName(job, prerequisite)
		if !found {
			// Prerequisite step doesn't exist in job's step list
			// This means it's not enabled in config, which is fine
			continue
		}

		// Prerequisite exists but hasn't completed
		if step.Status != models.StepStatusCompleted {
			return prerequisite, false
		}
	}

	// All prerequisites met
	return "", true
}

// CanRunStep checks if a step can be executed based on current job state
// Returns true if the step can run, false otherwise with the blocking prerequisite
func CanRunStep(job models.PipelineJob, stepName models.StepName) (bool, models.StepName) {
	prerequisite, canRun := ValidateStepPrerequisites(job, stepName)
	return canRun, prerequisite
}

// GetStepDependencies returns the list of steps that must complete before the given step
func GetStepDependencies(stepName models.StepName) []models.StepName {
	deps, exists := StepPrerequisites[stepName]
	if !exists {
		return []models.StepName{}
	}
	return deps
}

// DetectInputType determines the input source type from the input string
// Returns InputTypeLocal for directories, InputTypeHTTP for HTTP URLs,
// InputTypeTORCHURL for TORCH result URLs, InputTypeCRTDL for CRTDL files
func DetectInputType(inputSource string) (models.InputType, error) {
	if inputSource == "" {
		return "", fmt.Errorf("input source cannot be empty")
	}

	// Check if directory
	if stat, err := os.Stat(inputSource); err == nil && stat.IsDir() {
		return models.InputTypeLocal, nil
	}

	// Check if HTTP URL
	if strings.HasPrefix(inputSource, "http://") || strings.HasPrefix(inputSource, "https://") {
		// Check if TORCH result URL pattern (contains /fhir/extraction/ or /fhir/result/)
		if strings.Contains(inputSource, "/fhir/extraction/") || strings.Contains(inputSource, "/fhir/result/") {
			return models.InputTypeTORCHURL, nil
		}
		return models.InputTypeHTTP, nil
	}

	// Check if CRTDL file
	if strings.HasSuffix(inputSource, ".crtdl") || strings.HasSuffix(inputSource, ".json") {
		isCRTDL, _ := IsCRTDLFileWithHint(inputSource)
		if isCRTDL {
			return models.InputTypeCRTDL, nil
		}
		// If it's a JSON/CRTDL file but not valid CRTDL, default to local type
		// CRTDL validation errors will be caught during job creation, not during detection
		return models.InputTypeLocal, nil
	}

	// Default to local path (backward compatibility)
	// Validation of path existence and type happens later in ValidateImportSource
	return models.InputTypeLocal, nil
}

// IsCRTDLFile checks if the file at the given path is a valid CRTDL file
// by verifying it contains required cohortDefinition and dataExtraction keys
func IsCRTDLFile(path string) bool {
	isCRTDL, _ := IsCRTDLFileWithHint(path)
	return isCRTDL
}

// IsCRTDLFileWithHint checks if the file is a valid CRTDL and provides a hint if not
// Returns (isValid, hint) where hint explains what's wrong with the structure
func IsCRTDLFileWithHint(path string) (bool, string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, fmt.Sprintf("cannot read file: %v", err)
	}

	var crtdl map[string]any
	if err := json.Unmarshal(data, &crtdl); err != nil {
		return false, fmt.Sprintf("not valid JSON: %v", err)
	}

	// Check for FHIR Parameters format (newer format)
	if resourceType, ok := crtdl["resourceType"].(string); ok && resourceType == "Parameters" {
		return false, "file uses FHIR Parameters format - please convert to flat CRTDL structure (see example-crtdl.json)"
	}

	// Verify required CRTDL structure
	_, hasCohort := crtdl["cohortDefinition"]
	_, hasExtraction := crtdl["dataExtraction"]

	if !hasCohort && !hasExtraction {
		return false, "missing both 'cohortDefinition' and 'dataExtraction' keys"
	}
	if !hasCohort {
		return false, "missing 'cohortDefinition' key"
	}
	if !hasExtraction {
		return false, "missing 'dataExtraction' key"
	}

	return true, ""
}

// ValidateCRTDLSyntax validates the syntax of a CRTDL file
// Performs structural validation only - semantic validation is handled by TORCH server
func ValidateCRTDLSyntax(crtdlPath string) error {
	data, err := os.ReadFile(crtdlPath)
	if err != nil {
		return fmt.Errorf("failed to read CRTDL file '%s': %w", crtdlPath, err)
	}

	if len(data) == 0 {
		return fmt.Errorf("CRTDL file '%s' is empty", crtdlPath)
	}

	var crtdl map[string]any
	if err := json.Unmarshal(data, &crtdl); err != nil {
		return fmt.Errorf("CRTDL file '%s' contains invalid JSON: %w\n\nPlease ensure the file is valid JSON format", crtdlPath, err)
	}

	// Check for FHIR Parameters format (common mistake)
	if resourceType, ok := crtdl["resourceType"].(string); ok && resourceType == "Parameters" {
		return fmt.Errorf("CRTDL file '%s' uses FHIR Parameters format\n\nThis format is not supported. Please convert to flat CRTDL structure:\n{\n  \"cohortDefinition\": { \"inclusionCriteria\": [...] },\n  \"dataExtraction\": { \"attributeGroups\": [...] }\n}\n\nSee .github/test/torch/queries/example-crtdl.json for reference", crtdlPath)
	}

	// Check required keys
	cohort, hasCohort := crtdl["cohortDefinition"]
	if !hasCohort {
		availableKeys := make([]string, 0, len(crtdl))
		for k := range crtdl {
			availableKeys = append(availableKeys, k)
		}
		return fmt.Errorf("CRTDL file '%s' missing required key: 'cohortDefinition'\n\nFound keys: %v\n\nExpected structure:\n{\n  \"cohortDefinition\": { \"inclusionCriteria\": [...] },\n  \"dataExtraction\": { \"attributeGroups\": [...] }\n}", crtdlPath, availableKeys)
	}

	extraction, hasExtraction := crtdl["dataExtraction"]
	if !hasExtraction {
		availableKeys := make([]string, 0, len(crtdl))
		for k := range crtdl {
			availableKeys = append(availableKeys, k)
		}
		return fmt.Errorf("CRTDL file '%s' missing required key: 'dataExtraction'\n\nFound keys: %v\n\nExpected structure:\n{\n  \"cohortDefinition\": { \"inclusionCriteria\": [...] },\n  \"dataExtraction\": { \"attributeGroups\": [...] }\n}", crtdlPath, availableKeys)
	}

	// Validate cohortDefinition structure
	cohortMap, ok := cohort.(map[string]any)
	if !ok {
		return fmt.Errorf("CRTDL file '%s': 'cohortDefinition' must be an object, got %T", crtdlPath, cohort)
	}
	if _, hasInclusion := cohortMap["inclusionCriteria"]; !hasInclusion {
		cohortKeys := make([]string, 0, len(cohortMap))
		for k := range cohortMap {
			cohortKeys = append(cohortKeys, k)
		}
		return fmt.Errorf("CRTDL file '%s': cohortDefinition missing 'inclusionCriteria'\n\nFound keys in cohortDefinition: %v\n\nExpected: { \"inclusionCriteria\": [[...]] }", crtdlPath, cohortKeys)
	}

	// Validate dataExtraction structure
	extractionMap, ok := extraction.(map[string]any)
	if !ok {
		return fmt.Errorf("CRTDL file '%s': 'dataExtraction' must be an object, got %T", crtdlPath, extraction)
	}
	if _, hasGroups := extractionMap["attributeGroups"]; !hasGroups {
		extractionKeys := make([]string, 0, len(extractionMap))
		for k := range extractionMap {
			extractionKeys = append(extractionKeys, k)
		}
		return fmt.Errorf("CRTDL file '%s': dataExtraction missing 'attributeGroups'\n\nFound keys in dataExtraction: %v\n\nExpected: { \"attributeGroups\": [...] }", crtdlPath, extractionKeys)
	}

	return nil
}

// ValidateSplitConfig validates the Bundle split threshold configuration
// Ensures threshold is positive, within limits, and logs warnings if appropriate
func ValidateSplitConfig(thresholdMB int) error {
	if thresholdMB <= 0 {
		return fmt.Errorf("bundle_split_threshold_mb must be > 0, got %d", thresholdMB)
	}

	if thresholdMB > 100 {
		return fmt.Errorf("bundle_split_threshold_mb must be <= 100MB, got %d (likely misconfiguration)", thresholdMB)
	}

	// Note: Values > 50MB should trigger a warning at runtime in the pipeline step
	// This function only validates the value itself

	return nil
}

// DetectOversizedResource checks if a non-Bundle resource exceeds the threshold
// Returns OversizedResourceError if the resource is too large, nil otherwise
func DetectOversizedResource(resource map[string]any, thresholdBytes int) *models.OversizedResourceError {
	// Bundle resources are handled by the splitting logic, not this function
	if resourceType, ok := resource["resourceType"].(string); ok && resourceType == "Bundle" {
		return nil // Bundles are handled separately
	}

	// Calculate resource size
	jsonData, err := json.Marshal(resource)
	if err != nil {
		// If we can't marshal, assume it's okay (error will be caught elsewhere)
		return nil
	}

	resourceSize := len(jsonData)
	if resourceSize > thresholdBytes {
		resourceType := "Unknown"
		resourceID := "unknown"

		if rt, ok := resource["resourceType"].(string); ok {
			resourceType = rt
		}
		if id, ok := resource["id"].(string); ok {
			resourceID = id
		}

		guidance := fmt.Sprintf(
			"This resource cannot be split without violating FHIR semantics. " +
				"Solutions: (1) Review data quality - resource may contain unnecessary data; " +
				"(2) Increase DIMP server payload limit; (3) Increase bundle_split_threshold_mb configuration.",
		)

		return &models.OversizedResourceError{
			ResourceType: resourceType,
			ResourceID:   resourceID,
			Size:         resourceSize,
			Threshold:    thresholdBytes,
			Guidance:     guidance,
		}
	}

	return nil
}
