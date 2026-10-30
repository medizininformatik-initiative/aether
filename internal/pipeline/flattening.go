package pipeline

import (
	"bytes"
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
	resources    []json.RawMessage
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

	crtdl, lookupTables, err := loadFlatteningDefinitions(job, logger)
	if err != nil {
		return StepResult{}, err
	}

	inputDir := ctx.Layout.InputDir(stepName)
	outputDir := ctx.Layout.OutputDir(stepName)
	viewDefDir := ctx.Layout.ViewDefinitionsDir()

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return StepResult{}, fmt.Errorf("failed to create output directory: %w", err)
	}

	// A flattener that does not answer must fail the step before the stream.
	flattenerTransport, _ := services.BuildTLSTransport(job.Config.TLS, logger)
	flattenerClient := flattenerFactory(job.Config.Services.Flattening, job.Config.Retry, flattenerTransport, logger)
	if err := flattenerClient.HealthCheck(); err != nil {
		return StepResult{}, fmt.Errorf("flattener check failed: %w", err)
	}

	files, err := scanFlatteningInput(inputDir, job.JobID, logger)
	if err != nil {
		return StepResult{}, err
	}

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

	views := buildGroupViews(attributeGroups, viewDefBuilder, viewDefWriter, logger)

	// Stream resources and flatten in batches, routed by the Provenance of each Bundle
	totals, err := streamAndFlattenResources(
		files,
		attributeGroups,
		views.groupIDToIndex,
		views.viewDefs,
		views.headers,
		views.filenames,
		flattenerClient,
		csvWriter,
		logger,
		job.Config.Services.Flattening.GetBatchSizeBytes(),
	)
	if err != nil {
		return StepResult{}, annotateWithPartialFiles(err, csvWriter)
	}

	totalFilesWritten := printGroupTotals(attributeGroups, views, totals)

	logger.Debug("Flattening step completed",
		"files_written", totalFilesWritten,
		"job_id", job.JobID)

	return StepResult{FilesProcessed: totalFilesWritten}, nil
}

// loadFlatteningDefinitions validates the flattening configuration and loads
// the CRTDL and the lookup tables.
func loadFlatteningDefinitions(job *models.PipelineJob, logger *lib.Logger) (*models.CRTDLDocument, []models.LookupTable, error) {
	if err := job.Config.Services.Flattening.Validate(); err != nil {
		return nil, nil, err
	}

	// The CRTDL comes from the positional arg (for torch) or from --crtdl
	// (for http_import/local_import).
	if job.CRTDLPath == "" {
		return nil, nil, fmt.Errorf("flattening step requires a CRTDL file: pass one as the positional input or via --crtdl")
	}

	logger.Debug("Loading CRTDL file", "path", job.CRTDLPath)
	crtdl, err := services.ParseCRTDL(job.CRTDLPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse CRTDL file: %w", err)
	}

	lookupPath := job.Config.Services.Flattening.LookupPath
	logger.Debug("Loading lookup tables", "path", lookupPath)
	lookupTables, err := services.LoadLookupTables(lookupPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load lookup tables: %w", err)
	}

	return crtdl, lookupTables, nil
}

// scanFlatteningInput finds the FHIR NDJSON files in inputDir.
func scanFlatteningInput(inputDir, jobID string, logger *lib.Logger) ([]string, error) {
	files, err := findFHIRFiles(inputDir)
	if err != nil {
		return nil, fmt.Errorf("failed to list input files: %w", err)
	}

	if len(files) == 0 {
		return nil, fmt.Errorf("no FHIR NDJSON files found in %s", inputDir)
	}

	logger.Info("Streaming FHIR resources from input files",
		"input_dir", inputDir,
		"file_count", len(files),
		"job_id", jobID)

	return files, nil
}

// groupViews holds the ViewDefinition, CSV header, and CSV filename of each
// attribute group, by group index. A group without a ViewDefinition has a nil
// entry and no groupIDToIndex entry.
type groupViews struct {
	groupIDToIndex map[string]int
	viewDefs       []*models.ViewDefinition
	headers        [][]string
	filenames      []string
}

// buildGroupViews builds the ViewDefinition of each attribute group and saves
// it to disk. It skips a group whose ViewDefinition fails to build.
func buildGroupViews(groups []models.AttributeGroup, builder *services.ViewDefinitionBuilder, writer *services.ViewDefinitionWriter, logger *lib.Logger) groupViews {
	views := groupViews{
		groupIDToIndex: make(map[string]int),
		viewDefs:       make([]*models.ViewDefinition, len(groups)),
		headers:        make([][]string, len(groups)),
		filenames:      make([]string, len(groups)),
	}

	for i, group := range groups {
		viewDef, err := builder.BuildViewDefinition(group)
		if err != nil {
			logger.Warn("Failed to build ViewDefinition for group, skipping",
				"group_name", group.Name,
				"error", err)
			fmt.Printf("  ⚠ %s (skipped: %v)\n", group.Name, err)
			continue
		}

		views.viewDefs[i] = viewDef
		views.groupIDToIndex[group.ID] = i
		views.headers[i] = services.ExtractColumnNames(*viewDef)
		views.filenames[i] = services.BuildCSVFilename(group.Name)

		viewDefFilename := services.BuildViewDefinitionFilename(group.Name)
		if err := writer.WriteViewDefinition(viewDefFilename, *viewDef); err != nil {
			logger.Warn("Failed to save ViewDefinition, continuing",
				"group_name", group.Name,
				"filename", viewDefFilename,
				"error", err)
		}
	}

	return views
}

