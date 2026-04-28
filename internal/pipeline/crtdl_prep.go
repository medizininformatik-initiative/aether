package pipeline

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

// PrepareCRTDL ensures every pipeline step uses the same effective CRTDL.
// For CRTDL inputs it copies (or enriches) the file into the job directory
// and repoints job.InputSource at the saved file. The call is a no-op for
// non-CRTDL inputs and idempotent when InputSource already lives in jobDir.
//
// With CRTDLPreprocessing enabled and at least one enrichment configured,
// the result is written as enriched-crtdl.json. Otherwise the original is
// copied verbatim as crtdl.json.
func PrepareCRTDL(job *models.PipelineJob, logger *lib.Logger) error {
	if job.InputType != models.InputTypeCRTDL {
		return nil
	}

	jobDir := services.GetJobDir(job.Config.JobsDir, job.JobID)
	if isPreparedCRTDLPath(job.InputSource, jobDir) {
		logger.Debug("CRTDL already prepared, skipping", "path", job.InputSource)
		return nil
	}

	preprocessing := job.Config.Services.CRTDLPreprocessing
	if !preprocessing.Enabled {
		return copyOriginalCRTDL(job, jobDir, logger)
	}

	logger.Info("CRTDL preprocessing enabled, enriching CRTDL", "source", job.InputSource)

	originalDoc, err := services.ParseCRTDL(job.InputSource)
	if err != nil {
		return fmt.Errorf("failed to parse CRTDL: %w", err)
	}

	enrichments, err := services.LoadEnrichments(preprocessing)
	if err != nil {
		return fmt.Errorf("failed to load enrichments: %w", err)
	}

	if len(enrichments) == 0 {
		logger.Warn("CRTDL preprocessing enabled but no enrichments configured")
		return copyOriginalCRTDL(job, jobDir, logger)
	}

	logger.Info("Applying CRTDL enrichments",
		"enrichment_count", len(enrichments),
		"original_groups", len(originalDoc.DataExtraction.AttributeGroups))

	enrichedDoc, err := services.EnrichCRTDL(*originalDoc, enrichments)
	if err != nil {
		return fmt.Errorf("failed to enrich CRTDL: %w", err)
	}

	logger.Info("CRTDL enriched successfully",
		"enriched_groups", len(enrichedDoc.DataExtraction.AttributeGroups))

	enrichedContent, err := json.MarshalIndent(enrichedDoc, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize enriched CRTDL: %w", err)
	}

	return saveCRTDL(job, jobDir, "enriched-crtdl.json", enrichedContent, logger)
}

// copyOriginalCRTDL copies job.InputSource verbatim into jobDir/crtdl.json.
func copyOriginalCRTDL(job *models.PipelineJob, jobDir string, logger *lib.Logger) error {
	content, err := os.ReadFile(job.InputSource)
	if err != nil {
		return fmt.Errorf("failed to read CRTDL file: %w", err)
	}
	return saveCRTDL(job, jobDir, "crtdl.json", content, logger)
}

// isPreparedCRTDLPath reports whether path is the canonical prepared CRTDL
// inside jobDir (crtdl.json or enriched-crtdl.json). A path that merely lives
// inside jobDir under some other name still needs preparation.
func isPreparedCRTDLPath(path, jobDir string) bool {
	if filepath.Clean(filepath.Dir(path)) != filepath.Clean(jobDir) {
		return false
	}
	switch filepath.Base(path) {
	case "crtdl.json", "enriched-crtdl.json":
		return true
	}
	return false
}

// saveCRTDL writes content to jobDir/filename and repoints job.InputSource.
func saveCRTDL(job *models.PipelineJob, jobDir, filename string, content []byte, logger *lib.Logger) error {
	crtdlPath := filepath.Join(jobDir, filename)
	if err := os.WriteFile(crtdlPath, content, 0644); err != nil {
		return fmt.Errorf("failed to save CRTDL to job directory: %w", err)
	}
	job.InputSource = crtdlPath
	logger.Info("CRTDL saved to job directory", "path", crtdlPath)
	return nil
}
