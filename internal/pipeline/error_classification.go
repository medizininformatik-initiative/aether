package pipeline

import (
	"errors"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
)

// retryableError is implemented by every classified service error
// (services.ServiceError, TORCHError, FHIRError, S3Error).
type retryableError interface {
	IsRetryable() bool
}

// isServiceErrorRetryable reports whether a service-call error is transient.
// errors.As unwraps %w-wrapped errors, so classification is correct even when
// a step wraps the typed error with additional context.
func isServiceErrorRetryable(err error) bool {
	var re retryableError
	if errors.As(err, &re) {
		return re.IsRetryable()
	}
	return lib.IsNetworkError(err)
}

// classifyServiceError classifies a service-call error as transient or
// non-transient, unwrapping %w-wrapped errors via errors.As.
func classifyServiceError(err error) models.ErrorType {
	var re retryableError
	if errors.As(err, &re) {
		if re.IsRetryable() {
			return models.ErrorTypeTransient
		}
		return models.ErrorTypeNonTransient
	}
	if lib.IsNetworkError(err) {
		return models.ErrorTypeTransient
	}
	return models.ErrorTypeNonTransient
}
