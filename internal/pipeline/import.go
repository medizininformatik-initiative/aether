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
	logger := ctx.Logger
	currentStep := s.name

	// The import method is chosen by step name; the job's input type must be one
	// the method can handle. Nothing on the RunLoop/continue path cross-checks the
	// two (CreateJob picks the step from config, the input type from the source),
	// so without this guard a mismatch surfaces as a confusing downstream error.
	accepted := models.AcceptedInputTypes(currentStep)
	if len(accepted) == 0 {
		return StepResult{}, fmt.Errorf("unsupported import step: %s", currentStep)
	}
	if !models.ImportStepAcceptsInputType(currentStep, job.InputType) {
		return StepResult{}, fmt.Errorf("import step %q does not accept input type %q (accepts %v)", currentStep, job.InputType, accepted)
	}

	httpClient := ctx.HTTPClient
	if httpClient == nil {
		httpClient = services.NewHTTPClient(
			time.Duration(job.Config.Retry.InitialBackoffMs)*time.Millisecond*10,
			job.Config.Retry, job.Config.TLS, logger)
	}

	importDir := ctx.Layout.OutputDir(currentStep)
	compress := job.Config.Compression.Enabled
	compressionLevel := job.Config.Compression.Level

	var importedFiles []models.FHIRDataFile
	var err error

	switch currentStep {
	case models.StepLocalImport:
		// Config dir takes precedence, fallback to job.InputSource
		sourceDir := job.Config.Services.LocalImport.Dir
		if sourceDir == "" {
			sourceDir = job.InputSource
		}
		recursive := job.Config.Services.LocalImport.Recursive
		if verr := services.ValidateImportSource(sourceDir, models.InputTypeLocal, recursive); verr != nil {
			return StepResult{}, verr
		}
		logger.Info("Importing from local directory", "source", sourceDir, "compress", compress, "recursive", recursive)
		importedFiles, err = services.ImportFromLocalDirectory(sourceDir, importDir, logger, compress, compressionLevel, recursive)

	case models.StepHttpImport:
		if verr := services.ValidateImportSource(job.InputSource, models.InputTypeHTTP, false); verr != nil {
			return StepResult{}, verr
		}
		logger.Info("Downloading from URL", "source", job.InputSource, "compress", compress)
		if ctx.ShowProgress {
			importedFiles, err = services.DownloadFromURLWithProgress(job.InputSource, importDir, httpClient, logger, compress, compressionLevel)
		} else {
			importedFiles, err = services.DownloadFromURL(job.InputSource, importDir, httpClient, logger, false, compress, compressionLevel)
		}

	case models.StepTorchImport:
		// TORCH result URL: poll directly. Otherwise: submit the CRTDL for extraction.
		if job.InputType == models.InputTypeTORCHURL {
			if verr := services.ValidateImportSource(job.InputSource, models.InputTypeTORCHURL, false); verr != nil {
				return StepResult{}, verr
			}
			logger.Info("Downloading from TORCH result URL", "source", job.InputSource, "compress", compress)
			importedFiles, err = executeTORCHDownload(job, importDir, httpClient, logger, ctx.ShowProgress, compress, compressionLevel)
		} else {
			if verr := services.ValidateImportSource(job.CRTDLPath, models.InputTypeCRTDL, false); verr != nil {
				return StepResult{}, verr
			}
			logger.Info("Extracting data from TORCH using CRTDL", "crtdl", job.CRTDLPath, "compress", compress)
			importedFiles, err = executeTORCHExtraction(job, importDir, httpClient, logger, ctx.ShowProgress, compress, compressionLevel)
		}
	}

	if err != nil {
		return StepResult{}, err
	}

	var totalBytes int64
	for _, file := range importedFiles {
		totalBytes += file.FileSize
	}

	// Job-level totals are non-lifecycle state the import step owns.
	job.TotalFiles = len(importedFiles)
	job.TotalBytes = totalBytes

	return StepResult{FilesProcessed: len(importedFiles), BytesProcessed: totalBytes}, nil
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
// client (when it supports one) that writes each progress change to the torch
// step in the job file, so `pipeline status` shows it from another process.
// Persistence is best effort; a failed write never interrupts the extraction.
func attachTORCHProgressPersistence(job *models.PipelineJob, torchClient services.Extractor, logger *lib.Logger) {
	reporter, ok := torchClient.(interface {
		SetProgressHandler(func(services.TORCHProgress))
	})
	if !ok {
		return
	}
	reporter.SetProgressHandler(func(progress services.TORCHProgress) {
		// Mutate the step in place: runStep holds a pointer into job.Steps,
		// so the slice must not be replaced while the step runs.
		for i := range job.Steps {
			if job.Steps[i].Name != models.StepTorchImport {
				continue
			}
			job.Steps[i].Progress = &models.StepProgress{
				Message:   progress.Summary(),
				Completed: progress.BatchesCompleted,
				Total:     progress.BatchesTotal,
				UpdatedAt: time.Now(),
			}
			if err := UpdateJob(job.Config.JobsDir, job); err != nil {
				logger.Debug("failed to persist TORCH progress", "error", err)
			}
			return
		}
	})
}

// submitAndPersistTORCHHandle submits the CRTDL for extraction, then persists
// the resulting job handle (TORCHJobID + URL) to disk before any long poll, so
// a crash mid-extraction leaves a recoverable handle to re-attach to.
func submitAndPersistTORCHHandle(job *models.PipelineJob, torchClient services.Extractor, logger *lib.Logger) error {
	extractionURL, err := torchClient.SubmitExtraction(job.CRTDLPath)
	if err != nil {
		return fmt.Errorf("failed to submit TORCH extraction: %w", err)
	}

	job.TORCHExtractionURL = extractionURL
	job.TORCHJobID = services.JobIDFromStatusURL(extractionURL)
	if err := UpdateJob(job.Config.JobsDir, job); err != nil {
		return fmt.Errorf("failed to persist TORCH job handle: %w", err)
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
