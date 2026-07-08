package cmd

import (
	"fmt"
	"sort"
	"time"

	"github.com/mattn/go-runewidth"
	"github.com/spf13/cobra"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/pipeline"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

// statusFieldWidth is the display width of the combined status symbol + text
// column, matching the "%-15s" header allocation.
const statusFieldWidth = 15

// jobCmd represents the job command group
var jobCmd = &cobra.Command{
	Use:   "job",
	Short: "Manage pipeline jobs",
	Long: `Manage pipeline jobs: list, inspect, and control job execution.

Available subcommands:
  list - List all pipeline jobs
  run  - Execute a specific pipeline step manually`,
}

// jobListCmd represents the job list command
var jobListCmd = &cobra.Command{
	Use:   "list <config>",
	Short: "List all pipeline jobs",
	Long: `List all pipeline jobs in the jobs directory.

Arguments:
  <config>  Path to aether.yaml (required, first positional)

Shows:
  • Job ID (YYYYMMDD_HHMM_UUID or legacy UUID)
  • Status (✓ completed, → in_progress, ✗ failed, ○ pending)
  • Current step being executed
  • Total files processed
  • Retry count for current step
  • Job age (elapsed time since creation)

Jobs are sorted by creation time (newest first).

Status Symbols:
  ✓  - Job completed successfully
  →  - Job in progress
  ✗  - Job failed
  ○  - Job pending

Examples:
  # List all jobs
  aether job list aether.yaml

  # Continuously monitor all jobs
  watch -n 5 aether job list aether.yaml

Typical Workflow:
  1. Start pipeline:  aether pipeline start aether.yaml crtdl.json
  2. List jobs:       aether job list aether.yaml
  3. Get job ID from list
  4. Check status:    aether pipeline status aether.yaml <job-id>`,
	Args: cobra.ExactArgs(1),
	RunE: runJobList,
}

// jobRunCmd represents the job run command
var jobRunCmd = &cobra.Command{
	Use:   "run <config> <job-id> --step <step-name>",
	Short: "Execute a specific pipeline step manually",
	Long: `Execute a specific pipeline step for a job manually.

Arguments:
  <config>  Path to aether.yaml (required, first positional)
  <job-id>  Pipeline job ID (required, second positional)

This command allows you to run individual pipeline steps independently,
useful for:
  • Reprocessing a failed step
  • Running optional steps selectively
  • Testing individual steps
  • Manual recovery after errors

Available Steps:
  torch             - Import FHIR data from TORCH via CRTDL or TORCH result URL
  local_import      - Import FHIR data from a local directory
  http_import       - Import FHIR data from an HTTP URL
  dimp              - Pseudonymize data via DIMP service
  validation        - Validate FHIR data against a FHIR validation service
  flattening        - Flatten FHIR data to CSV via fhir-flattener
  send              - Send data to the configured transfer target

Prerequisites:
  • The step must be enabled in project configuration
  • Prerequisite steps must be completed (e.g., import before dimp)
  • Job must exist and be in a valid state
  • An already-completed step is not re-run unless --force is passed

Examples:
  # Run local import step manually
  aether job run aether.yaml abc123 --step local_import

  # Run DIMP pseudonymization step
  aether job run aether.yaml abc123 --step dimp

  # Re-run an already-completed step
  aether job run aether.yaml abc123 --step dimp --force

Error Handling:
  • Transient errors (network, 5xx) are retried automatically
  • Non-transient errors (4xx, validation) stop execution
  • Use 'pipeline status' to check step status after execution`,
	Args: cobra.ExactArgs(2),
	RunE: runJobRun,
}

var (
	stepFlag  string
	forceFlag bool
)

func init() {
	rootCmd.AddCommand(jobCmd)
	jobCmd.AddCommand(jobListCmd)
	jobCmd.AddCommand(jobRunCmd)

	// Add --step flag to job run command
	jobRunCmd.Flags().StringVar(&stepFlag, "step", "", "Pipeline step to execute (required)")
	if err := jobRunCmd.MarkFlagRequired("step"); err != nil {
		panic(fmt.Sprintf("failed to mark 'step' flag as required: %v", err))
	}
	jobRunCmd.Flags().BoolVar(&forceFlag, "force", false, "Re-run the step even if it has already completed")
}

func runJobList(cmd *cobra.Command, args []string) error {
	cfgPath := args[0]

	config, err := services.LoadConfig(cfgPath)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// List all job IDs
	jobIDs, err := services.ListAllJobs(config.JobsDir)
	if err != nil {
		return fmt.Errorf("failed to list jobs: %w", err)
	}

	if len(jobIDs) == 0 {
		fmt.Println("No jobs found")
		return nil
	}

	// Load each job and display summary
	type jobSummary struct {
		ID         string
		Status     string
		Step       string
		Files      int
		CreatedAt  time.Time
		ElapsedStr string
	}

	var jobs []jobSummary
	for _, jobID := range jobIDs {
		job, err := pipeline.LoadJob(config.JobsDir, jobID)
		if err != nil {
			lib.DefaultLogger.Warn("Failed to load job", "job_id", jobID, "error", err)
			continue
		}

		elapsed := time.Since(job.CreatedAt)
		elapsedStr := formatDuration(elapsed)

		jobs = append(jobs, jobSummary{
			ID:         job.JobID,
			Status:     string(job.Status),
			Step:       job.CurrentStep,
			Files:      job.TotalFiles,
			CreatedAt:  job.CreatedAt,
			ElapsedStr: elapsedStr,
		})
	}

	// Sort by creation time (newest first)
	sort.Slice(jobs, func(i, j int) bool {
		return jobs[i].CreatedAt.After(jobs[j].CreatedAt)
	})

	// Print table header
	fmt.Printf("%-52s %-15s %-20s %-8s %s\n", "JOB ID", "STATUS", "STEP", "FILES", "AGE")
	fmt.Println("--------------------------------------------------------------------------------------------------------------")

	// Print jobs
	for _, j := range jobs {
		fmt.Printf("%-52s %s %-20s %-8d %s\n",
			j.ID,
			formatStatusField(getJobStatusSymbol(j.Status), j.Status),
			j.Step,
			j.Files,
			j.ElapsedStr,
		)
	}

	fmt.Printf("\nTotal: %d jobs\n", len(jobs))

	return nil
}

// formatStatusField builds the STATUS column content padded to statusFieldWidth
// display columns. Go's fmt width specifier pads by rune count, which mismatches
// terminal display width for East Asian Ambiguous symbols like → (U+2192) and
// ○ (U+25CB); runewidth handles the LANG/LC_CTYPE-driven width correctly.
func formatStatusField(symbol, status string) string {
	return runewidth.FillRight(symbol+" "+status, statusFieldWidth)
}

func getJobStatusSymbol(status string) string {
	switch status {
	case "completed":
		return "✓"
	case "in_progress":
		return "→"
	case "failed":
		return "✗"
	case "pending":
		return "○"
	default:
		return " "
	}
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	days := int(d.Hours() / 24)
	return fmt.Sprintf("%dd", days)
}

func runJobRun(cmd *cobra.Command, args []string) error {
	cfgPath := args[0]
	jobID := args[1]

	stepName, err := validateStepName(stepFlag)
	if err != nil {
		return err
	}

	config, err := services.LoadConfig(cfgPath)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// Check if step is enabled in configuration
	if !isStepEnabledInConfig(config, stepName) {
		return fmt.Errorf("step '%s' is not enabled in configuration (check enabled_steps in config file)", stepName)
	}

	// Load job
	job, err := pipeline.LoadJob(config.JobsDir, jobID)
	if err != nil {
		return fmt.Errorf("failed to load job: %w", err)
	}

	fmt.Printf("Job: %s\n", job.JobID)
	fmt.Printf("Executing step: %s\n\n", stepName)

	// Validate prerequisites
	canRun, prerequisite := models.CanRunStep(*job, stepName)
	if !canRun {
		return fmt.Errorf("cannot run step '%s': prerequisite step '%s' must be completed first", stepName, prerequisite)
	}

	// Acquire job lock to prevent concurrent execution
	logger := lib.DefaultLogger
	lock, err := services.AcquireJobLock(config.JobsDir, jobID, logger)
	if err != nil {
		return fmt.Errorf("cannot execute step: %w\n\nAnother process may be working on this job. Wait for it to complete or check job status", err)
	}
	defer func() {
		if err := lock.Release(); err != nil {
			logger.Error("Failed to release job lock", "error", err)
		}
	}()

	// Execute the step (with lock held). executeStepManually already contextualizes
	// every error path, so return it as-is rather than double-wrapping.
	if err := executeStepManually(job, stepName, config, logger, forceFlag); err != nil {
		return err
	}

	fmt.Printf("\n✓ Step '%s' completed successfully\n", stepName)

	return nil
}

// validateStepName validates and converts step flag to StepName type
func validateStepName(step string) (models.StepName, error) {
	validSteps := map[string]models.StepName{
		"torch":        models.StepTorchImport,
		"local_import": models.StepLocalImport,
		"http_import":  models.StepHttpImport,
		"dimp":         models.StepDIMP,
		"validation":   models.StepValidation,
		"flattening":   models.StepFlattening,
		"send":         models.StepSend,
	}

	stepName, ok := validSteps[step]
	if !ok {
		return "", fmt.Errorf("invalid step name '%s'. Valid steps: torch, local_import, http_import, dimp, validation, flattening, send", step)
	}

	return stepName, nil
}

// isStepEnabledInConfig checks if a step is enabled in the project configuration
func isStepEnabledInConfig(config *models.ProjectConfig, stepName models.StepName) bool {
	for _, enabled := range config.Pipeline.EnabledSteps {
		if enabled == stepName {
			return true
		}
	}
	return false
}

// executeStepManually runs a single pipeline step through the shared RunStep seam —
// the same lifecycle the automatic RunLoop uses — keeping job status derived from its
// steps (see DeriveJobStatus) and recording job-level failure on error.
func executeStepManually(job *models.PipelineJob, stepName models.StepName, config *models.ProjectConfig, logger *lib.Logger, force bool) error {
	// A completed step is terminal for the transition gate, so re-running it would
	// fail an illegal completed->in_progress transition. Without --force, refuse as a
	// pre-execution guard: nothing runs, so job state is left untouched (not marked
	// failed) and the error surfaces for a non-zero exit. With --force, reset the step
	// and every step ordered after it to pending (a re-run invalidates the output later
	// steps consumed), flip the job to in_progress, and persist now so `job status`
	// shows the in-flight re-run. The invalidated downstream steps stay pending until
	// re-run; only the target step runs here.
	if s, ok := models.GetStepByName(*job, stepName); ok && s.Status == models.StepStatusCompleted {
		if !force {
			return fmt.Errorf("step '%s' already completed; pass --force to re-run", stepName)
		}
		*job = models.ResetStepAndDownstream(*job, stepName)
		*job = models.UpdateJobStatus(*job, models.JobStatusInProgress)
		if err := pipeline.UpdateJob(config.JobsDir, job); err != nil {
			return fmt.Errorf("failed to save job state: %w", err)
		}
	}

	ctx := &pipeline.StepContext{
		Job:          job,
		Layout:       services.NewJobLayout(config.JobsDir, job.JobID, job.Config.Pipeline.EnabledSteps),
		Logger:       logger,
		ShowProgress: true,
	}

	// Import steps must match the job's input type and take the manual HTTP client;
	// other steps build their own client internally and ignore ctx.HTTPClient.
	if isImportStep(stepName) {
		if err := validateImportStepMatchesInput(job, stepName); err != nil {
			return err
		}
		ctx.HTTPClient = services.NewHTTPClient(30*time.Second, job.Config.Retry, job.Config.TLS, logger)
	}

	if err := pipeline.RunStep(stepName, ctx); err != nil {
		failed := pipeline.FailJob(job, err.Error())
		if saveErr := pipeline.UpdateJob(config.JobsDir, failed); saveErr != nil {
			logger.Error("Failed to save job state", "error", saveErr)
		}
		return fmt.Errorf("%s step failed: %w", stepName, err)
	}

	// Re-derive job status from its steps: finishing the last step completes the job,
	// a forced re-run settles back to completed — status is derived, never stale.
	derived := models.DeriveJobStatus(*job)
	*job = models.UpdateJobStatus(*job, derived)
	if derived == models.JobStatusCompleted {
		job.CurrentStep = ""
	}
	if err := pipeline.UpdateJob(config.JobsDir, job); err != nil {
		return fmt.Errorf("failed to save job state: %w", err)
	}
	return nil
}

// isImportStep reports whether the step is one of the three import variants.
func isImportStep(stepName models.StepName) bool {
	switch stepName {
	case models.StepTorchImport, models.StepLocalImport, models.StepHttpImport:
		return true
	default:
		return false
	}
}

// validateImportStepMatchesInput rejects a manual --step import request whose
// step cannot handle the job's input type (e.g. http_import for a local job). It
// shares models.ImportStepAcceptsInputType with the RunLoop guard and job
// creation so all three paths agree on the mapping.
func validateImportStepMatchesInput(job *models.PipelineJob, stepName models.StepName) error {
	if !models.ImportStepAcceptsInputType(stepName, job.InputType) {
		return fmt.Errorf("import step %q does not accept input type %q (accepts %v)", stepName, job.InputType, models.AcceptedInputTypes(stepName))
	}
	return nil
}
