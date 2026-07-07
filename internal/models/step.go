package models

import (
	"fmt"
	"slices"
	"time"
)

// PipelineStep represents a discrete stage in the pipeline
type PipelineStep struct {
	Name           StepName   `json:"name"`
	Status         StepStatus `json:"status"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"`
	FilesProcessed int        `json:"files_processed"`
	BytesProcessed int64      `json:"bytes_processed"`
	LastError      *StepError `json:"last_error,omitempty"`
}

// StepName defines the available pipeline steps
type StepName string

const (
	StepTorchImport StepName = "torch"        // TORCH import via CRTDL or direct TORCH URL
	StepLocalImport StepName = "local_import" // Import from local directory
	StepHttpImport  StepName = "http_import"  // Import from HTTP URL
	StepDIMP        StepName = "dimp"
	StepValidation  StepName = "validation"
	StepWait        StepName = "wait"       // Pause pipeline for user inspection/modification
	StepFlattening  StepName = "flattening" // Transform FHIR data to CSV using fhir-flattener
	StepSend        StepName = "send"       // Prepare and send data to DSF transfer server
)

// StepStatus defines the execution state of a pipeline step
type StepStatus string

const (
	StepStatusPending    StepStatus = "pending"
	StepStatusInProgress StepStatus = "in_progress"
	StepStatusCompleted  StepStatus = "completed"
	StepStatusFailed     StepStatus = "failed"
	StepStatusWaiting    StepStatus = "waiting" // Paused, awaiting user to run 'pipeline continue'
)

// StepError captures error details for a failed step
type StepError struct {
	Type       ErrorType `json:"type"` // "transient" | "non_transient"
	Message    string    `json:"message"`
	HTTPStatus int       `json:"http_status,omitempty"`
	Timestamp  time.Time `json:"timestamp"`
}

// Error implements the error interface
func (e *StepError) Error() string {
	if e.HTTPStatus > 0 {
		return fmt.Sprintf("HTTP %d: %s", e.HTTPStatus, e.Message)
	}
	return e.Message
}

// ErrorType classifies errors for retry strategy
type ErrorType string

const (
	ErrorTypeTransient    ErrorType = "transient"     // Network, 5xx, timeout - automatic retry
	ErrorTypeNonTransient ErrorType = "non_transient" // 4xx, validation, malformed - manual intervention
)

// IsValidStepName checks if the step name is recognized
func IsValidStepName(name StepName) bool {
	switch name {
	case StepTorchImport, StepLocalImport, StepHttpImport, StepDIMP, StepValidation, StepWait, StepFlattening, StepSend:
		return true
	default:
		return false
	}
}

// AcceptedInputTypes returns the input types an import step can handle, or nil
// when the step is not an import step. The import method is selected by step
// name, so this expresses which sources each method understands. InputTypeLocal
// is the CRTDL-submission default for torch, hence it is accepted by both torch
// and local_import.
func AcceptedInputTypes(step StepName) []InputType {
	switch step {
	case StepTorchImport:
		return []InputType{InputTypeLocal, InputTypeCRTDL, InputTypeTORCHURL}
	case StepLocalImport:
		return []InputType{InputTypeLocal}
	case StepHttpImport:
		return []InputType{InputTypeHTTP}
	default:
		return nil
	}
}

// ImportStepAcceptsInputType reports whether the import step can handle the
// given input type. It is the single parity check shared by the RunLoop/continue
// resume path, the manual --step path, and job creation.
func ImportStepAcceptsInputType(step StepName, inputType InputType) bool {
	return slices.Contains(AcceptedInputTypes(step), inputType)
}

// IsValidStepStatus checks if the step status is recognized
func IsValidStepStatus(s StepStatus) bool {
	switch s {
	case StepStatusPending, StepStatusInProgress, StepStatusCompleted, StepStatusFailed, StepStatusWaiting:
		return true
	default:
		return false
	}
}

// IsTransientHTTPStatus classifies HTTP status codes for retry logic
func IsTransientHTTPStatus(status int) bool {
	// 5xx server errors are transient (service might recover)
	if status >= 500 && status < 600 {
		return true
	}
	// 408 Request Timeout, 429 Too Many Requests are transient
	if status == 408 || status == 429 {
		return true
	}
	return false
}
