package unit

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

func TestServiceError_Error_IncludesStatusAndBody(t *testing.T) {
	err := &services.ServiceError{
		Service:    "validation",
		StatusCode: 422,
		Status:     "422 Unprocessable Entity",
		ErrorType:  models.ErrorTypeNonTransient,
		Body:       `{"issue":"bad"}`,
	}

	msg := err.Error()
	assert.Contains(t, msg, "validation")
	assert.Contains(t, msg, "HTTP 422")
	assert.Contains(t, msg, "422 Unprocessable Entity")
	assert.Contains(t, msg, `{"issue":"bad"}`)
}

func TestServiceError_IsRetryable(t *testing.T) {
	transient := &services.ServiceError{ErrorType: models.ErrorTypeTransient}
	assert.True(t, transient.IsRetryable())

	nonTransient := &services.ServiceError{ErrorType: models.ErrorTypeNonTransient}
	assert.False(t, nonTransient.IsRetryable())
}

func TestServiceError_ServiceSpecificHints(t *testing.T) {
	dimp500 := &services.ServiceError{Service: "DIMP", StatusCode: 500, Status: "500 Internal Server Error"}
	assert.Contains(t, dimp500.Error(), "VFPS", "DIMP 500 should include VFPS namespace guidance")

	flat400 := &services.ServiceError{Service: "Flattener", StatusCode: 400, Status: "400 Bad Request"}
	assert.True(t, strings.Contains(flat400.Error(), "ViewDefinition"),
		"flattener 400 should include ViewDefinition guidance")
}
