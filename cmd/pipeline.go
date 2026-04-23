package cmd

import (
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/pipeline"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

var (
	noProgress     bool
	localImportDir string // CLI flag for local import directory override
	allowHTTPCRTDL bool   // CLI flag acknowledging http_import + CRTDL semantic mismatch
	// errPipelinePaused is returned when the pipeline pauses at a wait step
	errPipelinePaused = errors.New("pipeline paused at wait step")
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
	Use:   "start <crtdl> [input]",
	Short: "Start a new pipeline job",
	Long: `Start a new Data Use Process pipeline job.

The CRTDL file is always required as the first positional argument. The
optional second positional is the input source for the enabled import step:
  • omitted (torch_import): CRTDL is submitted to TORCH for extraction
  • omitted (local_import): data dir is read from --dir or config
  • local directory path (local_import): files imported from disk
  • HTTP(S) URL (http_import): single NDJSON file downloaded
  • TORCH result URL (torch_import): auto-detected when the URL contains
    /fhir/extraction/ or /fhir/result/; extraction is skipped and results
    are polled directly

Combining http_import with a CRTDL requires --allow-http-crtdl since HTTP
data may not match the CRTDL query.

Examples:
  # Extract data using CRTDL query via TORCH
  aether pipeline start query.crtdl

  # Local import with CRTDL for flattening (data dir from config)
  aether pipeline start query.crtdl

  # Local import with CRTDL (data dir as positional)
  aether pipeline start query.crtdl /path/to/fhir/data

  # Local import with CRTDL (data dir via flag)
  aether pipeline start query.crtdl --dir /path/to/fhir/data

  # HTTP import piped through flattening (acknowledges data/CRTDL mismatch)
  aether pipeline start query.crtdl https://example.com/fhir/Patient.ndjson \
      --allow-http-crtdl

  # Direct TORCH URL (skip extraction, poll and download results)
  aether pipeline start query.crtdl \
      "https://torch.example.com/fhir/extraction/result-123"

  # Start without progress indicators
  aether pipeline start query.crtdl --no-progress`,
	Args: cobra.RangeArgs(1, 2),
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

	pipelineStartCmd.Flags().BoolVar(&noProgress, "no-progress", false, "Disable progress indicators")
	pipelineStartCmd.Flags().StringVar(&localImportDir, "dir", "", "Directory for local import (overrides config)")
	pipelineStartCmd.Flags().BoolVar(&allowHTTPCRTDL, "allow-http-crtdl", false, "Acknowledge that combining http_import with a CRTDL may not match the endpoint's data")
}

// validateImportStepMatch ensures the step name is compatible with the input type
// CRTDL is compatible with both torch (for TORCH extraction) and local_import (for local import + flattening)
func validateImportStepMatch(inputType models.InputType, stepName models.StepName) error {
	switch inputType {
	case models.InputTypeCRTDL:
		// CRTDL is compatible with torch (extraction) or local_import (flattening with local data)
		if stepName != models.StepTorchImport && stepName != models.StepLocalImport {
			return fmt.Errorf("input type %s requires step '%s' or '%s', but got '%s'", inputType, models.StepTorchImport, models.StepLocalImport, stepName)
		}
	case models.InputTypeTORCHURL:
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
		if err := validateImportStepMatch(job.InputType, stepName); err != nil {
			return fmt.Errorf("step validation failed: %w", err)
		}

		httpClient := services.NewHTTPClient(30*time.Second, job.Config.Retry, job.Config.TLS, logger)
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
		fmt.Println("Starting DIMP pseudonymization step...")
		if err := pipeline.ExecuteDIMPStep(job, jobDir, logger); err != nil {
			failedJob := pipeline.FailJob(job, err.Error())
			if saveErr := pipeline.UpdateJob(config.JobsDir, failedJob); saveErr != nil {
				logger.Error("Failed to save job state", "error", saveErr)
			}
			return fmt.Errorf("DIMP step failed: %w", err)
		}

		if err := pipeline.UpdateJob(config.JobsDir, job); err != nil {
			return fmt.Errorf("failed to save job state: %w", err)
		}

		fmt.Printf("\n✓ DIMP pseudonymization completed\n")
		return nil

	case models.StepValidation:
		fmt.Println("Starting validation step...")
		if err := pipeline.ExecuteValidationStep(job, jobDir, logger); err != nil {
			failedJob := pipeline.FailJob(job, err.Error())
			if saveErr := pipeline.UpdateJob(config.JobsDir, failedJob); saveErr != nil {
				logger.Error("Failed to save job state", "error", saveErr)
			}
			return fmt.Errorf("validation step failed: %w", err)
		}

		if err := pipeline.UpdateJob(config.JobsDir, job); err != nil {
			return fmt.Errorf("failed to save job state: %w", err)
		}

		fmt.Printf("\n✓ Validation completed\n")
		return nil

	case models.StepFlattening:
		fmt.Println("Starting flattening step...")
		if err := pipeline.ExecuteFlatteningStep(job, jobDir, logger); err != nil {
			failedJob := pipeline.FailJob(job, err.Error())
			if saveErr := pipeline.UpdateJob(config.JobsDir, failedJob); saveErr != nil {
				logger.Error("Failed to save job state", "error", saveErr)
			}
			return fmt.Errorf("flattening step failed: %w", err)
		}

		if err := pipeline.UpdateJob(config.JobsDir, job); err != nil {
			return fmt.Errorf("failed to save job state: %w", err)
		}

		fmt.Printf("\n✓ Flattening completed\n")
		return nil

	case models.StepSend:
		fmt.Println("Starting send step...")
		if err := pipeline.ExecuteSendStep(job, jobDir, logger); err != nil {
			failedJob := pipeline.FailJob(job, err.Error())
			if saveErr := pipeline.UpdateJob(config.JobsDir, failedJob); saveErr != nil {
				logger.Error("Failed to save job state", "error", saveErr)
			}
			return fmt.Errorf("send step failed: %w", err)
		}

		if err := pipeline.UpdateJob(config.JobsDir, job); err != nil {
			return fmt.Errorf("failed to save job state: %w", err)
		}

		fmt.Printf("\n✓ Send completed\n")
		return nil

	case models.StepWait:
		// Execute wait step - creates empty wait directory and pauses pipeline
		stepIndex := -1
		for i, step := range job.Config.Pipeline.EnabledSteps {
			if step == models.StepWait && string(step) == job.CurrentStep {
				stepIndex = i
				break
			}
		}
		if stepIndex == -1 {
			return fmt.Errorf("wait step not found in enabled steps")
		}

		if err := pipeline.ExecuteWaitStep(job, config.JobsDir, stepIndex); err != nil {
			return fmt.Errorf("wait step failed: %w", err)
		}

		// Mark the wait step as waiting (job stays in_progress)
		for i := range job.Steps {
			if job.Steps[i].Name == models.StepWait && job.Steps[i].Status == models.StepStatusInProgress {
				job.Steps[i].Status = models.StepStatusWaiting
				break
			}
		}

		if err := pipeline.UpdateJob(config.JobsDir, job); err != nil {
			return fmt.Errorf("failed to save job state: %w", err)
		}

		// Get previous step name for wait directory path
		var prevStepName models.StepName
		for i := stepIndex - 1; i >= 0; i-- {
			if job.Config.Pipeline.EnabledSteps[i] != models.StepWait {
				prevStepName = job.Config.Pipeline.EnabledSteps[i]
				break
			}
		}
		waitDir := services.GetWaitStepDir(config.JobsDir, job.JobID, prevStepName)
		fmt.Printf("\n⏸ Pipeline paused at wait step\n")
		fmt.Printf("  Wait directory: %s\n", waitDir)
		fmt.Printf("  The directory is EMPTY - place your modified data there\n")
		fmt.Printf("\n  To continue: aether pipeline continue %s\n", job.JobID)
		return errPipelinePaused // Signal to break out of pipeline loop

	default:
		return fmt.Errorf("unknown step: %s", stepName)
	}
}

func runPipelineStart(cmd *cobra.Command, args []string) error {
	// First positional is always the CRTDL; second (optional) is the input
	// source for the enabled import step.
	crtdlPath := args[0]
	var inputSource string
	if len(args) > 1 {
		inputSource = args[1]
	}

	if t, err := lib.DetectInputType(crtdlPath); err != nil {
		return fmt.Errorf("invalid CRTDL argument %q: %w", crtdlPath, err)
	} else if t != models.InputTypeCRTDL {
		return fmt.Errorf("first argument must be a CRTDL file, got %s: %q", t, crtdlPath)
	}

	config, err := services.LoadConfig(cfgFile)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// Apply --dir flag override if provided
	if localImportDir != "" {
		config.Services.LocalImport.Dir = localImportDir
	}

	// Validate local_import directory is configured when local_import step is enabled
	if config.Pipeline.IsStepEnabled(models.StepLocalImport) && !config.Pipeline.IsStepEnabled(models.StepTorchImport) {
		hasSourceDir := config.Services.LocalImport.Dir != "" || inputSource != ""
		if !hasSourceDir {
			return fmt.Errorf("local_import step enabled but no directory specified\n\nProvide directory via:\n  1. Positional: aether pipeline start <crtdl> /path/to/data\n  2. --dir flag: aether pipeline start <crtdl> --dir /path/to/data\n  3. Config file: services.local_import.dir in aether.yaml")
		}
	}

	// Gate: http_import + CRTDL requires explicit acknowledgement that HTTP
	// data may not semantically match the CRTDL query.
	if config.Pipeline.IsStepEnabled(models.StepHttpImport) && !allowHTTPCRTDL {
		return fmt.Errorf("combining http_import with a CRTDL requires --allow-http-crtdl\n\nThe HTTP endpoint's data may not match the CRTDL query.\nPass --allow-http-crtdl to acknowledge and proceed")
	}

	fmt.Println("Validating service connectivity...")
	connectTransport, _ := services.BuildTLSTransport(config.TLS, lib.DefaultLogger)
	if err := config.ValidateServiceConnectivity(connectTransport); err != nil {
		return fmt.Errorf("service connectivity check failed: %w\n\nPlease ensure all required services are running and accessible", err)
	}
	fmt.Println("✓ All required services are reachable")

	logLevel := lib.LogLevelInfo
	if verbose {
		logLevel = lib.LogLevelDebug
	}
	logger := lib.NewLogger(logLevel)

	logger.Info("Creating new pipeline job", "crtdl", crtdlPath, "input", inputSource, "localImportDir", config.Services.LocalImport.Dir)
	job, err := pipeline.CreateJob(inputSource, crtdlPath, *config, logger)
	if err != nil {
		return fmt.Errorf("failed to create job: %w", err)
	}

	lib.LogJobCreated(logger, job.JobID, inputSource)

	fmt.Printf("✓ Created pipeline job: %s\n", job.JobID)
	fmt.Printf("  Input: %s\n", inputSource)
	fmt.Printf("  Type: %s\n", job.InputType)
	fmt.Printf("\n")

	lock, err := services.AcquireJobLock(config.JobsDir, job.JobID, logger)
	if err != nil {
		return fmt.Errorf("cannot start pipeline: %w\n\nAnother process may be working on this job", err)
	}
	defer func() {
		if err := lock.Release(); err != nil {
			logger.Error("Failed to release job lock", "error", err)
		}
	}()

	startedJob := pipeline.StartJob(job)

	if err := pipeline.UpdateJob(config.JobsDir, startedJob); err != nil {
		return fmt.Errorf("failed to update job state: %w", err)
	}

	fmt.Printf("Starting %s step...\n", startedJob.CurrentStep)
	httpClient := services.NewHTTPClient(
		time.Duration(config.Retry.InitialBackoffMs)*time.Millisecond*10, // Longer timeout for downloads
		config.Retry,
		config.TLS,
		logger,
	)

	showProgress := !noProgress
	importedJob, err := pipeline.ExecuteImportStep(startedJob, logger, httpClient, showProgress)

	if err != nil {
		failedJob := pipeline.FailJob(importedJob, err.Error())
		if saveErr := pipeline.UpdateJob(config.JobsDir, failedJob); saveErr != nil {
			logger.Error("Failed to save job state", "error", saveErr)
		}
		return fmt.Errorf("%s step failed: %w", startedJob.CurrentStep, err)
	}

	if saveErr := pipeline.UpdateJob(config.JobsDir, importedJob); saveErr != nil {
		logger.Error("Failed to save job state", "error", saveErr)
	}

	fmt.Printf("\n✓ %s completed successfully\n", importedJob.CurrentStep)
	fmt.Printf("  Files: %d\n", importedJob.TotalFiles)
	fmt.Printf("  Size: %s\n", formatBytes(importedJob.TotalBytes))
	fmt.Printf("\n")

	currentJob := importedJob
	for {
		currentStepName := models.StepName(currentJob.CurrentStep)
		nextStepName := currentJob.Config.Pipeline.GetNextStep(currentStepName)

		if nextStepName == "" {
			fmt.Println("All steps completed, marking job as complete...")
			completedJob := pipeline.CompleteJob(currentJob)
			if err := pipeline.UpdateJob(config.JobsDir, completedJob); err != nil {
				return fmt.Errorf("failed to update job: %w", err)
			}
			fmt.Printf("\n✓ Pipeline completed successfully\n")
			fmt.Printf("Job ID: %s\n", completedJob.JobID)
			return nil
		}

		fmt.Printf("\nAdvancing to step: %s\n", nextStepName)
		advancedJob, err := pipeline.AdvanceToNextStep(currentJob)
		if err != nil {
			return fmt.Errorf("failed to advance to next step: %w", err)
		}

		if err := pipeline.UpdateJob(config.JobsDir, advancedJob); err != nil {
			return fmt.Errorf("failed to save job state: %w", err)
		}

		if err := executeStep(advancedJob, nextStepName, config, logger, noProgress); err != nil {
			// Check if this is a "paused" signal from wait step
			if err == errPipelinePaused {
				return nil // Exit gracefully - pipeline is paused
			}
			// Mark job as failed
			failedJob := pipeline.FailJob(advancedJob, err.Error())
			if saveErr := pipeline.UpdateJob(config.JobsDir, failedJob); saveErr != nil {
				logger.Error("Failed to save failed job state", "error", saveErr)
			}
			return err
		}

		currentJob = advancedJob
	}
}

func runPipelineStatus(cmd *cobra.Command, args []string) error {
	jobID := args[0]

	config, err := services.LoadConfig(cfgFile)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	job, err := pipeline.LoadJob(config.JobsDir, jobID)
	if err != nil {
		return fmt.Errorf("failed to load job: %w", err)
	}

	fmt.Println(pipeline.GetJobSummary(job))

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

		if step.LastError != nil {
			fmt.Printf("\n    Error: %s", step.LastError.Message)
		}

		fmt.Println()
	}

	return nil
}

