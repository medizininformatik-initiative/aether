package unit

import (
	"testing"

	"github.com/medizininformatik-initiative/aether/cmd"
	"github.com/stretchr/testify/assert"
)

func TestSetVersion_DoesNotPanic(t *testing.T) {
	assert.NotPanics(t, func() {
		cmd.SetVersion("1.2.3")
	})
}
