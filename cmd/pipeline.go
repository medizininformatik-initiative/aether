package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/pipeline"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

var (
	noProgress bool
)

// pipelineCmd represents the pipeline command group
var pipelineCmd = &cobra.Command{
	Use:   "pipeline",
	Short: "Manage pipeline execution",
	Long: `Manage Data Use Process (DUP) pipeline execution.

Available subcommands:
  start   - Start a new pipeline job
  status  - Check pipeline job status
  continue - Resume a failed/paused pipeline job`,
}

// pipelineStartCmd represents the pipeline start command
var pipelineStartCmd = &cobra.Command{
	Use:   "start <input>",
	Short: "Start a new pipeline job",
	Long: `Start a new Data Use Process pipeline job.

The input can be:
  • CRTDL file (*.crtdl) for TORCH-based data extraction
  • Local directory containing FHIR NDJSON files
  • HTTP(S) URL to download FHIR data from
  • TORCH result URL for direct download

Examples:
  # Extract data using CRTDL query via TORCH
  aether pipeline start query.crtdl

  # Import from local directory
  aether pipeline start /path/to/fhir/data

  # Download from HTTP URL
  aether pipeline start https://example.com/fhir/Patient.ndjson

  # Download from TORCH result URL
  aether pipeline start http://torch-server/fhir/extraction/result-123

  # Start without progress indicators
  aether pipeline start query.crtdl --no-progress`,
	Args: cobra.ExactArgs(1),
	RunE: runPipelineStart,
}

// pipelineStatusCmd represents the pipeline status command
var pipelineStatusCmd = &cobra.Command{
	Use:   "status [job-id]",
	Short: "Check pipeline job status",
	Long: `Display the current status of a pipeline job.

Shows:
  • Job ID and current status
  • Current step being executed
  • Progress for each step (files processed, errors, retries)
  • Total files and data processed
  • Error messages if job failed

The status command is designed for quick checks (<2s response time).
Use 'watch' for continuous monitoring:
  watch -n 5 aether pipeline status <job-id>

Examples:
  # Check job status
  aether pipeline status abc-123-def

  # Continuous monitoring (every 5 seconds)
  watch -n 5 aether pipeline status abc-123-def`,
	Args: cobra.ExactArgs(1),
	RunE: runPipelineStatus,
}

// pipelineContinueCmd represents the pipeline continue command
var pipelineContinueCmd = &cobra.Command{
	Use:   "continue [job-id]",
	Short: "Resume a pipeline job",
	Long: `Resume pipeline execution from the next enabled step.

This command is useful for:
  • Resuming after terminal close (session-independent)
  • Continuing after fixing errors
  • Restarting failed jobs
  • Recovering from service downtime

The pipeline will resume from the next enabled step based on your configuration.
If the current step is incomplete, it will retry that step.

Common Scenarios:

  1. Resume after closing terminal:
     Terminal closed mid-pipeline? Just run continue:
       aether pipeline continue <job-id>

  2. Retry after fixing transient error:
     Service was down and retries exhausted? Fix the issue, then:
       aether pipeline continue <job-id>

  3. Continue after manual data correction:
     Fixed malformed FHIR data? Resume processing:
       aether pipeline continue <job-id>

Examples:
  # Resume a paused job
  aether pipeline continue abc-123-def

  # Check status first, then resume
  aether pipeline status abc-123-def
  aether pipeline continue abc-123-def`,
	Args: cobra.ExactArgs(1),
	RunE: runPipelineContinue,
}

func init() {
	rootCmd.AddCommand(pipelineCmd)
	pipelineCmd.AddCommand(pipelineStartCmd)
	pipelineCmd.AddCommand(pipelineStatusCmd)
	pipelineCmd.AddCommand(pipelineContinueCmd)

	// Flags for pipeline start
	pipelineStartCmd.Flags().BoolVar(&noProgress, "no-progress", false, "Disable progress indicators")
}

