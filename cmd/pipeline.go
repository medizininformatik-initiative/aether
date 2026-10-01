package cmd

import (
	"errors"
	"fmt"
	"net/http"
	"os"

	"github.com/spf13/cobra"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/pipeline"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

var (
	noProgress          bool
	localImportDir      string // CLI flag for local import directory override
	allowHTTPCRTDL      bool   // CLI flag acknowledging http_import + CRTDL semantic mismatch
	anonymizationConfig string // CLI flag for the DIMP anonymization YAML override
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
	Use:   "start <config> <crtdl> [input]",
	Short: "Start a new pipeline job",
	Long: `Start a new Data Use Process pipeline job.

Arguments:
  <config>  Path to aether.yaml (required, first positional)
  <crtdl>   CRTDL JSON file (required, second positional)
  [input]   Optional input source for the enabled import step:
              • omitted (torch_import): CRTDL is submitted to TORCH
              • omitted (local_import): data dir is read from --dir or config
              • local directory path (local_import): files imported from disk
              • HTTP(S) URL (http_import): single NDJSON file downloaded
              • TORCH result URL (torch_import): auto-detected when the URL
                contains /fhir/extraction/ or /fhir/result/; extraction is
                skipped and results are polled directly

Combining http_import with a CRTDL requires --allow-http-crtdl since HTTP
data may not match the CRTDL query.

--anonymization-config gives the anonymization YAML that aether sends to DIMP
with each request. It overrides services.dimp.anonymization_config. Without a
path, DIMP uses its own anonymization file.

Examples:
  # Extract data using CRTDL query via TORCH
  aether pipeline start aether.yaml crtdl.json

  # Local import with CRTDL for flattening (data dir from config)
  aether pipeline start aether.yaml crtdl.json

  # Local import with CRTDL (data dir as positional)
  aether pipeline start aether.yaml crtdl.json /path/to/fhir/data

  # Local import with CRTDL (data dir via flag)
  aether pipeline start aether.yaml crtdl.json --dir /path/to/fhir/data

  # HTTP import piped through flattening (acknowledges data/CRTDL mismatch)
  aether pipeline start aether.yaml crtdl.json https://example.com/fhir/Patient.ndjson \
      --allow-http-crtdl

  # Direct TORCH URL (skip extraction, poll and download results)
  aether pipeline start aether.yaml crtdl.json \
      "https://torch.example.com/fhir/extraction/result-123"

  # Send an anonymization YAML to DIMP with each request
  aether pipeline start aether.yaml crtdl.json \
      --anonymization-config anonymization.yaml

  # Start without progress indicators
  aether pipeline start aether.yaml crtdl.json --no-progress`,
	Args: cobra.RangeArgs(2, 3),
	RunE: runPipelineStart,
}

// pipelineStatusCmd represents the pipeline status command
var pipelineStatusCmd = &cobra.Command{
	Use:   "status <config> <job-id>",
	Short: "Check pipeline job status",
	Long: `Display the current status of a pipeline job.

Arguments:
  <config>  Path to aether.yaml (required, first positional)
  <job-id>  Pipeline job ID (required, second positional)

Shows:
  • Job ID and current status
  • Current step being executed
  • Progress for each step (files processed, errors, retries)
  • Total files and data processed
  • Error messages if job failed

The status command is designed for quick checks (<2s response time).
Use 'watch' for continuous monitoring:
  watch -n 5 aether pipeline status aether.yaml <job-id>

Examples:
  # Check job status
  aether pipeline status aether.yaml abc-123-def

  # Continuous monitoring (every 5 seconds)
  watch -n 5 aether pipeline status aether.yaml abc-123-def`,
	Args: cobra.ExactArgs(2),
	RunE: runPipelineStatus,
}

// pipelineContinueCmd represents the pipeline continue command
var pipelineContinueCmd = &cobra.Command{
	Use:   "continue <config> <job-id>",
	Short: "Resume a pipeline job",
	Long: `Resume pipeline execution from the next enabled step.

Arguments:
  <config>  Path to aether.yaml (required, first positional)
  <job-id>  Pipeline job ID (required, second positional)

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
       aether pipeline continue aether.yaml <job-id>

  2. Retry after fixing transient error:
     Service was down and retries exhausted? Fix the issue, then:
       aether pipeline continue aether.yaml <job-id>

  3. Continue after manual data correction:
     Fixed malformed FHIR data? Resume processing:
       aether pipeline continue aether.yaml <job-id>

Examples:
  # Resume a paused job
  aether pipeline continue aether.yaml abc-123-def

  # Check status first, then resume
  aether pipeline status aether.yaml abc-123-def
  aether pipeline continue aether.yaml abc-123-def

  # Resume without progress indicators
  aether pipeline continue aether.yaml abc-123-def --no-progress`,
	Args: cobra.ExactArgs(2),
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
	pipelineStartCmd.Flags().StringVar(&anonymizationConfig, "anonymization-config", "", "Anonymization YAML sent to DIMP with each request (overrides config)")

	pipelineContinueCmd.Flags().BoolVar(&noProgress, "no-progress", false, "Disable progress indicators")
}

// checkFlatteningLookup validates the flatten-lookup file before the first
// step starts, so a defective file stops the job before hours of extraction.
// The check runs only when the flattening step is enabled. It returns the
// warnings also with an error, and does not log them.
func checkFlatteningLookup(config *models.ProjectConfig) ([]services.LookupWarning, error) {
	if !config.Pipeline.IsStepEnabled(models.StepFlattening) {
		return nil, nil
	}
	warnings, err := services.VerifyLookupFile(config.Services.Flattening.LookupPath)
	if err != nil {
		return warnings, fmt.Errorf("lookup file check failed: %w\n\nCorrect the file at services.flattening.lookup_path before you start the pipeline", err)
	}
	return warnings, nil
}

// verifyFlatteningLookup runs checkFlatteningLookup and logs the warnings at
// once.
func verifyFlatteningLookup(config *models.ProjectConfig, logger *lib.Logger) error {
	warnings, err := checkFlatteningLookup(config)
	logLookupWarnings(warnings, logger)
	return err
}

// commandLogLevel gives the console log level that --verbose selects.
func commandLogLevel() lib.LogLevel {
	if verbose {
		return lib.LogLevelDebug
	}
	return lib.LogLevelInfo
}

// logLookupWarnings writes one WARN line with the count per code. The
// locations can fill many kilobytes, so they go to DEBUG only.
func logLookupWarnings(warnings []services.LookupWarning, logger *lib.Logger) {
	if len(warnings) == 0 {
		return
	}
	counts := make([]any, 0, 2*len(warnings))
	for _, warning := range warnings {
		counts = append(counts, warning.Code, warning.Count)
	}
	logger.Warn("Lookup file has warning findings (use --verbose to see them)", counts...)
	for _, warning := range warnings {
		logger.Debug("Lookup file finding", "code", warning.Code, "locations", warning.Locations)
	}
}

// verifyFlatteningLookupForJob applies the lookup file check to a resumed job.
// A flattening step that is already complete does not need the file, so a
// missing or changed file does not block the continue.
func verifyFlatteningLookupForJob(job *models.PipelineJob, logger *lib.Logger) error {
	for _, step := range job.Steps {
		if step.Name == models.StepFlattening && step.Status != models.StepStatusCompleted {
			return verifyFlatteningLookup(&job.Config, logger)
		}
	}
	return nil
}

func runPipelineStart(cmd *cobra.Command, args []string) error {
	// Positional contract: <config> <crtdl> [input]. The third positional, if
	// supplied, is the input source for the enabled import step.
	crtdlPath := args[1]
	inputSource := optionalArg(args, 2)

	config, err := prepareStartConfig(args[0], crtdlPath, inputSource)
	if err != nil {
		return err
	}

	logger := lib.NewLogger(commandLogLevel())
	defer func() { _ = logger.Close() }()

	lookupWarnings, err := preflightStart(config, crtdlPath, logger)
	if err != nil {
		return err
	}

	job, err := createStartJob(config, crtdlPath, inputSource, logger)
	if err != nil {
		return err
	}
	// The lookup warnings are logged after job.log is attached, so job.log
	// keeps the locations also without --verbose.
	logLookupWarnings(lookupWarnings, logger)
	return runStartJob(config, job, logger)
}

func optionalArg(args []string, index int) string {
	if len(args) > index {
		return args[index]
	}
	return ""
}

// prepareStartConfig loads the configuration, applies the command flags and
// checks that the arguments agree with the enabled import step.
func prepareStartConfig(cfgPath, crtdlPath, inputSource string) (*models.ProjectConfig, error) {
	if err := checkCRTDLArgument(crtdlPath); err != nil {
		return nil, err
	}

	config, err := services.LoadConfig(cfgPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load configuration: %w", err)
	}

	applyStartFlags(config)

	if err := validateStartSources(config, inputSource); err != nil {
		return nil, err
	}
	return config, nil
}

func checkCRTDLArgument(crtdlPath string) error {
	t, err := lib.DetectInputType(crtdlPath)
	if err != nil {
		return fmt.Errorf("invalid CRTDL argument %q: %w", crtdlPath, err)
	}
	if t != models.InputTypeCRTDL {
		return fmt.Errorf("second argument must be a CRTDL file, got %s: %q", t, crtdlPath)
	}
	return nil
}

func applyStartFlags(config *models.ProjectConfig) {
	if localImportDir != "" {
		config.Services.LocalImport.Dir = localImportDir
	}

	// The job keeps the resolved config, so `pipeline continue` sends the same
	// anonymization YAML without the flag.
	if anonymizationConfig != "" {
		config.Services.DIMP.AnonymizationConfig = anonymizationConfig
	}
}

func validateStartSources(config *models.ProjectConfig, inputSource string) error {
	if err := validateLocalImportSource(config, inputSource); err != nil {
		return err
	}

	// Gate: http_import + CRTDL requires explicit acknowledgement that HTTP
	// data may not semantically match the CRTDL query.
	if config.Pipeline.IsStepEnabled(models.StepHttpImport) && !allowHTTPCRTDL {
		return fmt.Errorf("combining http_import with a CRTDL requires --allow-http-crtdl\n\nThe HTTP endpoint's data may not match the CRTDL query.\nPass --allow-http-crtdl to acknowledge and proceed")
	}
	return nil
}

func validateLocalImportSource(config *models.ProjectConfig, inputSource string) error {
	if !config.Pipeline.IsStepEnabled(models.StepLocalImport) || config.Pipeline.IsStepEnabled(models.StepTorchImport) {
		return nil
	}
	if config.Services.LocalImport.Dir != "" || inputSource != "" {
		return nil
	}
	return fmt.Errorf("local_import step enabled but no directory specified\n\nProvide directory via:\n  1. Positional: aether pipeline start <config> <crtdl> /path/to/data\n  2. --dir flag: aether pipeline start <config> <crtdl> --dir /path/to/data\n  3. Config file: services.local_import.dir in aether.yaml")
}

// preflightStart runs the checks that stop a defective start before the job
// directory exists and before hours of extraction. It returns the lookup
// warnings, so the caller can log them after it attaches job.log. If the
// lookup check fails, it logs the warnings at once.
func preflightStart(config *models.ProjectConfig, crtdlPath string, logger *lib.Logger) ([]services.LookupWarning, error) {
	lookupWarnings, err := checkFlatteningLookup(config)
	if err != nil {
		logLookupWarnings(lookupWarnings, logger)
		return nil, err
	}

	if err := services.VerifyCRTDLFile(crtdlPath); err != nil {
		return nil, fmt.Errorf("CRTDL check failed: %w\n\nCorrect the CRTDL file before you start the pipeline", err)
	}

	fmt.Println("Validating service connectivity...")
	connectTransport, _ := services.BuildTLSTransport(config.TLS, lib.DefaultLogger)
	if err := config.ValidateServiceConnectivity(connectTransport); err != nil {
		return nil, fmt.Errorf("service connectivity check failed: %w\n\nPlease ensure all required services are running and accessible", err)
	}
	if err := verifyTORCHServer(config, lib.DefaultLogger); err != nil {
		return nil, err
	}
	if config.Pipeline.IsStepEnabled(models.StepFlattening) {
		if err := checkFlattener(config, connectTransport); err != nil {
			return nil, err
		}
	}
	fmt.Println("✓ All required services are reachable")
	return lookupWarnings, nil
}

// verifyTORCHServer reads the TORCH CapabilityStatement, so a wrong base URL or
// wrong credentials stop the start before the extraction is submitted.
func verifyTORCHServer(config *models.ProjectConfig, logger *lib.Logger) error {
	if !config.Pipeline.IsStepEnabled(models.StepTorchImport) {
		return nil
	}
	httpClient := services.NewHTTPClient(config.Services.TORCH.EffectiveRequestTimeout(), config.Retry, config.TLS, logger)
	torch := services.NewTORCHClient(config.Services.TORCH, httpClient, logger)
	if err := torch.CheckCapabilityStatement(); err != nil {
		return fmt.Errorf("%w\n\nCorrect services.torch.base_url and the TORCH credentials before you start the pipeline", err)
	}
	return nil
}

// checkFlattener verifies that the flattener answers. The check needs an HTTP
// 2xx status from /fhir/metadata and a CapabilityStatement that declares $run.
// ValidateServiceConnectivity accepts any response, and the models package
// cannot use the flattener client.
func checkFlattener(config *models.ProjectConfig, transport *http.Transport) error {
	if err := config.Services.Flattening.Validate(); err != nil {
		return fmt.Errorf("flattener check failed: %w\n\nCorrect the services.flattening settings before you start the pipeline", err)
	}
	client := services.NewFlattenerClient(config.Services.Flattening, config.Retry, transport, lib.DefaultLogger)
	if err := client.HealthCheck(); err != nil {
		return fmt.Errorf("flattener check failed: %w\n\nEnsure that services.flattening.service_url (%s) points to a running fhir-flattener service",
			err, config.Services.Flattening.ServiceURL)
	}
	return nil
}

func createStartJob(config *models.ProjectConfig, crtdlPath, inputSource string, logger *lib.Logger) (*models.PipelineJob, error) {
	// Attach job.log before CreateJob so its diagnostics are captured too. The
	// caller owns this side effect and closes the logger.
	jobID := models.GenerateJobID()
	jobDir := services.GetJobDir(config.JobsDir, jobID)
	if err := os.MkdirAll(jobDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create job directory: %w", err)
	}
	if err := logger.AttachJobLogFile(services.GetJobLogFilePath(config.JobsDir, jobID)); err != nil {
		return nil, fmt.Errorf("failed to attach job log file: %w", err)
	}

	logger.Info("Creating new pipeline job", "crtdl", crtdlPath, "input", inputSource, "localImportDir", config.Services.LocalImport.Dir)
	job, err := pipeline.CreateJob(jobID, inputSource, crtdlPath, *config, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create job: %w", err)
	}

	lib.LogJobCreated(logger, job.JobID, inputSource)

	fmt.Printf("✓ Created pipeline job: %s\n", job.JobID)
	fmt.Printf("  Input: %s\n", inputSource)
	fmt.Printf("  Type: %s\n", job.InputType)
	fmt.Printf("\n")
	return job, nil
}

// runStartJob holds the job lock and runs the job until it completes, pauses
// or fails.
func runStartJob(config *models.ProjectConfig, job *models.PipelineJob, logger *lib.Logger) error {
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

	defer installStopHandler(config.JobsDir, job.JobID, logger)()

	fmt.Printf("Starting %s step...\n", startedJob.CurrentStep)

	final, err := pipeline.RunLoop(startedJob, logger, pipeline.RunOptions{NoProgress: noProgress})
	if err != nil {
		if errors.Is(err, pipeline.ErrPaused) {
			return nil // Paused at a wait step; RunLoop persisted the state.
		}
		return err // RunLoop already marked the job failed and saved it.
	}

	fmt.Printf("\n✓ Pipeline completed successfully\n")
	fmt.Printf("Job ID: %s\n", final.JobID)
	return nil
}

func runPipelineStatus(cmd *cobra.Command, args []string) error {
	cfgPath := args[0]
	jobID := args[1]

	config, err := services.LoadConfig(cfgPath)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	job, err := pipeline.LoadJob(config.JobsDir, jobID)
	if err != nil {
		return fmt.Errorf("failed to load job: %w", err)
	}

	// Report the status the user sees elsewhere: an in_progress job that no
	// process holds is stopped, and the text names the cause. This is display
	// only; the state file keeps its value.
	fmt.Println(pipeline.GetJobSummary(job, services.EffectiveJobStatusText(config.JobsDir, *job)))

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

		fmt.Print(stepProgressSuffix(step))

		if step.LastError != nil {
			fmt.Printf("\n    Error: %s", step.LastError.Message)
		}

		fmt.Println()
	}

	return nil
}

