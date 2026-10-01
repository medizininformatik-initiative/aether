package pipeline

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

// defaultFlattenerFactory is the production flattener client constructor.
var defaultFlattenerFactory = func(config models.FlatteningConfig, retryConfig models.RetryConfig, transport *http.Transport, logger *lib.Logger) services.Flattener {
	return services.NewFlattenerClient(config, retryConfig, transport, logger)
}

// flattenerFactory creates a Flattener. Overridable in tests.
var flattenerFactory = defaultFlattenerFactory

// SetFlattenerFactoryForTesting replaces the flattener client factory for tests.
func SetFlattenerFactoryForTesting(factory func(models.FlatteningConfig, models.RetryConfig, *http.Transport, *lib.Logger) services.Flattener) {
	flattenerFactory = factory
}

// ResetFlattenerFactory restores the default flattener client factory.
func ResetFlattenerFactory() {
	flattenerFactory = defaultFlattenerFactory
}

// groupBatch accumulates resources for a single attribute group until the batch
// size threshold is reached, at which point it is flushed to the flattener service.
type groupBatch struct {
	resources    []map[string]any
	byteSize     int
	isFirstBatch bool // true until first flush
}

// flatteningStep transforms FHIR NDJSON data into CSV files using the fhir-flattener API.
// Reads from dimp/ (if DIMP enabled) or import/ directory, outputs to csv/.
type flatteningStep struct{}

func (flatteningStep) Name() models.StepName { return models.StepFlattening }

func (flatteningStep) Run(ctx *StepContext) (StepResult, error) {
	job := ctx.Job
	logger := ctx.Logger
	stepName := models.StepFlattening

	// Validate flattening configuration
	if err := job.Config.Services.Flattening.Validate(); err != nil {
		return StepResult{}, err
	}

	// CRTDL file is required for flattening. It may come from the positional
	// arg (for torch) or from --crtdl (for http_import/local_import).
	if job.CRTDLPath == "" {
		return StepResult{}, fmt.Errorf("flattening step requires a CRTDL file: pass one as the positional input or via --crtdl")
	}

	// Load CRTDL document
	crtdlPath := job.CRTDLPath
	logger.Debug("Loading CRTDL file", "path", crtdlPath)
	crtdl, err := services.ParseCRTDL(crtdlPath)
	if err != nil {
		return StepResult{}, fmt.Errorf("failed to parse CRTDL file: %w", err)
	}

	// Load lookup tables (LoadLookupTables normalizes and validates them)
	lookupPath := job.Config.Services.Flattening.LookupPath
	logger.Debug("Loading lookup tables", "path", lookupPath)
	lookupTables, err := services.LoadLookupTables(lookupPath)
	if err != nil {
		return StepResult{}, fmt.Errorf("failed to load lookup tables: %w", err)
	}

	inputDir := ctx.Layout.InputDir(stepName)
	outputDir := ctx.Layout.OutputDir(stepName)
	viewDefDir := ctx.Layout.ViewDefinitionsDir()

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return StepResult{}, fmt.Errorf("failed to create output directory: %w", err)
	}

	// Find FHIR NDJSON files in input directory
	files, err := findFHIRFiles(inputDir)
	if err != nil {
		return StepResult{}, fmt.Errorf("failed to list input files: %w", err)
	}

	if len(files) == 0 {
		return StepResult{}, fmt.Errorf("no FHIR NDJSON files found in %s", inputDir)
	}

	logger.Info("Streaming FHIR resources from input files",
		"input_dir", inputDir,
		"file_count", len(files),
		"job_id", job.JobID)

	// Pass 1: scan input files for provenance index
	provenanceIndex, err := scanProvenanceIndex(files)
	if err != nil {
		return StepResult{}, fmt.Errorf("failed to load resources: %w", err)
	}

	logger.Info("Built provenance index",
		"provenance_entries", len(provenanceIndex),
		"provenance_source", inputDir,
		"job_id", job.JobID)

	// Create clients
	flattenerTransport, _ := services.BuildTLSTransport(job.Config.TLS, logger)
	flattenerClient := flattenerFactory(job.Config.Services.Flattening, job.Config.Retry, flattenerTransport, logger)
	viewDefBuilder := services.NewViewDefinitionBuilder(lookupTables)
	csvWriter := services.NewCSVWriter(outputDir)
	viewDefWriter := services.NewViewDefinitionWriter(viewDefDir)

	// Drop stale partial files from an earlier failed run, so a retry never
	// keeps or reports rows it did not write
	if err := csvWriter.RemovePartials(); err != nil {
		return StepResult{}, err
	}

	attributeGroups := services.GetAttributeGroups(crtdl)
	fmt.Printf("Processing %d attribute group(s)...\n\n", len(attributeGroups))

	// Pre-compute: build groupIDToIndex mapping and ViewDefinitions
	groupIDToIndex := make(map[string]int)
	viewDefs := make([]*models.ViewDefinition, len(attributeGroups))
	headers := make([][]string, len(attributeGroups))
	filenames := make([]string, len(attributeGroups))

	for i, group := range attributeGroups {
		viewDef, err := viewDefBuilder.BuildViewDefinition(group)
		if err != nil {
			logger.Warn("Failed to build ViewDefinition for group, skipping",
				"group_name", group.Name,
				"error", err)
			fmt.Printf("  ⚠ %s (skipped: %v)\n", group.Name, err)
			continue
		}

		viewDefs[i] = viewDef
		groupIDToIndex[group.ID] = i
		headers[i] = services.ExtractColumnNames(*viewDef)
		filenames[i] = services.BuildCSVFilename(group.Name)

		// Save ViewDefinition to disk
		viewDefFilename := services.BuildViewDefinitionFilename(group.Name)
		if err := viewDefWriter.WriteViewDefinition(viewDefFilename, *viewDef); err != nil {
			logger.Warn("Failed to save ViewDefinition, continuing",
				"group_name", group.Name,
				"filename", viewDefFilename,
				"error", err)
		}
	}

	// Pass 2: stream resources and flatten in batches using provenance routing
	totals, err := streamAndFlattenResources(
		files,
		attributeGroups,
		provenanceIndex,
		groupIDToIndex,
		viewDefs,
		headers,
		filenames,
		flattenerClient,
		csvWriter,
		logger,
		job.Config.Services.Flattening.GetBatchSizeBytes(),
	)
	if err != nil {
		return StepResult{}, annotateWithPartialFiles(err, csvWriter)
	}

	// Print per-group progress
	totalFilesWritten := 0
	for i, group := range attributeGroups {
		if viewDefs[i] == nil {
			continue
		}
		if totals[i] == 0 {
			fmt.Printf("  ⊙ %s (no matching resources)\n", group.Name)
			continue
		}
		totalFilesWritten++
		fmt.Printf("  ✓ %s (%d resources → %s)\n", group.Name, totals[i], filenames[i])
	}

	logger.Debug("Flattening step completed",
		"files_written", totalFilesWritten,
		"job_id", job.JobID)

	return StepResult{FilesProcessed: totalFilesWritten}, nil
}