func runPipelineContinue(cmd *cobra.Command, args []string) error {
	jobID := args[0]

	config, err := services.LoadConfig(cfgFile)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	logLevel := lib.LogLevelInfo
	if verbose {
		logLevel = lib.LogLevelDebug
	}
	logger := lib.NewLogger(logLevel)

	fmt.Printf("Loading job %s...\n", jobID)
	job, err := pipeline.LoadJob(config.JobsDir, jobID)
	if err != nil {
		return fmt.Errorf("failed to load job: %w", err)
	}

	if job.Status == models.JobStatusCompleted {
		fmt.Println("✓ Job already completed")
		return nil
	}

	fmt.Printf("Current status: %s\n", job.Status)
	fmt.Printf("Current step: %s\n", job.CurrentStep)

	lock, err := services.AcquireJobLock(config.JobsDir, jobID, logger)
	if err != nil {
		return fmt.Errorf("cannot continue pipeline: %w\n\nAnother process may be working on this job. Wait for it to complete or check job status", err)
	}
	defer func() {
		if err := lock.Release(); err != nil {
			logger.Error("Failed to release job lock", "error", err)
		}
	}()

	// Idempotent — reconciles state if the original `pipeline start` crashed
	// between writing the prepared CRTDL and persisting the new CRTDLPath.
	if err := pipeline.PrepareCRTDL(job, logger); err != nil {
		return fmt.Errorf("failed to prepare CRTDL: %w", err)
	}
	if err := pipeline.UpdateJob(config.JobsDir, job); err != nil {
		return fmt.Errorf("failed to save job state: %w", err)
	}

	currentStepName := models.StepName(job.CurrentStep)
	currentStep, found := models.GetStepByName(*job, currentStepName)

	var stepToExecute models.StepName
	var jobToExecute *models.PipelineJob

	if !found {
		return fmt.Errorf("current step %s not found in job", currentStepName)
	}

	if currentStep.Status == models.StepStatusCompleted {
		nextStepName := job.Config.Pipeline.GetNextStep(currentStepName)

		if nextStepName == "" {
			fmt.Println("All steps completed, marking job as complete...")
			completedJob := pipeline.CompleteJob(job)
			if err := pipeline.UpdateJob(config.JobsDir, completedJob); err != nil {
				return fmt.Errorf("failed to update job: %w", err)
			}
			fmt.Println("✓ Job completed successfully")
			return nil
		}

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
	} else if currentStepName == models.StepWait && currentStep.Status == models.StepStatusWaiting {
		// Special handling for wait step in "waiting" status
		// Check if wait directory has files - if so, complete wait step and continue
		fmt.Printf("Resuming incomplete step: %s (status: %s)\n", currentStepName, currentStep.Status)

		// Find the wait step index to get the previous step
		waitStepIndex := -1
		for i, step := range job.Config.Pipeline.EnabledSteps {
			if step == models.StepWait && string(step) == job.CurrentStep {
				waitStepIndex = i
				break
			}
		}
		if waitStepIndex == -1 {
			return fmt.Errorf("wait step not found in enabled steps")
		}

		// Get previous step name to determine wait directory
		prevStepName, err := pipeline.GetPreviousStepForWait(job.Config.Pipeline.EnabledSteps, waitStepIndex)
		if err != nil {
			return fmt.Errorf("failed to find previous step for wait: %w", err)
		}

		waitDir := services.GetWaitStepDir(config.JobsDir, job.JobID, prevStepName)
		fileCount, err := pipeline.CountFilesInDir(waitDir)
		if err != nil {
			return fmt.Errorf("failed to check wait directory: %w", err)
		}

		if fileCount == 0 {
			// Directory is empty - stay paused
			fmt.Printf("\n⏸ Pipeline still paused at wait step\n")
			fmt.Printf("  Wait directory: %s\n", waitDir)
			fmt.Printf("  The directory is EMPTY - place your modified data there\n")
			fmt.Printf("\n  To continue: aether pipeline continue %s\n", job.JobID)
			return errPipelinePaused
		}

		// Files present - mark wait step as completed and continue
		fmt.Printf("  Found %d file(s) in wait directory\n", fileCount)
		fmt.Printf("  ✓ Wait step completed\n\n")

		// Mark wait step as completed
		for i := range job.Steps {
			if job.Steps[i].Name == models.StepWait && job.Steps[i].Status == models.StepStatusWaiting {
				now := time.Now()
				job.Steps[i].Status = models.StepStatusCompleted
				job.Steps[i].CompletedAt = &now
				break
			}
		}

		if err := pipeline.UpdateJob(config.JobsDir, job); err != nil {
			return fmt.Errorf("failed to save job state: %w", err)
		}

		// Get and advance to next step
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
		fmt.Printf("Advancing to step: %s\n", nextStepName)
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
		fmt.Printf("Resuming incomplete step: %s (status: %s)\n", currentStepName, currentStep.Status)
		stepToExecute = currentStepName
		jobToExecute = job
	}

	fmt.Printf("\nResuming pipeline execution...\n")
	fmt.Printf("Executing step: %s\n\n", stepToExecute)

	if err := executeStep(jobToExecute, stepToExecute, config, logger, noProgress); err != nil {
		if err == errPipelinePaused {
			return nil // Exit gracefully - pipeline is paused at wait step
		}
		// Mark job as failed
		failedJob := pipeline.FailJob(jobToExecute, err.Error())
		if saveErr := pipeline.UpdateJob(config.JobsDir, failedJob); saveErr != nil {
			logger.Error("Failed to save failed job state", "error", saveErr)
		}
		return err
	}

	// Loop through remaining steps (same as runPipelineStart)
	currentJob := jobToExecute
	for {
		currentStepName := models.StepName(currentJob.CurrentStep)
		nextStepName := currentJob.Config.Pipeline.GetNextStep(currentStepName)

		if nextStepName == "" {
			fmt.Println("All steps completed, marking job as complete...")
			completedJob := pipeline.CompleteJob(currentJob)
			if err := pipeline.UpdateJob(config.JobsDir, completedJob); err != nil {
				return fmt.Errorf("failed to update job: %w", err)
			}
			fmt.Printf("\n✓ Pipeline completed successfully\n")
			fmt.Printf("Job ID: %s\n", completedJob.JobID)
			return nil
		}

		fmt.Printf("\nAdvancing to step: %s\n", nextStepName)
		advancedJob, err := pipeline.AdvanceToNextStep(currentJob)
		if err != nil {
			return fmt.Errorf("failed to advance to next step: %w", err)
		}

		if err := pipeline.UpdateJob(config.JobsDir, advancedJob); err != nil {
			return fmt.Errorf("failed to save job state: %w", err)
		}

		if err := executeStep(advancedJob, nextStepName, config, logger, noProgress); err != nil {
			if err == errPipelinePaused {
				return nil // Exit gracefully - pipeline is paused at wait step
			}
			// Mark job as failed
			failedJob := pipeline.FailJob(advancedJob, err.Error())
			if saveErr := pipeline.UpdateJob(config.JobsDir, failedJob); saveErr != nil {
				logger.Error("Failed to save failed job state", "error", saveErr)
			}
			return err
		}

		currentJob = advancedJob
	}
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
