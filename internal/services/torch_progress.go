package services

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/medizininformatik-initiative/aether/internal/ui"
)

// TORCHProgressExtensionURL identifies the torch-job-progress extension on the
// TORCH Task resource.
const TORCHProgressExtensionURL = "https://torch.mii.de/fhir/torch-job-progress"

// TORCHActiveBatch is one batch that TORCH processes at this moment.
type TORCHActiveBatch struct {
	BatchID string
	Stage   string
}

// TORCHProgress is the extraction progress that TORCH reports on its Task
// resource once the cohort query is done.
type TORCHProgress struct {
	CohortSize       int
	BatchSize        int
	BatchesTotal     int
	BatchesCompleted int
	ActiveBatches    []TORCHActiveBatch
}

// torchStageOrder maps a batch pipeline stage to its 1-based position. TORCH
// processes each batch through these stages in this order.
var torchStageOrder = map[string]int{
	"CONSENT_FETCH":     1,
	"DIRECT_LOAD":       2,
	"REFERENCE_RESOLVE": 3,
	"CASCADING_DELETE":  4,
	"COPY_REDACT":       5,
}

const torchStageCount = 5

// StageIndex returns the 1-based position of the batch's stage, or 0 for an
// unknown stage.
func (b TORCHActiveBatch) StageIndex() int {
	return torchStageOrder[b.Stage]
}

// Fraction estimates the completed part of the extraction as a value in
// [0, 1]. An active batch counts as stageIndex/5 of a batch. Stages get equal
// weight, so the value is an estimate.
func (p TORCHProgress) Fraction() float64 {
	if p.BatchesTotal <= 0 {
		return 0
	}
	done := float64(p.BatchesCompleted)
	for _, batch := range p.ActiveBatches {
		done += float64(batch.StageIndex()) / torchStageCount
	}
	fraction := done / float64(p.BatchesTotal)
	if fraction > 1 {
		return 1
	}
	return fraction
}

// PatientsDone returns the number of patients in completed batches, capped at
// the cohort size because the last batch can be smaller than BatchSize.
func (p TORCHProgress) PatientsDone() int {
	patients := p.BatchesCompleted * p.BatchSize
	if patients > p.CohortSize {
		return p.CohortSize
	}
	return patients
}

// Summary returns a one-line text of the progress, for logs and the status
// command.
func (p TORCHProgress) Summary() string {
	summary := fmt.Sprintf("%d/%d batches (%d/%d patients)",
		p.BatchesCompleted, p.BatchesTotal, p.PatientsDone(), p.CohortSize)
	if len(p.ActiveBatches) == 0 {
		return summary
	}
	stages := make([]string, len(p.ActiveBatches))
	for i, batch := range p.ActiveBatches {
		stages[i] = fmt.Sprintf("%s (%d/%d)", batch.Stage, batch.StageIndex(), torchStageCount)
	}
	return summary + ", active: " + strings.Join(stages, ", ")
}

// terminalBarWidth is the character width of the textual progress bar.
const terminalBarWidth = 16

// TerminalLine returns the progress line for the terminal: a bar, a percent
// value, and the summary.
func (p TORCHProgress) TerminalLine() string {
	fraction := p.Fraction()
	return fmt.Sprintf("TORCH extraction %s %d%% — %s",
		ui.RenderBar(fraction, terminalBarWidth), int(fraction*100), p.Summary())
}

type fhirExtension struct {
	URL          string          `json:"url"`
	ValueInteger *int            `json:"valueInteger,omitempty"`
	ValueString  string          `json:"valueString,omitempty"`
	Extension    []fhirExtension `json:"extension,omitempty"`
}

type torchTask struct {
	ResourceType string          `json:"resourceType"`
	Extension    []fhirExtension `json:"extension"`
}

// FetchJobProgress reads the torch-job-progress extension from
// GET {BaseURL}/fhir/Task/{jobID}. It returns nil when the server does not
// supply usable progress (error, non-200, no extension). It never fails the
// extraction.
func (c *TORCHClient) FetchJobProgress(jobID string) *TORCHProgress {
	progress, _ := c.fetchJobProgressChecked(jobID)
	return progress
}

// fetchJobProgressChecked fetches progress and also reports whether the Task
// API is usable. `supported == false` (non-200, or a body that is not a Task)
// tells the poll loop to stop asking; a transport error or a Task without the
// extension keeps `supported == true` so a later poll asks again.
func (c *TORCHClient) fetchJobProgressChecked(jobID string) (progress *TORCHProgress, supported bool) {
	taskURL := fmt.Sprintf("%s/fhir/Task/%s", c.config.BaseURL, jobID)

	req, err := http.NewRequest("GET", taskURL, nil)
	if err != nil {
		c.logger.Debug("TORCH progress request creation failed", "error", err)
		return nil, false
	}
	if err := c.httpClient.ApplyAuth(req, c.config.EffectiveAuth()); err != nil {
		c.logger.Debug("TORCH progress auth failed", "error", err)
		return nil, false
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.DoOnce(req)
	if err != nil {
		c.logger.Debug("TORCH progress fetch failed", "error", err)
		return nil, true
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		c.logger.Debug("TORCH progress fetch returned non-OK status", "status_code", resp.StatusCode)
		return nil, false
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		c.logger.Debug("TORCH progress body read failed", "error", err)
		return nil, true
	}

	return parseTaskProgress(body)
}

// parseTaskProgress extracts TORCHProgress from a FHIR Task resource body.
// A body that is not a Task marks the Task API unsupported; a Task without
// the progress extension is supported but carries no progress yet.
func parseTaskProgress(body []byte) (*TORCHProgress, bool) {
	var task torchTask
	if err := json.Unmarshal(body, &task); err != nil {
		return nil, false
	}
	if task.ResourceType != "Task" {
		return nil, false
	}

	for _, ext := range task.Extension {
		if ext.URL == TORCHProgressExtensionURL {
			return progressFromExtension(ext), true
		}
	}
	return nil, true
}

func progressFromExtension(ext fhirExtension) *TORCHProgress {
	progress := &TORCHProgress{}
	for _, sub := range ext.Extension {
		switch sub.URL {
		case "cohortSize":
			progress.CohortSize = intValue(sub)
		case "batchSize":
			progress.BatchSize = intValue(sub)
		case "batchesTotal":
			progress.BatchesTotal = intValue(sub)
		case "batchesCompleted":
			progress.BatchesCompleted = intValue(sub)
		case "activeBatch":
			progress.ActiveBatches = append(progress.ActiveBatches, activeBatchFromExtension(sub))
		}
	}
	return progress
}

func activeBatchFromExtension(ext fhirExtension) TORCHActiveBatch {
	batch := TORCHActiveBatch{}
	for _, sub := range ext.Extension {
		switch sub.URL {
		case "batchId":
			batch.BatchID = sub.ValueString
		case "stage":
			batch.Stage = sub.ValueString
		}
	}
	return batch
}

func intValue(ext fhirExtension) int {
	if ext.ValueInteger == nil {
		return 0
	}
	return *ext.ValueInteger
}
