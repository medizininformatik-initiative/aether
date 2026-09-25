package pipeline

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/medizininformatik-initiative/aether/internal/models"
)

// A network error is transient only for the import steps that use the network.
func TestImportStep_ClassifyError_NetworkError(t *testing.T) {
	networkErr := errors.New("dial tcp 127.0.0.1:8080: connection refused")

	tests := []struct {
		step models.StepName
		want models.ErrorType
	}{
		{models.StepTorchImport, models.ErrorTypeTransient},
		{models.StepHttpImport, models.ErrorTypeTransient},
		{models.StepLocalImport, models.ErrorTypeNonTransient},
	}
	for _, tt := range tests {
		t.Run(string(tt.step), func(t *testing.T) {
			assert.Equal(t, tt.want, importStep{name: tt.step}.ClassifyError(networkErr))
		})
	}
}