// printGroupTotals prints the result of each attribute group and returns the
// number of CSV files written.
func printGroupTotals(groups []models.AttributeGroup, views groupViews, totals []int) int {
	filesWritten := 0
	for i, group := range groups {
		if views.viewDefs[i] == nil {
			continue
		}
		if totals[i] == 0 {
			fmt.Printf("  ⊙ %s (no matching resources)\n", group.Name)
			continue
		}
		filesWritten++
		fmt.Printf("  ✓ %s (%d resources → %s)\n", group.Name, totals[i], views.filenames[i])
	}
	return filesWritten
}

// perGroupBudget splits the total batch byte budget evenly across attribute groups,
// clamping to one byte so a tiny budget spread over many groups never yields a
// zero threshold that would flush after every single resource.
func perGroupBudget(batchSizeBytes, numGroups int) int {
	return max(batchSizeBytes/numGroups, 1)
}

// streamAndFlattenResources performs single-pass streaming over all input files,
// routing each Bundle entry to per-group batches via the Provenance resources
// of its own Bundle and flushing when the byte threshold is exceeded. Returns
// per-group resource totals.
func streamAndFlattenResources(
	files []string,
	groups []models.AttributeGroup,
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
		groupIDToIndex:  groupIDToIndex,
		viewDefs:        viewDefs,
		headers:         headers,
		filenames:       filenames,
		flattenerClient: flattenerClient,
		csvWriter:       csvWriter,
		logger:          logger,
		// Divide the byte budget across groups so pending batches stay within batchSizeBytes
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
	compactBuf      bytes.Buffer
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
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("failed to load %s: %w", filepath.Base(filePath), err)
		}
		if raw[0] != '{' {
			return fmt.Errorf("failed to load %s: %w", filepath.Base(filePath), errNotJSONObject)
		}
		if err := s.addTopLevel(raw); err != nil {
			return err
		}
	}
}

var errNotJSONObject = errors.New("value is not a JSON object")

// resourceHeader holds the fields that routing needs. The rest of the
// resource stays undecoded.
type resourceHeader struct {
	ResourceType string `json:"resourceType"`
	ID           string `json:"id"`
}

func (h resourceHeader) reference() string {
	if h.ResourceType == "" || h.ID == "" {
		return ""
	}
	return h.ResourceType + "/" + h.ID
}

// bundleEntry is a clinical entry of a Bundle, with its JSON undecoded.
type bundleEntry struct {
	raw json.RawMessage
	ref string
}

// addTopLevel routes the clinical entries of a Bundle via the index of its own
// Provenance entries. A resource outside a Bundle has no Provenance and goes
// to no group.
func (s *groupStreamer) addTopLevel(raw json.RawMessage) error {
	var header resourceHeader
	if json.Unmarshal(raw, &header) != nil || header.ResourceType != "Bundle" {
		return nil
	}
	entries, index := splitBundle(raw)
	for _, entry := range entries {
		if err := s.add(entry.raw, entry.ref, index); err != nil {
			return err
		}
	}
	return nil
}

// splitBundle returns the clinical entries of a Bundle and the provenance index
// of its Provenance entries. Only the Provenance entries are fully decoded.
func splitBundle(raw json.RawMessage) ([]bundleEntry, models.ProvenanceIndex) {
	var bundle struct {
		Entry []struct {
			Resource json.RawMessage `json:"resource"`
		} `json:"entry"`
	}
	// On a type mismatch, Unmarshal still fills the other entries.
	_ = json.Unmarshal(raw, &bundle)

	var entries []bundleEntry
	var provenances []map[string]any
	for _, entry := range bundle.Entry {
		var header resourceHeader
		if json.Unmarshal(entry.Resource, &header) != nil || header.ResourceType == "" {
			continue
		}
		if header.ResourceType != "Provenance" {
			entries = append(entries, bundleEntry{raw: entry.Resource, ref: header.reference()})
			continue
		}
		var provenance map[string]any
		if json.Unmarshal(entry.Resource, &provenance) == nil {
			provenances = append(provenances, provenance)
		}
	}
	return entries, buildProvenanceIndex(provenances)
}

// add routes the resource to its groups via the provenance index of its Bundle
// and flushes each group batch that reaches the byte threshold. A batch keeps
// the compact JSON of the resource in a slice of exactly that size, so the byte
// count equals the memory that the batch holds.
func (s *groupStreamer) add(raw json.RawMessage, ref string, index models.ProvenanceIndex) error {
	indices := routeResourceToGroups(ref, index, s.groupIDToIndex, s.viewDefs)
	if len(indices) == 0 {
		return nil
	}
	s.compactBuf.Reset()
	// raw comes from a JSON decoder, so it is valid and Compact cannot fail.
	_ = json.Compact(&s.compactBuf, raw)
	compact := json.RawMessage(bytes.Clone(s.compactBuf.Bytes()))
	size := len(compact)
	for _, i := range indices {
		s.batches[i].resources = append(s.batches[i].resources, compact)
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

// routeResourceToGroups determines which groups the resource reference belongs to based on
// the provenance index. Returns the group indices for all matching groups.
func routeResourceToGroups(ref string, provenanceIndex models.ProvenanceIndex, groupIDToIndex map[string]int, viewDefs []*models.ViewDefinition) []int {
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
