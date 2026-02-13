package unit

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/medizininformatik-initiative/aether/cmd"
)

func TestSetVersion_DoesNotPanic(t *testing.T) {
	assert.NotPanics(t, func() {
		cmd.SetVersion("1.2.3")
	})
}
