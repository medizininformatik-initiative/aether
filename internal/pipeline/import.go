package pipeline

import (
	"errors"
	"fmt"
	"time"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

// defaultExtractorFactory is the production TORCH client constructor.
var defaultExtractorFactory = func(config models.TORCHConfig, httpClient *services.HTTPClient, logger *lib.Logger) services.Extractor {
	return services.NewTORCHClient(config, httpClient, logger)
}

// extractorFactory creates an Extractor. Overridable in tests.
var extractorFactory = defaultExtractorFactory

// SetExtractorFactoryForTesting replaces the TORCH client factory for tests.
func SetExtractorFactoryForTesting(factory func(models.TORCHConfig, *services.HTTPClient, *lib.Logger) services.Extractor) {
	extractorFactory = factory
}

// ResetExtractorFactory restores the default TORCH client factory.
func ResetExtractorFactory() {
	extractorFactory = defaultExtractorFactory
}

// importStep imports FHIR data into the import/ directory. It carries its own
// step name (torch/local/http) because the registry maps all three names to it
// and the name selects both the import method and the error-classification rule.
type importStep struct {
	name models.StepName
}

func (s importStep) Name() models.StepName { return s.name }

// ClassifyError preserves import's step-name-gated classification (network
// errors are transient only for http/torch imports).
func (s importStep) ClassifyError(err error) models.ErrorType {
	return classifyImportError(err, s.name)
}

// Run selects the import method from the step name. It builds its own HTTP
// client when none was injected (the orchestration-loop path), using the
// longer download-safe timeout.
func (s importStep) Run(ctx *StepContext) (StepResult, error) {
	job := ctx.Job

	if err := checkImportInputType(s.name, job.InputType); err != nil {
		return StepResult{}, err
	}

	httpClient := resolveImportHTTPClient(ctx)

	importedFiles, err := s.importFiles(ctx, httpClient)
	if err != nil {
		return StepResult{}, err
	}

	totalBytes := sumFileSizes(importedFiles)

	// Job-level totals are non-lifecycle state the import step owns.
	job.TotalFiles = len(importedFiles)
	job.TotalBytes = totalBytes

	return StepResult{FilesProcessed: len(importedFiles), BytesProcessed: totalBytes}, nil
}

// checkImportInputType rejects a step name that is no import step and a job
// input type the import method cannot handle. Nothing on the RunLoop/continue
// path cross-checks the two (CreateJob picks the step from config, the input
// type from the source), so without this guard a mismatch surfaces as a
// confusing downstream error.
func checkImportInputType(step models.StepName, inputType models.InputType) error {
	accepted := models.AcceptedInputTypes(step)
	if len(accepted) == 0 {
		return fmt.Errorf("unsupported import step: %s", step)
	}
	if !models.ImportStepAcceptsInputType(step, inputType) {
		return fmt.Errorf("import step %q does not accept input type %q (accepts %v)", step, inputType, accepted)
	}
	return nil
}

func resolveImportHTTPClient(ctx *StepContext) *services.HTTPClient {
	if ctx.HTTPClient != nil {
		return ctx.HTTPClient
	}
	cfg := &ctx.Job.Config
	return services.NewHTTPClient(
		time.Duration(cfg.Retry.InitialBackoffMs)*time.Millisecond*10,
		cfg.Retry, cfg.TLS, ctx.Logger)
}

func (s importStep) importFiles(ctx *StepContext, httpClient *services.HTTPClient) ([]models.FHIRDataFile, error) {
	importDir := ctx.Layout.OutputDir(s.name)

	switch s.name {
	case models.StepLocalImport:
		return importLocal(ctx, importDir)
	case models.StepHttpImport:
		return importHTTP(ctx, httpClient, importDir)
	default:
		// checkImportInputType has already rejected every other step name.
		return importTORCH(ctx, httpClient, importDir)
	}
}

func importLocal(ctx *StepContext, importDir string) ([]models.FHIRDataFile, error) {
	job := ctx.Job
	compression := job.Config.Compression

	sourceDir := job.Config.Services.LocalImport.Dir
	if sourceDir == "" {
		sourceDir = job.InputSource
	}
	recursive := job.Config.Services.LocalImport.Recursive
	if err := services.ValidateImportSource(sourceDir, models.InputTypeLocal, recursive); err != nil {
		return nil, err
	}
	ctx.Logger.Info("Importing from local directory", "source", sourceDir, "compress", compression.Enabled, "recursive", recursive)
	return services.ImportFromLocalDirectory(sourceDir, importDir, ctx.Logger, compression.Enabled, compression.Level, recursive)
}

func importHTTP(ctx *StepContext, httpClient *services.HTTPClient, importDir string) ([]models.FHIRDataFile, error) {
	job := ctx.Job
	compression := job.Config.Compression

	if err := services.ValidateImportSource(job.InputSource, models.InputTypeHTTP, false); err != nil {
		return nil, err
	}
	ctx.Logger.Info("Downloading from URL", "source", job.InputSource, "compress", compression.Enabled)
	if ctx.ShowProgress {
		return services.DownloadFromURLWithProgress(job.InputSource, importDir, httpClient, ctx.Logger, compression.Enabled, compression.Level)
	}
	return services.DownloadFromURL(job.InputSource, importDir, httpClient, ctx.Logger, false, compression.Enabled, compression.Level)
}

// importTORCH polls a TORCH result URL directly. Otherwise it submits the CRTDL
// for extraction.
func importTORCH(ctx *StepContext, httpClient *services.HTTPClient, importDir string) ([]models.FHIRDataFile, error) {
	job := ctx.Job
	compression := job.Config.Compression

	if job.InputType == models.InputTypeTORCHURL {
		if err := services.ValidateImportSource(job.InputSource, models.InputTypeTORCHURL, false); err != nil {
			return nil, err
		}
		ctx.Logger.Info("Downloading from TORCH result URL", "source", job.InputSource, "compress", compression.Enabled)
		return executeTORCHDownload(job, importDir, httpClient, ctx.Logger, ctx.ShowProgress, compression.Enabled, compression.Level)
	}
	if err := services.ValidateImportSource(job.CRTDLPath, models.InputTypeCRTDL, false); err != nil {
		return nil, err
	}
	ctx.Logger.Info("Extracting data from TORCH using CRTDL", "crtdl", job.CRTDLPath, "compress", compression.Enabled)
	return executeTORCHExtraction(job, importDir, httpClient, ctx.Logger, ctx.ShowProgress, compression.Enabled, compression.Level)
}

func sumFileSizes(files []models.FHIRDataFile) int64 {
	var total int64
	for _, file := range files {
		total += file.FileSize
	}
	return total
}

// executeTORCHExtraction submits the (already prepared) CRTDL to TORCH,
// polls until extraction completes, and downloads the resulting NDJSON
// files. PrepareCRTDL ensures job.CRTDLPath points at the effective CRTDL
// shared by every downstream pipeline step.
func executeTORCHExtraction(job *models.PipelineJob, importDir string, httpClient *services.HTTPClient, logger *lib.Logger, showProgress bool, compress bool, compressionLevel string) ([]models.FHIRDataFile, error) {
	torchClient := extractorFactory(job.Config.Services.TORCH, httpClient, logger)
	attachTORCHProgressPersistence(job, torchClient, logger)

	if job.TORCHJobID != "" {
		logger.Info("Re-attaching to in-flight TORCH extraction", "job_id", job.TORCHJobID, "url", job.TORCHExtractionURL)
	} else if err := submitAndPersistTORCHHandle(job, torchClient, logger); err != nil {
		return nil, err
	}

	fileURLs, err := torchClient.PollExtractionStatus(job.TORCHExtractionURL, showProgress)
	if errors.Is(err, services.ErrHandleDead) {
		logger.Warn("TORCH job handle is gone; clearing handle and re-submitting a fresh extraction", "stale_job_id", job.TORCHJobID)
		job.TORCHJobID = ""
		job.TORCHExtractionURL = ""
		if err := submitAndPersistTORCHHandle(job, torchClient, logger); err != nil {
			return nil, err
		}
		fileURLs, err = torchClient.PollExtractionStatus(job.TORCHExtractionURL, showProgress)
	}
	if err != nil {
		return nil, fmt.Errorf("TORCH extraction failed: %w", err)
	}

	if len(fileURLs) == 0 {
		logger.Warn("TORCH extraction returned no files (empty cohort)")
		return []models.FHIRDataFile{}, nil
	}

	files, err := torchClient.DownloadExtractionFiles(fileURLs, importDir, showProgress, compress, compressionLevel)
	if err != nil {
		return nil, fmt.Errorf("failed to download TORCH files: %w", err)
	}
	return files, nil
}

// attachTORCHProgressPersistence registers a progress handler on the TORCH
// client (when it supports one) that writes batch progress to the torch step in
// the job file, so `pipeline status` shows it from another process.
// Persistence is best effort; a failed write never interrupts the extraction.
func attachTORCHProgressPersistence(job *models.PipelineJob, torchClient services.Extractor, logger *lib.Logger) {
	reporter, ok := torchClient.(services.ProgressReporter)
	if !ok {
		return
	}
	// Mutate the step in place: runStep holds a pointer into job.Steps, so the
	// slice must not be replaced while the step runs.
	step := findStep(job, models.StepTorchImport)
	if step == nil {
		return
	}
	reporter.SetProgressHandler(func(progress services.TORCHProgress) {
		// The batch stage changes on almost every poll, but a job-file write
		// costs a full marshal of the job. Write only when the batch counts
		// change; the in-memory value stays current for this process.
		persist := step.Progress == nil ||
			step.Progress.Completed != progress.BatchesCompleted ||
			step.Progress.Total != progress.BatchesTotal

		step.Progress = &models.StepProgress{
			Message:   progress.Summary(),
			Completed: progress.BatchesCompleted,
			Total:     progress.BatchesTotal,
			UpdatedAt: time.Now(),
		}
		if !persist {
			return
		}
		if err := UpdateJob(job.Config.JobsDir, job); err != nil {
			logger.Debug("failed to persist TORCH progress", "error", err)
		}
	})
}

// submitAndPersistTORCHHandle submits the CRTDL for extraction, then persists
// the resulting job handle (TORCHJobID + URL) to disk before any long poll, so
// a crash mid-extraction leaves a recoverable handle to re-attach to.
func submitAndPersistTORCHHandle(job *models.PipelineJob, torchClient services.Extractor, logger *lib.Logger) error {
	// The process of a stopped job exits soon, so a new TORCH job would run
	// without a collector.
	done, err := beginSubmit(job.Config.JobsDir, job.JobID)
	if err != nil {
		return err
	}
	extractionURL, err := torchClient.SubmitExtraction(job.CRTDLPath)
	if err != nil {
		done("")
		return fmt.Errorf("failed to submit TORCH extraction: %w", err)
	}

	job.TORCHExtractionURL = extractionURL
	job.TORCHJobID = services.JobIDFromStatusURL(extractionURL)
	err = persistSubmittedHandle(job.Config.JobsDir, job)
	// A stop that came during the submit waits for this ID to cancel the
	// TORCH job. A later stop finds the handle in the state file.
	done(job.TORCHJobID)
	if err != nil {
		return fmt.Errorf("failed to persist TORCH job handle: %w", err)
	}
	if jobStopped(job.Config.JobsDir, job.JobID) {
		return ErrJobStopped
	}

	logger.Info("TORCH extraction submitted; handle persisted for resume", "job_id", job.TORCHJobID, "url", extractionURL)
	return nil
}

// executeTORCHDownload downloads files from a direct TORCH result URL
// This bypasses extraction submission and directly downloads from an existing result
func executeTORCHDownload(job *models.PipelineJob, importDir string, httpClient *services.HTTPClient, logger *lib.Logger, showProgress bool, compress bool, compressionLevel string) ([]models.FHIRDataFile, error) {
	// Create TORCH client
	torchClient := extractorFactory(job.Config.Services.TORCH, httpClient, logger)
	attachTORCHProgressPersistence(job, torchClient, logger)

	// Poll the URL directly (it should return 200 immediately if extraction is complete)
	fileURLs, err := torchClient.PollExtractionStatus(job.InputSource, showProgress)
	if err != nil {
		return nil, fmt.Errorf("failed to get TORCH result: %w", err)
	}

	if len(fileURLs) == 0 {
		logger.Warn("TORCH result URL returned no files")
		return []models.FHIRDataFile{}, nil
	}

	// Download files
	files, err := torchClient.DownloadExtractionFiles(fileURLs, importDir, showProgress, compress, compressionLevel)
	if err != nil {
		return nil, fmt.Errorf("failed to download TORCH files: %w", err)
	}

	return files, nil
}

// classifyImportError determines if an import error is transient or non-transient
func classifyImportError(err error, step models.StepName) models.ErrorType {
	if err == nil {
		return models.ErrorTypeNonTransient
	}

	// Check for TORCH-specific errors, including wrapped ones (submit/poll paths
	// wrap the TORCHError with context).
	var torchErr *services.TORCHError
	if errors.As(err, &torchErr) {
		return torchErr.ErrorType
	}

	// Network-capable steps: network errors are transient
	if step == models.StepHttpImport || step == models.StepTorchImport {
		if lib.IsNetworkError(err) {
			return models.ErrorTypeTransient
		}
	}

	// For local imports, most errors are non-transient (file not found, permissions, etc.)
	// Default to non-transient
	return models.ErrorTypeNonTransient
}
