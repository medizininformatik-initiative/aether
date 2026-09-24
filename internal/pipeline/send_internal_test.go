package pipeline

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
)

func TestNewSendHTTPClient_Uses30SecondTimeout(t *testing.T) {
	client := newSendHTTPClient(&models.PipelineJob{}, lib.NewLogger(lib.LogLevelError))

	assert.Equal(t, 30*time.Second, client.Timeout())
}
