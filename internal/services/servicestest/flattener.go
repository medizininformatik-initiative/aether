package servicestest

import (
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

// MockFlattener is a test double for services.Flattener. It counts calls and,
// when FlattenFunc is unset, returns empty CSV.
type MockFlattener struct {
	FlattenFunc func(models.ViewDefinition, []map[string]any) (string, error)
	Calls       int
}

var _ services.Flattener = (*MockFlattener)(nil)

func (m *MockFlattener) Flatten(viewDef models.ViewDefinition, resources []map[string]any) (string, error) {
	m.Calls++
	if m.FlattenFunc != nil {
		return m.FlattenFunc(viewDef, resources)
	}
	return "", nil
}
