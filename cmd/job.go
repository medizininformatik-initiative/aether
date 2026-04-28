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
	Use:   "list",
	Short: "List all pipeline jobs",
	Long: `List all pipeline jobs in the jobs directory.

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
  aether job list

  # Continuously monitor all jobs
  watch -n 5 aether job list

Typical Workflow:
  1. Start pipeline:  aether pipeline start crtdl.json
  2. List jobs:       aether job list
  3. Get job ID from list
  4. Check status:    aether pipeline status <job-id>`,
	RunE: runJobList,
}

// jobRunCmd represents the job run command
var jobRunCmd = &cobra.Command{
	Use:   "run <job-id> --step <step-name>",
	Short: "Execute a specific pipeline step manually",
	Long: `Execute a specific pipeline step for a job manually.

This command allows you to run individual pipeline steps independently,
useful for:
  • Reprocessing a failed step
  • Running optional steps selectively
  • Testing individual steps
  • Manual recovery after errors

Available Steps:
  import            - Import FHIR data from local or HTTP source
  dimp              - Pseudonymize data via DIMP service
  validation        - Validate FHIR data (placeholder)
Prerequisites:
  • The step must be enabled in project configuration
  • Prerequisite steps must be completed (e.g., import before dimp)
  • Job must exist and be in a valid state

Examples:
  # Run import step manually
  aether job run abc123 --step import

  # Run DIMP pseudonymization step
  aether job run abc123 --step dimp

Error Handling:
  • Transient errors (network, 5xx) are retried automatically
  • Non-transient errors (4xx, validation) stop execution
  • Use 'pipeline status' to check step status after execution`,
	Args: cobra.ExactArgs(1),
	RunE: runJobRun,
}

var stepFlag string

func init() {
	rootCmd.AddCommand(jobCmd)
	jobCmd.AddCommand(jobListCmd)
	jobCmd.AddCommand(jobRunCmd)

	// Add --step flag to job run command
	jobRunCmd.Flags().StringVar(&stepFlag, "step", "", "Pipeline step to execute (required)")
	if err := jobRunCmd.MarkFlagRequired("step"); err != nil {
		panic(fmt.Sprintf("failed to mark 'step' flag as required: %v", err))
	}
}

func runJobList(cmd *cobra.Command, args []string) error {
	// Load configuration
	config, err := services.LoadConfig(cfgFile)
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
	jobID := args[0]

	// Validate step name
	stepName, err := validateStepName(stepFlag)
	if err != nil {
		return err
	}

	// Load configuration
	config, err := services.LoadConfig(cfgFile)
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
	canRun, prerequisite := lib.CanRunStep(*job, stepName)
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

	// Execute the step (with lock held)
	err = executeStepManually(job, stepName, config, logger)
	if err != nil {
		return fmt.Errorf("step execution failed: %w", err)
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
	}

	stepName, ok := validSteps[step]
	if !ok {
		return "", fmt.Errorf("invalid step name '%s'. Valid steps: torch, local_import, http_import, dimp, validation", step)
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

// executeStepManually executes a specific pipeline step manually
// This is similar to executeStep in pipeline.go but simplified for manual execution
func executeStepManually(job *models.PipelineJob, stepName models.StepName, config *models.ProjectConfig, logger *lib.Logger) error {
	jobDir := services.GetJobDir(config.JobsDir, job.JobID)

	switch stepName {
	case models.StepTorchImport, models.StepLocalImport, models.StepHttpImport:
		// Validate step name matches input type (imported from pipeline.go logic)
		var expectedStep models.StepName
		switch job.InputType {
		case models.InputTypeCRTDL, models.InputTypeTORCHURL:
			expectedStep = models.StepTorchImport
		case models.InputTypeLocal:
			expectedStep = models.StepLocalImport
		case models.InputTypeHTTP:
			expectedStep = models.StepHttpImport
		default:
			return fmt.Errorf("unknown input type: %s", job.InputType)
		}

		if stepName != expectedStep {
			return fmt.Errorf("step '%s' does not match input type %s (expected '%s')", stepName, job.InputType, expectedStep)
		}

		// Create HTTP client
		httpClient := services.NewHTTPClient(30*time.Second, job.Config.Retry, job.Config.TLS, logger)
		showProgress := true

		importedJob, err := pipeline.ExecuteImportStep(job, logger, httpClient, showProgress)
		if err != nil {
			return fmt.Errorf("%s step failed: %w", stepName, err)
		}

		if err := pipeline.UpdateJob(config.JobsDir, importedJob); err != nil {
			return fmt.Errorf("failed to save job state: %w", err)
		}

		fmt.Printf("\n✓ %s step completed (%d files)\n", stepName, importedJob.TotalFiles)
		return nil

	case models.StepDIMP:
		// Execute DIMP pseudonymization step
		fmt.Println("Starting DIMP pseudonymization step...")
		if err := pipeline.ExecuteDIMPStep(job, jobDir, logger); err != nil {
			// Save failed state
			if saveErr := pipeline.UpdateJob(config.JobsDir, job); saveErr != nil {
				logger.Error("Failed to save job state", "error", saveErr)
			}
			return fmt.Errorf("DIMP step failed: %w", err)
		}

		// Save successful state
		if err := pipeline.UpdateJob(config.JobsDir, job); err != nil {
			return fmt.Errorf("failed to save job state: %w", err)
		}

		fmt.Printf("\n✓ DIMP pseudonymization completed\n")
		return nil

	case models.StepValidation:
		return fmt.Errorf("validation step not yet implemented")

	default:
		return fmt.Errorf("unknown step: %s", stepName)
	}
}