// scanProvenanceIndex performs a lightweight first pass over all input files,
// extracting only Provenance resources from Bundles to build the provenance index.
// Clinical resources are discarded to keep memory usage minimal.
func scanProvenanceIndex(files []string) (models.ProvenanceIndex, error) {
	mergedIndex := make(models.ProvenanceIndex)

	for _, filePath := range files {
		_, err := lib.ReadNDJSONFile(filePath, func(resource lib.FHIRResource) error {
			if lib.IsBundle(resource) {
				_, bundleIndex := extractBundleResources(resource)
				for k, v := range bundleIndex {
					mergedIndex[k] = append(mergedIndex[k], v...)
				}
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("failed to load %s: %w", filepath.Base(filePath), err)
		}
	}

	return mergedIndex, nil
}

// perGroupBudget splits the total batch byte budget evenly across attribute groups,
// clamping to one byte so a tiny budget spread over many groups never yields a
// zero threshold that would flush after every single resource.
func perGroupBudget(batchSizeBytes, numGroups int) int {
	return max(batchSizeBytes/numGroups, 1)
}

// streamAndFlattenResources performs single-pass streaming over all input files,
// routing each resource to per-group batches via provenance index and flushing
// when the byte threshold is exceeded. Returns per-group resource totals.
func streamAndFlattenResources(
	files []string,
	groups []models.AttributeGroup,
	provenanceIndex models.ProvenanceIndex,
	groupIDToIndex map[string]int,
	viewDefs []*models.ViewDefinition,
	headers [][]string,
	filenames []string,
	flattenerClient services.Flattener,
	csvWriter *services.CSVWriter,
	logger *lib.Logger,
	batchSizeBytes int,
) ([]int, error) {
	s := &groupStreamer{
		groups:          groups,
		provenanceIndex: provenanceIndex,
		groupIDToIndex:  groupIDToIndex,
		viewDefs:        viewDefs,
		headers:         headers,
		filenames:       filenames,
		flattenerClient: flattenerClient,
		csvWriter:       csvWriter,
		logger:          logger,
		// Divide total memory budget across groups so peak usage stays within batchSizeBytes
		perGroupBytes: perGroupBudget(batchSizeBytes, len(groups)),
	}
	s.init()
	defer s.logSummaries()

	for _, filePath := range files {
		if err := s.streamFile(filePath); err != nil {
			return nil, err
		}
	}
	if err := s.flushRemaining(); err != nil {
		return nil, err
	}
	if err := s.finalize(); err != nil {
		return nil, err
	}
	return s.totals, nil
}

// groupStreamer holds the per-group batches, totals and size statistics of
// one streaming pass.
type groupStreamer struct {
	groups          []models.AttributeGroup
	provenanceIndex models.ProvenanceIndex
	groupIDToIndex  map[string]int
	viewDefs        []*models.ViewDefinition
	headers         [][]string
	filenames       []string
	flattenerClient services.Flattener
	csvWriter       *services.CSVWriter
	logger          *lib.Logger
	perGroupBytes   int
	batches         []groupBatch
	totals          []int
	stats           []*groupSizeStats
}

func (s *groupStreamer) init() {
	n := len(s.groups)
	s.batches = make([]groupBatch, n)
	s.totals = make([]int, n)
	s.stats = make([]*groupSizeStats, n)
	for i := range s.batches {
		s.batches[i].isFirstBatch = true
		s.stats[i] = &groupSizeStats{groupName: s.groups[i].Name, budgetBytes: s.perGroupBytes, logger: s.logger}
	}
}

// logSummaries logs the size statistics of each group that has a ViewDefinition.
func (s *groupStreamer) logSummaries() {
	for i, st := range s.stats {
		if s.viewDefs[i] != nil {
			st.logSummary()
		}
	}
}

func (s *groupStreamer) streamFile(filePath string) error {
	reader, err := lib.OpenFileForReading(filePath)
	if err != nil {
		return fmt.Errorf("failed to load %s: %w", filepath.Base(filePath), err)
	}
	defer func() { _ = reader.Close() }()

	dec := json.NewDecoder(reader)
	for {
		startOffset := dec.InputOffset()
		var resource map[string]any
		if err := dec.Decode(&resource); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("failed to load %s: %w", filepath.Base(filePath), err)
		}
		if err := s.addDecoded(resource, int(dec.InputOffset()-startOffset)); err != nil {
			return err
		}
	}
}

// addDecoded adds a decoded resource. A Bundle adds its clinical entries and
// skips its Provenance entries.
func (s *groupStreamer) addDecoded(resource map[string]any, size int) error {
	if !lib.IsBundle(resource) {
		return s.add(resource, size)
	}
	clinicalResources, _ := extractBundleResources(resource)
	for _, entry := range clinicalResources {
		entryBytes, err := json.Marshal(entry)
		if err != nil {
			continue
		}
		if err := s.add(entry, len(entryBytes)); err != nil {
			return err
		}
	}
	return nil
}

// add routes the resource to its groups via the provenance index and flushes
// each group batch that reaches the byte threshold.
func (s *groupStreamer) add(resource map[string]any, size int) error {
	ref := lib.ResourceReference(resource)
	for _, i := range routeResourceToGroups(resource, s.provenanceIndex, s.groupIDToIndex, s.viewDefs) {
		s.batches[i].resources = append(s.batches[i].resources, resource)
		s.batches[i].byteSize += size
		s.stats[i].addResource(ref, size)
		s.totals[i]++

		if s.batches[i].byteSize >= s.perGroupBytes {
			if err := s.flush(i); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *groupStreamer) flush(i int) error {
	return flushGroupBatch(&s.batches[i], s.viewDefs[i], s.headers[i], s.filenames[i],
		s.flattenerClient, s.csvWriter, s.logger, s.groups[i].Name, s.stats[i])
}

func (s *groupStreamer) flushRemaining() error {
	for i := range s.batches {
		if len(s.batches[i].resources) == 0 {
			continue
		}
		if err := s.flush(i); err != nil {
			return err
		}
	}
	return nil
}

// finalize publishes complete files under their final names. A group whose
// batch never flushed has no file to publish.
func (s *groupStreamer) finalize() error {
	for i := range s.batches {
		if s.batches[i].isFirstBatch {
			continue
		}
		if err := s.csvWriter.Finalize(s.filenames[i]); err != nil {
			return fmt.Errorf("failed to finalize CSV for group '%s': %w", s.groups[i].Name, err)
		}
	}
	return nil
}

// annotateWithPartialFiles names the partial CSV files that a failed run left
// behind, so nobody mistakes them for complete exports.
func annotateWithPartialFiles(err error, csvWriter *services.CSVWriter) error {
	partials, listErr := csvWriter.PartialFiles()
	if listErr != nil || len(partials) == 0 {
		return err
	}
	return fmt.Errorf("%w (incomplete output kept as: %s)", err, strings.Join(partials, ", "))
}

// routeResourceToGroups determines which groups a resource belongs to based on
// the provenance index. Returns the group indices for all matching groups.
func routeResourceToGroups(resource map[string]any, provenanceIndex models.ProvenanceIndex, groupIDToIndex map[string]int, viewDefs []*models.ViewDefinition) []int {
	ref := lib.ResourceReference(resource)
	if ref == "" {
		return nil
	}

	groupIDs, exists := provenanceIndex[ref]
	if !exists {
		return nil
	}

	var indices []int
	for _, groupID := range groupIDs {
		groupIdx, exists := groupIDToIndex[groupID]
		if !exists {
			continue
		}
		if viewDefs[groupIdx] == nil {
			continue
		}
		indices = append(indices, groupIdx)
	}

	return indices
}

// flushGroupBatch sends the accumulated batch to the flattener and appends the result to CSV.
func flushGroupBatch(
	batch *groupBatch,
	viewDef *models.ViewDefinition,
	header []string,
	filename string,
	flattenerClient services.Flattener,
	csvWriter *services.CSVWriter,
	logger *lib.Logger,
	groupName string,
	stats *groupSizeStats,
) error {
	logger.Debug("Flushing batch",
		"group_name", groupName,
		"resource_count", len(batch.resources),
		"byte_size", batch.byteSize)

	stats.addBatch(batch.byteSize)
	rows, err := flattenerClient.Flatten(*viewDef, batch.resources)
	if err != nil {
		return fmt.Errorf("flattener failed for group '%s': %w", groupName, err)
	}

	if err := csvWriter.AppendCSVData(filename, header, rows, batch.isFirstBatch); err != nil {
		return fmt.Errorf("failed to write CSV for group '%s': %w", groupName, err)
	}

	// Reset batch state
	batch.resources = nil
	batch.byteSize = 0
	batch.isFirstBatch = false

	return nil
}

// extractBundleResources separates Bundle entries into clinical resources and a provenance index.
// Provenance resources are used to build the index and excluded from the returned resources.
func extractBundleResources(bundle map[string]any) ([]map[string]any, models.ProvenanceIndex) {
	var resources []map[string]any
	var provenances []map[string]any

	for _, entryResource := range lib.BundleResources(bundle) {
		if lib.IsProvenance(entryResource) {
			provenances = append(provenances, entryResource)
		} else {
			resources = append(resources, entryResource)
		}
	}

	index := buildProvenanceIndex(provenances)
	return resources, index
}

// buildProvenanceIndex creates a mapping from resource references to CRTDL attribute group IDs.
// Each Provenance resource's target references are mapped to the attribute group ID found
// in its entity with the attribute_group NamingSystem.
func buildProvenanceIndex(provenances []map[string]any) models.ProvenanceIndex {
	index := make(models.ProvenanceIndex)

	for _, prov := range provenances {
		groupID := extractAttributeGroupID(prov)
		if groupID == "" {
			continue
		}

		targets, ok := prov["target"].([]any)
		if !ok {
			continue
		}

		for _, target := range targets {
			targetMap, ok := target.(map[string]any)
			if !ok {
				continue
			}
			ref, ok := targetMap["reference"].(string)
			if !ok || ref == "" {
				continue
			}
			index[ref] = append(index[ref], groupID)
		}
	}

	return index
}

// extractAttributeGroupID finds the CRTDL attribute group ID from a Provenance resource's entities.
// Looks for an entity with role "source" and the attribute_group NamingSystem.
func extractAttributeGroupID(provenance map[string]any) string {
	entities, ok := provenance["entity"].([]any)
	if !ok {
		return ""
	}

	for _, entity := range entities {
		entityMap, ok := entity.(map[string]any)
		if !ok {
			continue
		}

		what, ok := entityMap["what"].(map[string]any)
		if !ok {
			continue
		}

		identifier, ok := what["identifier"].(map[string]any)
		if !ok {
			continue
		}

		system, _ := identifier["system"].(string)
		if system == models.AttributeGroupNamingSystem {
			value, _ := identifier["value"].(string)
			return value
		}
	}

	return ""
}

// FilterResourcesByProvenance returns resources whose "ResourceType/id" reference
// maps to the given attribute group ID in the provenance index.
func FilterResourcesByProvenance(resources []map[string]any, index models.ProvenanceIndex, groupID string) []map[string]any {
	var matching []map[string]any

	for _, resource := range resources {
		ref := lib.ResourceReference(resource)
		if ref == "" {
			continue
		}
		for _, gid := range index[ref] {
			if gid == groupID {
				matching = append(matching, resource)
				break
			}
		}
	}

	return matching
}

// IsFlatteningErrorRetryable checks if a flattening error should be retried.
func IsFlatteningErrorRetryable(err error) bool {
	return isServiceErrorRetryable(err)
}

// ClassifyFlatteningError classifies a flattening error as transient or non-transient.
func ClassifyFlatteningError(err error) models.ErrorType {
	return classifyServiceError(err)
}
