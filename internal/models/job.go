package models

import "time"

// PipelineJob represents a single execution of the Data Use Process pipeline
type PipelineJob struct {
	JobID              string         `json:"job_id"`
	CreatedAt          time.Time      `json:"created_at"`
	UpdatedAt          time.Time      `json:"updated_at"`
	InputSource        string         `json:"input_source"`                   // Local path, HTTP(S) URL, or CRTDL file
	InputType          InputType      `json:"input_type"`                     // "local_directory" | "http_url" | "crtdl_file" | "torch_result_url"
	CRTDLPath          string         `json:"crtdl_path,omitempty"`           // Optional CRTDL file, decoupled from InputSource (see issue #286)
	TORCHExtractionURL string         `json:"torch_extraction_url,omitempty"` // Content-Location URL for TORCH polling/resume
	CurrentStep        string         `json:"current_step"`                   // Current pipeline step
	Status             JobStatus      `json:"status"`                         // Job execution status
	Steps              []PipelineStep `json:"steps"`                          // Ordered list of pipeline steps
	Config             ProjectConfig  `json:"config"`                         // Project configuration snapshot
	TotalFiles         int            `json:"total_files"`                    // Total FHIR files processed
	TotalBytes         int64          `json:"total_bytes"`                    // Total data volume in bytes
	ErrorMessage       string         `json:"error_message,omitempty"`        // Last error if failed
}

// InputType defines the source type for FHIR data
type InputType string

const (
	InputTypeLocal    InputType = "local_directory"
	InputTypeHTTP     InputType = "http_url"
	InputTypeCRTDL    InputType = "crtdl_file"
	InputTypeTORCHURL InputType = "torch_result_url"
)

// JobStatus defines the execution state of a pipeline job
type JobStatus string

const (
	JobStatusPending    JobStatus = "pending"
	JobStatusInProgress JobStatus = "in_progress"
	JobStatusCompleted  JobStatus = "completed"
	JobStatusFailed     JobStatus = "failed"
)

// IsValidInputType checks if the input type is recognized
func IsValidInputType(t InputType) bool {
	return t == InputTypeLocal || t == InputTypeHTTP || t == InputTypeCRTDL || t == InputTypeTORCHURL
}

// IsValidJobStatus checks if the job status is recognized
func IsValidJobStatus(s JobStatus) bool {
	switch s {
	case JobStatusPending, JobStatusInProgress, JobStatusCompleted, JobStatusFailed:
		return true
	default:
		return false
	}
}

// CanTransitionTo checks if state transition is valid
// Valid transitions:
//
//	pending -> in_progress
//	in_progress -> completed | failed
//	failed -> in_progress (manual retry)
func (s JobStatus) CanTransitionTo(next JobStatus) bool {
	switch s {
	case JobStatusPending:
		return next == JobStatusInProgress
	case JobStatusInProgress:
		return next == JobStatusCompleted || next == JobStatusFailed
	case JobStatusFailed:
		return next == JobStatusInProgress // Allow retry
	case JobStatusCompleted:
		return false // Terminal state
	default:
		return false
	}
}
