// Package servicestest holds hand-written test doubles for the service-client
// seams defined in package services (Extractor, Flattener, ...). They live here,
// outside package services, so the doubles are not compiled into the production
// binary, yet — unlike _test.go helpers — remain importable from external test
// packages such as tests/unit.
package servicestest

import (
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

// MockExtractor is a test double for services.Extractor. Unset funcs return zero values.
type MockExtractor struct {
	SubmitFunc   func(crtdlPath string) (string, error)
	PollFunc     func(extractionURL string, showProgress bool) ([]string, error)
	DownloadFunc func(fileURLs []string, destinationDir string, showProgress, compress bool, compressionLevel string) ([]models.FHIRDataFile, error)
}

var _ services.Extractor = (*MockExtractor)(nil)

func (m *MockExtractor) SubmitExtraction(crtdlPath string) (string, error) {
	if m.SubmitFunc != nil {
		return m.SubmitFunc(crtdlPath)
	}
	return "", nil
}

func (m *MockExtractor) PollExtractionStatus(extractionURL string, showProgress bool) ([]string, error) {
	if m.PollFunc != nil {
		return m.PollFunc(extractionURL, showProgress)
	}
	return nil, nil
}

func (m *MockExtractor) DownloadExtractionFiles(fileURLs []string, destinationDir string, showProgress, compress bool, compressionLevel string) ([]models.FHIRDataFile, error) {
	if m.DownloadFunc != nil {
		return m.DownloadFunc(fileURLs, destinationDir, showProgress, compress, compressionLevel)
	}
	return nil, nil
}
