package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
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
// column, matching the "%-24s" header allocation. The longest text is
// "stopped (process died)" plus the symbol and a space.
const statusFieldWidth = 24

// jobCmd represents the job command group
var jobCmd = &cobra.Command{
	Use:   "job",
	Short: "Manage pipeline jobs",
	Long: `Manage pipeline jobs: list, inspect, and control job execution.

Available subcommands:
  list - List all pipeline jobs
  run  - Execute a specific pipeline step manually
  stop - Stop a job and cancel its TORCH job`,
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
  • Status (✓ completed, → in_progress, ✗ failed, ○ pending, ■ stopped, ‖ waiting)
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
  ■  - Job stopped (no process runs it; use 'pipeline continue')
  ‖  - Job waits at the wait step (use 'pipeline continue')

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

// jobStopCmd represents the job stop command
var jobStopCmd = &cobra.Command{
	Use:   "stop <config> <job-id>",
	Short: "Stop a job and cancel its TORCH job",
	Long: `Stop a job for good.

Arguments:
  <config>  Path to aether.yaml (required, first positional)
  <job-id>  Pipeline job ID (required, second positional)

If a process runs the job, this command asks that process to stop and waits
until it ends. At most 30 seconds. If the job waits for a TORCH job, the
command cancels that TORCH job on the TORCH server.

If no process runs the job, the command sets the job status to stopped and
cancels the TORCH job.

A job that is completed or failed cannot be stopped.

Examples:
  # Stop a job
  aether job stop aether.yaml 20240115_1030_550e8400-e29b-41d4-a716-446655440000

  # Start the job again with a new TORCH extraction
  aether pipeline continue aether.yaml 20240115_1030_550e8400-e29b-41d4-a716-446655440000`,
	Args: cobra.ExactArgs(2),
	RunE: runJobStop,
}

var (
	stepFlag  string
	forceFlag bool
)

func init() {
	rootCmd.AddCommand(jobCmd)
	jobCmd.AddCommand(jobListCmd)
	jobCmd.AddCommand(jobRunCmd)
	jobCmd.AddCommand(jobStopCmd)

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
		Status     models.JobStatus
		StatusText string
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
			Status:     services.EffectiveJobStatus(config.JobsDir, *job),
			StatusText: services.EffectiveJobStatusText(config.JobsDir, *job),
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
	fmt.Printf("%-52s %-24s %-20s %-8s %s\n", "JOB ID", "STATUS", "STEP", "FILES", "AGE")
	fmt.Println("-----------------------------------------------------------------------------------------------------------------------")

	// Print jobs
	for _, j := range jobs {
		fmt.Printf("%-52s %s %-20s %-8d %s\n",
			j.ID,
			formatStatusField(getJobStatusSymbol(j.Status), j.StatusText),
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
func formatStatusField(symbol string, statusText string) string {
	return runewidth.FillRight(symbol+" "+statusText, statusFieldWidth)
}

func getJobStatusSymbol(status models.JobStatus) string {
	switch status {
	case models.JobStatusCompleted:
		return "✓"
	case models.JobStatusInProgress:
		return "→"
	case models.JobStatusFailed:
		return "✗"
	case models.JobStatusPending:
		return "○"
	case models.JobStatusStopped:
		return "■"
	case models.JobStatusWaiting:
		return "‖"
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

const (
	stopProcessWait = 30 * time.Second
	stopProcessPoll = 100 * time.Millisecond
)

func runJobStop(cmd *cobra.Command, args []string) error {
	cfgPath := args[0]
	jobID := args[1]

	config, err := services.LoadConfig(cfgPath)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	return stopJob(config.JobsDir, cfgPath, jobID, stopProcessWait, stopProcessPoll, cmd.OutOrStdout(), lib.DefaultLogger)
}

// stopJob stops the job. If a process holds the job lock, it asks that process
// to stop and waits up to wait for the lock to go. If no process holds the lock,
// or the process ended without a record of the stop, it stops the job itself.
// It returns an error if the job ended before, or if the TORCH cancel fails.
// The job stays stopped in the second case.
func stopJob(
	jobsDir string,
	cfgPath string,
	jobID string,
	wait time.Duration,
	poll time.Duration,
	out io.Writer,
	logger *lib.Logger,
) error {
	if _, err := pipeline.LoadJob(jobsDir, jobID); err != nil {
		return fmt.Errorf("failed to load job: %w", err)
	}

	if services.IsJobLocked(jobsDir, jobID) {
		err := stopRunningJob(jobsDir, cfgPath, jobID, wait, poll, out)
		if !errors.Is(err, errProcessEndedWithoutStop) {
			return err
		}
	}

	if err := pipeline.StopIdleJob(context.Background(), jobsDir, jobID, logger); err != nil {
		var ended *pipeline.JobEndedError
		if errors.As(err, &ended) {
			return err
		}
		return fmt.Errorf("job %s is stopped, but the TORCH cancel failed: %w", jobID, err)
	}
	reportStopped(out, cfgPath, jobID)
	return nil
}

// errProcessEndedWithoutStop tells that the job process ended without a record
// of the stop, for example after SIGKILL. The caller then stops the job as if no
// process held the lock.
var errProcessEndedWithoutStop = errors.New("the job process ended without a record of the stop")

// stopRunningJob asks the process that holds the job lock to stop. That process
// records the stop and cancels the TORCH job. If the job ended before it could
// stop, it returns *pipeline.JobEndedError.
func stopRunningJob(
	jobsDir string,
	cfgPath string,
	jobID string,
	wait time.Duration,
	poll time.Duration,
	out io.Writer,
) error {
	if err := services.RequestStop(jobsDir, jobID); err != nil {
		return fmt.Errorf("failed to ask the job process to stop: %w", err)
	}
	defer func() { _ = services.ClearStopRequest(jobsDir, jobID) }()

	deadline := time.Now().Add(wait)
	for services.IsJobLocked(jobsDir, jobID) {
		if time.Now().After(deadline) {
			return fmt.Errorf("the process of job %s did not end in %s", jobID, wait)
		}
		time.Sleep(poll)
	}

	job, err := pipeline.LoadJob(jobsDir, jobID)
	if err != nil {
		return fmt.Errorf("failed to load job: %w", err)
	}
	switch {
	case job.Status == models.JobStatusStopped:
		reportStopped(out, cfgPath, jobID)
		return nil
	case job.Status.IsEnded():
		return &pipeline.JobEndedError{JobID: jobID, Status: job.Status}
	}
	return errProcessEndedWithoutStop
}

func reportStopped(out io.Writer, cfgPath string, jobID string) {
	_, _ = fmt.Fprintf(out, "Job %s stopped\n", jobID)
	_, _ = fmt.Fprintf(out, "Continue it with: aether pipeline continue %s %s\n", cfgPath, jobID)
}

func runJobRun(cmd *cobra.Command, args []string) error {
	cfgPath := args[0]
	jobID := args[1]

	stepName, err := validateStepName(stepFlag)
	if err != nil {
		return err
	}

	config, job, err := loadRunnableJob(cfgPath, jobID, stepName)
	if err != nil {
		return err
	}

	// Acquire job lock to prevent concurrent execution
	logger := lib.DefaultLogger
	lock, err := services.AcquireJobLock(config.JobsDir, jobID, logger)
	if err != nil {
		return fmt.Errorf("cannot execute step: %w\n\nAnother process may be working on this job. Wait for it to complete or check its status with 'pipeline status'", err)
	}
	defer releaseJobLock(lock, logger)

	defer installStopTriggers(config.JobsDir, jobID, logger)()

	// Execute the step (with lock held). executeStepManually already contextualizes
	// every error path, so return it as-is rather than double-wrapping.
	if err := executeStepManually(job, stepName, config, logger, forceFlag); err != nil {
		return err
	}

	fmt.Printf("\n✓ Step '%s' completed successfully\n", stepName)

	return nil
}

// loadRunnableJob loads the configuration and the job, and checks that the step
// is enabled and that its prerequisite step is completed.
func loadRunnableJob(cfgPath string, jobID string, stepName models.StepName) (*models.ProjectConfig, *models.PipelineJob, error) {
	config, err := services.LoadConfig(cfgPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load configuration: %w", err)
	}

	if !isStepEnabledInConfig(config, stepName) {
		return nil, nil, fmt.Errorf("step '%s' is not enabled in configuration (check enabled_steps in config file)", stepName)
	}

	job, err := pipeline.LoadJob(config.JobsDir, jobID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load job: %w", err)
	}

	fmt.Printf("Job: %s\n", job.JobID)
	fmt.Printf("Executing step: %s\n\n", stepName)

	if canRun, prerequisite := models.CanRunStep(*job, stepName); !canRun {
		return nil, nil, fmt.Errorf("cannot run step '%s': prerequisite step '%s' must be completed first", stepName, prerequisite)
	}
	return config, job, nil
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
	// steps consumed), flip the job to in_progress, and persist now so `pipeline status`
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
	// a forced re-run settles back to completed. A job left mid-pipeline stays
	// in_progress on disk; because no process runs it after this command exits,
	// EffectiveJobStatus shows it as stopped.
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
