package servicestest

import "github.com/medizininformatik-initiative/aether/internal/services"

// MockDIMPProcessor is a test double for services.DIMPProcessor. It records calls
// and, when PseudonymizeFunc is unset, echoes the input resource.
type MockDIMPProcessor struct {
	PseudonymizeFunc func(map[string]any) (map[string]any, error)
	Calls            []map[string]any
}

var _ services.DIMPProcessor = (*MockDIMPProcessor)(nil)

func (m *MockDIMPProcessor) Pseudonymize(resource map[string]any) (map[string]any, error) {
	m.Calls = append(m.Calls, resource)
	if m.PseudonymizeFunc != nil {
		return m.PseudonymizeFunc(resource)
	}
	return resource, nil
}
