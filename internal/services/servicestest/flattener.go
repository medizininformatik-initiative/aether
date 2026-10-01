package servicestest

import (
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

// MockFlattener is a test double for services.Flattener. It counts calls and,
// when FlattenFunc is unset, returns no rows. HealthCheck returns HealthCheckErr.
type MockFlattener struct {
	FlattenFunc      func(models.ViewDefinition, []map[string]any) ([][]string, error)
	HealthCheckErr   error
	Calls            int
	HealthCheckCalls int
}

var _ services.Flattener = (*MockFlattener)(nil)

func (m *MockFlattener) Flatten(viewDef models.ViewDefinition, resources []map[string]any) ([][]string, error) {
	m.Calls++
	if m.FlattenFunc != nil {
		return m.FlattenFunc(viewDef, resources)
	}
	return nil, nil
}

func (m *MockFlattener) HealthCheck() error {
	m.HealthCheckCalls++
	return m.HealthCheckErr
}
