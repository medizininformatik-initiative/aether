package services

import (
	"fmt"
	"io"
	"net/http"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
)

// ServiceError is the single classified error for FHIR-JSON service calls
// (DIMP, validation, flattener). It carries the originating service so its
// message can surface service-specific operator guidance.
type ServiceError struct {
	Service    string
	StatusCode int
	Status     string
	ErrorType  models.ErrorType
	Body       string
}

func (e *ServiceError) Error() string {
	msg := fmt.Sprintf("%s service error: HTTP %d: %s", e.Service, e.StatusCode, e.Status)
	if e.Body != "" {
		msg += fmt.Sprintf("\nResponse: %s", e.Body)
	}
	msg += serviceHint(e.Service, e.StatusCode)
	return msg
}

// IsRetryable reports whether this error should be retried.
func (e *ServiceError) IsRetryable() bool {
	return e.ErrorType == models.ErrorTypeTransient
}

// serviceHint returns operator guidance for known service/status combinations.
func serviceHint(service string, statusCode int) string {
	switch service {
	case "DIMP":
		if statusCode == 500 {
			return "\n\nPossible causes:" +
				"\n  - VFPS pseudonymization namespace not initialized" +
				"\n  - Invalid FHIR resource structure" +
				"\n  - DIMP service configuration issue" +
				"\n\nFor VFPS namespace errors, ensure the required namespace(s) are created via the VFPS API:" +
				"\n  curl -X POST http://vfps:8080/api/v1/Namespace -H 'content-type: application/json' -d '{\"name\":\"patient-identifiers\",...}'"
		}
	case "Flattener":
		switch statusCode {
		case 400:
			return "\n\nPossible causes:" +
				"\n  - Invalid ViewDefinition structure" +
				"\n  - Malformed FHIR resources" +
				"\n  - Missing required fields in request"
		case 500:
			return "\n\nPossible causes:" +
				"\n  - ViewDefinition processing error" +
				"\n  - fhir-flattener service configuration issue"
		}
	}
	return ""
}

// classifyHTTPResponse reads an error response body and builds the single
// classified ServiceError. This is the one HTTP-status error-classification
// block shared by every FHIR-JSON client.
func classifyHTTPResponse(service string, resp *http.Response) *ServiceError {
	bodyBytes, _ := io.ReadAll(resp.Body)
	return &ServiceError{
		Service:    service,
		StatusCode: resp.StatusCode,
		Status:     resp.Status,
		ErrorType:  lib.ClassifyHTTPError(resp.StatusCode),
		Body:       string(bodyBytes),
	}
}
