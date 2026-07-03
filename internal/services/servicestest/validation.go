package servicestest

import "github.com/medizininformatik-initiative/aether/internal/services"

// MockResourceValidator is a test double for services.ResourceValidator. It
// records calls and, when ValidateFunc is unset, returns an issue-free
// OperationOutcome.
type MockResourceValidator struct {
	ValidateFunc func(map[string]any) (*services.ValidationResult, error)
	Calls        []map[string]any
}

var _ services.ResourceValidator = (*MockResourceValidator)(nil)

func (m *MockResourceValidator) ValidateResource(resource map[string]any) (*services.ValidationResult, error) {
	m.Calls = append(m.Calls, resource)
	if m.ValidateFunc != nil {
		return m.ValidateFunc(resource)
	}
	return &services.ValidationResult{OperationOutcome: map[string]any{"resourceType": "OperationOutcome"}}, nil
}