// validateImportStepMatch ensures the step name matches the input type
func validateImportStepMatch(inputType models.InputType, stepName models.StepName) error {
	switch inputType {
	case models.InputTypeCRTDL, models.InputTypeTORCHURL:
		if stepName != models.StepTorchImport {
			return fmt.Errorf("input type %s requires step '%s', but got '%s'", inputType, models.StepTorchImport, stepName)
		}
	case models.InputTypeLocal:
		if stepName != models.StepLocalImport {
			return fmt.Errorf("input type %s requires step '%s', but got '%s'", inputType, models.StepLocalImport, stepName)
		}
	case models.InputTypeHTTP:
		if stepName != models.StepHttpImport {
			return fmt.Errorf("input type %s requires step '%s', but got '%s'", inputType, models.StepHttpImport, stepName)
		}
	default:
		return fmt.Errorf("unknown input type: %s", inputType)
	}
	return nil
}

// executeStep executes a single pipeline step based on its name
// Returns error if step execution fails
func executeStep(job *models.PipelineJob, stepName models.StepName, config *models.ProjectConfig, logger *lib.Logger, noProgress bool) error {
	jobDir := services.GetJobDir(config.JobsDir, job.JobID)

	switch stepName {
	case models.StepTorchImport, models.StepLocalImport, models.StepHttpImport:
		// Validate step name matches input type
		if err := validateImportStepMatch(job.InputType, stepName); err != nil {
			return fmt.Errorf("step validation failed: %w", err)
		}

		// Create HTTP client
		httpClient := services.NewHTTPClient(30*time.Second, job.Config.Retry, logger)
		showProgress := !noProgress

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
			// Mark job as failed
			failedJob := pipeline.FailJob(job, err.Error())
			// Save failed state
			if saveErr := pipeline.UpdateJob(config.JobsDir, failedJob); saveErr != nil {
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
		fmt.Println("Validation step not yet implemented - job will remain at this step")
		return nil

	case models.StepCSVConversion:
		fmt.Println("CSV conversion step not yet implemented - job will remain at this step")
		return nil

	case models.StepParquetConversion:
		fmt.Println("Parquet conversion step not yet implemented - job will remain at this step")
		return nil

	default:
		return fmt.Errorf("unknown step: %s", stepName)
	}
}

func runPipelineStart(cmd *cobra.Command, args []string) error {
	inputSource := args[0]

	// Load configuration
	config, err := services.LoadConfig(cfgFile)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// Validate service connectivity (T062)
	fmt.Println("Validating service connectivity...")
	if err := config.ValidateServiceConnectivity(); err != nil {
		return fmt.Errorf("service connectivity check failed: %w\n\nPlease ensure all required services are running and accessible", err)
	}
	fmt.Println("✓ All required services are reachable")

	// Create logger
	logLevel := lib.LogLevelInfo
	if verbose {
		logLevel = lib.LogLevelDebug
	}
	logger := lib.NewLogger(logLevel)

	// Create job
	logger.Info("Creating new pipeline job", "input", inputSource)
	job, err := pipeline.CreateJob(inputSource, *config, logger)
	if err != nil {
		return fmt.Errorf("failed to create job: %w", err)
	}

	lib.LogJobCreated(logger, job.JobID, inputSource)

	fmt.Printf("✓ Created pipeline job: %s\n", job.JobID)
	fmt.Printf("  Input: %s\n", inputSource)
	fmt.Printf("  Type: %s\n", job.InputType)
	fmt.Printf("\n")

	// Acquire job lock to prevent concurrent execution
	// Lock is automatically released when function returns (via defer)
	lock, err := services.AcquireJobLock(config.JobsDir, job.JobID, logger)
	if err != nil {
		return fmt.Errorf("cannot start pipeline: %w\n\nAnother process may be working on this job", err)
	}
	defer func() {
		if err := lock.Release(); err != nil {
			logger.Error("Failed to release job lock", "error", err)
		}
	}()

	// Start the job (with lock held)
	startedJob := pipeline.StartJob(job)

	// Save updated state
	if err := pipeline.UpdateJob(config.JobsDir, startedJob); err != nil {
		return fmt.Errorf("failed to update job state: %w", err)
	}

	// Execute import step
	fmt.Printf("Starting %s step...\n", startedJob.CurrentStep)
	httpClient := services.NewHTTPClient(
		time.Duration(config.Retry.InitialBackoffMs)*time.Millisecond*10, // Longer timeout for downloads
		config.Retry,
		logger,
	)

	showProgress := !noProgress
	importedJob, err := pipeline.ExecuteImportStep(startedJob, logger, httpClient, showProgress)

	if err != nil {
		// Mark job as failed
		failedJob := pipeline.FailJob(importedJob, err.Error())
		// Save failed state
		if saveErr := pipeline.UpdateJob(config.JobsDir, failedJob); saveErr != nil {
			logger.Error("Failed to save job state", "error", saveErr)
		}
		return fmt.Errorf("%s step failed: %w", startedJob.CurrentStep, err)
	}

	// Save successful state after import
	if saveErr := pipeline.UpdateJob(config.JobsDir, importedJob); saveErr != nil {
		logger.Error("Failed to save job state", "error", saveErr)
	}

	fmt.Printf("\n✓ %s completed successfully\n", importedJob.CurrentStep)
	fmt.Printf("  Files: %d\n", importedJob.TotalFiles)
	fmt.Printf("  Size: %s\n", formatBytes(importedJob.TotalBytes))
	fmt.Printf("\n")

	// Continue with remaining enabled steps automatically
	currentJob := importedJob
	for {
		// Determine next step
		currentStepName := models.StepName(currentJob.CurrentStep)
		nextStepName := currentJob.Config.Pipeline.GetNextStep(currentStepName)

		if nextStepName == "" {
			// No more steps - mark job as complete
			fmt.Println("All steps completed, marking job as complete...")
			completedJob := pipeline.CompleteJob(currentJob)
			if err := pipeline.UpdateJob(config.JobsDir, completedJob); err != nil {
				return fmt.Errorf("failed to update job: %w", err)
			}
			fmt.Printf("\n✓ Pipeline completed successfully\n")
			fmt.Printf("Job ID: %s\n", completedJob.JobID)
			return nil
		}

		// Advance to next step
		fmt.Printf("\nAdvancing to step: %s\n", nextStepName)
		advancedJob, err := pipeline.AdvanceToNextStep(currentJob)
		if err != nil {
			return fmt.Errorf("failed to advance to next step: %w", err)
		}

		if err := pipeline.UpdateJob(config.JobsDir, advancedJob); err != nil {
			return fmt.Errorf("failed to save job state: %w", err)
		}

		// Execute the next step
		if err := executeStep(advancedJob, nextStepName, config, logger, noProgress); err != nil {
			// Mark job as failed
			failedJob := pipeline.FailJob(advancedJob, err.Error())
			if saveErr := pipeline.UpdateJob(config.JobsDir, failedJob); saveErr != nil {
				logger.Error("Failed to save failed job state", "error", saveErr)
			}
			return err
		}

		// Update current job reference
		currentJob = advancedJob
	}
}

func runPipelineStatus(cmd *cobra.Command, args []string) error {
	jobID := args[0]

	// Load configuration
	config, err := services.LoadConfig(cfgFile)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// Load job
	job, err := pipeline.LoadJob(config.JobsDir, jobID)
	if err != nil {
		return fmt.Errorf("failed to load job: %w", err)
	}

	// Display job status
	fmt.Println(pipeline.GetJobSummary(job))

	// Display step details
	fmt.Println("Steps:")
	for _, step := range job.Steps {
		status := getStatusSymbol(step.Status)
		fmt.Printf("  %s %-20s - %s", status, step.Name, step.Status)

		if step.Status == models.StepStatusCompleted || step.Status == models.StepStatusInProgress {
			fmt.Printf(" (%d files", step.FilesProcessed)
			if step.BytesProcessed > 0 {
				fmt.Printf(", %s", formatBytes(step.BytesProcessed))
			}
			fmt.Printf(")")
		}

		if step.RetryCount > 0 {
			fmt.Printf(" [%d retries]", step.RetryCount)
		}

		if step.LastError != nil {
			fmt.Printf("\n    Error: %s", step.LastError.Message)
		}

		fmt.Println()
	}

	return nil
}

func runPipelineContinue(cmd *cobra.Command, args []string) error {
	jobID := args[0]

	// Load configuration
	config, err := services.LoadConfig(cfgFile)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// Create logger
	logLevel := lib.LogLevelInfo
	if verbose {
		logLevel = lib.LogLevelDebug
	}
	logger := lib.NewLogger(logLevel)

	// Load existing job
	fmt.Printf("Loading job %s...\n", jobID)
	job, err := pipeline.LoadJob(config.JobsDir, jobID)
	if err != nil {
		return fmt.Errorf("failed to load job: %w", err)
	}

	// Check job status
	if job.Status == models.JobStatusCompleted {
		fmt.Println("✓ Job already completed")
		return nil
	}

	fmt.Printf("Current status: %s\n", job.Status)
	fmt.Printf("Current step: %s\n", job.CurrentStep)

	// Acquire job lock to prevent concurrent execution
	lock, err := services.AcquireJobLock(config.JobsDir, jobID, logger)
	if err != nil {
		return fmt.Errorf("cannot continue pipeline: %w\n\nAnother process may be working on this job. Wait for it to complete or check job status", err)
	}
	defer func() {
		if err := lock.Release(); err != nil {
			logger.Error("Failed to release job lock", "error", err)
		}
	}()

	// Get current step and check if it's completed
	currentStepName := models.StepName(job.CurrentStep)
	currentStep, found := models.GetStepByName(*job, currentStepName)

	var stepToExecute models.StepName
	var jobToExecute *models.PipelineJob

	if !found {
		return fmt.Errorf("current step %s not found in job", currentStepName)
	}

	// Check if current step is completed
	if currentStep.Status == models.StepStatusCompleted {
		// Current step is done, move to next step
		nextStepName := job.Config.Pipeline.GetNextStep(currentStepName)

		if nextStepName == "" {
			// No more steps - mark job as complete
			fmt.Println("All steps completed, marking job as complete...")
			completedJob := pipeline.CompleteJob(job)
			if err := pipeline.UpdateJob(config.JobsDir, completedJob); err != nil {
				return fmt.Errorf("failed to update job: %w", err)
			}
			fmt.Println("✓ Job completed successfully")
			return nil
		}

		// Advance to next step
		fmt.Printf("Current step '%s' is completed, advancing to next step: %s\n", currentStepName, nextStepName)
		advancedJob, err := pipeline.AdvanceToNextStep(job)
		if err != nil {
			return fmt.Errorf("failed to advance to next step: %w", err)
		}

		if err := pipeline.UpdateJob(config.JobsDir, advancedJob); err != nil {
			return fmt.Errorf("failed to save job state: %w", err)
		}

		stepToExecute = nextStepName
		jobToExecute = advancedJob
	} else {
		// Current step is NOT completed (in_progress, failed, or pending) - resume it
		fmt.Printf("Resuming incomplete step: %s (status: %s)\n", currentStepName, currentStep.Status)
		stepToExecute = currentStepName
		jobToExecute = job
	}

	fmt.Printf("\nResuming pipeline execution...\n")
	fmt.Printf("Executing step: %s\n\n", stepToExecute)

	// Execute the step
	if err := executeStep(jobToExecute, stepToExecute, config, logger, noProgress); err != nil {
		return err
	}

	fmt.Printf("\nUse 'aether pipeline status %s' to check progress\n", jobID)
	fmt.Printf("Or run 'aether pipeline continue %s' to proceed to the next step\n", jobID)

	return nil
}

func getStatusSymbol(status models.StepStatus) string {
	switch status {
	case models.StepStatusCompleted:
		return "✓"
	case models.StepStatusInProgress:
		return "→"
	case models.StepStatusFailed:
		return "✗"
	case models.StepStatusPending:
		return " "
	default:
		return " "
	}
}

func formatBytes(bytes int64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
	)

	if bytes >= GB {
		return fmt.Sprintf("%.2f GB", float64(bytes)/GB)
	} else if bytes >= MB {
		return fmt.Sprintf("%.2f MB", float64(bytes)/MB)
	} else if bytes >= KB {
		return fmt.Sprintf("%.2f KB", float64(bytes)/KB)
	}
	return fmt.Sprintf("%d B", bytes)
}