// stepProgressSuffix returns the in-step progress for the status line, or ""
// when the step is not running or reports no progress.
func stepProgressSuffix(step models.PipelineStep) string {
	if step.Status != models.StepStatusInProgress || step.Progress == nil {
		return ""
	}
	return " — " + step.Progress.Message
}

func runPipelineContinue(cmd *cobra.Command, args []string) error {
	cfgPath := args[0]
	jobID := args[1]

	config, err := services.LoadConfig(cfgPath)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	logger := lib.NewLogger(commandLogLevel())
	defer func() { _ = logger.Close() }()

	fmt.Printf("Loading job %s...\n", jobID)
	job, err := pipeline.LoadJob(config.JobsDir, jobID)
	if err != nil {
		return fmt.Errorf("failed to load job: %w", err)
	}

	// Append this run's logs to the job's existing job.log.
	if err := logger.AttachJobLogFile(services.GetJobLogFilePath(config.JobsDir, jobID)); err != nil {
		return fmt.Errorf("failed to attach job log file: %w", err)
	}

	if err := verifyFlatteningLookupForJob(job, logger); err != nil {
		return err
	}

	if job.Status == models.JobStatusCompleted {
		fmt.Println("✓ Job already completed")
		return nil
	}

	fmt.Printf("Current status: %s\n", services.EffectiveJobStatusText(config.JobsDir, *job))
	fmt.Printf("Current step: %s\n", job.CurrentStep)

	lock, err := services.AcquireJobLock(config.JobsDir, jobID, logger)
	if err != nil {
		return fmt.Errorf("cannot continue pipeline: %w\n\nAnother process may be working on this job. Wait for it to complete or check its status with 'pipeline status'", err)
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
	if !found {
		return fmt.Errorf("current step %s not found in job", currentStepName)
	}

	// If the current step already completed, advance to the next before running.
	// Every other status (pending/in_progress/failed/waiting) resumes in place —
	// RunLoop's transition guard and the idempotent wait step handle each.
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
		job = advancedJob
	} else {
		fmt.Printf("Resuming incomplete step: %s (status: %s)\n", currentStepName, currentStep.Status)
	}

	fmt.Printf("\nResuming pipeline execution...\n")

	defer installStopHandler(config.JobsDir, jobID, logger)()

	final, err := pipeline.RunLoop(job, logger, pipeline.RunOptions{NoProgress: noProgress})
	if err != nil {
		if errors.Is(err, pipeline.ErrPaused) {
			return nil // Still paused at a wait step; RunLoop persisted the state.
		}
		return err // RunLoop already marked the job failed and saved it.
	}

	fmt.Printf("\n✓ Pipeline completed successfully\n")
	fmt.Printf("Job ID: %s\n", final.JobID)
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
	case models.StepStatusWaiting:
		return "‖"
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
