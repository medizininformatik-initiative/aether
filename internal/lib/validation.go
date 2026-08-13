package lib

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/medizininformatik-initiative/aether/internal/models"
)

// DetectInputType determines the input source type from the input string
// Returns InputTypeLocal for directories, InputTypeHTTP for HTTP URLs,
// InputTypeTORCHURL for TORCH result URLs, InputTypeCRTDL for CRTDL files
func DetectInputType(inputSource string) (models.InputType, error) {
	if inputSource == "" {
		return "", fmt.Errorf("input source cannot be empty")
	}

	if stat, err := os.Stat(inputSource); err == nil && stat.IsDir() {
		return models.InputTypeLocal, nil
	}

	if strings.HasPrefix(inputSource, "http://") || strings.HasPrefix(inputSource, "https://") {
		// TORCH result URLs contain /fhir/extraction/ or /fhir/result/
		if strings.Contains(inputSource, "/fhir/extraction/") || strings.Contains(inputSource, "/fhir/result/") {
			return models.InputTypeTORCHURL, nil
		}
		return models.InputTypeHTTP, nil
	}

	if strings.HasSuffix(inputSource, ".json") {
		isCRTDL, hint := IsCRTDLFileWithHint(inputSource)
		if isCRTDL {
			return models.InputTypeCRTDL, nil
		}
		// A .json extension is a strong signal of user intent; surface the
		// structural diagnostic instead of silently demoting to local_directory
		// and failing downstream with an unrelated-looking torch import error.
		return "", fmt.Errorf("file %q has a .json extension but is not a valid CRTDL: %s", inputSource, hint)
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

	// FHIR Parameters format is not supported
	if ResourceType(crtdl) == "Parameters" {
		return false, "file uses FHIR Parameters format - please convert to flat CRTDL structure (see example-crtdl.json)"
	}

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
	if IsBundle(resource) {
		return nil // Bundles are handled separately
	}

	jsonData, err := json.Marshal(resource)
	if err != nil {
		// If we can't marshal, assume it's okay (error will be caught elsewhere)
		return nil
	}

	resourceSize := len(jsonData)
	if resourceSize > thresholdBytes {
		resourceType := "Unknown"
		resourceID := "unknown"

		if rt := ResourceType(resource); rt != "" {
			resourceType = rt
		}
		if id := ResourceID(resource); id != "" {
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

// ValidateWaitStepPlacement ensures wait steps are correctly positioned in the pipeline
// Rules:
//   - Wait cannot be the first step (needs previous step output)
//   - No consecutive wait steps (redundant)
func ValidateWaitStepPlacement(steps []models.StepName) error {
	for i, step := range steps {
		if step == models.StepWait {
			if i == 0 {
				return fmt.Errorf("wait step cannot be first in pipeline: requires previous step output")
			}
			if i > 0 && steps[i-1] == models.StepWait {
				return fmt.Errorf("consecutive wait steps not allowed at position %d: redundant", i)
			}
		}
	}
	return nil
}
